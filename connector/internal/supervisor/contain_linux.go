//go:build linux

package supervisor

import (
	"os/exec"
	"syscall"
)

// scope is ADR-0005 rule 5's Linux half, and on this platform the primitive is in the standard library:
// syscall.SysProcAttr.Pdeathsig is PR_SET_PDEATHSIG, issued by the child between fork and exec, which is the
// only moment at which the parent-death promise can be made without a race. doc 21 §11's rejection of a service
// and doc 30 §2's containment chain both assume exactly this.
type scope struct{}

func newScope() (*scope, error) { return &scope{}, nil }

func (sc *scope) prepare(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// SIGKILL rather than SIGTERM: PR_SET_PDEATHSIG fires when the parent's thread exits, and a child given a
	// choice about how to die is a child that can keep routing traffic while nobody supervises it.
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}

// attach reports the promise the kernel already made. There is no handle to hold and no second answer to give,
// so contained is true whenever the child exists at all.
func (sc *scope) attach(int) (bool, string) { return true, "" }

func (sc *scope) close() error { return nil }

// askGraceful is core.stop's graceful half: SIGTERM is the only polite channel Linux gives a process whose
// own protocol this corpus does not describe.
func (c *child) askGraceful() bool {
	return c.cmd.Process.Signal(syscall.SIGTERM) == nil
}

func processAlive(pid int) bool {
	// Signal 0 asks the kernel to validate the pid and sends nothing: ESRCH means the process is gone, and an
	// error other than that means it exists and this process may not speak to it.
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
