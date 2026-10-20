package bootstrap

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/rs/zerolog"

	"Orpbal/ent"
	"Orpbal/ent/platformuser"
	"Orpbal/internal/security/password"
)

func EnsureAdmin(ctx context.Context, client *ent.Client, log zerolog.Logger) error {
	exists, err := client.PlatformUser.Query().
		Where(platformuser.IsPlatformAdmin(true), platformuser.IsActive(true)).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check platform admin: %w", err)
	}
	if exists {
		return nil
	}

	username := strings.TrimSpace(os.Getenv("ORPBAL_BOOTSTRAP_ADMIN_USERNAME"))
	plain := os.Getenv("ORPBAL_BOOTSTRAP_ADMIN_PASSWORD")
	if username == "" || plain == "" {
		return fmt.Errorf("no active platform admin: set ORPBAL_BOOTSTRAP_ADMIN_USERNAME and ORPBAL_BOOTSTRAP_ADMIN_PASSWORD")
	}
	if err := password.ValidateUsername(username); err != nil {
		return fmt.Errorf("bootstrap admin username: %w", err)
	}
	if err := password.ValidatePassword(plain, username); err != nil {
		return fmt.Errorf("bootstrap admin password: %w", err)
	}

	hash, err := password.Hash(plain)
	if err != nil {
		return fmt.Errorf("hash bootstrap password: %w", err)
	}

	created, err := client.PlatformUser.Create().
		SetUsername(username).
		SetPasswordHash(hash).
		SetIsPlatformAdmin(true).
		SetIsActive(true).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("create bootstrap admin: %w", err)
	}

	log.Info().Str("username", created.Username).Msg("Bootstrap platform admin created")
	return nil
}
