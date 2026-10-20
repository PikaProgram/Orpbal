package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	memory      = 64 * 1024
	iterations  = 3
	parallelism = 2
	saltLength  = 16
	keyLength   = 32

	minPasswordLength = 8
	maxPasswordLength = 128
)

var (
	usernamePattern = regexp.MustCompile(`^[a-z0-9_.-]{3,64}$`)
	errInvalidHash  = errors.New("invalid password hash")
)

// Hash derives an argon2id hash encoded as a PHC string.
func Hash(plain string) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(plain), salt, iterations, memory, parallelism, keyLength)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		memory,
		iterations,
		parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// Verify reports whether plain matches the encoded argon2id hash.
func Verify(plain, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}

	var mem, iter uint32
	var par uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &iter, &par); err != nil {
		return false
	}
	if mem > 1024*1024 || iter > 10 || par > 16 || mem < 8*1024 || iter < 1 || par < 1 {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 || len(salt) == 0 {
		return false
	}

	got := argon2.IDKey([]byte(plain), salt, iter, mem, par, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// ValidateUsername enforces the canonical username format.
func ValidateUsername(username string) error {
	if !usernamePattern.MatchString(username) {
		return errors.New("username must be 3-64 characters of lowercase letters, digits, '_', '.' or '-'")
	}
	return nil
}

// ValidatePassword enforces the password policy without trimming the value.
func ValidatePassword(plain, username string) error {
	length := utf8.RuneCountInString(plain)
	if length < minPasswordLength || length > maxPasswordLength {
		return fmt.Errorf("password must be %d-%d characters", minPasswordLength, maxPasswordLength)
	}
	if username != "" && strings.EqualFold(plain, username) {
		return errors.New("password must differ from username")
	}

	var upper, lower, digit, symbol bool
	for _, r := range plain {
		switch {
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsLower(r):
			lower = true
		case unicode.IsDigit(r):
			digit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			symbol = true
		}
	}
	if !upper || !lower || !digit || !symbol {
		return errors.New("password must contain uppercase, lowercase, digit and symbol characters")
	}
	return nil
}
