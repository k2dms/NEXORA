package localpipe

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var (
	createNamedPipe     = kernel32.NewProc("CreateNamedPipeW")
	connectNamedPipe    = kernel32.NewProc("ConnectNamedPipe")
	disconnectNamedPipe = kernel32.NewProc("DisconnectNamedPipe")
	createEvent         = kernel32.NewProc("CreateEventW")
	getOverlappedResult = kernel32.NewProc("GetOverlappedResult")
)

const (
	pipeRejectRemote = 0x00000008
	firstInstance    = 0x00080000
	pipeConnected    = syscall.Errno(535)
	pipeNotConnected = syscall.Errno(233)
	pipeNoData       = syscall.Errno(232)
)

type Listener struct {
	mu       sync.Mutex
	acceptMu sync.Mutex
	name     string
	security *securityDescriptor
	pending  *pipeConn
	closed   bool
	closeErr error
}

type pipeConn struct {
	mu        sync.Mutex
	readMu    sync.Mutex
	writeMu   sync.Mutex
	active    sync.WaitGroup
	closeOnce sync.Once
	handle    syscall.Handle
	closed    bool
	closeErr  error
}

func validateName(name string) error {
	const prefix = `\\.\pipe\nexora-`
	if !strings.HasPrefix(name, prefix) || len(name) <= len(prefix) || len(name) > 256 || strings.ContainsAny(name[len(prefix):], "\\/\x00") {
		return fmt.Errorf("invalid local NEXORA pipe name")
	}
	return nil
}

func newPipe(name string, security *securityDescriptor, first bool) (*pipeConn, error) {
	path, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	flags := uint32(3 | syscall.FILE_FLAG_OVERLAPPED)
	if first {
		flags |= firstInstance
	}
	attributes := security.attributes()
	handle, _, err := createNamedPipe.Call(uintptr(unsafe.Pointer(path)), uintptr(flags), pipeRejectRemote, 255, 65536, 65536, 0, uintptr(unsafe.Pointer(attributes)))
	if syscall.Handle(handle) == syscall.InvalidHandle {
		return nil, fmt.Errorf("create local pipe: %w", err)
	}
	return &pipeConn{handle: syscall.Handle(handle)}, nil
}

func Listen(name string) (*Listener, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	security, err := userSecurity(pipeAccess)
	if err != nil {
		return nil, err
	}
	pending, err := newPipe(name, security, true)
	if err != nil {
		security.close()
		return nil, err
	}
	return &Listener{name: name, security: security, pending: pending}, nil
}

func (l *Listener) Accept() (io.ReadWriteCloser, error) {
	l.acceptMu.Lock()
	defer l.acceptMu.Unlock()
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, os.ErrClosed
	}
	pending := l.pending
	l.mu.Unlock()
	for {
		err := pending.connect()
		if err == nil {
			break
		}
		if !errors.Is(err, pipeNoData) && !errors.Is(err, syscall.ERROR_BROKEN_PIPE) {
			return nil, err
		}
		pending.mu.Lock()
		if pending.closed {
			pending.mu.Unlock()
			return nil, os.ErrClosed
		}
		disconnectNamedPipe.Call(uintptr(pending.handle))
		pending.mu.Unlock()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil, os.ErrClosed
	}
	next, err := newPipe(l.name, l.security, false)
	if err != nil {
		l.closed = true
		l.closeErr = pending.Close()
		l.security.close()
		return nil, err
	}
	l.pending = next
	return pending, nil
}

func (l *Listener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.closed {
		l.closed = true
		l.closeErr = l.pending.Close()
		l.security.close()
	}
	return l.closeErr
}

func Dial(name, expectedServerExe string) (io.ReadWriteCloser, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	expected, err := canonicalPath(expectedServerExe)
	if err != nil {
		return nil, fmt.Errorf("resolve expected server executable: %w", err)
	}
	path, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(path, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OVERLAPPED|0x00100000|0x00010000, 0)
	if err != nil {
		if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
			return nil, ErrAbsent
		}
		if errors.Is(err, syscall.Errno(231)) {
			return nil, ErrBusy
		}
		return nil, fmt.Errorf("open local pipe: %w", err)
	}
	conn := &pipeConn{handle: handle}
	security, err := userSecurity(pipeAccess)
	if err != nil {
		conn.Close()
		return nil, err
	}
	err = security.verify(handle)
	security.close()
	if err != nil {
		conn.Close()
		return nil, err
	}
	actual, err := PeerExecutable(conn, false)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("authenticate pipe server: %w", err)
	}
	if !SameExecutable(actual, expected) {
		conn.Close()
		return nil, fmt.Errorf("pipe server executable %q does not match %q", actual, expected)
	}
	return conn, nil
}

func (c *pipeConn) operation(call func(syscall.Handle, *syscall.Overlapped, *uint32) error) (uint32, error) {
	event, _, err := createEvent.Call(0, 1, 0, 0)
	if event == 0 {
		return 0, fmt.Errorf("create I/O event: %w", err)
	}
	defer syscall.CloseHandle(syscall.Handle(event))
	overlapped := syscall.Overlapped{HEvent: syscall.Handle(event)}
	var transferred uint32
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, os.ErrClosed
	}
	handle := c.handle
	c.active.Add(1)
	err = call(handle, &overlapped, &transferred)
	c.mu.Unlock()
	defer c.active.Done()
	if errors.Is(err, syscall.ERROR_IO_PENDING) {
		ok, _, waitErr := getOverlappedResult.Call(uintptr(handle), uintptr(unsafe.Pointer(&overlapped)), uintptr(unsafe.Pointer(&transferred)), 1)
		if ok == 0 {
			err = waitErr
		} else {
			err = nil
		}
	}
	runtime.KeepAlive(call)
	if errors.Is(err, syscall.ERROR_OPERATION_ABORTED) {
		err = os.ErrClosed
	}
	return transferred, err
}

func (c *pipeConn) connect() error {
	_, err := c.operation(func(handle syscall.Handle, overlapped *syscall.Overlapped, _ *uint32) error {
		ok, _, err := connectNamedPipe.Call(uintptr(handle), uintptr(unsafe.Pointer(overlapped)))
		if ok != 0 || errors.Is(err, pipeConnected) {
			return nil
		}
		return err
	})
	return err
}

func (c *pipeConn) Read(buffer []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if len(buffer) == 0 {
		return 0, nil
	}
	n, err := c.operation(func(handle syscall.Handle, overlapped *syscall.Overlapped, n *uint32) error {
		return syscall.ReadFile(handle, buffer, n, overlapped)
	})
	if errors.Is(err, syscall.ERROR_BROKEN_PIPE) || errors.Is(err, pipeNotConnected) || errors.Is(err, pipeNoData) || (err == nil && n == 0) {
		err = io.EOF
	}
	return int(n), err
}

func (c *pipeConn) Write(buffer []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if len(buffer) == 0 {
		return 0, nil
	}
	n, err := c.operation(func(handle syscall.Handle, overlapped *syscall.Overlapped, n *uint32) error {
		return syscall.WriteFile(handle, buffer, n, overlapped)
	})
	if errors.Is(err, syscall.ERROR_BROKEN_PIPE) || errors.Is(err, pipeNotConnected) || errors.Is(err, pipeNoData) {
		err = io.ErrClosedPipe
	}
	if err == nil && int(n) != len(buffer) {
		err = io.ErrShortWrite
	}
	return int(n), err
}

func (c *pipeConn) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		err := syscall.CancelIoEx(c.handle, nil)
		c.mu.Unlock()
		c.active.Wait()
		c.closeErr = syscall.CloseHandle(c.handle)
		if err != nil && !errors.Is(err, syscall.ERROR_NOT_FOUND) {
			c.closeErr = errors.Join(err, c.closeErr)
		}
	})
	return c.closeErr
}
