package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/k2dms/nexora/connector/internal/contract"
	"github.com/k2dms/nexora/connector/internal/hello"
	"github.com/k2dms/nexora/connector/internal/localpipe"
	"github.com/k2dms/nexora/connector/internal/supervisor"
)

type senderPorts struct {
	mu     sync.Mutex
	ports  map[string][]io.ReadWriteCloser
	closed bool
}

func (p *senderPorts) add(sender string, conn io.ReadWriteCloser) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		_ = conn.Close()
		return false
	}
	list := p.ports[sender]
	if len(list) == contract.PortsPerSenderMaxConcurrent {
		_ = list[0].Close()
		list = list[1:]
	}
	p.ports[sender] = append(list, conn)
	return true
}

func (p *senderPorts) remove(sender string, conn io.ReadWriteCloser) {
	p.mu.Lock()
	defer p.mu.Unlock()
	list := p.ports[sender]
	for i, item := range list {
		if item == conn {
			p.ports[sender] = append(list[:i], list[i+1:]...)
			return
		}
	}
}

func (p *senderPorts) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, list := range p.ports {
		n += len(list)
	}
	return n
}

func (p *senderPorts) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	for _, list := range p.ports {
		for _, conn := range list {
			_ = conn.Close()
		}
	}
}

func (s *service) connection(sender string) *service {
	return &service{ctx: s.ctx, d: s.d, core: s.core, sess: new(hello.Session), senderID: sender, allowlisted: true}
}

func runShared(ctx context.Context, cfg config, notes io.Writer) error {
	if cfg.instanceRoot == "" || cfg.hostExe == "" || cfg.version == "" {
		return errors.New("shared Connector requires an instance root, authenticated host executable and release version")
	}
	if len(cfg.senderIDs) == 0 || len(cfg.senderIDs) > 64 {
		return errors.New("shared Connector requires 1 to 64 startup sender endpoints")
	}
	seen := map[string]bool{}
	for _, id := range cfg.senderIDs {
		if id == "" || id != strings.TrimRight(id, "/") || seen[id] {
			return errors.New("shared Connector sender endpoints must be nonempty, normalized and unique")
		}
		seen[id] = true
	}
	ownership, err := localpipe.Mutex(cfg.instanceRoot)
	if err != nil {
		return err
	}
	defer ownership.Close()
	s, err := newService(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	s.ctx = ctx
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = s.core.Stop(stop, false)
	}()
	listeners := make([]*localpipe.Listener, 0, len(cfg.senderIDs))
	ports := &senderPorts{ports: map[string][]io.ReadWriteCloser{}}
	var accepting, serving sync.WaitGroup
	defer func() {
		cancel()
		for _, listener := range listeners {
			_ = listener.Close()
		}
		accepting.Wait()
		ports.close()
		serving.Wait()
	}()
	for _, id := range cfg.senderIDs {
		listener, err := localpipe.Listen(localpipe.Name(cfg.instanceRoot, id))
		if err != nil {
			return fmt.Errorf("opening a sender endpoint: %w", err)
		}
		listeners = append(listeners, listener)
	}
	metadataPath, err := writeInstanceRecord(cfg.instanceRoot, s.stateDir)
	if err != nil {
		return err
	}
	defer os.Remove(metadataPath)
	failed := make(chan error, len(listeners))
	for i, listener := range listeners {
		sender := cfg.senderIDs[i]
		accepting.Add(1)
		go func() {
			defer accepting.Done()
			for {
				conn, err := listener.Accept()
				if err != nil {
					failed <- err
					return
				}
				image, err := localpipe.PeerExecutable(conn, true)
				if err != nil || !localpipe.SameExecutable(image, cfg.hostExe) {
					_ = conn.Close()
					continue
				}
				if !ports.add(sender, conn) {
					return
				}
				serving.Add(1)
				go func() {
					defer serving.Done()
					defer ports.remove(sender, conn)
					defer conn.Close()
					_ = s.connection(sender).serveFramed(conn)
				}()
			}
		}()
	}
	fmt.Fprintln(notes, "nexora-connector: shared sender endpoints ready; host EOF does not stop Core")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	idleSince := time.Now()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-failed:
			return err
		case now := <-ticker.C:
			live := s.core.Status()
			if ports.count() != 0 || live.Running || live.Restarting {
				idleSince = now
			} else if now.Sub(idleSince) >= time.Minute {
				return nil
			}
		}
	}
}

// writeInstanceRecord is `framing.json#/singleInstance/recordFile` as a write, and its timing is the rule the shim
// leans on: every sender endpoint is listening before this runs, so a record that exists means the endpoints do.
//
// The configHash is the one the applied-state record holds, and an empty string before the first apply is the
// honest answer rather than a guess — nothing has been applied yet.
func writeInstanceRecord(root, stateDir string) (string, error) {
	applied, err := supervisor.ReadState(stateDir)
	if err != nil {
		return "", err
	}
	rec := localpipe.NewInstanceRecord(contract.FrameFormatVersion, applied.AppliedHash)
	if err := localpipe.WriteInstanceRecord(root, rec); err != nil {
		return "", err
	}
	return localpipe.InstanceRecordPath(root), nil
}
