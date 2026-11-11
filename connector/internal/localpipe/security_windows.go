package localpipe

import (
	"bytes"
	"fmt"
	"syscall"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	convertSecurityDescriptor = advapi32.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
	getSecurityInfo           = advapi32.NewProc("GetSecurityInfo")
	getDescriptorControl      = advapi32.NewProc("GetSecurityDescriptorControl")
	getDescriptorDACL         = advapi32.NewProc("GetSecurityDescriptorDacl")
	localFree                 = kernel32.NewProc("LocalFree")
)

const (
	pipeAccess  = 0x001f01ff
	mutexAccess = 0x001f0001
)

type securityDescriptor struct {
	pointer unsafe.Pointer
}

func userSecurity(access uint32) (*securityDescriptor, error) {
	token, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return nil, fmt.Errorf("open current user token: %w", err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("get current user SID: %w", err)
	}
	sid, err := user.User.Sid.String()
	if err != nil {
		return nil, err
	}
	sddl, err := syscall.UTF16PtrFromString(fmt.Sprintf("D:P(A;;0x%08x;;;%s)", access, sid))
	if err != nil {
		return nil, err
	}
	d := &securityDescriptor{}
	ok, _, err := convertSecurityDescriptor.Call(uintptr(unsafe.Pointer(sddl)), 1, uintptr(unsafe.Pointer(&d.pointer)), 0)
	if ok == 0 {
		return nil, fmt.Errorf("create protected user DACL: %w", err)
	}
	return d, nil
}

func (d *securityDescriptor) attributes() *syscall.SecurityAttributes {
	return &syscall.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(syscall.SecurityAttributes{})),
		SecurityDescriptor: uintptr(d.pointer),
	}
}

func (d *securityDescriptor) close() {
	if d.pointer != nil {
		localFree.Call(uintptr(d.pointer))
		d.pointer = nil
	}
}

func descriptorDACL(pointer unsafe.Pointer) ([]byte, error) {
	var control uint16
	var revision uint32
	ok, _, err := getDescriptorControl.Call(uintptr(pointer), uintptr(unsafe.Pointer(&control)), uintptr(unsafe.Pointer(&revision)))
	if ok == 0 {
		return nil, fmt.Errorf("get DACL control: %w", err)
	}
	if control&0x1000 == 0 {
		return nil, fmt.Errorf("object DACL is not protected")
	}
	var present, defaulted int32
	var acl unsafe.Pointer
	ok, _, err = getDescriptorDACL.Call(uintptr(pointer), uintptr(unsafe.Pointer(&present)), uintptr(unsafe.Pointer(&acl)), uintptr(unsafe.Pointer(&defaulted)))
	if ok == 0 {
		return nil, fmt.Errorf("get object DACL: %w", err)
	}
	if present == 0 || acl == nil || defaulted != 0 {
		return nil, fmt.Errorf("object lacks an explicit DACL")
	}
	size := *(*uint16)(unsafe.Add(acl, 2))
	if size < 8 {
		return nil, fmt.Errorf("invalid DACL size")
	}
	return unsafe.Slice((*byte)(acl), int(size)), nil
}

func (d *securityDescriptor) verify(handle syscall.Handle) error {
	var actual unsafe.Pointer
	code, _, _ := getSecurityInfo.Call(uintptr(handle), 6, 4, 0, 0, 0, 0, uintptr(unsafe.Pointer(&actual)))
	if code != 0 {
		return fmt.Errorf("get kernel object security: %w", syscall.Errno(code))
	}
	defer localFree.Call(uintptr(actual))
	actualACL, err := descriptorDACL(actual)
	if err != nil {
		return err
	}
	expectedACL, err := descriptorDACL(d.pointer)
	if err != nil {
		return err
	}
	if !bytes.Equal(actualACL, expectedACL) {
		return fmt.Errorf("object DACL does not exclusively grant the current user")
	}
	return nil
}
