//go:build !windows

package clientstate

import (
	"os"

	"remlink/internal/identity"
	"remlink/internal/model"
)

func Prepare(model.NodeType, string, string) (*Paths, error) {
	return nil, identity.ErrWindowsRequired
}

func PrepareDesktop(model.NodeType, string, string) (*Paths, error) {
	return nil, identity.ErrWindowsRequired
}

func publishMissing(source, destination string) error { return os.Link(source, destination) }
