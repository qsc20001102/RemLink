//go:build windows

// Package dpapi protects Windows Node secrets using the operating system DPAPI.
package dpapi

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var entropy = []byte("RemLink-v1-NodeIdentity")

// Protector uses machine-scoped DPAPI so Console/service identities can read
// the package-local identity after Windows restart. Directory ACLs remain required.
type Protector struct{}

func (Protector) Protect(plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return nil, errors.New("DPAPI plaintext must not be empty")
	}
	input := blob(plain)
	extra := blob(entropy)
	var output windows.DataBlob
	name, err := windows.UTF16PtrFromString("RemLink Node Identity")
	if err != nil {
		return nil, err
	}
	flags := uint32(windows.CRYPTPROTECT_LOCAL_MACHINE | windows.CRYPTPROTECT_UI_FORBIDDEN)
	if err := windows.CryptProtectData(&input, name, &extra, 0, nil, flags, &output); err != nil {
		return nil, fmt.Errorf("protect secret with DPAPI: %w", err)
	}
	return copyAndFree(output)
}

func (Protector) Unprotect(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, errors.New("DPAPI ciphertext must not be empty")
	}
	input := blob(ciphertext)
	extra := blob(entropy)
	var output windows.DataBlob
	flags := uint32(windows.CRYPTPROTECT_UI_FORBIDDEN)
	if err := windows.CryptUnprotectData(&input, nil, &extra, 0, nil, flags, &output); err != nil {
		return nil, fmt.Errorf("unprotect secret with DPAPI: %w", err)
	}
	return copyAndFree(output)
}

func blob(value []byte) windows.DataBlob {
	return windows.DataBlob{Size: uint32(len(value)), Data: &value[0]}
}

func copyAndFree(value windows.DataBlob) ([]byte, error) {
	if value.Data == nil || value.Size == 0 {
		return nil, errors.New("DPAPI returned empty output")
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(value.Data))))
	result := make([]byte, int(value.Size))
	copy(result, unsafe.Slice(value.Data, int(value.Size)))
	return result, nil
}
