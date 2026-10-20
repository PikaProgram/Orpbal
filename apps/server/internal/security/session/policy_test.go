package session

import (
	"testing"
	"time"
)

func TestDefaultPolicyIsValid(t *testing.T) {
	p := Default()
	if err := p.Validate(); err != nil {
		t.Fatalf("Default().Validate() error = %v", err)
	}
	if p.IdleTimeout != 30*time.Minute || p.AbsoluteTimeout != 8*time.Hour ||
		p.RotationInterval != 15*time.Minute || p.GracePeriod != 30*time.Second {
		t.Fatalf("Default() = %+v, want 30m/8h/15m/30s", p)
	}
}

func TestPolicyValidate(t *testing.T) {
	valid := Default()
	tests := []struct {
		name   string
		mutate func(*Policy)
	}{
		{"zero idle", func(p *Policy) { p.IdleTimeout = 0 }},
		{"zero absolute", func(p *Policy) { p.AbsoluteTimeout = 0 }},
		{"zero rotation", func(p *Policy) { p.RotationInterval = 0 }},
		{"zero grace", func(p *Policy) { p.GracePeriod = 0 }},
		{"grace equals rotation", func(p *Policy) { p.GracePeriod = p.RotationInterval }},
		{"idle exceeds absolute", func(p *Policy) { p.IdleTimeout = p.AbsoluteTimeout + time.Minute }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := valid
			tt.mutate(&p)
			if err := p.Validate(); err == nil {
				t.Fatalf("Validate() accepted %+v", p)
			}
		})
	}
}

func TestExpiredAtBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	p := Default()
	idle := p.IdleExpiry(now)
	absolute := p.AbsoluteExpiry(now)

	if p.Expired(now, idle, absolute) {
		t.Fatal("Expired() reported a fresh session as expired")
	}
	if !p.Expired(idle, idle, absolute) {
		t.Fatal("Expired() ignored the idle deadline")
	}
	if !p.Expired(absolute, idle, absolute) {
		t.Fatal("Expired() ignored the absolute deadline")
	}
	if !p.Expired(absolute.Add(time.Second), idle, absolute) {
		t.Fatal("Expired() ignored a deadline in the past")
	}
}

func TestRotationDueAtBoundary(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	p := Default()
	before := now.Add(p.RotationInterval - time.Nanosecond)
	at := now.Add(p.RotationInterval)

	if p.RotationDue(before, now) {
		t.Fatal("RotationDue() fired before the interval")
	}
	if !p.RotationDue(at, now) {
		t.Fatal("RotationDue() did not fire at the interval")
	}
	if p.RotationDue(now.Add(-time.Second), now) {
		t.Fatal("RotationDue() fired for a clock in the past")
	}
}

func TestGraceUntil(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	p := Default()
	if got := p.GraceUntil(now); !got.Equal(now.Add(30 * time.Second)) {
		t.Fatalf("GraceUntil() = %v, want %v", got, now.Add(30*time.Second))
	}
}
