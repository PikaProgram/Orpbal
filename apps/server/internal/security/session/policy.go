package session

import (
	"errors"
	"time"
)

// Policy holds the documented session lifetimes.
type Policy struct {
	IdleTimeout      time.Duration
	AbsoluteTimeout  time.Duration
	RotationInterval time.Duration
	GracePeriod      time.Duration
}

// Default returns the documented Milestone 1 policy.
func Default() Policy {
	return Policy{
		IdleTimeout:      30 * time.Minute,
		AbsoluteTimeout:  8 * time.Hour,
		RotationInterval: 15 * time.Minute,
		GracePeriod:      30 * time.Second,
	}
}

// Validate rejects lifetimes that break the rotation model.
func (p Policy) Validate() error {
	switch {
	case p.IdleTimeout <= 0:
		return errors.New("idle timeout must be positive")
	case p.AbsoluteTimeout <= 0:
		return errors.New("absolute timeout must be positive")
	case p.RotationInterval <= 0:
		return errors.New("rotation interval must be positive")
	case p.GracePeriod <= 0:
		return errors.New("grace period must be positive")
	case p.GracePeriod >= p.RotationInterval:
		return errors.New("grace period must be shorter than the rotation interval")
	case p.IdleTimeout > p.AbsoluteTimeout:
		return errors.New("idle timeout must not exceed the absolute timeout")
	}
	return nil
}

// IdleExpiry is the deadline for the next activity.
func (p Policy) IdleExpiry(now time.Time) time.Time {
	return now.Add(p.IdleTimeout)
}

// AbsoluteExpiry is the deadline that no activity extends.
func (p Policy) AbsoluteExpiry(now time.Time) time.Time {
	return now.Add(p.AbsoluteTimeout)
}

// GraceUntil is how long a rotated-out token stays accepted.
func (p Policy) GraceUntil(now time.Time) time.Time {
	return now.Add(p.GracePeriod)
}

// Expired reports whether either deadline has been reached.
func (p Policy) Expired(now, idleExpiresAt, absoluteExpiresAt time.Time) bool {
	return !now.Before(idleExpiresAt) || !now.Before(absoluteExpiresAt)
}

// RotationDue reports whether the session token should be replaced.
func (p Policy) RotationDue(now, rotatedAt time.Time) bool {
	return !now.Before(rotatedAt.Add(p.RotationInterval))
}
