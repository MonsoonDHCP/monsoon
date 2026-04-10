package ipam

import "time"

type Subnet struct {
	CIDR      string    `json:"cidr"`
	Name      string    `json:"name"`
	VLAN      int       `json:"vlan"`
	Gateway   string    `json:"gateway"`
	DNS       []string  `json:"dns"`
	DHCP      DHCPPool  `json:"dhcp"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type DHCPPool struct {
	Enabled      bool   `json:"enabled"`
	PoolStart    string `json:"pool_start"`
	PoolEnd      string `json:"pool_end"`
	LeaseTimeSec int64  `json:"lease_time_sec"`
}

type SubnetSummary struct {
	CIDR         string `json:"cidr"`
	Name         string `json:"name"`
	VLAN         int    `json:"vlan"`
	ActiveLeases int    `json:"active_leases"`
	TotalLeases  int    `json:"total_leases"`
	Utilization  int    `json:"utilization"`
}

type UpsertSubnetInput struct {
	CIDR       string   `json:"cidr"`
	Name       string   `json:"name"`
	VLAN       int      `json:"vlan"`
	Gateway    string   `json:"gateway"`
	DNS        []string `json:"dns"`
	DHCPEnable bool     `json:"dhcp_enabled"`
	PoolStart  string   `json:"pool_start"`
	PoolEnd    string   `json:"pool_end"`
	LeaseSec   int64    `json:"lease_time_sec"`
}

type IPState string

const (
	IPStateAvailable   IPState = "available"
	IPStateDHCP        IPState = "dhcp"
	IPStateReserved    IPState = "reserved"
	IPStateConflict    IPState = "conflict"
	IPStateQuarantined IPState = "quarantined"
)

type AddressRecord struct {
	IP         string    `json:"ip"`
	SubnetCIDR string    `json:"subnet_cidr,omitempty"`
	State      IPState   `json:"state"`
	MAC        string    `json:"mac,omitempty"`
	Hostname   string    `json:"hostname,omitempty"`
	LeaseState string    `json:"lease_state,omitempty"`
	Source     string    `json:"source,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}

type UpsertAddressInput struct {
	IP         string    `json:"ip"`
	SubnetCIDR string    `json:"subnet_cidr"`
	State      IPState   `json:"state"`
	MAC        string    `json:"mac"`
	Hostname   string    `json:"hostname"`
	Source     string    `json:"source"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type AddressFilter struct {
	SubnetCIDR string
	State      IPState
	Query      string
	Limit      int
}

type Reservation struct {
	MAC        string    `json:"mac"`
	IP         string    `json:"ip"`
	Hostname   string    `json:"hostname,omitempty"`
	SubnetCIDR string    `json:"subnet_cidr"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type UpsertReservationInput struct {
	MAC        string `json:"mac"`
	IP         string `json:"ip"`
	Hostname   string `json:"hostname"`
	SubnetCIDR string `json:"subnet_cidr"`
}
