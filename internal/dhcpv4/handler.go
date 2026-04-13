package dhcpv4

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/monsoondhcp/monsoon/internal/lease"
)

type Handler struct {
	store        lease.Store
	pools        *PoolManager
	serverIP     net.IP
	defaultLease time.Duration
	maxLease     time.Duration
	onLeaseEvent func(string, lease.Lease)
}

func NewHandler(store lease.Store, pools *PoolManager, serverIP net.IP, defaultLease, maxLease time.Duration) *Handler {
	if defaultLease <= 0 {
		defaultLease = 12 * time.Hour
	}
	if maxLease <= 0 {
		maxLease = 24 * time.Hour
	}
	return &Handler{
		store:        store,
		pools:        pools,
		serverIP:     serverIP.To4(),
		defaultLease: defaultLease,
		maxLease:     maxLease,
	}
}

func (h *Handler) SetOnLeaseEvent(fn func(string, lease.Lease)) {
	h.onLeaseEvent = fn
}

func (h *Handler) Handle(ctx context.Context, req Packet, _ *net.UDPAddr) (*Packet, error) {
	msgType, ok := req.Options.MessageType()
	if !ok {
		return nil, fmt.Errorf("missing dhcp message type")
	}
	if req.Options == nil {
		req.Options = Options{}
	}
	switch msgType {
	case MessageDiscover:
		if HasRapidCommit(req.Options) {
			return h.handleDiscover(ctx, req, true)
		}
		return h.handleDiscover(ctx, req, false)
	case MessageRequest:
		return h.handleRequest(ctx, req)
	case MessageRelease:
		return nil, h.handleRelease(ctx, req)
	case MessageDecline:
		return nil, h.handleDecline(ctx, req)
	case MessageInform:
		return h.handleInform(ctx, req)
	default:
		return nil, nil
	}
}

func (h *Handler) handleDiscover(ctx context.Context, req Packet, rapidCommit bool) (*Packet, error) {
	reqIP, _ := req.Options.RequestedIP()
	alloc, err := h.pools.Allocate(ctx, AllocationRequest{
		MAC:         req.ClientMAC().String(),
		ClientID:    req.Options.ClientIdentifier(),
		RequestedIP: reqIP,
		RelayAddr:   req.GIAddr,
	})
	if err != nil {
		return nil, err
	}
	state := lease.StateOffered
	msg := MessageOffer
	if rapidCommit {
		state = lease.StateBound
		msg = MessageAck
	}
	now := time.Now().UTC()
	l := lease.Lease{
		IP:          alloc.IP.String(),
		MAC:         req.ClientMAC().String(),
		ClientID:    req.Options.ClientIdentifier(),
		Hostname:    req.Options.Hostname(),
		State:       state,
		StartTime:   now,
		Duration:    alloc.LeaseDuration,
		ExpiryTime:  now.Add(alloc.LeaseDuration),
		SubnetID:    alloc.SubnetID,
		RelayAddr:   req.GIAddr.String(),
		VendorClass: req.Options.VendorClass(),
		UserClass:   req.Options.UserClass(),
		LastSeen:    now,
	}
	if rawRelay, ok := req.Options[OptionRelayAgentInfo]; ok {
		ri := ParseRelayAgentInfo(rawRelay)
		l.RelayInfo = ri.Raw
		l.CircuitID = ri.CircuitID
		l.RemoteID = ri.RemoteID
	}
	if err := h.store.Upsert(ctx, l); err != nil {
		return nil, err
	}
	if rapidCommit && h.onLeaseEvent != nil {
		h.onLeaseEvent("lease.created", l)
	}

	resp := h.baseResponse(req)
	resp.YIAddr = alloc.IP
	resp.Options = h.leaseOptions(msg, alloc)
	if rapidCommit {
		EnableRapidCommit(resp.Options)
	}
	return &resp, nil
}

func (h *Handler) handleRequest(ctx context.Context, req Packet) (*Packet, error) {
	targetIP, _ := req.Options.RequestedIP()
	if ip := req.CIAddr.To4(); ip != nil && !isZeroIPv4(ip) {
		targetIP = ip
	}
	alloc, err := h.pools.Allocate(ctx, AllocationRequest{
		MAC:         req.ClientMAC().String(),
		ClientID:    req.Options.ClientIdentifier(),
		RequestedIP: targetIP,
		RelayAddr:   req.GIAddr,
	})
	if err != nil {
		resp := h.baseResponse(req)
		resp.Options = Options{}
		resp.Options.SetMessageType(MessageNak)
		resp.Options.SetIPv4(OptionServerIdentifier, h.serverIP)
		resp.Options.SetString(OptionMessage, "unable to allocate requested address")
		return &resp, nil
	}

	now := time.Now().UTC()
	old, oldErr := h.store.GetByIP(ctx, alloc.IP.String())
	l := lease.Lease{
		IP:          alloc.IP.String(),
		MAC:         req.ClientMAC().String(),
		ClientID:    req.Options.ClientIdentifier(),
		Hostname:    req.Options.Hostname(),
		State:       lease.StateBound,
		StartTime:   now,
		Duration:    alloc.LeaseDuration,
		ExpiryTime:  now.Add(alloc.LeaseDuration),
		SubnetID:    alloc.SubnetID,
		RelayAddr:   req.GIAddr.String(),
		VendorClass: req.Options.VendorClass(),
		UserClass:   req.Options.UserClass(),
		LastSeen:    now,
	}
	if err := h.store.Upsert(ctx, l); err != nil {
		return nil, err
	}
	if h.onLeaseEvent != nil {
		eventType := "lease.created"
		if oldErr == nil && old.State != lease.StateFree {
			eventType = "lease.renewed"
		}
		h.onLeaseEvent(eventType, l)
	}

	resp := h.baseResponse(req)
	resp.YIAddr = alloc.IP
	resp.Options = h.leaseOptions(MessageAck, alloc)
	return &resp, nil
}

func (h *Handler) handleRelease(ctx context.Context, req Packet) error {
	ip := req.CIAddr.To4()
	if ip == nil || isZeroIPv4(ip) {
		ip, _ = req.Options.RequestedIP()
	}
	if ip == nil {
		return nil
	}
	l, err := h.store.GetByIP(ctx, ip.String())
	if err != nil {
		return nil
	}
	now := time.Now().UTC()
	l.State = lease.StateReleased
	l.UpdatedAt = now
	l.ExpiryTime = now
	if err := h.store.Upsert(ctx, l); err != nil {
		return err
	}
	if h.onLeaseEvent != nil {
		h.onLeaseEvent("lease.released", l)
	}
	h.pools.Release(ip, l.SubnetID)
	return nil
}

func (h *Handler) handleDecline(ctx context.Context, req Packet) error {
	ip, _ := req.Options.RequestedIP()
	if ip == nil {
		ip = req.CIAddr.To4()
	}
	if ip == nil || isZeroIPv4(ip) {
		return nil
	}
	l, err := h.store.GetByIP(ctx, ip.String())
	if err != nil {
		return nil
	}
	now := time.Now().UTC()
	l.State = lease.StateDeclined
	l.QuarantineUntil = now.Add(15 * time.Minute)
	l.UpdatedAt = now
	if err := h.store.Upsert(ctx, l); err != nil {
		return err
	}
	if h.onLeaseEvent != nil {
		h.onLeaseEvent("lease.conflict", l)
	}
	return nil
}

func (h *Handler) handleInform(_ context.Context, req Packet) (*Packet, error) {
	resp := h.baseResponse(req)
	resp.YIAddr = nil
	resp.Options = Options{}
	resp.Options.SetMessageType(MessageAck)
	resp.Options.SetIPv4(OptionServerIdentifier, h.serverIP)
	return &resp, nil
}

func (h *Handler) baseResponse(req Packet) Packet {
	resp := Packet{
		Op:     2,
		HType:  req.HType,
		HLen:   req.HLen,
		XID:    req.XID,
		Flags:  req.Flags,
		CIAddr: req.CIAddr,
		GIAddr: req.GIAddr,
	}
	resp.CHAddr = req.CHAddr
	return resp
}

func (h *Handler) leaseOptions(msgType byte, alloc AllocationResult) Options {
	opts := Options{}
	opts.SetMessageType(msgType)
	opts.SetIPv4(OptionServerIdentifier, h.serverIP)
	leaseSec := durationToUint32Seconds(alloc.LeaseDuration)
	opts.SetDurationSeconds(OptionIPAddressLeaseTime, leaseSec)
	opts.SetDurationSeconds(OptionRenewalTimeValue, leaseSec/2)
	opts.SetDurationSeconds(OptionRebindingTimeValue, leaseSec-leaseSec/8)
	mask := net.CIDRMask(alloc.Prefix.Bits(), 32)
	opts[OptionSubnetMask] = append([]byte(nil), mask...)
	if alloc.Gateway != nil {
		opts.SetIPv4(OptionRouter, alloc.Gateway)
	}
	if len(alloc.DNS) > 0 {
		opts.SetIPv4List(OptionDomainNameServer, alloc.DNS)
	}
	return opts
}

func isZeroIPv4(ip net.IP) bool {
	ip = ip.To4()
	return ip == nil || (ip[0] == 0 && ip[1] == 0 && ip[2] == 0 && ip[3] == 0)
}

func durationToUint32Seconds(value time.Duration) uint32 {
	if value <= 0 {
		return 0
	}
	seconds := value / time.Second
	if seconds <= 0 {
		return 1
	}
	max := time.Duration(^uint32(0))
	if seconds > max {
		return ^uint32(0)
	}
	// #nosec G115 -- bounded to uint32 range above.
	return uint32(seconds)
}
