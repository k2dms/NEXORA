//go:build windows

package supervisor

import (
	"testing"
	"unsafe"
)

// SetInformationJobObject answers ERROR_BAD_LENGTH for a buffer whose length it does not recognise, so the one
// thing this file must assert before any behavioural test means anything is that the size handed to the API is
// the size Win32's own header produces. The 64-bit layout is the one this release runs; the 32-bit branch is
// written out rather than skipped because a wrong length is silent in exactly the way this call is not.
func TestJobObjectLayoutMatchesWin32(t *testing.T) {
	var l extendedLimits
	word := unsafe.Sizeof(l.MinimumWorkingSetSize)
	wantSize, wantIoInfo := uintptr(0), uintptr(0)
	switch word {
	case 8: // 64 basic (16 basic-limit fields and their padding aside), 48 IO_COUNTERS, four 8-byte words.
		wantSize, wantIoInfo = 144, 64
	case 4: // 48 basic, 48 IO_COUNTERS, four 4-byte words.
		wantSize, wantIoInfo = 112, 48
	default:
		t.Fatalf("a %d-byte SIZE_T is not a layout Win32 documents", word)
	}
	if unsafe.Sizeof(l) != wantSize {
		t.Fatalf("JOBOBJECT_EXTENDED_LIMIT_INFORMATION is %d bytes here, want %d", unsafe.Sizeof(l), wantSize)
	}
	if off := unsafe.Offsetof(l.LimitFlags); off != 16 {
		t.Errorf("LimitFlags is at offset %d, and Win32 reads it at 16; the flag word would land in the working-set pair", off)
	}
	if off := unsafe.Offsetof(l.ReadOperationCount); off != wantIoInfo {
		t.Errorf("IoInfo begins at offset %d, want %d", off, wantIoInfo)
	}
	if off := unsafe.Offsetof(l.ProcessMemoryLimit); off != wantSize-4*word {
		t.Errorf("the four SIZE_T fields do not end the struct: ProcessMemoryLimit at %d of %d", off, wantSize)
	}
}

func TestScopeIsMadeAndReleasedWithoutError(t *testing.T) {
	sc, err := newScope()
	if err != nil {
		t.Fatalf("newScope: %v", err)
	}
	if sc.job == 0 {
		t.Fatal("a scope without a job handle reports containment it cannot deliver")
	}
	if err := sc.close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := sc.close(); err != nil {
		t.Errorf("closing twice should be nothing, got %v", err)
	}
}
