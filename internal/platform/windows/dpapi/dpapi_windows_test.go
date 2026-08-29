//go:build windows

package dpapi

import (
	"bytes"
	"testing"
)

func TestProtectUnprotect(t *testing.T) {
	protector := Protector{}
	plain := []byte("RemLink test secret")
	protected, err := protector.Protect(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(protected, plain) {
		t.Fatal("DPAPI returned plaintext")
	}
	unprotected, err := protector.Unprotect(protected)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unprotected, plain) {
		t.Fatalf("unprotected = %q, want %q", unprotected, plain)
	}
}
