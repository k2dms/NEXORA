//go:build !windows

package keystore

import "errors"

// There is no protector here on purpose. ADR-0011 binds Windows, and its own consequences section says Linux and
// macOS need a new ADR rather than an edit of that one; ADR-0011 rule 5 forbids filling the gap with a plaintext
// file. So the answer on this platform is a refusal that maps to KeystoreUnavailable, which is what makes
// `Open` fail closed here instead of quietly working.
func platformProtector() (Protector, error) {
	return nil, errors.New("no keystore protector is implemented for this platform; ADR-0011 binds Windows, and a Linux or macOS store needs its own ADR")
}

// platformKeystoreDir has no root to name either: `%LOCALAPPDATA%` is a Windows path, and the ADR's directory is
// part of what it binds.
func platformKeystoreDir() (string, error) {
	return "", errors.New("ADR-0011 names no keystore root for this platform")
}
