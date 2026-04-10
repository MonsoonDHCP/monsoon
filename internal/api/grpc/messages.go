package grpc

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/monsoondhcp/monsoon/internal/discovery"
	"github.com/monsoondhcp/monsoon/internal/ipam"
	"github.com/monsoondhcp/monsoon/internal/lease"
)

type emptyMessage struct{}

func (emptyMessage) marshalProto() []byte { return nil }

type cidrRequest struct {
	CIDR string
}

func (r cidrRequest) marshalProto() []byte {
	var out []byte
	out = appendString(out, 1, r.CIDR)
	return out
}

func (r *cidrRequest) unmarshalProto(data []byte) error {
	return readProtoFields(data, func(field int, wireType int, raw []byte, _ uint64) error {
		if field == 1 && wireType == wireBytes {
			r.CIDR = strings.TrimSpace(string(raw))
		}
		return nil
	})
}

type subnetMutationRequest struct {
	CIDR       string
	Name       string
	VLAN       int
	Gateway    string
	DNS        []string
	DHCPEnable bool
	PoolStart  string
	PoolEnd    string
	LeaseSec   int64
}

func (r subnetMutationRequest) marshalProto() []byte {
	var out []byte
	out = appendString(out, 1, r.CIDR)
	out = appendString(out, 2, r.Name)
	out = appendInt64(out, 3, int64(r.VLAN))
	out = appendString(out, 4, r.Gateway)
	out = appendStrings(out, 5, r.DNS)
	out = appendBool(out, 6, r.DHCPEnable)
	out = appendString(out, 7, r.PoolStart)
	out = appendString(out, 8, r.PoolEnd)
	out = appendInt64(out, 9, r.LeaseSec)
	return out
}

func (r *subnetMutationRequest) unmarshalProto(data []byte) error {
	return readProtoFields(data, func(field int, wireType int, raw []byte, value uint64) error {
		switch field {
		case 1:
			r.CIDR = strings.TrimSpace(string(raw))
		case 2:
			r.Name = strings.TrimSpace(string(raw))
		case 3:
			r.VLAN = int(value)
		case 4:
			r.Gateway = strings.TrimSpace(string(raw))
		case 5:
			r.DNS = append(r.DNS, strings.TrimSpace(string(raw)))
		case 6:
			r.DHCPEnable = parseBool(value)
		case 7:
			r.PoolStart = strings.TrimSpace(string(raw))
		case 8:
			r.PoolEnd = strings.TrimSpace(string(raw))
		case 9:
			r.LeaseSec = int64(value)
		}
		return nil
	})
}

type listSubnetsResponse struct {
	Items []subnetMessage
}

func (r listSubnetsResponse) marshalProto() []byte {
	var out []byte
	for _, item := range r.Items {
		out = appendMessage(out, 1, item)
	}
	return out
}

type subnetMessage struct {
	CIDR      string
	Name      string
	VLAN      int
	Gateway   string
	DNS       []string
	DHCP      bool
	PoolStart string
	PoolEnd   string
	LeaseSec  int64
	CreatedAt int64
	UpdatedAt int64
}

func newSubnetMessage(item ipam.Subnet) subnetMessage {
	return subnetMessage{
		CIDR:      item.CIDR,
		Name:      item.Name,
		VLAN:      item.VLAN,
		Gateway:   item.Gateway,
		DNS:       append([]string(nil), item.DNS...),
		DHCP:      item.DHCP.Enabled,
		PoolStart: item.DHCP.PoolStart,
		PoolEnd:   item.DHCP.PoolEnd,
		LeaseSec:  item.DHCP.LeaseTimeSec,
		CreatedAt: item.CreatedAt.Unix(),
		UpdatedAt: item.UpdatedAt.Unix(),
	}
}

func (m subnetMessage) marshalProto() []byte {
	var out []byte
	out = appendString(out, 1, m.CIDR)
	out = appendString(out, 2, m.Name)
	out = appendInt64(out, 3, int64(m.VLAN))
	out = appendString(out, 4, m.Gateway)
	out = appendStrings(out, 5, m.DNS)
	out = appendBool(out, 6, m.DHCP)
	out = appendString(out, 7, m.PoolStart)
	out = appendString(out, 8, m.PoolEnd)
	out = appendInt64(out, 9, m.LeaseSec)
	out = appendInt64(out, 10, m.CreatedAt)
	out = appendInt64(out, 11, m.UpdatedAt)
	return out
}

func (m *subnetMessage) unmarshalProto(data []byte) error {
	return readProtoFields(data, func(field int, wireType int, raw []byte, value uint64) error {
		switch field {
		case 1:
			m.CIDR = string(raw)
		case 2:
			m.Name = string(raw)
		case 3:
			m.VLAN = int(value)
		case 4:
			m.Gateway = string(raw)
		case 5:
			m.DNS = append(m.DNS, string(raw))
		case 6:
			m.DHCP = parseBool(value)
		case 7:
			m.PoolStart = string(raw)
		case 8:
			m.PoolEnd = string(raw)
		case 9:
			m.LeaseSec = int64(value)
		case 10:
			m.CreatedAt = int64(value)
		case 11:
			m.UpdatedAt = int64(value)
		}
		return nil
	})
}

type utilizationResponse struct {
	CIDR         string
	Name         string
	ActiveLeases int
	TotalLeases  int
	Utilization  int
}

func (m utilizationResponse) marshalProto() []byte {
	var out []byte
	out = appendString(out, 1, m.CIDR)
	out = appendString(out, 2, m.Name)
	out = appendInt64(out, 3, int64(m.ActiveLeases))
	out = appendInt64(out, 4, int64(m.TotalLeases))
	out = appendInt64(out, 5, int64(m.Utilization))
	return out
}

type listLeasesRequest struct {
	SubnetCIDR string
	State      string
	Query      string
	Limit      int
}

func (r *listLeasesRequest) unmarshalProto(data []byte) error {
	return readProtoFields(data, func(field int, wireType int, raw []byte, value uint64) error {
		switch field {
		case 1:
			r.SubnetCIDR = strings.TrimSpace(string(raw))
		case 2:
			r.State = strings.TrimSpace(string(raw))
		case 3:
			r.Query = strings.TrimSpace(string(raw))
		case 4:
			r.Limit = int(value)
		}
		return nil
	})
}

type ipRequest struct {
	IP string
}

func (r ipRequest) marshalProto() []byte {
	var out []byte
	out = appendString(out, 1, r.IP)
	return out
}

func (r *ipRequest) unmarshalProto(data []byte) error {
	return readProtoFields(data, func(field int, wireType int, raw []byte, _ uint64) error {
		if field == 1 && wireType == wireBytes {
			r.IP = strings.TrimSpace(string(raw))
		}
		return nil
	})
}

type listLeasesResponse struct {
	Items []leaseMessage
}

func (r listLeasesResponse) marshalProto() []byte {
	var out []byte
	for _, item := range r.Items {
		out = appendMessage(out, 1, item)
	}
	return out
}

type leaseMessage struct {
	IP         string
	MAC        string
	Hostname   string
	State      string
	SubnetID   string
	RelayAddr  string
	ExpiryUnix int64
	UpdatedAt  int64
	Duration   int64
}

func newLeaseMessage(item lease.Lease) leaseMessage {
	return leaseMessage{
		IP:         item.IP,
		MAC:        item.MAC,
		Hostname:   item.Hostname,
		State:      string(item.State),
		SubnetID:   item.SubnetID,
		RelayAddr:  item.RelayAddr,
		ExpiryUnix: item.ExpiryTime.Unix(),
		UpdatedAt:  item.UpdatedAt.Unix(),
		Duration:   int64(item.Duration.Seconds()),
	}
}

func (m leaseMessage) marshalProto() []byte {
	var out []byte
	out = appendString(out, 1, m.IP)
	out = appendString(out, 2, m.MAC)
	out = appendString(out, 3, m.Hostname)
	out = appendString(out, 4, m.State)
	out = appendString(out, 5, m.SubnetID)
	out = appendString(out, 6, m.RelayAddr)
	out = appendInt64(out, 7, m.ExpiryUnix)
	out = appendInt64(out, 8, m.UpdatedAt)
	out = appendInt64(out, 9, m.Duration)
	return out
}

func (m *leaseMessage) unmarshalProto(data []byte) error {
	return readProtoFields(data, func(field int, wireType int, raw []byte, value uint64) error {
		switch field {
		case 1:
			m.IP = string(raw)
		case 2:
			m.MAC = string(raw)
		case 3:
			m.Hostname = string(raw)
		case 4:
			m.State = string(raw)
		case 5:
			m.SubnetID = string(raw)
		case 6:
			m.RelayAddr = string(raw)
		case 7:
			m.ExpiryUnix = int64(value)
		case 8:
			m.UpdatedAt = int64(value)
		case 9:
			m.Duration = int64(value)
		}
		return nil
	})
}

type watchLeasesRequest struct {
	SubnetCIDR string
}

func (r watchLeasesRequest) marshalProto() []byte {
	var out []byte
	out = appendString(out, 1, r.SubnetCIDR)
	return out
}

func (r *watchLeasesRequest) unmarshalProto(data []byte) error {
	return readProtoFields(data, func(field int, wireType int, raw []byte, _ uint64) error {
		if field == 1 && wireType == wireBytes {
			r.SubnetCIDR = strings.TrimSpace(string(raw))
		}
		return nil
	})
}

type leaseEventMessage struct {
	Type       string
	IP         string
	OccurredAt int64
	Lease      *leaseMessage
}

func (m leaseEventMessage) marshalProto() []byte {
	var out []byte
	out = appendString(out, 1, m.Type)
	out = appendString(out, 2, m.IP)
	if m.Lease != nil {
		out = appendMessage(out, 3, *m.Lease)
	}
	out = appendInt64(out, 4, m.OccurredAt)
	return out
}

func (m *leaseEventMessage) unmarshalProto(data []byte) error {
	return readProtoFields(data, func(field int, wireType int, raw []byte, value uint64) error {
		switch field {
		case 1:
			m.Type = string(raw)
		case 2:
			m.IP = string(raw)
		case 3:
			item := &leaseMessage{}
			if err := item.unmarshalProto(raw); err != nil {
				return err
			}
			m.Lease = item
		case 4:
			m.OccurredAt = int64(value)
		}
		return nil
	})
}

type searchAddressesRequest struct {
	SubnetCIDR string
	State      string
	Query      string
	Limit      int
}

func (r searchAddressesRequest) marshalProto() []byte {
	var out []byte
	out = appendString(out, 1, r.SubnetCIDR)
	out = appendString(out, 2, r.State)
	out = appendString(out, 3, r.Query)
	out = appendInt64(out, 4, int64(r.Limit))
	return out
}

func (r *searchAddressesRequest) unmarshalProto(data []byte) error {
	return readProtoFields(data, func(field int, wireType int, raw []byte, value uint64) error {
		switch field {
		case 1:
			r.SubnetCIDR = strings.TrimSpace(string(raw))
		case 2:
			r.State = strings.TrimSpace(string(raw))
		case 3:
			r.Query = strings.TrimSpace(string(raw))
		case 4:
			r.Limit = int(value)
		}
		return nil
	})
}

type reserveAddressRequest struct {
	IP         string
	SubnetCIDR string
	MAC        string
	Hostname   string
}

func (r reserveAddressRequest) marshalProto() []byte {
	var out []byte
	out = appendString(out, 1, r.IP)
	out = appendString(out, 2, r.SubnetCIDR)
	out = appendString(out, 3, r.MAC)
	out = appendString(out, 4, r.Hostname)
	return out
}

func (r *reserveAddressRequest) unmarshalProto(data []byte) error {
	return readProtoFields(data, func(field int, wireType int, raw []byte, _ uint64) error {
		switch field {
		case 1:
			r.IP = strings.TrimSpace(string(raw))
		case 2:
			r.SubnetCIDR = strings.TrimSpace(string(raw))
		case 3:
			r.MAC = strings.TrimSpace(string(raw))
		case 4:
			r.Hostname = strings.TrimSpace(string(raw))
		}
		return nil
	})
}

type nextAvailableRequest struct {
	SubnetCIDR string
}

func (r nextAvailableRequest) marshalProto() []byte {
	var out []byte
	out = appendString(out, 1, r.SubnetCIDR)
	return out
}

func (r *nextAvailableRequest) unmarshalProto(data []byte) error {
	return readProtoFields(data, func(field int, wireType int, raw []byte, _ uint64) error {
		if field == 1 && wireType == wireBytes {
			r.SubnetCIDR = strings.TrimSpace(string(raw))
		}
		return nil
	})
}

type searchAddressesResponse struct {
	Items []ipAddressMessage
}

func (r searchAddressesResponse) marshalProto() []byte {
	var out []byte
	for _, item := range r.Items {
		out = appendMessage(out, 1, item)
	}
	return out
}

type ipAddressMessage struct {
	IP         string
	SubnetCIDR string
	State      string
	MAC        string
	Hostname   string
	LeaseState string
	Source     string
	UpdatedAt  int64
}

func newIPAddressMessage(item ipam.AddressRecord) ipAddressMessage {
	return ipAddressMessage{
		IP:         item.IP,
		SubnetCIDR: item.SubnetCIDR,
		State:      string(item.State),
		MAC:        item.MAC,
		Hostname:   item.Hostname,
		LeaseState: item.LeaseState,
		Source:     item.Source,
		UpdatedAt:  item.UpdatedAt.Unix(),
	}
}

func (m ipAddressMessage) marshalProto() []byte {
	var out []byte
	out = appendString(out, 1, m.IP)
	out = appendString(out, 2, m.SubnetCIDR)
	out = appendString(out, 3, m.State)
	out = appendString(out, 4, m.MAC)
	out = appendString(out, 5, m.Hostname)
	out = appendString(out, 6, m.LeaseState)
	out = appendString(out, 7, m.Source)
	out = appendInt64(out, 8, m.UpdatedAt)
	return out
}

func (m *ipAddressMessage) unmarshalProto(data []byte) error {
	return readProtoFields(data, func(field int, wireType int, raw []byte, value uint64) error {
		switch field {
		case 1:
			m.IP = string(raw)
		case 2:
			m.SubnetCIDR = string(raw)
		case 3:
			m.State = string(raw)
		case 4:
			m.MAC = string(raw)
		case 5:
			m.Hostname = string(raw)
		case 6:
			m.LeaseState = string(raw)
		case 7:
			m.Source = string(raw)
		case 8:
			m.UpdatedAt = int64(value)
		}
		return nil
	})
}

type triggerScanRequest struct {
	Reason  string
	Subnets []string
}

func (r triggerScanRequest) marshalProto() []byte {
	var out []byte
	out = appendString(out, 1, r.Reason)
	out = appendStrings(out, 2, r.Subnets)
	return out
}

func (r *triggerScanRequest) unmarshalProto(data []byte) error {
	return readProtoFields(data, func(field int, wireType int, raw []byte, _ uint64) error {
		switch field {
		case 1:
			r.Reason = strings.TrimSpace(string(raw))
		case 2:
			r.Subnets = append(r.Subnets, strings.TrimSpace(string(raw)))
		}
		return nil
	})
}

type scanResponse struct {
	Status      string
	ScanID      string
	EstimatedIn string
}

func (m scanResponse) marshalProto() []byte {
	var out []byte
	out = appendString(out, 1, m.Status)
	out = appendString(out, 2, m.ScanID)
	out = appendString(out, 3, m.EstimatedIn)
	return out
}

type getConflictsRequest struct{}

func (r getConflictsRequest) marshalProto() []byte { return nil }

func (r *getConflictsRequest) unmarshalProto(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return readProtoFields(data, func(int, int, []byte, uint64) error { return nil })
}

type conflictsResponse struct {
	Items []conflictMessage
}

func (r conflictsResponse) marshalProto() []byte {
	var out []byte
	for _, item := range r.Items {
		out = appendMessage(out, 1, item)
	}
	return out
}

type conflictMessage struct {
	IP       string
	MACs     []string
	Severity string
	Note     string
}

func newConflictMessage(item discovery.Conflict) conflictMessage {
	return conflictMessage{
		IP:       item.IP,
		MACs:     append([]string(nil), item.MACs...),
		Severity: item.Severity,
		Note:     item.Note,
	}
}

func (m conflictMessage) marshalProto() []byte {
	var out []byte
	out = appendString(out, 1, m.IP)
	out = appendStrings(out, 2, m.MACs)
	out = appendString(out, 3, m.Severity)
	out = appendString(out, 4, m.Note)
	return out
}

type watchDiscoveryRequest struct{}

func (r watchDiscoveryRequest) marshalProto() []byte { return nil }

func (r *watchDiscoveryRequest) unmarshalProto(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return readProtoFields(data, func(int, int, []byte, uint64) error { return nil })
}

type discoveryEventMessage struct {
	Type       string
	ScanID     string
	Subnet     string
	IP         string
	Found      int
	MACs       []string
	OccurredAt int64
	Note       string
}

func (m discoveryEventMessage) marshalProto() []byte {
	var out []byte
	out = appendString(out, 1, m.Type)
	out = appendString(out, 2, m.ScanID)
	out = appendString(out, 3, m.Subnet)
	out = appendString(out, 4, m.IP)
	out = appendInt64(out, 5, int64(m.Found))
	out = appendStrings(out, 6, m.MACs)
	out = appendInt64(out, 7, m.OccurredAt)
	out = appendString(out, 8, m.Note)
	return out
}

func (m *discoveryEventMessage) unmarshalProto(data []byte) error {
	return readProtoFields(data, func(field int, wireType int, raw []byte, value uint64) error {
		switch field {
		case 1:
			m.Type = string(raw)
		case 2:
			m.ScanID = string(raw)
		case 3:
			m.Subnet = string(raw)
		case 4:
			m.IP = string(raw)
		case 5:
			m.Found = int(value)
		case 6:
			m.MACs = append(m.MACs, string(raw))
		case 7:
			m.OccurredAt = int64(value)
		case 8:
			m.Note = string(raw)
		}
		return nil
	})
}

func decodeEmpty(_ []byte) (any, error) {
	return emptyMessage{}, nil
}

func decodeCIDRRequest(data []byte) (any, error) {
	var req cidrRequest
	return req, req.unmarshalProto(data)
}

func decodeSubnetMutationRequest(data []byte) (any, error) {
	var req subnetMutationRequest
	return req, req.unmarshalProto(data)
}

func decodeListLeasesRequest(data []byte) (any, error) {
	var req listLeasesRequest
	return req, req.unmarshalProto(data)
}

func decodeIPRequest(data []byte) (any, error) {
	var req ipRequest
	return req, req.unmarshalProto(data)
}

func decodeWatchLeasesRequest(data []byte) (any, error) {
	var req watchLeasesRequest
	return req, req.unmarshalProto(data)
}

func decodeSearchAddressesRequest(data []byte) (any, error) {
	var req searchAddressesRequest
	return req, req.unmarshalProto(data)
}

func decodeReserveAddressRequest(data []byte) (any, error) {
	var req reserveAddressRequest
	return req, req.unmarshalProto(data)
}

func decodeNextAvailableRequest(data []byte) (any, error) {
	var req nextAvailableRequest
	return req, req.unmarshalProto(data)
}

func decodeTriggerScanRequest(data []byte) (any, error) {
	var req triggerScanRequest
	return req, req.unmarshalProto(data)
}

func decodeGetConflictsRequest(data []byte) (any, error) {
	var req getConflictsRequest
	return req, req.unmarshalProto(data)
}

func decodeWatchDiscoveryRequest(data []byte) (any, error) {
	var req watchDiscoveryRequest
	return req, req.unmarshalProto(data)
}

func mustString(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func mustInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	default:
		return 0
	}
}

func mustStrings(value any) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s := strings.TrimSpace(mustString(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func unixNow() int64 {
	return time.Now().UTC().Unix()
}
