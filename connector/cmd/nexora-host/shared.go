package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/k2dms/nexora/connector/internal/localpipe"
)

// instanceUnreachable is the shim's answer when the mutex says an owner is alive and the record says who it is, yet
// its sender endpoint does not answer. It is a distinct error type because run turns it into a refusal with a code
// (`CoreAlreadyRunning`), while every other open failure is a failure of this process.
type instanceUnreachable struct{ record localpipe.InstanceRecord }

func (e *instanceUnreachable) Error() string {
	return fmt.Sprintf("the per-user Connector holding this root is running (pid %d, started %s) but did not answer its sender endpoint; it is adopted rather than replaced, so no second Core is started",
		e.record.PID, e.record.StartedAt)
}

// sharedOpen hands the frame to the per-user Connector, and decides only one thing: whether this process may start
// one. `framing.json#/singleInstance` states the rule, and the two facts that answer it are the owner mutex and the
// record the owner writes after its endpoints are listening.
func sharedOpen(binary, allowlist string) hop2 {
	return func(sender string) (io.ReadWriteCloser, error) {
		root, err := nexoraRoot()
		if err != nil {
			return nil, err
		}
		binary, err = filepath.Abs(binary)
		if err != nil {
			return nil, err
		}
		sender = strings.TrimRight(sender, "/")
		name := localpipe.Name(root, sender)
		deadline := time.Now().Add(10 * time.Second)
		launched := false
		for {
			conn, err := localpipe.Dial(name, binary)
			if err == nil {
				return conn, nil
			}
			if !errors.Is(err, localpipe.ErrAbsent) && !errors.Is(err, localpipe.ErrBusy) {
				return nil, err
			}
			if !launched && errors.Is(err, localpipe.ErrAbsent) {
				if localpipe.OwnerLive(root) {
					// A live owner that is not answering yet is either between opening its listeners and writing
					// the record, or busy restarting them. A second process cannot shorten either wait, and the
					// rule forbids becoming one — so one more dial answers the first case, the record answers
					// the second, and otherwise this shim waits rather than spawning.
					if conn, retry := localpipe.Dial(name, binary); retry == nil {
						return conn, nil
					}
					record, readErr := localpipe.ReadInstanceRecord(root)
					switch {
					case readErr == nil:
						return nil, &instanceUnreachable{record: record}
					case !errors.Is(readErr, os.ErrNotExist):
						return nil, readErr
					}
				} else {
					allowed := loadAllowlist(allowlist, os.Stderr)
					if !allowed[sender] || len(allowed) > 64 {
						return nil, errors.New("the shared endpoint is not admitted by the startup allowlist")
					}
					ids := make([]string, 0, len(allowed))
					for id := range allowed {
						ids = append(ids, id)
					}
					sort.Strings(ids)
					encoded, err := json.Marshal(ids)
					if err != nil {
						return nil, err
					}
					host, err := os.Executable()
					if err != nil {
						return nil, err
					}
					cmd := exec.Command(binary, "--shared", "--instance-root", root, "--host-exe", host, "--sender-ids", string(encoded))
					if err := localpipe.Detach(cmd); err != nil {
						return nil, err
					}
					if err := cmd.Start(); err != nil {
						return nil, fmt.Errorf("starting detached Connector: %w", err)
					}
					go func() { _ = cmd.Wait() }()
					launched = true
				}
			}
			if time.Now().After(deadline) {
				return nil, errors.New("the per-user Connector could not be adopted; no replacement Core is started")
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}
