//go:build windows

package identity

import (
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"

	"remlink/internal/model"
)

// DefaultPath returns a machine-local identity independent of the package path.
// The two roles keep separate identities, including when used by a service.
func DefaultPath(nodeType model.NodeType) (string, error) {
	var role string
	switch nodeType {
	case model.NodeTypeEngineer:
		role = "Engineer"
	case model.NodeTypeSite:
		role = "Site"
	default:
		return "", errors.New("invalid Windows Node type")
	}
	root, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return "", fmt.Errorf("locate Windows machine data directory: %w", err)
	}
	return filepath.Join(root, "RemLink", role, "identity.json"), nil
}
