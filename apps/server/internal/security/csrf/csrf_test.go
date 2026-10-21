package csrf

import (
	"bytes"
	"testing"
)

func newManager(t *testing.T) *Manager {
	t.Helper()
	m, err := New(bytes.Repeat([]byte{0x11}, 32))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return m
}

func TestGenerateAndValidate(t *testing.T) {
	m := newManager(t)
	token, hash, err := m.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if token == "" || hash == "" {
		t.Fatal("Generate() returned an empty value")
	}
	if !m.Validate(token, hash) {
		t.Fatal("Validate() rejected the generated token")
	}
	if m.Validate(token+"x", hash) {
		t.Fatal("Validate() accepted a modified token")
	}
	if m.Validate(token, hash+"x") {
		t.Fatal("Validate() accepted a modified hash")
	}
	if m.Token(hash) != token {
		t.Fatal("Token() is not stable for the same hash")
	}
}

func TestTokensDifferPerSession(t *testing.T) {
	m := newManager(t)
	first, firstHash, err := m.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	second, secondHash, err := m.Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if firstHash == secondHash || first == second {
		t.Fatal("two sessions received the same CSRF values")
	}
	if m.Validate(first, secondHash) {
		t.Fatal("a token validated against another session hash")
	}
}

func TestNewRejectsBadKey(t *testing.T) {
	if _, err := New(make([]byte, 16)); err == nil {
		t.Fatal("New() accepted a short key")
	}
}
