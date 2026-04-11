package lease

import (
	"testing"
	"time"
)

func TestNormalizeDefaultsAndClone(t *testing.T) {
	now := time.Now().UTC()
	l := Lease{
		IP:        "10.0.0.20",
		ClientID:  []byte{1, 2, 3},
		RelayInfo: []byte{4, 5, 6},
		Tags:      map[string]string{"role": "printer"},
	}

	l.NormalizeDefaults(now, time.Hour)
	if l.StartTime.IsZero() || l.Duration != time.Hour || l.T1 != 30*time.Minute || l.T2 != 52*time.Minute+30*time.Second {
		t.Fatalf("unexpected normalized timings: %+v", l)
	}
	if l.ExpiryTime != l.StartTime.Add(l.Duration) {
		t.Fatalf("expiry = %v, want %v", l.ExpiryTime, l.StartTime.Add(l.Duration))
	}
	if l.CreatedAt.IsZero() || l.LastSeen.IsZero() || l.UpdatedAt.IsZero() {
		t.Fatalf("expected timestamps to be set: %+v", l)
	}

	clone := l.Clone()
	clone.ClientID[0] = 99
	clone.RelayInfo[0] = 88
	clone.Tags["role"] = "camera"
	if l.ClientID[0] == 99 || l.RelayInfo[0] == 88 || l.Tags["role"] != "printer" {
		t.Fatalf("clone must deep copy slices and maps")
	}
}
