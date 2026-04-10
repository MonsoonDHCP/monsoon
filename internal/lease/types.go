package lease

import "time"

type LeaseState string

const (
	StateFree        LeaseState = "free"
	StateOffered     LeaseState = "offered"
	StateBound       LeaseState = "bound"
	StateRenewing    LeaseState = "renewing"
	StateReleased    LeaseState = "released"
	StateDeclined    LeaseState = "declined"
	StateQuarantined LeaseState = "quarantined"
	StateExpired     LeaseState = "expired"
)

type Lease struct {
	IP              string            `json:"ip"`
	MAC             string            `json:"mac"`
	ClientID        []byte            `json:"client_id,omitempty"`
	Hostname        string            `json:"hostname,omitempty"`
	State           LeaseState        `json:"state"`
	StartTime       time.Time         `json:"start_time"`
	Duration        time.Duration     `json:"duration"`
	T1              time.Duration     `json:"t1"`
	T2              time.Duration     `json:"t2"`
	ExpiryTime      time.Time         `json:"expiry_time"`
	QuarantineUntil time.Time         `json:"quarantine_until,omitempty"`
	SubnetID        string            `json:"subnet_id,omitempty"`
	RelayAddr       string            `json:"relay_addr,omitempty"`
	RelayInfo       []byte            `json:"relay_info,omitempty"`
	CircuitID       string            `json:"circuit_id,omitempty"`
	RemoteID        string            `json:"remote_id,omitempty"`
	VendorClass     string            `json:"vendor_class,omitempty"`
	UserClass       string            `json:"user_class,omitempty"`
	LastSeen        time.Time         `json:"last_seen"`
	DDNSForward     string            `json:"ddns_forward,omitempty"`
	DDNSReverse     string            `json:"ddns_reverse,omitempty"`
	Tags            map[string]string `json:"tags,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

func (l *Lease) NormalizeDefaults(now time.Time, defaultDuration time.Duration) {
	if l.StartTime.IsZero() {
		l.StartTime = now
	}
	if l.Duration <= 0 {
		l.Duration = defaultDuration
	}
	if l.T1 <= 0 {
		l.T1 = l.Duration / 2
	}
	if l.T2 <= 0 {
		l.T2 = (l.Duration * 7) / 8
	}
	if l.ExpiryTime.IsZero() {
		l.ExpiryTime = l.StartTime.Add(l.Duration)
	}
	if l.CreatedAt.IsZero() {
		l.CreatedAt = now
	}
	if l.LastSeen.IsZero() {
		l.LastSeen = now
	}
	l.UpdatedAt = now
}

func (l Lease) Clone() Lease {
	out := l
	if l.ClientID != nil {
		out.ClientID = append([]byte(nil), l.ClientID...)
	}
	if l.RelayInfo != nil {
		out.RelayInfo = append([]byte(nil), l.RelayInfo...)
	}
	if l.Tags != nil {
		out.Tags = make(map[string]string, len(l.Tags))
		for k, v := range l.Tags {
			out.Tags[k] = v
		}
	}
	return out
}
