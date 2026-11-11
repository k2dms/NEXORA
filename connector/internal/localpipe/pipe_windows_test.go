package localpipe

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func pair(t *testing.T) (io.ReadWriteCloser, io.ReadWriteCloser) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := Listen(Name(t.TempDir(), "sender"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	accepted := make(chan io.ReadWriteCloser, 1)
	failed := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			failed <- err
			return
		}
		accepted <- conn
	}()
	client, err := Dial(listener.name, exe)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	select {
	case server := <-accepted:
		t.Cleanup(func() { _ = server.Close() })
		return server, client
	case err := <-failed:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("accept timed out")
	}
	return nil, nil
}

func TestPipePreservesBytesAndAuthenticatesBothPeers(t *testing.T) {
	server, client := pair(t)
	exe, _ := os.Executable()
	for _, side := range []struct {
		conn   io.ReadWriteCloser
		server bool
	}{{server, true}, {client, false}} {
		image, err := PeerExecutable(side.conn, side.server)
		if err != nil || !SameExecutable(image, exe) {
			t.Fatalf("peer image %q: %v", image, err)
		}
	}
	payload := bytes.Repeat([]byte{0, 1, 0xff, '\n'}, 32768)
	for _, ends := range [][2]io.ReadWriteCloser{{client, server}, {server, client}} {
		done := make(chan error, 1)
		go func() { _, err := ends[0].Write(payload); done <- err }()
		got := make([]byte, len(payload))
		if _, err := io.ReadFull(ends[1], got); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatal("pipe changed payload")
		}
	}
}

func TestClosingPipeAndListenerUnblocksPendingOperations(t *testing.T) {
	server, _ := pair(t)
	done := make(chan error, 1)
	go func() { _, err := server.Read(make([]byte, 1)); done <- err }()
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, os.ErrClosed) {
			t.Fatalf("read ended with %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("close did not unblock read")
	}
	listener, err := Listen(Name(t.TempDir(), "sender"))
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, err := listener.Accept(); done <- err }()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, os.ErrClosed) {
			t.Fatalf("accept ended with %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("close did not unblock accept")
	}
}

func TestMutexOwnershipAndPipeSquattingAreExclusive(t *testing.T) {
	root := t.TempDir()
	owner, err := Mutex(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if owner != nil {
			owner.Close()
		}
	}()
	if second, err := Mutex(root); !errors.Is(err, ErrOwned) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("second owner: %v", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	owner = nil
	next, err := Mutex(root)
	if err != nil {
		t.Fatalf("reacquire: %v", err)
	}
	defer next.Close()
	listener, err := Listen(Name(root, "sender"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if second, err := Listen(listener.name); err == nil {
		second.Close()
		t.Fatal("second first pipe instance accepted")
	}
}

func TestAbsentAndWrongExecutableAreNotAdopted(t *testing.T) {
	exe, _ := os.Executable()
	if _, err := Dial(Name(t.TempDir(), "absent"), exe); !errors.Is(err, ErrAbsent) {
		t.Fatal(err)
	}
	listener, err := Listen(Name(t.TempDir(), "sender"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	wrong := filepath.Join(os.Getenv("WINDIR"), "System32", "cmd.exe")
	if conn, err := Dial(listener.name, wrong); err == nil {
		conn.Close()
		t.Fatal("wrong server executable adopted")
	}
	accepted := make(chan io.ReadWriteCloser, 1)
	failed := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			failed <- err
			return
		}
		accepted <- conn
	}()
	client, err := Dial(listener.name, exe)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	select {
	case conn := <-accepted:
		conn.Close()
	case err := <-failed:
		t.Fatalf("rejected peer killed listener: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("listener did not recover from rejected client")
	}
}
