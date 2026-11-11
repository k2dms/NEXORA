package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/k2dms/nexora/connector/internal/dispatch"
	"github.com/k2dms/nexora/connector/internal/envelope"
	"github.com/k2dms/nexora/connector/internal/receipt"
	"github.com/k2dms/nexora/connector/internal/supervisor"
)

type coreStatus struct {
	core    coreProcess
	version func() (string, error)
	now     func() time.Time
}

func newCoreStatus(core coreProcess, coreDir string) (*coreStatus, error) {
	if core == nil {
		return nil, errors.New("core.status: no Core process owner; every field this command answers is a fact about the process it holds")
	}
	if coreDir == "" {
		return nil, errors.New("core.status: no core directory; the installed version is the install receipt's and doc 33 §7 puts it there")
	}
	return &coreStatus{
		core: core,
		version: func() (string, error) {
			r, err := receipt.Load(filepath.Join(coreDir, "receipt.json"))
			if err != nil {
				return "", err
			}
			return strings.TrimPrefix(r.Tag, "v"), nil
		},
		now: time.Now,
	}, nil
}

type coreStatusAnswer struct {
	Running           bool                 `json:"running"`
	PID               *int                 `json:"pid"`
	CoreVersion       *string              `json:"coreVersion"`
	ConfigHash        *string              `json:"configHash"`
	StartedAt         *string              `json:"startedAt"`
	UptimeSeconds     *int64               `json:"uptimeSeconds"`
	Contained         *bool                `json:"contained"`
	LastExit          *coreExit            `json:"lastExit"`
	SelectedProfileID *string              `json:"selectedProfileId"`
	InboundPorts      []supervisor.Inbound `json:"inboundPorts"`
}

type coreExit struct {
	At       string  `json:"at"`
	ExitCode int     `json:"exitCode"`
	Signal   *string `json:"signal,omitempty"`
}

func (s *coreStatus) Handle(_ context.Context, f *envelope.Frame) (any, *dispatch.Refusal) {
	if bad := readStatusArgs(f); bad != nil {
		return nil, bad
	}
	live := s.core.Status()
	ans := coreStatusAnswer{Running: live.Running, InboundPorts: []supervisor.Inbound{}}
	// An unreadable receipt cannot establish a version, but process facts still answer.
	if v, err := s.version(); err == nil && v != "" {
		ans.CoreVersion = &v
	}
	if live.Hash != "" {
		ans.ConfigHash = &live.Hash
	}
	if live.Running {
		ans.InboundPorts = append(ans.InboundPorts, live.Inbounds...)
		if reApplyID.MatchString(live.ProfileID) {
			ans.SelectedProfileID = &live.ProfileID
		}
		ans.PID = &live.PID
		ans.Contained = &live.Contained
		at := live.StartedAt.UTC().Format(time.RFC3339Nano)
		ans.StartedAt = &at
		up := max(int64(s.now().Sub(live.StartedAt)/time.Second), 0)
		ans.UptimeSeconds = &up
	}
	if e := live.LastExit; e != nil {
		exit := coreExit{At: e.At.UTC().Format(time.RFC3339Nano), ExitCode: e.Code}
		if e.Signal != "" {
			exit.Signal = &e.Signal
		}
		ans.LastExit = &exit
	}
	return ans, nil
}

func readStatusArgs(f *envelope.Frame) *dispatch.Refusal {
	if len(f.Args) == 0 {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(f.Args, &raw); err != nil {
		return badStatusArgs("args are not a JSON object")
	}
	if len(raw) != 0 {
		return badStatusArgs("args carry a field core.status does not declare; its args object is closed and empty")
	}
	return nil
}

func badStatusArgs(rule string) *dispatch.Refusal {
	return &dispatch.Refusal{Code: "Internal", Message: clip("core.status: "+rule, maxApplyMessage)}
}
