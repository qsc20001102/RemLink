//go:build windows

package clientstate

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"

	"remlink/internal/appdir"
	"remlink/internal/identity"
	"remlink/internal/model"
	"remlink/internal/platform/windows/dpapi"
)

// Prepare resolves defaults, imports legacy files, and prevents two packages
// from concurrently using the same role identity on this machine.
func Prepare(role model.NodeType, configOverride, identityOverride string) (*Paths, error) {
	return prepareWindows(role, configOverride, identityOverride, false)
}

func PrepareDesktop(role model.NodeType, configOverride, identityOverride string) (*Paths, error) {
	return prepareWindows(role, configOverride, identityOverride, true)
}

func prepareWindows(role model.NodeType, configOverride, identityOverride string, desktop bool) (*Paths, error) {
	defaultIdentity, err := identity.DefaultPath(role)
	if err != nil {
		return nil, err
	}
	if !windows.GetCurrentProcessToken().IsElevated() {
		return nil, errors.New("administrator privileges are required to manage the RemLink Wintun adapter")
	}
	packageDirectory, err := appdir.Executable()
	if err != nil {
		return nil, err
	}
	lockIdentity := defaultIdentity
	if identityOverride != "" {
		lockIdentity, err = filepath.Abs(identityOverride)
		if err != nil {
			return nil, err
		}
	}
	release, err := lockIdentityPath(lockIdentity)
	if err != nil {
		return nil, err
	}
	dataDirectory := filepath.Dir(defaultIdentity)
	if err := secureDirectory(dataDirectory); err != nil {
		_ = release()
		return nil, err
	}
	prepare := prepareAt
	if desktop {
		prepare = prepareDesktopAt
	}
	paths, err := prepare(role, packageDirectory, dataDirectory, configOverride, identityOverride, dpapi.Protector{}, func(err error) bool {
		return errors.Is(err, windows.ERROR_INVALID_DATA)
	})
	if err != nil {
		_ = release()
		return nil, err
	}
	paths.release = release
	return paths, nil
}

func secureDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create machine client directory: %w", err)
	}
	// Machine-scoped DPAPI requires the files to be readable only by the
	// elevated client and service accounts, not ordinary Windows users.
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		return fmt.Errorf("protect machine client directory (run as administrator): %w", err)
	}
	return nil
}

func lockIdentityPath(path string) (func() error, error) {
	hash := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(path))))
	name, err := windows.UTF16PtrFromString(fmt.Sprintf("Global\\RemLink-Node-%x", hash))
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(handle)
		return nil, errors.New("这台电脑上已有程序正在使用此节点身份，请先退出旧程序再运行新包")
	}
	if err != nil {
		return nil, fmt.Errorf("lock Node identity: %w", err)
	}
	return func() error { return windows.CloseHandle(handle) }, nil
}

func publishMissing(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	// Publish a complete file atomically, without replacing a concurrently
	// created configuration or preference file.
	err = windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
		return os.ErrExist
	}
	return err
}
