package token

import (
	"encoding/base64"
	"testing"
)

func TestNewProduces256BitTokens(t *testing.T) {
	raw, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("token is not base64url: %v", err)
	}
	if len(decoded) != rawLength {
		t.Fatalf("token length = %d bytes, want %d", len(decoded), rawLength)
	}
}

func TestNewProducesDistinctTokens(t *testing.T) {
	seen := make(map[string]bool, 100)
	for range 100 {
		raw, err := New()
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if seen[raw] {
			t.Fatal("New() returned a duplicate token")
		}
		seen[raw] = true
	}
}

func TestHashAndVerify(t *testing.T) {
	raw, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	hash := Hash(raw)
	if hash == raw {
		t.Fatal("Hash() returned the raw token")
	}
	if !VerifyHash(raw, hash) {
		t.Fatal("VerifyHash() rejected the matching token")
	}
	if VerifyHash(raw+"x", hash) {
		t.Fatal("VerifyHash() accepted a different token")
	}
	if VerifyHash(raw, "") {
		t.Fatal("VerifyHash() accepted an empty hash")
	}
}
