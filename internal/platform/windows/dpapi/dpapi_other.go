//go:build !windows

package dpapi

import "errors"

var ErrWindowsRequired = errors.New("DPAPI requires Windows")

type Protector struct{}

func (Protector) Protect([]byte) ([]byte, error)   { return nil, ErrWindowsRequired }
func (Protector) Unprotect([]byte) ([]byte, error) { return nil, ErrWindowsRequired }
