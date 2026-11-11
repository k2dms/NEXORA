package localpipe

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	getClientPID = kernel32.NewProc("GetNamedPipeClientProcessId")
	getServerPID = kernel32.NewProc("GetNamedPipeServerProcessId")
	queryImage   = kernel32.NewProc("QueryFullProcessImageNameW")
	createMutex  = kernel32.NewProc("CreateMutexW")
)

func canonicalPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("executable path is empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(absolute)
}

func nameRoot(root string) string {
	absolute, err := filepath.Abs(root)
	if err != nil {
		absolute = filepath.Clean(root)
	}
	return strings.ToLower(absolute)
}

func SameExecutable(actual, expected string) bool {
	a, err := os.Stat(actual)
	if err != nil {
		return false
	}
	b, err := os.Stat(expected)
	return err == nil && os.SameFile(a, b)
}

func PeerExecutable(conn io.ReadWriteCloser, server bool) (string, error) {
	c, ok := conn.(*pipeConn)
	if !ok {
		return "", fmt.Errorf("not a Windows pipe connection")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return "", os.ErrClosed
	}
	proc := getServerPID
	if server {
		proc = getClientPID
	}
	var pid uint32
	result, _, err := proc.Call(uintptr(c.handle), uintptr(unsafe.Pointer(&pid)))
	if result == 0 || pid == 0 {
		return "", fmt.Errorf("resolve pipe peer PID: %w", err)
	}
	handle, err := syscall.OpenProcess(0x1000, false, pid)
	if err != nil {
		return "", fmt.Errorf("open pipe peer: %w", err)
	}
	defer syscall.CloseHandle(handle)
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	result, _, err = queryImage.Call(uintptr(handle), 0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)))
	if result == 0 {
		return "", fmt.Errorf("resolve pipe peer executable: %w", err)
	}
	return syscall.UTF16ToString(buffer[:size]), nil
}

type mutexHandle syscall.Handle

func (h mutexHandle) Close() error { return syscall.CloseHandle(syscall.Handle(h)) }

func Mutex(root string) (io.Closer, error) {
	security, err := userSecurity(mutexAccess)
	if err != nil {
		return nil, err
	}
	defer security.close()
	name, err := syscall.UTF16PtrFromString(mutexName(root))
	if err != nil {
		return nil, err
	}
	attributes := security.attributes()
	// Object existence owns the instance, avoiding thread-affine mutex ownership in Go.
	handle, _, callErr := createMutex.Call(uintptr(unsafe.Pointer(attributes)), 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return nil, fmt.Errorf("create Connector mutex: %w", callErr)
	}
	if err := security.verify(syscall.Handle(handle)); err != nil {
		syscall.CloseHandle(syscall.Handle(handle))
		return nil, err
	}
	if callErr == syscall.ERROR_ALREADY_EXISTS {
		syscall.CloseHandle(syscall.Handle(handle))
		return nil, ErrOwned
	}
	return mutexHandle(handle), nil
}

func Detach(cmd *exec.Cmd) error {
	if cmd.SysProcAttr != nil {
		return fmt.Errorf("detached Connector already has process attributes")
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200 | 0x00000008 | 0x01000000}
	return nil
}
