package dhcpv4

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/monsoondhcp/monsoon/internal/config"
	"github.com/monsoondhcp/monsoon/internal/lease"
)

type AllocationRequest struct {
	MAC         string
	ClientID    []byte
	RequestedIP net.IP
	RelayAddr   net.IP
}

type AllocationResult struct {
	IP            net.IP
	SubnetID      string
	Prefix        netip.Prefix
	Gateway       net.IP
	DNS           []net.IP
	LeaseDuration time.Duration
}

type subnetPool struct {
	id       string
	prefix   netip.Prefix
	start    uint32
	end      uint32
	bitmap   []bool
	gateway  net.IP
	dns      []net.IP
	leaseDur time.Duration
}

type PoolManager struct {
	mu    sync.Mutex
	store lease.Store
	pools map[string]*subnetPool
	order []string
}

func NewPoolManager(subnets []config.SubnetConfig, defaultLease time.Duration, store lease.Store) (*PoolManager, error) {
	pm := &PoolManager{store: store, pools: make(map[string]*subnetPool), order: make([]string, 0, len(subnets))}
	for _, s := range subnets {
		if !s.DHCP.Enabled {
			continue
		}
		prefix, err := netip.ParsePrefix(s.CIDR)
		if err != nil {
			return nil, fmt.Errorf("parse subnet %s: %w", s.CIDR, err)
		}
		startAddr, err := netip.ParseAddr(s.DHCP.PoolStart)
		if err != nil {
			return nil, fmt.Errorf("pool_start %s: %w", s.DHCP.PoolStart, err)
		}
		endAddr, err := netip.ParseAddr(s.DHCP.PoolEnd)
		if err != nil {
			return nil, fmt.Errorf("pool_end %s: %w", s.DHCP.PoolEnd, err)
		}
		start := ipv4ToUint32(startAddr.As4())
		end := ipv4ToUint32(endAddr.As4())
		if end < start {
			return nil, fmt.Errorf("invalid range %s-%s", s.DHCP.PoolStart, s.DHCP.PoolEnd)
		}
		leaseDur := s.DHCP.LeaseTime.Duration
		if leaseDur <= 0 {
			leaseDur = defaultLease
		}
		pool := &subnetPool{
			id:       s.CIDR,
			prefix:   prefix,
			start:    start,
			end:      end,
			bitmap:   make([]bool, end-start+1),
			gateway:  net.ParseIP(s.Gateway).To4(),
			dns:      parseIPs(s.DNS),
			leaseDur: leaseDur,
		}
		pm.pools[pool.id] = pool
		pm.order = append(pm.order, pool.id)
	}
	if store != nil {
		_ = pm.SyncFromLeases(context.Background())
	}
	return pm, nil
}

func (pm *PoolManager) SyncFromLeases(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for _, p := range pm.pools {
		for i := range p.bitmap {
			p.bitmap[i] = false
		}
	}
	leases, err := pm.store.ListAll(ctx)
	if err != nil {
		return err
	}
	for _, l := range leases {
		switch l.State {
		case lease.StateBound, lease.StateRenewing, lease.StateOffered, lease.StateQuarantined, lease.StateDeclined:
			ip := net.ParseIP(l.IP).To4()
			if ip == nil {
				continue
			}
			pm.markAllocated(ip, l.SubnetID)
		}
	}
	return nil
}

func (pm *PoolManager) Allocate(ctx context.Context, req AllocationRequest) (AllocationResult, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.store != nil && req.MAC != "" {
		leases, err := pm.store.GetByMAC(ctx, req.MAC)
		if err == nil {
			now := time.Now().UTC()
			for _, l := range leases {
				if l.ExpiryTime.Before(now) {
					continue
				}
				if l.State == lease.StateBound || l.State == lease.StateRenewing || l.State == lease.StateOffered {
					if pool, ok := pm.pools[l.SubnetID]; ok {
						ip := net.ParseIP(l.IP).To4()
						if ip == nil {
							continue
						}
						pm.setUsed(pool, ip, true)
						return pm.buildResult(pool, ip), nil
					}
				}
			}
		}
	}

	candidates := pm.selectCandidatePools(req)
	if req.RequestedIP != nil {
		if res, ok := pm.tryAllocateSpecific(candidates, req.RequestedIP.To4()); ok {
			return res, nil
		}
	}
	for _, pool := range candidates {
		if ip, ok := pm.nextFree(pool); ok {
			pm.setUsed(pool, ip, true)
			return pm.buildResult(pool, ip), nil
		}
	}
	return AllocationResult{}, fmt.Errorf("no free address available")
}

func (pm *PoolManager) Release(ip net.IP, subnetID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.markFree(ip.To4(), subnetID)
}

func (pm *PoolManager) markAllocated(ip net.IP, subnetID string) {
	if subnetID != "" {
		if pool, ok := pm.pools[subnetID]; ok {
			pm.setUsed(pool, ip, true)
			return
		}
	}
	for _, pool := range pm.pools {
		if pool.prefix.Contains(netip.AddrFrom4([4]byte{ip[0], ip[1], ip[2], ip[3]})) {
			pm.setUsed(pool, ip, true)
			return
		}
	}
}

func (pm *PoolManager) markFree(ip net.IP, subnetID string) {
	if subnetID != "" {
		if pool, ok := pm.pools[subnetID]; ok {
			pm.setUsed(pool, ip, false)
			return
		}
	}
	for _, pool := range pm.pools {
		if pool.prefix.Contains(netip.AddrFrom4([4]byte{ip[0], ip[1], ip[2], ip[3]})) {
			pm.setUsed(pool, ip, false)
			return
		}
	}
}

func (pm *PoolManager) setUsed(pool *subnetPool, ip net.IP, used bool) {
	if pool == nil || ip == nil {
		return
	}
	u := ipv4ToUint32([4]byte{ip[0], ip[1], ip[2], ip[3]})
	if u < pool.start || u > pool.end {
		return
	}
	pool.bitmap[u-pool.start] = used
}

func (pm *PoolManager) tryAllocateSpecific(candidates []*subnetPool, ip net.IP) (AllocationResult, bool) {
	if ip == nil {
		return AllocationResult{}, false
	}
	u := ipv4ToUint32([4]byte{ip[0], ip[1], ip[2], ip[3]})
	for _, pool := range candidates {
		if u < pool.start || u > pool.end {
			continue
		}
		off := u - pool.start
		if !pool.bitmap[off] {
			pool.bitmap[off] = true
			return pm.buildResult(pool, ip), true
		}
	}
	return AllocationResult{}, false
}

func (pm *PoolManager) nextFree(pool *subnetPool) (net.IP, bool) {
	for i, used := range pool.bitmap {
		if !used {
			u := pool.start + uint32(i)
			return uint32ToIPv4(u), true
		}
	}
	return nil, false
}

func (pm *PoolManager) selectCandidatePools(req AllocationRequest) []*subnetPool {
	if req.RelayAddr != nil {
		if addr, ok := netip.AddrFromSlice(req.RelayAddr.To4()); ok {
			for _, id := range pm.order {
				pool := pm.pools[id]
				if pool.prefix.Contains(addr) {
					return []*subnetPool{pool}
				}
			}
		}
	}
	res := make([]*subnetPool, 0, len(pm.order))
	for _, id := range pm.order {
		res = append(res, pm.pools[id])
	}
	return res
}

func (pm *PoolManager) buildResult(pool *subnetPool, ip net.IP) AllocationResult {
	return AllocationResult{
		IP:            append(net.IP(nil), ip...),
		SubnetID:      pool.id,
		Prefix:        pool.prefix,
		Gateway:       append(net.IP(nil), pool.gateway...),
		DNS:           append([]net.IP(nil), pool.dns...),
		LeaseDuration: pool.leaseDur,
	}
}

func parseIPs(raw []string) []net.IP {
	out := make([]net.IP, 0, len(raw))
	for _, s := range raw {
		if ip := net.ParseIP(s).To4(); ip != nil {
			out = append(out, ip)
		}
	}
	return out
}

func ipv4ToUint32(ip [4]byte) uint32 {
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func uint32ToIPv4(v uint32) net.IP {
	return net.IPv4(byte(v>>24), byte(v>>16), byte(v>>8), byte(v)).To4()
}
