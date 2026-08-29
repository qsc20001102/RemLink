//go:build !windows

package nodeagent

import (
	"context"
	"errors"
)

var ErrWindowsRequired = errors.New("RemLink Engineer and Site Nodes require Windows")

func Run(context.Context, Options) error { return ErrWindowsRequired }
