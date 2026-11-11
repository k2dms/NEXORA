package localpipe

import (
	"crypto/sha256"
	"errors"
	"fmt"
)

var (
	ErrAbsent      = errors.New("local pipe is absent")
	ErrBusy        = errors.New("local pipe is busy")
	ErrOwned       = errors.New("local connector mutex is owned")
	ErrUnsupported = errors.New("local connector IPC requires Windows")
)

func Name(root, sender string) string {
	return fmt.Sprintf(`\\.\pipe\nexora-%x-%x`, sha256.Sum256([]byte(nameRoot(root))), sha256.Sum256([]byte(sender)))
}

func mutexName(root string) string {
	return fmt.Sprintf(`Global\nexora-%x`, sha256.Sum256([]byte(nameRoot(root))))
}
