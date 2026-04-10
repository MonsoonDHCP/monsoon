package lease

import "testing"

func TestStateTransitions(t *testing.T) {
	l := Lease{State: StateFree}
	next, err := Transition(l, StateOffered)
	if err != nil {
		t.Fatalf("transition failed: %v", err)
	}
	if next.State != StateOffered {
		t.Fatalf("state mismatch")
	}
	if _, err := Transition(next, StateDeclined); err == nil {
		t.Fatalf("expected invalid transition error")
	}
}
