package dhcpv6

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/monsoondhcp/monsoon/internal/config"
	"github.com/monsoondhcp/monsoon/internal/lease"
)

type AllocationRequest struct {
	ClientDUID  []byte
	IAID        uint32
	RequestedIP net.IP
	RelayAddr   net.IP
}

type AllocationResult struct {
	IP                net.IP
	Prefix            netip.Prefix
	Gateway           net.IP
	DNS               []net.IP
	LeaseDuration     time.Duration
	SubnetID          string
	IAID              uint32
	PreferredLifetime uint32
	ValidLifetime     uint32
}

type PoolManager struct {
	pools        []managedPool
	store        lease.Store
	defaultLease time.Duration
}

type managedPool struct {
	id       string
	prefix   netip.Prefix
	start    netip.Addr
	end      netip.Addr
	gateway  net.IP
	dns      []net.IP
	leaseDur time.Duration
}

func NewPoolManager(subnets []config.SubnetConfig, defaultLease time.Duration, store lease.Store) (*PoolManager, error) {
	if defaultLease <= 0 {
		defaultLease = 12 * time.Hour
	}
	manager := &PoolManager{
		pools:        make([]managedPool, 0, len(subnets)),
		store:        store,
		defaultLease: defaultLease,
	}
	for _, subnet := range subnets {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(subnet.CIDR))
		if err != nil || !prefix.Addr().Is6() {
			continue
		}
		if !subnet.DHCP.Enabled {
			continue
		}
		start, err := netip.ParseAddr(strings.TrimSpace(subnet.DHCP.PoolStart))
		if err != nil || !prefix.Contains(start) {
			return nil, fmt.Errorf("invalid dhcpv6 pool_start for %s", subnet.CIDR)
		}
		end, err := netip.ParseAddr(strings.TrimSpace(subnet.DHCP.PoolEnd))
		if err != nil || !prefix.Contains(end) || start.Compare(end) > 0 {
			return nil, fmt.Errorf("invalid dhcpv6 pool_end for %s", subnet.CIDR)
		}

		leaseDur := subnet.DHCP.LeaseTime.Duration
		if leaseDur <= 0 {
			leaseDur = defaultLease
		}
		item := managedPool{
			id:       prefix.String(),
			prefix:   prefix,
			start:    start,
			end:      end,
			leaseDur: leaseDur,
		}
		if gateway := net.ParseIP(strings.TrimSpace(subnet.Gateway)).To16(); gateway != nil {
			item.gateway = gateway
		}
		for _, dns := range subnet.DNS {
			if ip := net.ParseIP(strings.TrimSpace(dns)).To16(); ip != nil {
				item.dns = append(item.dns, ip)
			}
		}
		manager.pools = append(manager.pools, item)
	}
	if len(manager.pools) == 0 {
		return nil, fmt.Errorf("no dhcpv6 pools configured")
	}
	return manager, nil
}

func (m *PoolManager) Allocate(ctx context.Context, req AllocationRequest) (AllocationResult, error) {
	pool, err := m.selectPool(req)
	if err != nil {
		return AllocationResult{}, err
	}

	if len(req.ClientDUID) > 0 && m.store != nil {
		existing, err := m.store.GetByClientID(ctx, req.ClientDUID)
		if err == nil {
			for _, item := range existing {
				ip, parseErr := netip.ParseAddr(strings.TrimSpace(item.IP))
				if parseErr == nil && pool.prefix.Contains(ip) {
					return m.resultFor(pool, ip, req.IAID), nil
				}
			}
		}
	}

	if ip := req.RequestedIP.To16(); ip != nil {
		addr, ok := netip.AddrFromSlice(ip)
		if ok && pool.prefix.Contains(addr) {
			if available, _ := m.availableForClient(ctx, addr, req.ClientDUID); available {
				return m.resultFor(pool, addr, req.IAID), nil
			}
		}
	}

	for current := pool.start; ; current = current.Next() {
		if current.Compare(pool.end) > 0 {
			break
		}
		if available, _ := m.availableForClient(ctx, current, req.ClientDUID); available {
			return m.resultFor(pool, current, req.IAID), nil
		}
	}
	return AllocationResult{}, fmt.Errorf("no dhcpv6 addresses available in %s", pool.prefix)
}

func (m *PoolManager) selectPool(req AllocationRequest) (managedPool, error) {
	if ip := req.RequestedIP.To16(); ip != nil {
		if addr, ok := netip.AddrFromSlice(ip); ok {
			for _, pool := range m.pools {
				if pool.prefix.Contains(addr) {
					return pool, nil
				}
			}
		}
	}
	if ip := req.RelayAddr.To16(); ip != nil {
		if addr, ok := netip.AddrFromSlice(ip); ok {
			for _, pool := range m.pools {
				if pool.prefix.Contains(addr) {
					return pool, nil
				}
			}
		}
	}
	return m.pools[0], nil
}

func (m *PoolManager) availableForClient(ctx context.Context, addr netip.Addr, clientID []byte) (bool, error) {
	if m.store == nil {
		return true, nil
	}
	item, err := m.store.GetByIP(ctx, addr.String())
	if err != nil {
		return true, nil
	}
	if len(clientID) > 0 && string(item.ClientID) == string(clientID) {
		return true, nil
	}
	switch item.State {
	case lease.StateReleased, lease.StateExpired, lease.StateFree:
		return true, nil
	default:
		return false, nil
	}
}

func (m *PoolManager) resultFor(pool managedPool, addr netip.Addr, iaid uint32) AllocationResult {
	leaseDurSec := durationToUint32Seconds(pool.leaseDur)
	if leaseDurSec == 0 {
		leaseDurSec = durationToUint32Seconds(m.defaultLease)
	}
	if leaseDurSec == 0 {
		leaseDurSec = durationToUint32Seconds(12 * time.Hour)
	}
	preferred := leaseDurSec / 2
	if preferred == 0 {
		preferred = leaseDurSec
	}
	return AllocationResult{
		IP:                net.IP(addr.AsSlice()),
		Prefix:            pool.prefix,
		Gateway:           append(net.IP(nil), pool.gateway...),
		DNS:               append([]net.IP(nil), pool.dns...),
		LeaseDuration:     pool.leaseDur,
		SubnetID:          pool.id,
		IAID:              iaid,
		PreferredLifetime: preferred,
		ValidLifetime:     leaseDurSec,
	}
}
