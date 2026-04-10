package lease

import "fmt"

var validTransitions = map[LeaseState]map[LeaseState]struct{}{
	StateFree: {
		StateOffered: {},
		StateBound:   {},
	},
	StateOffered: {
		StateBound:   {},
		StateFree:    {},
		StateExpired: {},
	},
	StateBound: {
		StateRenewing: {},
		StateReleased: {},
		StateDeclined: {},
		StateExpired:  {},
	},
	StateRenewing: {
		StateBound:   {},
		StateExpired: {},
	},
	StateReleased: {
		StateFree:        {},
		StateQuarantined: {},
	},
	StateDeclined: {
		StateQuarantined: {},
	},
	StateExpired: {
		StateQuarantined: {},
		StateFree:        {},
	},
	StateQuarantined: {
		StateFree: {},
	},
}

func CanTransition(from, to LeaseState) bool {
	if from == to {
		return true
	}
	next, ok := validTransitions[from]
	if !ok {
		return false
	}
	_, ok = next[to]
	return ok
}

func Transition(l Lease, to LeaseState) (Lease, error) {
	if !CanTransition(l.State, to) {
		return Lease{}, fmt.Errorf("invalid transition %s -> %s", l.State, to)
	}
	l.State = to
	return l, nil
}
