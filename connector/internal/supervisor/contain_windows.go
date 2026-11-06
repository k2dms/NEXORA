//go:build windows

package supervisor

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"
)

// scope is ADR-0005 rule 5's Windows half: a Job Object with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, because that
// rule states the platform performs no parent-death cleanup by default and doc 21 §11 names the same limit as
// the reason a bare child outlives its parent here. The job is created before the child exists so that the
// answer about the child is given about a process created into a scope this process already holds.
//
// kernel32 is reached the way connector/internal/keystore reaches crypt32: one lazy DLL, no dependency added.
type scope struct {
	job syscall.Handle
}

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObject    = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJob  = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJob = kernel32.NewProc("AssignProcessToJobObject")
	procOpenProcess        = kernel32.NewProc("OpenProcess")
	procIsProcessInJob     = kernel32.NewProc("IsProcessInJob")
	procCloseHandle        = kernel32.NewProc("CloseHandle")
	procGetExitCodeProcess = kernel32.NewProc("GetExitCodeProcess")

	// jobObjectExtendedLimits is JobObjectExtendedLimitInformation. The basic-limit class beside it was measured
	// here first and it does not work on this host: SetInformationJobObject(1, sizeof(basic)) answers
	// ERROR_BAD_LENGTH while the same limit word set through class 9 succeeds and is read back. The extended
	// struct begins with the basic one, so the limit flags land at the same offset either way.
	jobObjectExtendedLimits = uint32(9)
	jobLimitKillOnClose     = uint32(0x00002000)

	// processSetQuota and processTerminate are the two rights AssignProcessToJobObject asks for. Their values are
	// easy to transpose from memory and the failure is not silent: a handle opened with 0x0010 instead of 0x0001
	// gets PROCESS_VM_READ, the OpenProcess succeeds, and every assignment then answers ERROR_ACCESS_DENIED.
	processSetQuota         = uint32(0x00000100)
	processTerminate        = uint32(0x00000001)
	processQueryLimitedInfo = uint32(0x00001000)
	stillActive             = uint32(259)
)

// extendedLimits is Win32's JOBOBJECT_EXTENDED_LIMIT_INFORMATION. Only the first word of the embedded basic
// struct is read by the call below, and the layout has to match exactly because the size is passed to the API:
// contain_windows_test.go asserts it.
type extendedLimits struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
	ReadOperationCount      uint64
	WriteOperationCount     uint64
	OtherOperationCount     uint64
	ReadTransferCount       uint64
	WriteTransferCount      uint64
	OtherTransferCount      uint64
	ProcessMemoryLimit      uintptr
	JobMemoryLimit          uintptr
	PeakProcessMemoryUsed   uintptr
	PeakJobMemoryUsed       uintptr
}

func newScope() (*scope, error) {
	ret, _, err := procCreateJobObject.Call(0, 0)
	if ret == 0 {
		return nil, fmt.Errorf("CreateJobObjectW: %w", err)
	}
	job := syscall.Handle(ret)
	limits := extendedLimits{LimitFlags: jobLimitKillOnClose}
	ok, _, err := procSetInformationJob.Call(
		uintptr(job),
		uintptr(jobObjectExtendedLimits),
		uintptr(unsafe.Pointer(&limits)),
		uintptr(unsafe.Sizeof(limits)),
	)
	if ok == 0 {
		closeHandle(job)
		return nil, fmt.Errorf("SetInformationJobObject(JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE): %w", err)
	}
	return &scope{job: job}, nil
}

// prepare has nothing to do here: Go's syscall.SysProcAttr carries no job attribute list, which is why the
// Windows answer is made after the pid exists and reported rather than assumed.
func (sc *scope) prepare(*exec.Cmd) {}

// attach puts one child in the job and reports the truth about it.
//
// A failure here is not necessarily an uncovered child: if the Connector itself is already inside the host's
// job — ADR-0005 rule 6's chain, Xray ⊆ connector ⊆ host — a second job cannot claim the child, and Windows
// keeps the containment it has. That case is answered by IsProcessInJob with a null job handle, and it is
// reported as contained with the reason named, because rule 5 asks for the result, not for a mechanism.
func (sc *scope) attach(pid int) (bool, string) {
	h, err := openProcess(pid, processSetQuota|processTerminate)
	if err != nil {
		return false, err.Error()
	}
	defer closeHandle(h)
	ok, _, assignErr := procAssignProcessToJob.Call(uintptr(sc.job), uintptr(h))
	if ok != 0 {
		return true, ""
	}
	var inJob int32
	ok, _, err = procIsProcessInJob.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&inJob)))
	if ok != 0 && inJob != 0 {
		return true, "the child is already inside a job this process cannot name (the host's), so ADR-0005 rule 6's chain holds by inheritance"
	}
	return false, fmt.Sprintf("AssignProcessToJobObject: %v (IsProcessInJob said %d: %v)", assignErr, inJob, err)
}

// close releases the job handle, which is what KILL_ON_JOB_CLOSE is a limit on: process exit does this for
// free, and the test does it on purpose.
func (sc *scope) close() error {
	if sc == nil || sc.job == 0 {
		return nil
	}
	if !closeHandle(sc.job) {
		return fmt.Errorf("CloseHandle on the job object failed")
	}
	sc.job = 0
	return nil
}

// askGraceful is false on Windows: nothing in packages/schema or docs/ states a channel Xray would take a
// polite stop on, and an invented one is a fact about a binary this host has never run.
func (c *child) askGraceful() bool { return false }

func closeHandle(h syscall.Handle) bool {
	ret, _, _ := procCloseHandle.Call(uintptr(h))
	return ret != 0
}

func openProcess(pid int, access uint32) (syscall.Handle, error) {
	ret, _, err := procOpenProcess.Call(uintptr(access), 0, uintptr(pid))
	if ret == 0 {
		return 0, fmt.Errorf("OpenProcess(%d): %w", pid, err)
	}
	return syscall.Handle(ret), nil
}

// processAlive is the containment test's only lever: a pid the job has killed answers a status other than
// STILL_ACTIVE, and the handle it opens with is query-only, so the test cannot disturb anything it did not spawn.
func processAlive(pid int) bool {
	h, err := openProcess(pid, processQueryLimitedInfo)
	if err != nil {
		return false
	}
	defer closeHandle(h)
	var code uint32
	ok, _, _ := procGetExitCodeProcess.Call(uintptr(h), uintptr(unsafe.Pointer(&code)))
	return ok != 0 && code == stillActive
}
