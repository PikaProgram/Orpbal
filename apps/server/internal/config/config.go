package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	sessionpolicy "Orpbal/internal/security/session"
)

// Config holds runtime settings that are not database credentials.
type Config struct {
	AppEnv        string
	AppOrigin     string
	CookieSecure  bool
	EncryptionKey []byte
	Session       sessionpolicy.Policy
}

// Load reads and validates the server configuration.
func Load() (Config, error) {
	encodedKey := strings.TrimSpace(os.Getenv("ORPBAL_ENCRYPTION_KEY"))
	if encodedKey == "" {
		return Config{}, fmt.Errorf("ORPBAL_ENCRYPTION_KEY is required")
	}
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return Config{}, fmt.Errorf("decode ORPBAL_ENCRYPTION_KEY: %w", err)
	}
	if len(key) != 32 {
		return Config{}, fmt.Errorf("ORPBAL_ENCRYPTION_KEY must decode to 32 bytes, got %d", len(key))
	}

	policy := sessionpolicy.Default()
	if policy.IdleTimeout, err = durationEnv("ORPBAL_SESSION_IDLE_TIMEOUT", policy.IdleTimeout); err != nil {
		return Config{}, err
	}
	if policy.AbsoluteTimeout, err = durationEnv("ORPBAL_SESSION_ABSOLUTE_TIMEOUT", policy.AbsoluteTimeout); err != nil {
		return Config{}, err
	}
	if policy.RotationInterval, err = durationEnv("ORPBAL_SESSION_ROTATION_INTERVAL", policy.RotationInterval); err != nil {
		return Config{}, err
	}
	if policy.GracePeriod, err = durationEnv("ORPBAL_SESSION_GRACE_PERIOD", policy.GracePeriod); err != nil {
		return Config{}, err
	}
	if err := policy.Validate(); err != nil {
		return Config{}, fmt.Errorf("session policy: %w", err)
	}

	appEnv := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	if appEnv == "" {
		appEnv = "development"
	}
	origin := strings.TrimSpace(os.Getenv("APP_ORIGIN"))
	if origin == "" {
		origin = "http://localhost:4321"
	}

	return Config{
		AppEnv:        appEnv,
		AppOrigin:     strings.TrimRight(origin, "/"),
		CookieSecure:  appEnv == "production",
		EncryptionKey: key,
		Session:       policy,
	}, nil
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}
