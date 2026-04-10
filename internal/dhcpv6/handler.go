package dhcpv6

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"time"

	"github.com/monsoondhcp/monsoon/internal/lease"
)

type Handler struct {
	store        lease.Store
	pools        *PoolManager
	serverDUID   []byte
	defaultLease time.Duration
	maxLease     time.Duration
	domainList   []string
	onLeaseEvent func(string, lease.Lease)
}

func NewHandler(store lease.Store, pools *PoolManager, serverDUID []byte, defaultLease, maxLease time.Duration) *Handler {
	if defaultLease <= 0 {
		defaultLease = 12 * time.Hour
	}
	if maxLease <= 0 {
		maxLease = 24 * time.Hour
	}
	return &Handler{
		store:        store,
		pools:        pools,
		serverDUID:   append([]byte(nil), serverDUID...),
		defaultLease: defaultLease,
		maxLease:     maxLease,
	}
}

func (h *Handler) SetOnLeaseEvent(fn func(string, lease.Lease)) {
	h.onLeaseEvent = fn
}

func (h *Handler) SetDomainList(domains []string) {
	h.domainList = append([]string(nil), domains...)
}

func (h *Handler) Handle(ctx context.Context, req Packet, remote *net.UDPAddr) (*Packet, error) {
	if req.IsRelay() {
		return h.handleRelay(ctx, req, remote)
	}
	switch req.MessageType {
	case MessageSolicit:
		return h.handleSolicit(ctx, req, nil)
	case MessageRequest:
		return h.handleRequest(ctx, req, nil)
	case MessageRenew, MessageRebind:
		return h.handleRenew(ctx, req, nil)
	case MessageRelease:
		return h.handleRelease(ctx, req, nil)
	case MessageDecline:
		return h.handleDecline(ctx, req, nil)
	case MessageInformationRequest:
		return h.handleInformationRequest(req), nil
	default:
		return nil, nil
	}
}

func (h *Handler) handleRelay(ctx context.Context, req Packet, remote *net.UDPAddr) (*Packet, error) {
	inner, ok, err := req.Encapsulated()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("relay-forward missing relay-message option")
	}
	resp, err := func() (*Packet, error) {
		switch inner.MessageType {
		case MessageSolicit:
			return h.handleSolicit(ctx, inner, req.LinkAddress)
		case MessageRequest:
			return h.handleRequest(ctx, inner, req.LinkAddress)
		case MessageRenew, MessageRebind:
			return h.handleRenew(ctx, inner, req.LinkAddress)
		case MessageRelease:
			return h.handleRelease(ctx, inner, req.LinkAddress)
		case MessageDecline:
			return h.handleDecline(ctx, inner, req.LinkAddress)
		case MessageInformationRequest:
			return h.handleInformationRequest(inner), nil
		default:
			return nil, nil
		}
	}()
	if err != nil || resp == nil {
		return resp, err
	}
	reply := encapsulateRelayReply(req, *resp)
	return &reply, nil
}

func (h *Handler) handleSolicit(ctx context.Context, req Packet, relayAddr net.IP) (*Packet, error) {
	clientID := req.Options.ClientID()
	if len(clientID) == 0 {
		return nil, fmt.Errorf("missing client identifier")
	}
	iana := firstIANA(req.Options.IANAs())
	alloc, err := h.pools.Allocate(ctx, AllocationRequest{
		ClientDUID:  clientID,
		IAID:        iana.IAID,
		RequestedIP: requestedIAAddr(iana),
		RelayAddr:   relayAddr,
	})
	if err != nil {
		return nil, err
	}
	state := lease.StateOffered
	msgType := MessageAdvertise
	if req.Options.HasRapidCommit() {
		state = lease.StateBound
		msgType = MessageReply
	}
	record := h.buildLeaseRecord(req, alloc, relayAddr, state)
	if err := h.store.Upsert(ctx, record); err != nil {
		return nil, err
	}
	if state == lease.StateBound && h.onLeaseEvent != nil {
		h.onLeaseEvent("lease.created", record)
	}
	resp := h.baseResponse(req, msgType)
	resp.Options = h.replyOptions(req, alloc, true)
	return &resp, nil
}

func (h *Handler) handleRequest(ctx context.Context, req Packet, relayAddr net.IP) (*Packet, error) {
	if err := h.ensureServerID(req); err != nil {
		return h.errorReply(req, StatusUnspecFail, err.Error()), nil
	}
	clientID := req.Options.ClientID()
	if len(clientID) == 0 {
		return h.errorReply(req, StatusUnspecFail, "missing client identifier"), nil
	}
	iana := firstIANA(req.Options.IANAs())
	alloc, err := h.pools.Allocate(ctx, AllocationRequest{
		ClientDUID:  clientID,
		IAID:        iana.IAID,
		RequestedIP: requestedIAAddr(iana),
		RelayAddr:   relayAddr,
	})
	if err != nil {
		return h.errorReply(req, StatusNoAddrsAvail, err.Error()), nil
	}
	old, _ := h.store.GetByIP(ctx, alloc.IP.String())
	record := h.buildLeaseRecord(req, alloc, relayAddr, lease.StateBound)
	if err := h.store.Upsert(ctx, record); err != nil {
		return nil, err
	}
	if h.onLeaseEvent != nil {
		eventType := "lease.created"
		if len(old.ClientID) > 0 {
			eventType = "lease.renewed"
		}
		h.onLeaseEvent(eventType, record)
	}
	resp := h.baseResponse(req, MessageReply)
	resp.Options = h.replyOptions(req, alloc, false)
	return &resp, nil
}

func (h *Handler) handleRenew(ctx context.Context, req Packet, relayAddr net.IP) (*Packet, error) {
	if err := h.ensureServerID(req); err != nil {
		return h.errorReply(req, StatusNoBinding, err.Error()), nil
	}
	clientID := req.Options.ClientID()
	if len(clientID) == 0 {
		return h.errorReply(req, StatusNoBinding, "missing client identifier"), nil
	}
	leases, err := h.store.GetByClientID(ctx, clientID)
	if err != nil || len(leases) == 0 {
		return h.errorReply(req, StatusNoBinding, "lease not found"), nil
	}
	item := leases[0]
	item.State = lease.StateRenewing
	item.LastSeen = time.Now().UTC()
	item.ExpiryTime = item.LastSeen.Add(item.Duration)
	item.UpdatedAt = item.LastSeen
	item.RelayAddr = relayAddr.String()
	if err := h.store.Upsert(ctx, item); err != nil {
		return nil, err
	}
	if h.onLeaseEvent != nil {
		h.onLeaseEvent("lease.renewed", item)
	}
	addr := net.ParseIP(item.IP)
	alloc := AllocationResult{
		IP:                addr,
		LeaseDuration:     item.Duration,
		SubnetID:          item.SubnetID,
		IAID:              firstIANA(req.Options.IANAs()).IAID,
		PreferredLifetime: uint32(item.Duration / time.Second / 2),
		ValidLifetime:     uint32(item.Duration / time.Second),
	}
	resp := h.baseResponse(req, MessageReply)
	resp.Options = h.replyOptions(req, alloc, false)
	return &resp, nil
}

func (h *Handler) handleRelease(ctx context.Context, req Packet, relayAddr net.IP) (*Packet, error) {
	clientID := req.Options.ClientID()
	if len(clientID) == 0 {
		return h.errorReply(req, StatusNoBinding, "missing client identifier"), nil
	}
	leases, err := h.store.GetByClientID(ctx, clientID)
	if err != nil || len(leases) == 0 {
		return h.errorReply(req, StatusNoBinding, "lease not found"), nil
	}
	item := leases[0]
	now := time.Now().UTC()
	item.State = lease.StateReleased
	item.ExpiryTime = now
	item.UpdatedAt = now
	item.LastSeen = now
	item.RelayAddr = relayAddr.String()
	if err := h.store.Upsert(ctx, item); err != nil {
		return nil, err
	}
	if h.onLeaseEvent != nil {
		h.onLeaseEvent("lease.released", item)
	}
	resp := h.baseResponse(req, MessageReply)
	resp.Options.SetClientID(clientID)
	resp.Options.SetServerID(h.serverDUID)
	resp.Options.SetStatus(StatusSuccess, "released")
	return &resp, nil
}

func (h *Handler) handleDecline(ctx context.Context, req Packet, relayAddr net.IP) (*Packet, error) {
	clientID := req.Options.ClientID()
	if len(clientID) == 0 {
		return h.errorReply(req, StatusNoBinding, "missing client identifier"), nil
	}
	leases, err := h.store.GetByClientID(ctx, clientID)
	if err != nil || len(leases) == 0 {
		return h.errorReply(req, StatusNoBinding, "lease not found"), nil
	}
	item := leases[0]
	now := time.Now().UTC()
	item.State = lease.StateDeclined
	item.QuarantineUntil = now.Add(15 * time.Minute)
	item.UpdatedAt = now
	item.LastSeen = now
	item.RelayAddr = relayAddr.String()
	if err := h.store.Upsert(ctx, item); err != nil {
		return nil, err
	}
	if h.onLeaseEvent != nil {
		h.onLeaseEvent("lease.conflict", item)
	}
	resp := h.baseResponse(req, MessageReply)
	resp.Options.SetClientID(clientID)
	resp.Options.SetServerID(h.serverDUID)
	resp.Options.SetStatus(StatusSuccess, "declined")
	return &resp, nil
}

func (h *Handler) handleInformationRequest(req Packet) *Packet {
	resp := h.baseResponse(req, MessageReply)
	if clientID := req.Options.ClientID(); len(clientID) > 0 {
		resp.Options.SetClientID(clientID)
	}
	resp.Options.SetServerID(h.serverDUID)
	if len(h.domainList) > 0 {
		resp.Options.SetDomainList(h.domainList)
	}
	return &resp
}

func (h *Handler) baseResponse(req Packet, msgType byte) Packet {
	return Packet{
		MessageType:   msgType,
		TransactionID: req.TransactionID,
		Options:       Options{},
	}
}

func (h *Handler) replyOptions(req Packet, alloc AllocationResult, includeRapid bool) Options {
	opts := Options{}
	opts.SetClientID(req.Options.ClientID())
	opts.SetServerID(h.serverDUID)
	if includeRapid && req.Options.HasRapidCommit() {
		opts.SetRapidCommit()
	}
	addr := IAAddress{
		Address:           alloc.IP,
		PreferredLifetime: alloc.PreferredLifetime,
		ValidLifetime:     alloc.ValidLifetime,
	}
	opts.AddIANA(IANA{
		IAID: alloc.IAID,
		T1:   alloc.PreferredLifetime,
		T2:   alloc.ValidLifetime * 7 / 8,
		Options: Options{
			{Code: OptionIAAddr, Value: addr.Encode()},
		},
	})
	if len(alloc.DNS) > 0 {
		opts.SetDNSServers(alloc.DNS)
	}
	if len(h.domainList) > 0 {
		opts.SetDomainList(h.domainList)
	}
	return opts
}

func (h *Handler) errorReply(req Packet, status uint16, message string) *Packet {
	resp := h.baseResponse(req, MessageReply)
	if clientID := req.Options.ClientID(); len(clientID) > 0 {
		resp.Options.SetClientID(clientID)
	}
	resp.Options.SetServerID(h.serverDUID)
	resp.Options.SetStatus(status, message)
	return &resp
}

func (h *Handler) ensureServerID(req Packet) error {
	if serverID := req.Options.ServerID(); len(serverID) > 0 && !bytes.Equal(serverID, h.serverDUID) {
		return fmt.Errorf("server identifier mismatch")
	}
	return nil
}

func (h *Handler) buildLeaseRecord(req Packet, alloc AllocationResult, relayAddr net.IP, state lease.LeaseState) lease.Lease {
	now := time.Now().UTC()
	return lease.Lease{
		IP:         alloc.IP.String(),
		ClientID:   req.Options.ClientID(),
		State:      state,
		StartTime:  now,
		Duration:   alloc.LeaseDuration,
		ExpiryTime: now.Add(alloc.LeaseDuration),
		SubnetID:   alloc.SubnetID,
		RelayAddr:  relayAddr.String(),
		LastSeen:   now,
	}
}

func firstIANA(items []IANA) IANA {
	if len(items) == 0 {
		return IANA{}
	}
	return items[0]
}

func requestedIAAddr(iana IANA) net.IP {
	for _, item := range iana.Options {
		if item.Code != OptionIAAddr {
			continue
		}
		addr, err := DecodeIAAddress(item.Value)
		if err == nil {
			return addr.Address
		}
	}
	return nil
}
