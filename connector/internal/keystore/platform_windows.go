//go:build windows

package keystore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// dpapi is ADR-0011's protector: CryptProtectData in the user's own scope, with no application entropy, so the
// blob follows the account rather than this binary. That is also the ADR's stated residual — rule 7 concedes
// every process running as this user can call CryptUnprotectData — and the reason this file exists instead of a
// keyring dependency.
type dpapi struct{}

var (
	crypt32                 = syscall.NewLazyDLL("crypt32.dll")
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procCryptProtectData    = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData  = crypt32.NewProc("CryptUnprotectData")
	procLocalFree           = kernel32.NewProc("LocalFree")
	cryptProtectUIForbidden = 0x1
)

// dataBlob is Win32's `DATA_BLOB`: a byte count and a pointer, with the padding Go inserts by alignment rather
// than by hand, so the layout matches what crypt32 reads.
type dataBlob struct {
	cbData uint32
	pbData *byte
}

func platformProtector() (Protector, error) {
	if err := procCryptProtectData.Find(); err != nil {
		return nil, fmt.Errorf("crypt32.dll exposes no CryptProtectData: %w", err)
	}
	if err := procCryptUnprotectData.Find(); err != nil {
		return nil, fmt.Errorf("crypt32.dll exposes no CryptUnprotectData: %w", err)
	}
	return dpapi{}, nil
}

// platformKeystoreDir is ADR-0011 rule 1 read literally: %LOCALAPPDATA%\NEXORA\keystore. There is no fallback to
// a temp directory or to the user profile root, because a secret store whose location depends on an environment
// variable being unset is a secret store the user cannot find again.
func platformKeystoreDir() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "", errors.New("%LOCALAPPDATA% is empty, and ADR-0011 rule 1 names no other keystore root")
	}
	return filepath.Join(base, "NEXORA", "keystore"), nil
}

func (dpapi) Protect(plain []byte) ([]byte, error) {
	return callDataBlob(plain, procCryptProtectData, "CryptProtectData")
}

func (dpapi) Unprotect(blob []byte) ([]byte, error) {
	return callDataBlob(blob, procCryptUnprotectData, "CryptUnprotectData")
}

// callDataBlob runs one of the two crypt32 entry points. Both take, in order, an input blob, a description
// string, an optional entropy blob, a reserved pointer, a prompt struct, a flag word, and the output blob — the
// four middle arguments are NULL here because ADR-0011 rule 1 says user scope and no application entropy, which
// is the same thing the DPAPI docs call the default. Windows allocates the output, so it is copied before
// LocalFree returns it.
func callDataBlob(in []byte, proc *syscall.LazyProc, name string) ([]byte, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("%s: refusing to hand crypt32 an empty blob", name)
	}
	inBlob := dataBlob{cbData: uint32(len(in)), pbData: &in[0]}
	var out dataBlob
	ret, _, err := proc.Call(
		uintptr(unsafe.Pointer(&inBlob)),
		0, // szDataDescr
		0, // pOptionalEntropy
		0, // pvReserved
		0, // pPromptStruct
		uintptr(cryptProtectUIForbidden),
		uintptr(unsafe.Pointer(&out)),
	)
	if ret == 0 {
		return nil, fmt.Errorf("%s failed: %w", name, err)
	}
	if out.pbData == nil || out.cbData == 0 {
		return nil, fmt.Errorf("%s returned an empty blob", name)
	}
	value := unsafe.Slice(out.pbData, int(out.cbData))
	outBytes := make([]byte, len(value))
	copy(outBytes, value)
	procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return outBytes, nil
}
