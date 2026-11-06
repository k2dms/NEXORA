//go:build !windows && !linux

package supervisor

import (
	"os/exec"
	"runtime"
	"syscall"
)

// scope is the answer for every platform ADR-0011 rule 1 already treats as deferred, and macOS is the one
// ADR-0005 rule 5 names beside Linux while the standard library gives it no PR_SET_PDEATHSIG field. The honest
// shape of that gap is a false reported with its reason, which is what core.start's `contained` field exists to
// carry: "false means the Core could outlive the Connector, which is a reported condition, not a silent one".
//
// A macOS implementation needs either a dependency this module does not have or a re-exec launcher that calls
// prctl-equivalent work in the child; both belong to the platform half of ADR-0011's deferral, not to a
// supervisor inventing one in passing.
type scope struct{}

func newScope() (*scope, error) { return &scope{}, nil }

func (sc *scope) prepare(*exec.Cmd) {}

func (sc *scope) attach(int) (bool, string) {
	return false, "no parent-death primitive is reachable from the standard library on " + runtime.GOOS + ", and ADR-0011 defers this platform's backend rather than faking it"
}

func (sc *scope) close() error { return nil }

func (c *child) askGraceful() bool {
	return c.cmd.Process.Signal(syscall.SIGTERM) == nil
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
