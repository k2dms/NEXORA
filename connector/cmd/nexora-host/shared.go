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
			if time.Now().After(deadline) {
				return nil, errors.New("the per-user Connector could not be adopted; no replacement Core is started")
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}
