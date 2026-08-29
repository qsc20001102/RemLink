package clientwg

import (
	"encoding/base64"
	"testing"
)

func TestParseKeyBase64(t *testing.T) {
	t.Parallel()
	raw := make([]byte, keySize)
	for index := range raw {
		raw[index] = byte(index + 1)
	}
	key, err := ParseKeyBase64(base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatalf("ParseKeyBase64() error = %v", err)
	}
	if key[0] != 1 || key[31] != 32 {
		t.Fatalf("ParseKeyBase64() returned unexpected bytes: first=%d last=%d", key[0], key[31])
	}
	if _, err := ParseKeyBase64("not-base64"); err == nil {
		t.Fatal("ParseKeyBase64() accepted invalid base64")
	}
	if _, err := ParseKeyBase64(base64.StdEncoding.EncodeToString([]byte{1, 2, 3})); err == nil {
		t.Fatal("ParseKeyBase64() accepted a short key")
	}
}
