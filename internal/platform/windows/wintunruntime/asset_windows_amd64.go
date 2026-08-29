//go:build windows && amd64

package wintunruntime

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

//go:embed assets/amd64/wintun.dll
var embeddedDLL []byte

func assetBytes() ([]byte, [sha256.Size]byte, error) {
	var expected [sha256.Size]byte
	decoded, err := hex.DecodeString(DLLSHA256AMD64)
	if err != nil {
		return nil, expected, err
	}
	copy(expected[:], decoded)
	return embeddedDLL, expected, nil
}
