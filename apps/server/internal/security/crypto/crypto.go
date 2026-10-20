package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrDecrypt is returned for malformed or unauthenticated ciphertext.
var ErrDecrypt = errors.New("decrypt: authentication failed")

// Cipher encrypts managed database credentials with AES-256-GCM.
type Cipher struct {
	aead cipher.AEAD
}

// New builds a Cipher from a 32-byte AES key.
func New(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// NewFromBase64 builds a Cipher from a base64-encoded 32-byte key.
func NewFromBase64(encoded string) (*Cipher, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("decode encryption key: %w", err)
	}
	return New(key)
}

// Encrypt returns nonce || ciphertext. A fresh nonce is generated per call.
func (c *Cipher) Encrypt(plain []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	return c.aead.Seal(nonce, nonce, plain, nil), nil
}

// Decrypt opens nonce-prefixed ciphertext and never returns partial plaintext.
func (c *Cipher) Decrypt(sealed []byte) ([]byte, error) {
	nonceSize := c.aead.NonceSize()
	if len(sealed) < nonceSize {
		return nil, ErrDecrypt
	}
	plain, err := c.aead.Open(nil, sealed[:nonceSize], sealed[nonceSize:], nil)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plain, nil
}
