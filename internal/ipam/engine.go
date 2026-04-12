package ipam

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/monsoondhcp/monsoon/internal/config"
	"github.com/monsoondhcp/monsoon/internal/lease"
	"github.com/monsoondhcp/monsoon/internal/storage"
)

const (
	treeSubnets      = "subnets"
	treeAddresses    = "addresses"
	treeReservations = "reservations"
)

type Engine struct {
	store      *storage.Engine
	leaseStore lease.Store
}

func NewEngine(store *storage.Engine, leaseStore lease.Store) *Engine {
	return &Engine{store: store, leaseStore: leaseStore}
}

func (e *Engine) SeedFromConfig(ctx context.Context, subnets []config.SubnetConfig) error {
	for _, s := range subnets {
		if strings.TrimSpace(s.CIDR) == "" {
			continue
		}
		if _, err := e.GetSubnet(ctx, s.CIDR); err == nil {
			continue
		}
		leaseSec := int64(s.DHCP.LeaseTime.Duration.Seconds())
		if leaseSec <= 0 {
			leaseSec = int64((12 * time.Hour).Seconds())
		}
		if _, err := e.UpsertSubnet(ctx, UpsertSubnetInput{
			CIDR:       s.CIDR,
			Name:       s.Name,
			VLAN:       s.VLAN,
			Gateway:    s.Gateway,
			DNS:        append([]string(nil), s.DNS...),
			DHCPEnable: s.DHCP.Enabled,
			PoolStart:  s.DHCP.PoolStart,
			PoolEnd:    s.DHCP.PoolEnd,
			LeaseSec:   leaseSec,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) UpsertSubnet(ctx context.Context, in UpsertSubnetInput) (Subnet, error) {
	normalized, err := e.ValidateSubnet(ctx, in)
	if err != nil {
		return Subnet{}, err
	}

	now := time.Now().UTC()
	old, err := e.GetSubnet(ctx, normalized.CIDR)
	if err == nil {
		normalized.CreatedAt = old.CreatedAt
	} else {
		normalized.CreatedAt = now
	}
	normalized.UpdatedAt = now

	raw, err := json.Marshal(normalized)
	if err != nil {
		return Subnet{}, err
	}
	if err := e.store.Put(treeSubnets, []byte(normalized.CIDR), raw); err != nil {
		return Subnet{}, err
	}
	return normalized, nil
}

func (e *Engine) ValidateSubnet(ctx context.Context, in UpsertSubnetInput) (Subnet, error) {
	normalized, err := e.validateSubnet(in)
	if err != nil {
		return Subnet{}, err
	}

	current, _ := e.ListSubnets(ctx)
	for _, s := range current {
		if s.CIDR == normalized.CIDR {
			continue
		}
		a, _ := netip.ParsePrefix(s.CIDR)
		b, _ := netip.ParsePrefix(normalized.CIDR)
		if Overlaps(a, b) {
			return Subnet{}, fmt.Errorf("subnet %s overlaps with %s", normalized.CIDR, s.CIDR)
		}
	}
	return normalized, nil
}

func (e *Engine) GetSubnet(_ context.Context, cidr string) (Subnet, error) {
	raw, err := e.store.Get(treeSubnets, []byte(strings.TrimSpace(cidr)))
	if err != nil {
		return Subnet{}, err
	}
	var out Subnet
	if err := json.Unmarshal(raw, &out); err != nil {
		return Subnet{}, err
	}
	return out, nil
}

func (e *Engine) DeleteSubnet(_ context.Context, cidr string) error {
	return e.store.Delete(treeSubnets, []byte(strings.TrimSpace(cidr)))
}

func (e *Engine) ListSubnets(_ context.Context) ([]Subnet, error) {
	out := make([]Subnet, 0, 64)
	err := e.store.Iterate(treeSubnets, nil, nil, func(_, v []byte) bool {
		var s Subnet
		if json.Unmarshal(v, &s) == nil {
			out = append(out, s)
		}
		return true
	})
	if err != nil && err != storage.ErrNotFound {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CIDR < out[j].CIDR })
	return out, nil
}

func (e *Engine) ListSummaries(ctx context.Context) ([]SubnetSummary, error) {
	subnets, err := e.ListSubnets(ctx)
	if err != nil {
		return nil, err
	}
	leaseMap := map[string]struct{ total, active int }{}
	if e.leaseStore != nil {
		leases, err := e.leaseStore.ListAll(ctx)
		if err == nil {
			for _, l := range leases {
				s := l.SubnetID
				if s == "" {
					s = "unassigned"
				}
				entry := leaseMap[s]
				entry.total++
				if l.State == lease.StateBound || l.State == lease.StateRenewing {
					entry.active++
				}
				leaseMap[s] = entry
			}
		}
	}
	result := make([]SubnetSummary, 0, len(subnets))
	for _, s := range subnets {
		entry := leaseMap[s.CIDR]
		util := utilizationPercent(entry.active, subnetCapacity(s))
		result = append(result, SubnetSummary{
			CIDR:         s.CIDR,
			Name:         s.Name,
			VLAN:         s.VLAN,
			ActiveLeases: entry.active,
			TotalLeases:  entry.total,
			Utilization:  util,
		})
	}
	if unassigned, ok := leaseMap["unassigned"]; ok {
		result = append(result, SubnetSummary{
			CIDR:         "unassigned",
			Name:         "Unassigned",
			VLAN:         0,
			ActiveLeases: unassigned.active,
			TotalLeases:  unassigned.total,
			Utilization:  0,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CIDR < result[j].CIDR })
	return result, nil
}

func (e *Engine) UpsertReservation(ctx context.Context, in UpsertReservationInput) (Reservation, error) {
	result, err := e.ValidateReservation(ctx, in)
	if err != nil {
		return Reservation{}, err
	}

	raw, err := json.Marshal(result)
	if err != nil {
		return Reservation{}, err
	}
	if err := e.store.Put(treeReservations, []byte(result.MAC), raw); err != nil {
		return Reservation{}, err
	}
	return result, nil
}

func (e *Engine) ValidateReservation(ctx context.Context, in UpsertReservationInput) (Reservation, error) {
	mac := normalizeMAC(in.MAC)
	if mac == "" {
		return Reservation{}, fmt.Errorf("mac is required")
	}
	if _, err := net.ParseMAC(mac); err != nil {
		return Reservation{}, fmt.Errorf("invalid mac: %w", err)
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(in.IP))
	if err != nil {
		return Reservation{}, fmt.Errorf("invalid ip: %w", err)
	}

	subnets, err := e.ListSubnets(ctx)
	if err != nil {
		return Reservation{}, err
	}
	subnetCIDR := strings.TrimSpace(in.SubnetCIDR)
	if subnetCIDR != "" {
		subnet, ok := lookupSubnet(subnets, subnetCIDR)
		if !ok {
			return Reservation{}, fmt.Errorf("subnet not found: %s", subnetCIDR)
		}
		prefix, parseErr := netip.ParsePrefix(subnet.CIDR)
		if parseErr != nil || !prefix.Contains(addr) {
			return Reservation{}, fmt.Errorf("ip is outside subnet %s", subnet.CIDR)
		}
		subnetCIDR = subnet.CIDR
	} else {
		subnetCIDR = inferSubnetForAddr(addr, subnets)
		if subnetCIDR == "" {
			return Reservation{}, fmt.Errorf("ip does not belong to a configured subnet")
		}
	}

	records, err := e.ListReservations(ctx)
	if err != nil {
		return Reservation{}, err
	}
	for _, existing := range records {
		if existing.IP == addr.String() && normalizeMAC(existing.MAC) != mac {
			return Reservation{}, fmt.Errorf("ip %s is already reserved for %s", existing.IP, existing.MAC)
		}
	}

	now := time.Now().UTC()
	createdAt := now
	if old, oldErr := e.GetReservationByMAC(ctx, mac); oldErr == nil {
		createdAt = old.CreatedAt
	}

	return Reservation{
		MAC:        mac,
		IP:         addr.String(),
		Hostname:   strings.TrimSpace(in.Hostname),
		SubnetCIDR: subnetCIDR,
		CreatedAt:  createdAt,
		UpdatedAt:  now,
	}, nil
}

func (e *Engine) GetReservationByMAC(_ context.Context, mac string) (Reservation, error) {
	raw, err := e.store.Get(treeReservations, []byte(normalizeMAC(mac)))
	if err != nil {
		return Reservation{}, err
	}
	var out Reservation
	if err := json.Unmarshal(raw, &out); err != nil {
		return Reservation{}, err
	}
	return out, nil
}

func (e *Engine) DeleteReservation(_ context.Context, mac string) error {
	return e.store.Delete(treeReservations, []byte(normalizeMAC(mac)))
}

func (e *Engine) ListReservations(_ context.Context) ([]Reservation, error) {
	out := make([]Reservation, 0, 64)
	err := e.store.Iterate(treeReservations, nil, nil, func(_, v []byte) bool {
		var r Reservation
		if json.Unmarshal(v, &r) == nil {
			out = append(out, r)
		}
		return true
	})
	if err != nil && err != storage.ErrNotFound {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IP == out[j].IP {
			return out[i].MAC < out[j].MAC
		}
		return out[i].IP < out[j].IP
	})
	return out, nil
}

func (e *Engine) ListAddresses(ctx context.Context, filter AddressFilter) ([]AddressRecord, error) {
	subnets, err := e.ListSubnets(ctx)
	if err != nil {
		return nil, err
	}

	targetSubnet := strings.TrimSpace(filter.SubnetCIDR)
	if targetSubnet != "" && targetSubnet != "all" {
		subnet, ok := lookupSubnet(subnets, targetSubnet)
		if !ok {
			return nil, fmt.Errorf("subnet not found: %s", targetSubnet)
		}
		targetSubnet = subnet.CIDR
	}

	records := map[string]AddressRecord{}

	stored, err := e.ListStoredAddresses(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range stored {
		if targetSubnet != "" && item.SubnetCIDR != targetSubnet {
			continue
		}
		records[item.IP] = mergeAddressRecord(records[item.IP], item)
	}

	reservations, err := e.ListReservations(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range reservations {
		if targetSubnet != "" && r.SubnetCIDR != targetSubnet {
			continue
		}
		next := AddressRecord{
			IP:         r.IP,
			SubnetCIDR: r.SubnetCIDR,
			State:      IPStateReserved,
			MAC:        r.MAC,
			Hostname:   r.Hostname,
			Source:     "reservation",
			UpdatedAt:  r.UpdatedAt,
		}
		records[r.IP] = mergeAddressRecord(records[r.IP], next)
	}

	if e.leaseStore != nil {
		leases, leaseErr := e.leaseStore.ListAll(ctx)
		if leaseErr == nil {
			for _, l := range leases {
				subnetCIDR := strings.TrimSpace(l.SubnetID)
				if subnetCIDR == "" {
					if addr, parseErr := netip.ParseAddr(strings.TrimSpace(l.IP)); parseErr == nil {
						subnetCIDR = inferSubnetForAddr(addr, subnets)
					}
				}
				if targetSubnet != "" && subnetCIDR != targetSubnet {
					continue
				}

				state := leaseToIPState(l)
				next := AddressRecord{
					IP:         l.IP,
					SubnetCIDR: subnetCIDR,
					State:      state,
					MAC:        normalizeMAC(l.MAC),
					Hostname:   l.Hostname,
					LeaseState: string(l.State),
					Source:     "lease",
					UpdatedAt:  l.UpdatedAt,
				}
				records[l.IP] = mergeAddressRecord(records[l.IP], next)
			}
		}
	}

	if targetSubnet != "" {
		subnet, _ := lookupSubnet(subnets, targetSubnet)
		if subnet.DHCP.Enabled {
			if start, end, ok := poolRange(subnet); ok {
				maxFill := 4096
				for ip, seen := start, 0; ; ip = ip.Next() {
					key := ip.String()
					if _, exists := records[key]; !exists {
						records[key] = AddressRecord{
							IP:         key,
							SubnetCIDR: subnet.CIDR,
							State:      IPStateAvailable,
							Source:     "pool",
						}
					}
					seen++
					if ip.Compare(end) == 0 || seen >= maxFill {
						break
					}
				}
			}
		}
	}

	query := strings.ToLower(strings.TrimSpace(filter.Query))
	out := make([]AddressRecord, 0, len(records))
	for _, r := range records {
		if filter.State != "" && r.State != filter.State {
			continue
		}
		if query != "" {
			haystack := strings.ToLower(strings.Join([]string{r.IP, r.MAC, r.Hostname, r.SubnetCIDR, r.Source, r.LeaseState}, " "))
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		out = append(out, r)
	}
	sortAddressRecords(out)
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (e *Engine) GetAddress(ctx context.Context, ip string) (AddressRecord, error) {
	candidateIP := strings.TrimSpace(ip)
	if _, err := netip.ParseAddr(candidateIP); err != nil {
		return AddressRecord{}, fmt.Errorf("invalid ip: %w", err)
	}

	subnets, err := e.ListSubnets(ctx)
	if err != nil {
		return AddressRecord{}, err
	}

	found := false
	var out AddressRecord

	if stored, storedErr := e.GetStoredAddress(ctx, candidateIP); storedErr == nil {
		out = mergeAddressRecord(out, stored)
		found = true
	}

	reservations, err := e.ListReservations(ctx)
	if err != nil {
		return AddressRecord{}, err
	}
	for _, r := range reservations {
		if r.IP != candidateIP {
			continue
		}
		out = mergeAddressRecord(out, AddressRecord{
			IP:         r.IP,
			SubnetCIDR: r.SubnetCIDR,
			State:      IPStateReserved,
			MAC:        r.MAC,
			Hostname:   r.Hostname,
			Source:     "reservation",
			UpdatedAt:  r.UpdatedAt,
		})
		found = true
	}

	if e.leaseStore != nil {
		if l, leaseErr := e.leaseStore.GetByIP(ctx, candidateIP); leaseErr == nil {
			subnetCIDR := strings.TrimSpace(l.SubnetID)
			if subnetCIDR == "" {
				if addr, parseErr := netip.ParseAddr(candidateIP); parseErr == nil {
					subnetCIDR = inferSubnetForAddr(addr, subnets)
				}
			}
			out = mergeAddressRecord(out, AddressRecord{
				IP:         l.IP,
				SubnetCIDR: subnetCIDR,
				State:      leaseToIPState(l),
				MAC:        normalizeMAC(l.MAC),
				Hostname:   l.Hostname,
				LeaseState: string(l.State),
				Source:     "lease",
				UpdatedAt:  l.UpdatedAt,
			})
			found = true
		}
	}

	if !found {
		if addr, parseErr := netip.ParseAddr(candidateIP); parseErr == nil {
			if subnetCIDR := inferSubnetForAddr(addr, subnets); subnetCIDR != "" {
				out = AddressRecord{
					IP:         candidateIP,
					SubnetCIDR: subnetCIDR,
					State:      IPStateAvailable,
					Source:     "subnet",
				}
				found = true
			}
		}
	}

	if !found {
		return AddressRecord{}, storage.ErrNotFound
	}
	return out, nil
}

func (e *Engine) UpsertAddress(ctx context.Context, in UpsertAddressInput) (AddressRecord, error) {
	record, err := e.ValidateAddress(ctx, in)
	if err != nil {
		return AddressRecord{}, err
	}

	raw, err := json.Marshal(record)
	if err != nil {
		return AddressRecord{}, err
	}
	if err := e.store.Put(treeAddresses, []byte(record.IP), raw); err != nil {
		return AddressRecord{}, err
	}
	return record, nil
}

func (e *Engine) ValidateAddress(ctx context.Context, in UpsertAddressInput) (AddressRecord, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(in.IP))
	if err != nil {
		return AddressRecord{}, fmt.Errorf("invalid ip: %w", err)
	}

	subnets, err := e.ListSubnets(ctx)
	if err != nil {
		return AddressRecord{}, err
	}

	subnetCIDR := strings.TrimSpace(in.SubnetCIDR)
	if subnetCIDR != "" {
		subnet, ok := lookupSubnet(subnets, subnetCIDR)
		if !ok {
			return AddressRecord{}, fmt.Errorf("subnet not found: %s", subnetCIDR)
		}
		prefix, parseErr := netip.ParsePrefix(subnet.CIDR)
		if parseErr != nil || !prefix.Contains(addr) {
			return AddressRecord{}, fmt.Errorf("ip is outside subnet %s", subnet.CIDR)
		}
		subnetCIDR = subnet.CIDR
	} else {
		subnetCIDR = inferSubnetForAddr(addr, subnets)
		if subnetCIDR == "" {
			return AddressRecord{}, fmt.Errorf("ip does not belong to a configured subnet")
		}
	}

	state := in.State
	if state == "" {
		state = IPStateAvailable
	}
	switch state {
	case IPStateAvailable, IPStateDHCP, IPStateReserved, IPStateConflict, IPStateQuarantined:
	default:
		return AddressRecord{}, fmt.Errorf("invalid address state: %s", state)
	}

	updatedAt := in.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}

	record := AddressRecord{
		IP:         addr.String(),
		SubnetCIDR: subnetCIDR,
		State:      state,
		MAC:        normalizeMAC(in.MAC),
		Hostname:   strings.TrimSpace(in.Hostname),
		Source:     strings.TrimSpace(in.Source),
		UpdatedAt:  updatedAt,
	}
	if record.Source == "" {
		record.Source = "manual"
	}
	return record, nil
}

func (e *Engine) GetStoredAddress(_ context.Context, ip string) (AddressRecord, error) {
	raw, err := e.store.Get(treeAddresses, []byte(strings.TrimSpace(ip)))
	if err != nil {
		return AddressRecord{}, err
	}
	var out AddressRecord
	if err := json.Unmarshal(raw, &out); err != nil {
		return AddressRecord{}, err
	}
	return out, nil
}

func (e *Engine) DeleteStoredAddress(_ context.Context, ip string) error {
	return e.store.Delete(treeAddresses, []byte(strings.TrimSpace(ip)))
}

func (e *Engine) ListStoredAddresses(_ context.Context) ([]AddressRecord, error) {
	out := make([]AddressRecord, 0, 64)
	err := e.store.Iterate(treeAddresses, nil, nil, func(_, v []byte) bool {
		var item AddressRecord
		if json.Unmarshal(v, &item) == nil {
			out = append(out, item)
		}
		return true
	})
	if err != nil && err != storage.ErrNotFound {
		return nil, err
	}
	sortAddressRecords(out)
	return out, nil
}

func (e *Engine) validateSubnet(in UpsertSubnetInput) (Subnet, error) {
	cidr := strings.TrimSpace(in.CIDR)
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return Subnet{}, fmt.Errorf("invalid cidr: %w", err)
	}
	cidr = prefix.Masked().String()

	if in.Gateway != "" {
		gw, err := netip.ParseAddr(strings.TrimSpace(in.Gateway))
		if err != nil {
			return Subnet{}, fmt.Errorf("invalid gateway: %w", err)
		}
		if !prefix.Contains(gw) {
			return Subnet{}, fmt.Errorf("gateway outside subnet")
		}
	}

	if in.DHCPEnable {
		start, err := netip.ParseAddr(strings.TrimSpace(in.PoolStart))
		if err != nil {
			return Subnet{}, fmt.Errorf("invalid pool_start: %w", err)
		}
		end, err := netip.ParseAddr(strings.TrimSpace(in.PoolEnd))
		if err != nil {
			return Subnet{}, fmt.Errorf("invalid pool_end: %w", err)
		}
		if !prefix.Contains(start) || !prefix.Contains(end) {
			return Subnet{}, fmt.Errorf("pool range outside subnet")
		}
		if start.Compare(end) > 0 {
			return Subnet{}, fmt.Errorf("pool_start must be <= pool_end")
		}
	}

	leaseSec := in.LeaseSec
	if leaseSec <= 0 {
		leaseSec = int64((12 * time.Hour).Seconds())
	}
	if in.VLAN < 0 || in.VLAN > 4094 {
		return Subnet{}, fmt.Errorf("vlan must be between 0 and 4094")
	}

	return Subnet{
		CIDR:    cidr,
		Name:    strings.TrimSpace(in.Name),
		VLAN:    in.VLAN,
		Gateway: strings.TrimSpace(in.Gateway),
		DNS:     normalizeDNS(in.DNS),
		DHCP: DHCPPool{
			Enabled:      in.DHCPEnable,
			PoolStart:    strings.TrimSpace(in.PoolStart),
			PoolEnd:      strings.TrimSpace(in.PoolEnd),
			LeaseTimeSec: leaseSec,
		},
	}, nil
}

func normalizeDNS(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func leaseToIPState(l lease.Lease) IPState {
	switch l.State {
	case lease.StateDeclined:
		return IPStateConflict
	case lease.StateQuarantined, lease.StateExpired, lease.StateReleased:
		return IPStateQuarantined
	default:
		return IPStateDHCP
	}
}

func normalizeMAC(in string) string {
	return strings.ToUpper(strings.TrimSpace(in))
}

func lookupSubnet(subnets []Subnet, cidr string) (Subnet, bool) {
	normalized := strings.TrimSpace(cidr)
	for _, s := range subnets {
		if s.CIDR == normalized {
			return s, true
		}
	}
	return Subnet{}, false
}

func inferSubnetForAddr(addr netip.Addr, subnets []Subnet) string {
	for _, s := range subnets {
		prefix, err := netip.ParsePrefix(s.CIDR)
		if err != nil {
			continue
		}
		if prefix.Contains(addr) {
			return s.CIDR
		}
	}
	return ""
}

func poolRange(subnet Subnet) (netip.Addr, netip.Addr, bool) {
	start, err := netip.ParseAddr(strings.TrimSpace(subnet.DHCP.PoolStart))
	if err != nil {
		return netip.Addr{}, netip.Addr{}, false
	}
	end, err := netip.ParseAddr(strings.TrimSpace(subnet.DHCP.PoolEnd))
	if err != nil {
		return netip.Addr{}, netip.Addr{}, false
	}
	if !start.Is4() || !end.Is4() || start.Compare(end) > 0 {
		return netip.Addr{}, netip.Addr{}, false
	}
	return start, end, true
}

func subnetCapacity(subnet Subnet) int {
	if start, end, ok := poolRange(subnet); ok {
		return ipv4Span(start, end)
	}

	prefix, err := netip.ParsePrefix(strings.TrimSpace(subnet.CIDR))
	if err != nil {
		return 0
	}

	total := AddressCount(prefix)
	if prefix.Addr().Is4() && prefix.Bits() <= 30 && total >= 2 {
		total -= 2
	}
	if total > uint64(^uint(0)>>1) {
		return int(^uint(0) >> 1)
	}
	return int(total)
}

func utilizationPercent(active int, capacity int) int {
	if active <= 0 || capacity <= 0 {
		return 0
	}
	if active >= capacity {
		return 100
	}
	return int(float64(active) / float64(capacity) * 100.0)
}

func ipv4Span(start netip.Addr, end netip.Addr) int {
	if !start.Is4() || !end.Is4() || start.Compare(end) > 0 {
		return 0
	}
	start4 := start.As4()
	end4 := end.As4()
	startValue := uint32(start4[0])<<24 | uint32(start4[1])<<16 | uint32(start4[2])<<8 | uint32(start4[3])
	endValue := uint32(end4[0])<<24 | uint32(end4[1])<<16 | uint32(end4[2])<<8 | uint32(end4[3])
	return int(endValue-startValue) + 1
}

func mergeAddressRecord(current AddressRecord, next AddressRecord) AddressRecord {
	if current.IP == "" {
		return next
	}
	out := current
	if out.SubnetCIDR == "" && next.SubnetCIDR != "" {
		out.SubnetCIDR = next.SubnetCIDR
	}
	if out.Hostname == "" && next.Hostname != "" {
		out.Hostname = next.Hostname
	}
	if out.MAC == "" && next.MAC != "" {
		out.MAC = next.MAC
	}
	if out.LeaseState == "" && next.LeaseState != "" {
		out.LeaseState = next.LeaseState
	}
	if out.Source == "" {
		out.Source = next.Source
	} else if next.Source != "" && out.Source != next.Source && !strings.Contains(out.Source, next.Source) {
		out.Source = out.Source + "+" + next.Source
	}
	if next.UpdatedAt.After(out.UpdatedAt) {
		out.UpdatedAt = next.UpdatedAt
	}

	if out.MAC != "" && next.MAC != "" && out.MAC != next.MAC {
		out.State = IPStateConflict
		return out
	}
	if rankState(next.State) > rankState(out.State) {
		out.State = next.State
	}
	return out
}

func rankState(state IPState) int {
	switch state {
	case IPStateConflict:
		return 5
	case IPStateDHCP:
		return 4
	case IPStateQuarantined:
		return 3
	case IPStateReserved:
		return 2
	default:
		return 1
	}
}

func sortAddressRecords(items []AddressRecord) {
	sort.Slice(items, func(i, j int) bool {
		a, errA := netip.ParseAddr(items[i].IP)
		b, errB := netip.ParseAddr(items[j].IP)
		if errA == nil && errB == nil {
			return a.Compare(b) < 0
		}
		return items[i].IP < items[j].IP
	})
}
