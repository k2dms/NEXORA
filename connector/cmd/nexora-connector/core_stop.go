package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/k2dms/nexora/connector/internal/apply"
	"github.com/k2dms/nexora/connector/internal/dispatch"
	"github.com/k2dms/nexora/connector/internal/envelope"
)

type coreStop struct {
	core       coreProcess
	configPath string
}

func newCoreStop(core coreProcess, configDir string) (*coreStop, error) {
	if core == nil || configDir == "" {
		return nil, errors.New("core.stop: a Core process owner and compiled-config directory are required")
	}
	return &coreStop{core: core, configPath: filepath.Join(configDir, apply.FileName)}, nil
}

type coreStopAnswer struct {
	Running      bool   `json:"running"`
	WasRunning   bool   `json:"wasRunning"`
	ExitCode     int    `json:"exitCode"`
	TerminatedBy string `json:"terminatedBy"`
	DurationMS   int64  `json:"durationMs"`
}

func (s *coreStop) Handle(ctx context.Context, f *envelope.Frame) (any, *dispatch.Refusal) {
	graceful, bad := readStopArgs(f)
	if bad != nil {
		return nil, bad
	}
	var expected struct {
		Hash string `json:"expectedConfigHash"`
	}
	if len(f.Args) != 0 {
		_ = json.Unmarshal(f.Args, &expected)
	}
	if expected.Hash != "" {
		live := s.core.Status()
		if (live.Running || live.Restarting) && live.Hash != expected.Hash {
			return nil, &dispatch.Refusal{Code: "Busy", Message: "core.stop: another config owns the live Core; the stale session cannot stop it"}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	stopped, err := s.core.Stop(ctx, graceful)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			return nil, &dispatch.Refusal{Code: "Timeout", Message: "core.stop: the stop context was canceled or its deadline expired"}
		}
		return nil, &dispatch.Refusal{Code: "Internal", Message: "core.stop: the Core process could not be stopped"}
	}
	if stopped == nil {
		return nil, &dispatch.Refusal{Code: "Internal", Message: "core.stop: the Core process owner returned no stop result"}
	}
	if err := os.Remove(s.configPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, &dispatch.Refusal{Code: "Internal", Message: "core.stop: Core stopped but its materialized config could not be removed"}
	}
	return coreStopAnswer{
		Running:      false,
		WasRunning:   stopped.WasRunning,
		ExitCode:     stopped.ExitCode,
		TerminatedBy: stopped.TerminatedBy,
		DurationMS:   stopped.DurationMS,
	}, nil
}

func readStopArgs(f *envelope.Frame) (bool, *dispatch.Refusal) {
	graceful := true
	if len(f.Args) == 0 {
		return graceful, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(f.Args, &raw); err != nil {
		return false, badStopArgs("args are not a JSON object")
	}
	for k := range raw {
		switch k {
		case "graceful", "reason", "expectedConfigHash":
		default:
			return false, badStopArgs("args carry a field core.stop does not declare")
		}
	}
	if v, ok := raw["expectedConfigHash"]; ok {
		var hash string
		if err := json.Unmarshal(v, &hash); err != nil || len(hash) != 64 || !reAppliedHash.MatchString(hash) {
			return false, badStopArgs("expectedConfigHash is not a lowercase sha256")
		}
	}
	if v, ok := raw["graceful"]; ok {
		var b *bool
		if err := json.Unmarshal(v, &b); err != nil || b == nil {
			return false, badStopArgs("graceful is not a boolean")
		}
		graceful = *b
	}
	if v, ok := raw["reason"]; ok {
		var reason *string
		if err := json.Unmarshal(v, &reason); err != nil || reason == nil {
			return false, badStopArgs("reason is not a string")
		}
		if utf8.RuneCountInString(*reason) > 40 {
			return false, badStopArgs("reason exceeds 40 characters")
		}
	}
	return graceful, nil
}

func badStopArgs(rule string) *dispatch.Refusal {
	return &dispatch.Refusal{Code: "Internal", Message: "core.stop: " + rule}
}
