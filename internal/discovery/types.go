package discovery

import "time"

type Status struct {
	SensorOnline      bool      `json:"sensor_online"`
	LastScanAt        time.Time `json:"last_scan_at,omitempty"`
	RogueDetected     bool      `json:"rogue_detected"`
	ActiveConflicts   int       `json:"active_conflicts"`
	NextScheduledScan time.Time `json:"next_scheduled_scan,omitempty"`
	Scanning          bool      `json:"scanning"`
	LatestScanID      string    `json:"latest_scan_id,omitempty"`
}

type ScanRequest struct {
	Reason  string   `json:"reason,omitempty"`
	Subnets []string `json:"subnets,omitempty"`
}

type ScanResult struct {
	ScanID       string         `json:"scan_id"`
	Status       string         `json:"status"`
	Reason       string         `json:"reason,omitempty"`
	Subnets      []string       `json:"subnets,omitempty"`
	StartedAt    time.Time      `json:"started_at"`
	CompletedAt  time.Time      `json:"completed_at,omitempty"`
	DurationMS   int64          `json:"duration_ms"`
	TotalHosts   int            `json:"total_hosts"`
	NewHosts     int            `json:"new_hosts"`
	KnownHosts   int            `json:"known_hosts"`
	MissingHosts int            `json:"missing_hosts"`
	ChangedHosts int            `json:"changed_hosts"`
	Conflicts    []Conflict     `json:"conflicts,omitempty"`
	RogueServers []RogueServer  `json:"rogue_servers,omitempty"`
	Hosts        []ObservedHost `json:"hosts,omitempty"`
}

type Conflict struct {
	IP       string   `json:"ip"`
	MACs     []string `json:"macs"`
	Severity string   `json:"severity"`
	Note     string   `json:"note,omitempty"`
}

type RogueServer struct {
	IP       string    `json:"ip"`
	MAC      string    `json:"mac,omitempty"`
	Source   string    `json:"source,omitempty"`
	Detected time.Time `json:"detected"`
}

type ObservedHost struct {
	IP       string    `json:"ip"`
	MAC      string    `json:"mac,omitempty"`
	Hostname string    `json:"hostname,omitempty"`
	Subnet   string    `json:"subnet,omitempty"`
	State    string    `json:"state"`
	SeenAt   time.Time `json:"seen_at"`
}
