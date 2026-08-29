//go:build windows && !amd64

package wintunruntime

import (
	"crypto/sha256"
	"fmt"
	"runtime"
)

func assetBytes() ([]byte, [sha256.Size]byte, error) {
	return nil, [sha256.Size]byte{}, fmt.Errorf("Phase 1 Wintun asset is not bundled for windows/%s", runtime.GOARCH)
}
