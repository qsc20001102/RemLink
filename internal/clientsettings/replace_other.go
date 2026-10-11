//go:build !windows

package clientsettings

import "os"

func replace(source, destination string) error { return os.Rename(source, destination) }
