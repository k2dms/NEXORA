package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/dispatch"
	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/hello"
)

type trackedPort struct {
	bytes.Buffer
	closes int
}

func (p *trackedPort) Close() error { p.closes++; return nil }

func TestSenderPortsEvictsOldestPerSenderAndClosesOnShutdown(t *testing.T) {
	if contract.PortsPerSenderMaxConcurrent != 2 {
		t.Fatal("test requires the two-port contract")
	}
	ports := &senderPorts{ports: map[string][]io.ReadWriteCloser{}}
	first, second, third, other := new(trackedPort), new(trackedPort), new(trackedPort), new(trackedPort)
	for _, conn := range []*trackedPort{first, second} {
		if !ports.add(pinned, conn) {
			t.Fatal("open manager rejected a port")
		}
	}
	if !ports.add("other", other) || ports.count() != 3 || first.closes != 0 || second.closes != 0 {
		t.Fatal("two same-sender ports or an independent sender were not retained")
	}
	if !ports.add(pinned, third) || first.closes != 1 || second.closes != 0 || third.closes != 0 || other.closes != 0 || ports.count() != 3 {
		t.Fatal("third same-sender port did not close only the oldest")
	}
	ports.remove(pinned, first)
	ports.remove("other", third)
	if ports.count() != 3 {
		t.Fatal("removing an evicted or foreign port removed a live port")
	}
	ports.remove(pinned, second)
	if ports.count() != 2 || second.closes != 0 {
		t.Fatal("remove did not only unregister its port")
	}
	replacement, newest := new(trackedPort), new(trackedPort)
	if !ports.add(pinned, replacement) || !ports.add(pinned, newest) || third.closes != 1 || replacement.closes != 0 || newest.closes != 0 || ports.count() != 3 {
		t.Fatal("reconnection lost oldest-first ordering")
	}
	ports.close()
	if other.closes != 1 || replacement.closes != 1 || newest.closes != 1 || first.closes != 1 || third.closes != 1 || second.closes != 0 {
		t.Fatal("shutdown did not close exactly the registered ports")
	}
	rejected := new(trackedPort)
	if ports.add(pinned, rejected) || rejected.closes != 1 {
		t.Fatal("closed manager accepted or leaked a new port")
	}
	for _, conn := range []*trackedPort{replacement, newest} {
		ports.remove(pinned, conn)
	}
	ports.remove("other", other)
	if ports.count() != 0 {
		t.Fatal("closed ports were not unregistered")
	}
}

func sharedReply(t *testing.T, s *service, id, method, args string) *envelope.Frame {
	t.Helper()
	res, err := s.respond(mustParse(t, requestLine(t, id, method, args)))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func sharedDispatchService(t *testing.T, method string) (*service, *int) {
	t.Helper()
	responder, err := hello.New(hello.Facts{ConnectorVersion: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	calls := new(int)
	d, err := dispatch.New(dispatch.Config{
		Now: func() time.Time { return time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC) },
		Handlers: map[string]dispatch.Handler{
			"connector.hello": responder.Handle,
			method: func(context.Context, *envelope.Frame) (any, *dispatch.Refusal) {
				*calls++
				return map[string]int{"calls": *calls}, nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &service{ctx: context.Background(), d: d}, calls
}

func TestSharedConnectionsHaveIndependentHelloAndShareCore(t *testing.T) {
	cfg := framedConfig(t, true)
	cfg.shared = true
	root, err := newService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	first, second := root.connection(pinned), root.connection(pinned)
	for _, conn := range []*service{first, second} {
		if conn.d != root.d || conn.core != root.core || conn.core == nil || conn.senderID != pinned || !conn.allowlisted {
			t.Fatal("connection did not retain shared dispatcher/Core and authenticated sender")
		}
	}
	if first.sess == second.sess || first.sess == root.sess || second.sess == root.sess {
		t.Fatal("connections reused a hello session")
	}
	checkRefusalOrOK(t, sharedReply(t, first, "hello-shared", "connector.hello", recordedHelloArgs), "hello-shared")
	checkRefusalOrOK(t, sharedReply(t, first, "version-shared", "connector.version", `{}`), "version-shared")
	checkRefusal(t, sharedReply(t, second, "version-shared", "connector.version", `{}`), "version-shared", "HandshakeFailed")
	checkRefusalOrOK(t, sharedReply(t, second, "hello-shared", "connector.hello", recordedHelloArgs), "hello-shared")
	if !first.sess.Negotiated() || !second.sess.Negotiated() || root.sess.Negotiated() {
		t.Fatal("same-id hello did not negotiate each port independently")
	}
	badHello := `{"extensionVersion":"3.6.0","protocolVersion":2,"minCompatibleProtocolVersion":2}`
	checkRefusal(t, sharedReply(t, second, "hello-mismatch", "connector.hello", badHello), "hello-mismatch", "VersionMismatch")
	if !first.sess.Negotiated() || second.sess.Negotiated() {
		t.Fatal("one port's hello withdrawal changed another port")
	}
	checkRefusalOrOK(t, sharedReply(t, first, "version-after", "connector.version", `{}`), "version-after")
	if root.connection(pinned).sess.Negotiated() {
		t.Fatal("reconnection inherited an old hello")
	}
}

func TestSharedDispatcherReplayAndMutationLimitSurviveReconnection(t *testing.T) {
	root, calls := sharedDispatchService(t, "core.stop")
	first, second := root.connection(pinned), root.connection(pinned)
	for _, conn := range []*service{first, second} {
		checkRefusalOrOK(t, sharedReply(t, conn, "hello-shared", "connector.hello", recordedHelloArgs), "hello-shared")
	}
	stored := sharedReply(t, first, "stop-shared", "core.stop", `{}`)
	checkRefusalOrOK(t, stored, "stop-shared")
	replayed := sharedReply(t, second, "stop-shared", "core.stop", `{"graceful":false}`)
	checkRefusalOrOK(t, replayed, "stop-shared")
	if *calls != 1 || !bytes.Equal(stored.Result, replayed.Result) {
		t.Fatal("second port repeated the mutation instead of replaying the stored answer")
	}
	limit, ok := contract.RateLimitFor("core.stop")
	if !ok {
		t.Fatal("core.stop has no mutation limit")
	}
	for i := 1; i < limit.MaxRequests; i++ {
		id := fmt.Sprintf("stop-fresh-%d", i)
		conn := []*service{first, second}[i%2]
		checkRefusalOrOK(t, sharedReply(t, conn, id, "core.stop", `{}`), id)
	}
	reconnected := root.connection(pinned)
	checkRefusalOrOK(t, sharedReply(t, reconnected, "hello-reconnect", "connector.hello", recordedHelloArgs), "hello-reconnect")
	replayed = sharedReply(t, reconnected, "stop-shared", "core.stop", `{}`)
	checkRefusalOrOK(t, replayed, "stop-shared")
	if !bytes.Equal(stored.Result, replayed.Result) {
		t.Fatal("reconnection lost the stored answer")
	}
	checkRefusal(t, sharedReply(t, reconnected, "stop-over", "core.stop", `{}`), "stop-over", "Busy")
	if *calls != limit.MaxRequests {
		t.Fatalf("shared budget/replay invoked %d handlers, want %d", *calls, limit.MaxRequests)
	}
	other := root.connection("other")
	checkRefusalOrOK(t, sharedReply(t, other, "hello-shared", "connector.hello", recordedHelloArgs), "hello-shared")
	independent := sharedReply(t, other, "stop-shared", "core.stop", `{}`)
	checkRefusalOrOK(t, independent, "stop-shared")
	if *calls != limit.MaxRequests+1 || bytes.Equal(stored.Result, independent.Result) {
		t.Fatal("another sender inherited spent budget or a cached answer")
	}
}

func TestSharedRequestsReceiveServiceCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	observed := make(chan error, 1)
	responder, err := hello.New(hello.Facts{ConnectorVersion: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := dispatch.New(dispatch.Config{Handlers: map[string]dispatch.Handler{
		"connector.hello": responder.Handle,
		"subscription.refresh": func(ctx context.Context, _ *envelope.Frame) (any, *dispatch.Refusal) {
			close(started)
			select {
			case <-ctx.Done():
				observed <- ctx.Err()
			case <-time.After(250 * time.Millisecond):
				observed <- fmt.Errorf("request outlived service cancellation")
			}
			return nil, &dispatch.Refusal{Code: "Timeout", Message: "cancelled"}
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	root := &service{ctx: ctx, d: d}
	conn := root.connection(pinned)
	checkRefusalOrOK(t, sharedReply(t, conn, "hello-cancel", "connector.hello", recordedHelloArgs), "hello-cancel")
	frame := mustParse(t, requestLine(t, "refresh-cancel", "subscription.refresh", `{}`))
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = conn.respond(frame)
	}()
	<-started
	cancel()
	if err := <-observed; err != context.Canceled {
		t.Fatal(err)
	}
	<-done
}

func TestSharedDispatcherCatchAllLimitCountsBothPortsAndReplays(t *testing.T) {
	root, calls := sharedDispatchService(t, "connector.version")
	first, second := root.connection(pinned), root.connection(pinned)
	for _, conn := range []*service{first, second} {
		checkRefusalOrOK(t, sharedReply(t, conn, "hello-shared", "connector.hello", recordedHelloArgs), "hello-shared")
	}
	limit := contract.CatchAllRateLimit()
	for i := 0; i < limit.MaxRequests-2; i++ {
		conn := []*service{first, second}[i%2]
		checkRefusalOrOK(t, sharedReply(t, conn, "version-replay", "connector.version", `{}`), "version-replay")
	}
	checkRefusal(t, sharedReply(t, second, "version-replay", "connector.version", `{}`), "version-replay", "Busy")
	if *calls != 1 {
		t.Fatalf("replay burst ran the handler %d times, want once", *calls)
	}
	other := root.connection("other")
	checkRefusalOrOK(t, sharedReply(t, other, "hello-shared", "connector.hello", recordedHelloArgs), "hello-shared")
	checkRefusalOrOK(t, sharedReply(t, other, "version-replay", "connector.version", `{}`), "version-replay")
	if *calls != 2 {
		t.Fatal("catch-all budget was not per sender")
	}
}
