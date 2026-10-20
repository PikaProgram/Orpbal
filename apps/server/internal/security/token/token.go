package token

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const rawLength = 32

// New returns a fresh 256-bit token encoded as base64url.
func New() (string, error) {
	buf := make([]byte, rawLength)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Hash returns the hex-encoded SHA-256 of a raw token.
func Hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// VerifyHash compares a raw token with a stored hash in constant time.
func VerifyHash(raw, hash string) bool {
	return subtle.ConstantTimeCompare([]byte(Hash(raw)), []byte(hash)) == 1
}
