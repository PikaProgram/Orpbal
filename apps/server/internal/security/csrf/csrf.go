package csrf

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	tokenpkg "Orpbal/internal/security/token"
)

// Manager derives per-session synchronizer tokens from a server key.
// The session stores only the hash of a random secret; the token returned to
// the client is an HMAC over that hash, so it is stable across session-token
// rotation and cannot be reconstructed from a PlatformDatabase leak alone.
type Manager struct {
	key []byte
}

// New derives a dedicated CSRF key from the 32-byte encryption key.
func New(encryptionKey []byte) (*Manager, error) {
	if len(encryptionKey) != 32 {
		return nil, fmt.Errorf("CSRF key must be 32 bytes, got %d", len(encryptionKey))
	}
	mac := hmac.New(sha256.New, encryptionKey)
	mac.Write([]byte("orpbal/csrf/v1"))
	return &Manager{key: mac.Sum(nil)}, nil
}

// Generate creates a random session secret and its derived CSRF token.
func (m *Manager) Generate() (token, hash string, err error) {
	secret, err := tokenpkg.New()
	if err != nil {
		return "", "", err
	}
	hash = tokenpkg.Hash(secret)
	return m.Token(hash), hash, nil
}

// Token derives the CSRF token from a stored session hash.
func (m *Manager) Token(hash string) string {
	mac := hmac.New(sha256.New, m.key)
	mac.Write([]byte(hash))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Validate compares a client token with the one derived from the stored hash.
func (m *Manager) Validate(token, hash string) bool {
	return hmac.Equal([]byte(token), []byte(m.Token(hash)))
}
