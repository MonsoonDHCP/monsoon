package ha

import (
	"testing"
	"time"
)

func TestElectRoleUsesPriorityBeforeHostname(t *testing.T) {
	now := time.Now().UTC()

	if got := electRole("zeta", "alpha", "active-passive", true, time.Time{}, now, false, 10, 20); got != RolePrimary {
		t.Fatalf("expected lower priority node to win, got %s", got)
	}
	if got := electRole("alpha", "beta", "active-passive", true, time.Time{}, now, false, 200, 50); got != RoleSecondary {
		t.Fatalf("expected higher priority value to lose, got %s", got)
	}
	if got := electRole("alpha", "beta", "active-passive", true, time.Time{}, now, false, 100, 100); got != RolePrimary {
		t.Fatalf("expected hostname tie-break to keep alpha primary, got %s", got)
	}
}

func TestElectRoleHonorsManualDraining(t *testing.T) {
	now := time.Now().UTC()
	if got := electRole("alpha", "beta", "active-passive", true, now.Add(time.Minute), now, false, 10, 20); got != RoleSecondary {
		t.Fatalf("expected draining node to step down, got %s", got)
	}
	if got := electRole("beta", "alpha", "active-passive", true, time.Time{}, now, true, 20, 10); got != RolePrimary {
		t.Fatalf("expected peer draining signal to promote local node, got %s", got)
	}
}
