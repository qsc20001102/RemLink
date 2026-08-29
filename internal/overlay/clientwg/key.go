package clientwg

import (
	"encoding/base64"
	"fmt"
)

// ParseKeyBase64 parses the standard WireGuard 32-byte base64 key format.
func ParseKeyBase64(value string) (Key, error) {
	var key Key
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return key, fmt.Errorf("decode WireGuard key: %w", err)
	}
	if len(decoded) != keySize {
		return key, fmt.Errorf("WireGuard key decoded length is %d, want %d", len(decoded), keySize)
	}
	copy(key[:], decoded)
	return key, nil
}
