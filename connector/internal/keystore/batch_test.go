package keystore

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
)

type batchProtector struct {
	calls       int
	failAt      int
	unchangedAt int
	hook        func(int)
}

func (p *batchProtector) Protect(value []byte) ([]byte, error) {
	p.calls++
	if p.hook != nil {
		p.hook(p.calls)
	}
	if p.calls == p.failAt {
		return nil, errors.New("second staged protection refused")
	}
	if p.calls == p.unchangedAt {
		return value, nil
	}
	return encodingProtector{}.Protect(value)
}
func (*batchProtector) Unprotect(blob []byte) ([]byte, error) {
	return encodingProtector{}.Unprotect(blob)
}

func batchSnapshot(t *testing.T, s *Store) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(s.Dir())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		blob, err := os.ReadFile(s.Dir() + "/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		out[entry.Name()] = blob
	}
	return out
}

func TestPutBatchProtectionFailureLeavesByteIdenticalStore(t *testing.T) {
	for _, unchanged := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "plaintext fallback"}[unchanged], func(t *testing.T) {
			p := &batchProtector{}
			s := openStore(t, p)
			if err := s.Put(userIDRef, []byte("prior encrypted credential")); err != nil {
				t.Fatal(err)
			}
			before := batchSnapshot(t, s)
			p.calls = 0
			if unchanged {
				p.unchangedAt = 2
			} else {
				p.failAt = 2
			}
			err := s.PutBatch(map[string][]byte{
				userIDRef: []byte("rotated credential"), headerRef: []byte(`{"Host":"secret"}`),
			})
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("PutBatch = %v", err)
			}
			if p.calls != 2 {
				t.Fatalf("protect calls = %d", p.calls)
			}
			if after := batchSnapshot(t, s); !reflect.DeepEqual(before, after) {
				t.Fatal("protection failure changed encrypted blobs or left staged files")
			}
		})
	}
}

func TestPutBatchValidatesAllEntriesBeforeProtection(t *testing.T) {
	for _, values := range []map[string][]byte{
		{userIDRef: []byte("valid"), "keystore://server/zzzzzzzz/user-id": []byte("invalid ref")},
		{userIDRef: []byte("valid"), headerRef: nil},
	} {
		p := &batchProtector{}
		s := openStore(t, p)
		if err := s.PutBatch(values); err == nil {
			t.Fatal("invalid batch accepted")
		}
		if p.calls != 0 || len(batchSnapshot(t, s)) != 0 {
			t.Fatal("invalid batch was staged or written")
		}
	}
}

func TestPutBatchRenameFailureRollsBackEncryptedBlobsAndNewTargets(t *testing.T) {
	p := &batchProtector{}
	s := openStore(t, p)
	first := "keystore://server/11111111/user-id"
	newRef := "keystore://server/22222222/user-id"
	blocked := "keystore://server/33333333/user-id"
	if err := s.Put(first, []byte("prior value")); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(headerRef, []byte("unrelated value")); err != nil {
		t.Fatal(err)
	}
	before := batchSnapshot(t, s)
	blockedRef, err := Parse(blocked)
	if err != nil {
		t.Fatal(err)
	}
	// All old targets are read before protection. Simulate an external rename
	// obstruction arising during staging, using a real directory on Windows.
	p.calls = 0
	p.hook = func(call int) {
		if call == 3 {
			if err := os.Mkdir(s.path(blockedRef), 0o700); err != nil {
				t.Fatal(err)
			}
		}
	}
	err = s.PutBatch(map[string][]byte{
		first: []byte("rotation"), newRef: []byte("new value"), blocked: []byte("blocked value"),
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("rename failure = %v", err)
	}
	if p.calls != 3 {
		t.Fatalf("rollback called protector: %d calls", p.calls)
	}
	if after := batchSnapshot(t, s); !reflect.DeepEqual(before, after) {
		t.Fatal("rollback did not restore the byte-identical encrypted snapshot")
	}
	if has, err := s.Has(newRef); err != nil || has {
		t.Fatalf("new target survived: %v, %v", has, err)
	}
	if info, err := os.Stat(s.path(blockedRef)); err != nil || !info.IsDir() {
		t.Fatal("batch removed a failed target it never installed")
	}
	p.hook = nil
	got, err := s.Resolve(first)
	if err != nil || !bytes.Equal(got.Value, []byte("prior value")) {
		t.Fatal("old credential was not restored")
	}
}

func TestPutBatchRotationAndEmptyBatch(t *testing.T) {
	s := openStore(t, encodingProtector{})
	if err := s.PutBatch(nil); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"first", "rotated"} {
		if err := s.PutBatch(map[string][]byte{userIDRef: []byte(value), headerRef: []byte(`{"Host":"x"}`)}); err != nil {
			t.Fatal(err)
		}
		got, err := s.Resolve(userIDRef)
		if err != nil || string(got.Value) != value {
			t.Fatal("batch rotation failed")
		}
	}
	if len(batchSnapshot(t, s)) != 2 {
		t.Fatal("batch left scratch files or extra blobs")
	}
}

func TestPutBatchSerializesExistingOperations(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	p := &batchProtector{hook: func(call int) {
		if call == 1 {
			close(entered)
			<-release
		}
	}}
	s := openStore(t, p)
	done := make(chan error, 1)
	go func() { done <- s.PutBatch(map[string][]byte{userIDRef: []byte("batch")}) }()
	<-entered
	if s.mu.TryLock() {
		s.mu.Unlock()
		close(release)
		t.Fatal("batch released its lock while staging")
	}
	// Launch all old APIs while the batch is suspended in protection. They
	// finish only once the batch releases its lock, with no recursive locking.
	var wg sync.WaitGroup
	wg.Add(4)
	go func() { defer wg.Done(); _, _ = s.Resolve(userIDRef) }()
	go func() { defer wg.Done(); _, _ = s.Has(userIDRef) }()
	go func() { defer wg.Done(); _, _ = s.Delete(headerRef) }()
	go func() { defer wg.Done(); _ = s.Put(headerRef, []byte("concurrent")) }()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	wg.Wait()
}
