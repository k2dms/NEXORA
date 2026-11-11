//go:build !windows

package localpipe

import (
	"io"
	"os/exec"
	"path/filepath"
)

type Listener struct{}

func nameRoot(root string) string {
	if absolute, err := filepath.Abs(root); err == nil {
		return filepath.Clean(absolute)
	}
	return filepath.Clean(root)
}

func Listen(string) (*Listener, error) { return nil, ErrUnsupported }

func (*Listener) Accept() (io.ReadWriteCloser, error) { return nil, ErrUnsupported }

func (*Listener) Close() error { return ErrUnsupported }

func Dial(string, string) (io.ReadWriteCloser, error) { return nil, ErrUnsupported }

func PeerExecutable(io.ReadWriteCloser, bool) (string, error) { return "", ErrUnsupported }

func SameExecutable(string, string) bool { return false }

func Mutex(string) (io.Closer, error) { return nil, ErrUnsupported }

func Detach(*exec.Cmd) error { return ErrUnsupported }
