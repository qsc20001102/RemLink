//go:build windows

package identity

import (
	"errors"
	"path/filepath"

	"remlink/internal/appdir"
	"remlink/internal/model"
)

// DefaultPath returns the portable identity path beside the role executable.
// Engineer and Site are shipped in different directories, so the common file
// name cannot collide across the two package bodies.
func DefaultPath(nodeType model.NodeType) (string, error) {
	switch nodeType {
	case model.NodeTypeEngineer, model.NodeTypeSite:
		root, err := appdir.Executable()
		if err != nil {
			return "", err
		}
		return filepath.Join(root, "identity.json"), nil
	default:
		return "", errors.New("invalid Windows Node type")
	}
}
