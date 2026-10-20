package crypto

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
)

func newTestCipher(t *testing.T) *Cipher {
	t.Helper()
	c, err := New(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return c
}

func TestRoundTrip(t *testing.T) {
	c := newTestCipher(t)
	for _, plain := range [][]byte{
		[]byte("postgres-password"),
		[]byte("p@ss:w0rd/with spaces and ünicode"),
		{0x00, 0x01, 0xff},
		{},
	} {
		sealed, err := c.Encrypt(plain)
		if err != nil {
			t.Fatalf("Encrypt() error = %v", err)
		}
		if bytes.Contains(sealed, plain) && len(plain) > 0 {
			t.Fatal("ciphertext contains plaintext")
		}
		opened, err := c.Decrypt(sealed)
		if err != nil {
			t.Fatalf("Decrypt() error = %v", err)
		}
		if !bytes.Equal(opened, plain) {
			t.Fatalf("Decrypt() = %q, want %q", opened, plain)
		}
	}
}

func TestEncryptUsesFreshNonce(t *testing.T) {
	c := newTestCipher(t)
	first, err := c.Encrypt([]byte("same"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	second, err := c.Encrypt([]byte("same"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("two encryptions produced identical ciphertext")
	}
}

func TestDecryptRejectsTamperingAndWrongKeys(t *testing.T) {
	c := newTestCipher(t)
	sealed, err := c.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := c.Decrypt(tampered); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("Decrypt(tampered) error = %v, want ErrDecrypt", err)
	}
	if _, err := c.Decrypt(sealed[:3]); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("Decrypt(short) error = %v, want ErrDecrypt", err)
	}

	other, err := New(bytes.Repeat([]byte{0x24}, 32))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := other.Decrypt(sealed); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("Decrypt(wrong key) error = %v, want ErrDecrypt", err)
	}
}

func TestNewRejectsBadKeys(t *testing.T) {
	if _, err := New(make([]byte, 16)); err == nil {
		t.Fatal("New() accepted a 16-byte key")
	}
	if _, err := NewFromBase64("not-base64!"); err == nil {
		t.Fatal("NewFromBase64() accepted invalid base64")
	}
	valid := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if _, err := NewFromBase64(valid); err != nil {
		t.Fatalf("NewFromBase64() error = %v", err)
	}
}
