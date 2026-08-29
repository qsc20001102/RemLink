//go:build !windows

package identity

import (
	"errors"

	"remlink/internal/model"
)

var ErrWindowsRequired = errors.New("default Node identity path requires Windows")

func DefaultPath(model.NodeType) (string, error) { return "", ErrWindowsRequired }
