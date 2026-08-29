// Package appdir resolves files that belong to a portable RemLink package.
package appdir

import (
	"fmt"
	"os"
	"path/filepath"
)

// Executable returns the absolute directory containing the running executable.
// Engineer and Site use this directory as their portable data root so launching
// them from another working directory does not redirect generated files.
func Executable() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	return filepath.Dir(executable), nil
}

// Join returns a path rooted at the directory containing the executable.
func Join(elements ...string) (string, error) {
	root, err := Executable()
	if err != nil {
		return "", err
	}
	parts := append([]string{root}, elements...)
	return filepath.Join(parts...), nil
}
