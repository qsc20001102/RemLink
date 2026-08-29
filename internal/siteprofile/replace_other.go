//go:build !windows

package siteprofile

import "os"

func replaceFile(source, destination string) error { return os.Rename(source, destination) }
