package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"Orpbal/ent"
	"Orpbal/ent/platformuser"
	"Orpbal/ent/session"
	"Orpbal/internal/security/csrf"
	"Orpbal/internal/security/password"
	sessionpolicy "Orpbal/internal/security/session"
	tokenpkg "Orpbal/internal/security/token"
)

// CookieName is the opaque session cookie.
const CookieName = "orpbal_session"

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidSession     = errors.New("invalid session")
	ErrLastAdmin          = errors.New("last active platform admin")
)

// Session is an authenticated session plus values the HTTP layer needs.
type Session struct {
	User      *ent.PlatformUser
	Row       *ent.Session
	RawToken  string
	CSRFToken string
}

// Service owns session creation, validation, rotation and revocation.
type Service struct {
	client    *ent.Client
	policy    sessionpolicy.Policy
	csrf      *csrf.Manager
	dummyHash string
	now       func() time.Time
}

// NewService builds a session service and a dummy hash for timing-equal login.
func NewService(client *ent.Client, policy sessionpolicy.Policy, csrfManager *csrf.Manager) (*Service, error) {
	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("session policy: %w", err)
	}
	dummy, err := password.Hash("orpbal-dummy-password-9!")
	if err != nil {
		return nil, fmt.Errorf("create dummy hash: %w", err)
	}
	return &Service{
		client:    client,
		policy:    policy,
		csrf:      csrfManager,
		dummyHash: dummy,
		now:       time.Now,
	}, nil
}

// Login verifies credentials and creates a fresh session.
// Any session presented in oldToken is revoked first.
func (s *Service) Login(ctx context.Context, username, plainPassword, oldToken string) (*Session, error) {
	user, err := s.client.PlatformUser.Query().
		Where(platformuser.UsernameEQ(username)).
		Only(ctx)
	if ent.IsNotFound(err) {
		_ = password.Verify(plainPassword, s.dummyHash)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, fmt.Errorf("query user: %w", err)
	}
	if !user.IsActive || !password.Verify(plainPassword, user.PasswordHash) {
		return nil, ErrInvalidCredentials
	}

	if oldToken != "" {
		if err := s.revokeByRaw(ctx, oldToken); err != nil {
			return nil, err
		}
	}

	rawToken, err := tokenpkg.New()
	if err != nil {
		return nil, err
	}
	csrfToken, csrfHash, err := s.csrf.Generate()
	if err != nil {
		return nil, err
	}

	now := s.now()
	row, err := s.client.Session.Create().
		SetUserID(user.ID).
		SetTokenHash(tokenpkg.Hash(rawToken)).
		SetCsrfHash(csrfHash).
		SetLastSeenAt(now).
		SetRotatedAt(now).
		SetIdleExpiresAt(s.policy.IdleExpiry(now)).
		SetAbsoluteExpiresAt(s.policy.AbsoluteExpiry(now)).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	return &Session{User: user, Row: row, RawToken: rawToken, CSRFToken: csrfToken}, nil
}

// Authenticate validates a session cookie, applies idle expiry, rotates when
// requested and due, and returns a new raw token when rotation happened.
func (s *Service) Authenticate(ctx context.Context, rawToken string, rotate bool) (*Session, error) {
	if rawToken == "" {
		return nil, ErrInvalidSession
	}
	now := s.now()
	hash := tokenpkg.Hash(rawToken)

	row, err := s.client.Session.Query().
		Where(session.Or(
			session.TokenHashEQ(hash),
			session.And(
				session.PreviousTokenHashEQ(hash),
				session.PreviousValidUntilGT(now),
			),
		)).
		WithUser().
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrInvalidSession
	}
	if err != nil {
		return nil, fmt.Errorf("query session: %w", err)
	}

	user := row.Edges.User
	if user == nil || !user.IsActive {
		_ = s.client.Session.DeleteOneID(row.ID).Exec(ctx)
		return nil, ErrInvalidSession
	}
	if s.policy.Expired(now, row.IdleExpiresAt, row.AbsoluteExpiresAt) {
		_ = s.client.Session.DeleteOneID(row.ID).Exec(ctx)
		return nil, ErrInvalidSession
	}

	newIdle := s.policy.IdleExpiry(now)
	isCurrent := subtle.ConstantTimeCompare([]byte(row.TokenHash), []byte(hash)) == 1
	newRawToken := ""

	if rotate && isCurrent && s.policy.RotationDue(now, row.RotatedAt) {
		candidate, err := tokenpkg.New()
		if err != nil {
			return nil, err
		}
		newHash := tokenpkg.Hash(candidate)
		graceUntil := s.policy.GraceUntil(now)

		count, err := s.client.Session.Update().
			Where(session.ID(row.ID), session.TokenHashEQ(row.TokenHash)).
			SetTokenHash(newHash).
			SetPreviousTokenHash(row.TokenHash).
			SetPreviousValidUntil(graceUntil).
			SetRotatedAt(now).
			SetLastSeenAt(now).
			SetIdleExpiresAt(newIdle).
			Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("rotate session: %w", err)
		}
		if count == 1 {
			newRawToken = candidate
			row.PreviousTokenHash = row.TokenHash
			row.PreviousValidUntil = graceUntil
			row.TokenHash = newHash
			row.RotatedAt = now
		} else {
			// A concurrent request rotated first; accept the old token only
			// while its grace window is still open.
			row, err = s.client.Session.Query().
				Where(
					session.PreviousTokenHashEQ(hash),
					session.PreviousValidUntilGT(now),
				).
				WithUser().
				Only(ctx)
			if err != nil {
				return nil, ErrInvalidSession
			}
			user = row.Edges.User
			if user == nil || !user.IsActive {
				return nil, ErrInvalidSession
			}
		}
	} else {
		if err := s.client.Session.UpdateOneID(row.ID).
			SetLastSeenAt(now).
			SetIdleExpiresAt(newIdle).
			Exec(ctx); err != nil {
			if ent.IsNotFound(err) {
				return nil, ErrInvalidSession
			}
			return nil, fmt.Errorf("touch session: %w", err)
		}
	}

	row.LastSeenAt = now
	row.IdleExpiresAt = newIdle
	return &Session{
		User:      user,
		Row:       row,
		RawToken:  newRawToken,
		CSRFToken: s.csrf.Token(row.CsrfHash),
	}, nil
}

// Logout deletes one session.
func (s *Service) Logout(ctx context.Context, sessionID int) error {
	if err := s.client.Session.DeleteOneID(sessionID).Exec(ctx); err != nil && !ent.IsNotFound(err) {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// ChangePassword verifies the current password, updates it, revokes every
// other session and rotates the current session.
func (s *Service) ChangePassword(ctx context.Context, current *ent.Session, plainCurrent, plainNext string) (string, error) {
	user, err := s.client.PlatformUser.Get(ctx, current.UserID)
	if err != nil {
		if ent.IsNotFound(err) {
			return "", ErrInvalidSession
		}
		return "", fmt.Errorf("query user: %w", err)
	}
	if !password.Verify(plainCurrent, user.PasswordHash) {
		return "", ErrInvalidCredentials
	}
	if err := password.ValidatePassword(plainNext, user.Username); err != nil {
		return "", err
	}
	hash, err := password.Hash(plainNext)
	if err != nil {
		return "", err
	}

	newRawToken, err := tokenpkg.New()
	if err != nil {
		return "", err
	}
	now := s.now()

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	if err := tx.PlatformUser.UpdateOneID(user.ID).SetPasswordHash(hash).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return "", fmt.Errorf("update password: %w", err)
	}
	if _, err := tx.Session.Delete().
		Where(session.UserID(user.ID), session.IDNEQ(current.ID)).
		Exec(ctx); err != nil {
		_ = tx.Rollback()
		return "", fmt.Errorf("revoke other sessions: %w", err)
	}
	if err := tx.Session.UpdateOneID(current.ID).
		SetTokenHash(tokenpkg.Hash(newRawToken)).
		SetPreviousTokenHash(current.TokenHash).
		SetPreviousValidUntil(s.policy.GraceUntil(now)).
		SetRotatedAt(now).
		SetLastSeenAt(now).
		SetIdleExpiresAt(s.policy.IdleExpiry(now)).
		Exec(ctx); err != nil {
		_ = tx.Rollback()
		return "", fmt.Errorf("rotate session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit password change: %w", err)
	}
	return newRawToken, nil
}

// RevokeUserSessions deletes every session belonging to a user.
func (s *Service) RevokeUserSessions(ctx context.Context, userID int) error {
	if _, err := s.client.Session.Delete().Where(session.UserID(userID)).Exec(ctx); err != nil {
		return fmt.Errorf("revoke user sessions: %w", err)
	}
	return nil
}

func (s *Service) revokeByRaw(ctx context.Context, rawToken string) error {
	hash := tokenpkg.Hash(rawToken)
	if _, err := s.client.Session.Delete().
		Where(session.Or(
			session.TokenHashEQ(hash),
			session.PreviousTokenHashEQ(hash),
		)).
		Exec(ctx); err != nil {
		return fmt.Errorf("revoke previous session: %w", err)
	}
	return nil
}
