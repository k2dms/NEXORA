package main

import (
	"io"
	"net"
	"strings"
	"testing"

	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/localpipe"
)

// TestALiveOwnerThatDoesNotAnswerIsRefusedAsAlreadyRunning is
// `framing.json#/singleInstance/adoption/unreachable` at the shim's own layer: the open failed for a reason that
// names a live owner, so every frame gets one CoreAlreadyRunning back — in reply to its own request, not retryable
// — and no second Connector is started.
func TestALiveOwnerThatDoesNotAnswerIsRefusedAsAlreadyRunning(t *testing.T) {
	open := func(string) (io.ReadWriteCloser, error) {
		return nil, &instanceUnreachable{record: localpipe.InstanceRecord{
			PID: 4242, PipeVersion: 1, StartedAt: "2026-11-19T00:00:00Z",
		}}
	}
	cfg := config{senderID: pinnedOrigin, allowlistPath: allowlistFile(t, `["`+pinnedOrigin+`"]`)}

	browser, ext := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- run(cfg, browser, open, io.Discard) }()

	for i := 0; i < 2; i++ {
		writeFrame(t, ext, requestPayload(t, "abcdefgh", "core.start", `{}`))
		f, err := envelope.Parse(readFrame(t, ext))
		if err != nil {
			t.Fatalf("refusal %d was not a frame the codec accepts: %v", i, err)
		}
		if f.Type != envelope.TypeResponse || f.InReplyTo == nil || *f.InReplyTo != "abcdefgh" {
			t.Errorf("refusal %d is not an answer in reply to its own request: type %s, inReplyTo %v", i, f.Type, f.InReplyTo)
		}
		if f.Error == nil || f.Error.Code != "CoreAlreadyRunning" {
			t.Fatalf("refusal %d carries %v, want CoreAlreadyRunning", i, f.Error)
		}
		if f.Error.Retryable == nil || *f.Error.Retryable {
			t.Error("the taxonomy declares CoreAlreadyRunning not retryable, and an adoption retry loop is a port storm")
		}
		if f.Error.Message == nil || !strings.Contains(*f.Error.Message, "4242") {
			t.Errorf("the refusal has to name the instance it refuses to replace: %v", f.Error.Message)
		}
	}
	_ = ext.Close()
	if err := <-done; err != nil {
		t.Errorf("the run ended with %v; a port the extension closed ends a stream", err)
	}
}
