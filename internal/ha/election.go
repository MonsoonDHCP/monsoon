package ha

import (
	"strings"
	"time"
)

type Role string

const (
	RoleUnknown   Role = "unknown"
	RolePrimary   Role = "primary"
	RoleSecondary Role = "secondary"
)

type PeerState string

const (
	PeerStateUnknown      PeerState = "unknown"
	PeerStateConnected    PeerState = "connected"
	PeerStateDisconnected PeerState = "disconnected"
)

func normalizeMode(mode string) string {
	mode = strings.TrimSpace(strings.ToLower(mode))
	switch mode {
	case "load-sharing":
		return "load-sharing"
	default:
		return "active-passive"
	}
}

// electRole uses stable hostname ordering as a deterministic priority source.
// Lower configured priority wins when both peers are alive. Ties fall back to
// lexical hostname ordering, unless one side is
// explicitly draining to hand primary responsibilities to its peer.
func electRole(localNode, remoteNode, mode string, peerConnected bool, localStepDownUntil, now time.Time, peerDraining bool, localPriority, remotePriority int) Role {
	if peerConnected && !localStepDownUntil.IsZero() && now.Before(localStepDownUntil) {
		return RoleSecondary
	}
	if !peerConnected || strings.TrimSpace(remoteNode) == "" {
		return RolePrimary
	}
	if peerDraining {
		return RolePrimary
	}
	if localPriority <= 0 {
		localPriority = 100
	}
	if remotePriority <= 0 {
		remotePriority = 100
	}
	if localPriority < remotePriority {
		return RolePrimary
	}
	if localPriority > remotePriority {
		return RoleSecondary
	}
	local := strings.ToLower(strings.TrimSpace(localNode))
	remote := strings.ToLower(strings.TrimSpace(remoteNode))
	if local == "" {
		local = "local"
	}
	if remote == "" {
		return RolePrimary
	}
	if local <= remote {
		return RolePrimary
	}
	if normalizeMode(mode) == "load-sharing" {
		return RoleSecondary
	}
	return RoleSecondary
}
