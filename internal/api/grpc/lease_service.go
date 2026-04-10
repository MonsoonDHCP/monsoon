package grpc

import (
	"context"
	"strings"
	"time"

	wsapi "github.com/monsoondhcp/monsoon/internal/api/websocket"
	"github.com/monsoondhcp/monsoon/internal/auth"
	"github.com/monsoondhcp/monsoon/internal/events"
	"github.com/monsoondhcp/monsoon/internal/lease"
)

func (h *Handler) registerLeaseService() {
	h.methods["/monsoon.v1.LeaseService/ListLeases"] = methodDesc{
		requiredRole: auth.DefaultRoleViewer,
		decode:       decodeListLeasesRequest,
		unary:        h.listLeases,
	}
	h.methods["/monsoon.v1.LeaseService/GetLease"] = methodDesc{
		requiredRole: auth.DefaultRoleViewer,
		decode:       decodeIPRequest,
		unary:        h.getLease,
	}
	h.methods["/monsoon.v1.LeaseService/ReleaseLease"] = methodDesc{
		requiredRole: auth.DefaultRoleOperator,
		decode:       decodeIPRequest,
		unary:        h.releaseLease,
	}
	h.methods["/monsoon.v1.LeaseService/WatchLeases"] = methodDesc{
		requiredRole: auth.DefaultRoleViewer,
		decode:       decodeWatchLeasesRequest,
		stream:       h.watchLeases,
	}
}

func (h *Handler) listLeases(ctx context.Context, raw any) (protoMarshaler, error) {
	if h.deps.LeaseStore == nil {
		return nil, grpcError(codeFailedPrecondition, "lease store is not configured")
	}
	req := raw.(listLeasesRequest)
	leases, err := h.deps.LeaseStore.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	state := strings.ToLower(strings.TrimSpace(req.State))
	query := strings.ToLower(strings.TrimSpace(req.Query))
	items := make([]leaseMessage, 0, len(leases))
	for _, item := range leases {
		if req.SubnetCIDR != "" && item.SubnetID != req.SubnetCIDR {
			continue
		}
		if state != "" && strings.ToLower(string(item.State)) != state {
			continue
		}
		if query != "" {
			haystack := strings.ToLower(strings.Join([]string{item.IP, item.MAC, item.Hostname, item.SubnetID, string(item.State)}, " "))
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		items = append(items, newLeaseMessage(item))
	}
	if req.Limit > 0 && len(items) > req.Limit {
		items = items[:req.Limit]
	}
	return listLeasesResponse{Items: items}, nil
}

func (h *Handler) getLease(ctx context.Context, raw any) (protoMarshaler, error) {
	if h.deps.LeaseStore == nil {
		return nil, grpcError(codeFailedPrecondition, "lease store is not configured")
	}
	req := raw.(ipRequest)
	item, err := h.deps.LeaseStore.GetByIP(ctx, req.IP)
	if err != nil {
		return nil, grpcError(codeNotFound, "lease not found")
	}
	msg := newLeaseMessage(item)
	return msg, nil
}

func (h *Handler) releaseLease(ctx context.Context, raw any) (protoMarshaler, error) {
	if h.deps.LeaseStore == nil {
		return nil, grpcError(codeFailedPrecondition, "lease store is not configured")
	}
	req := raw.(ipRequest)
	item, err := h.deps.LeaseStore.GetByIP(ctx, req.IP)
	if err != nil {
		return nil, grpcError(codeNotFound, "lease not found")
	}
	now := time.Now().UTC()
	item.State = lease.StateReleased
	item.ExpiryTime = now
	item.UpdatedAt = now
	if err := h.deps.LeaseStore.Upsert(ctx, item); err != nil {
		return nil, grpcError(codeInternal, err.Error())
	}
	if h.deps.EventBroker != nil {
		h.deps.EventBroker.Publish(events.Event{Type: "lease.released", Data: map[string]any{"ip": item.IP, "subnet": item.SubnetID}})
	}
	return emptyMessage{}, nil
}

func (h *Handler) watchLeases(ctx context.Context, raw any, stream *serverStream) error {
	if h.deps.EventBroker == nil {
		return grpcError(codeFailedPrecondition, "event broker is not configured")
	}
	req := raw.(watchLeasesRequest)
	_, ch, unsubscribe := h.deps.EventBroker.Subscribe()
	defer unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return nil
		case evt, ok := <-ch:
			if !ok {
				return nil
			}
			for _, item := range wsapi.NormalizeEvent(evt) {
				if !strings.HasPrefix(item.Type, "lease.") {
					continue
				}
				if req.SubnetCIDR != "" && !leaseEventMatchesSubnet(req.SubnetCIDR, item.Data, h.deps.LeaseStore) {
					continue
				}
				msg := leaseEventMessage{
					Type:       item.Type,
					IP:         mustString(item.Data["ip"]),
					OccurredAt: item.Timestamp.Unix(),
				}
				if h.deps.LeaseStore != nil && msg.IP != "" {
					if l, err := h.deps.LeaseStore.GetByIP(context.Background(), msg.IP); err == nil {
						leaseMsg := newLeaseMessage(l)
						msg.Lease = &leaseMsg
					}
				}
				if err := stream.Send(msg); err != nil {
					return err
				}
			}
		}
	}
}

func leaseEventMatchesSubnet(target string, data map[string]any, store lease.Store) bool {
	if mustString(data["subnet"]) == target || mustString(data["subnet_cidr"]) == target {
		return true
	}
	if store == nil {
		return false
	}
	ip := mustString(data["ip"])
	if ip == "" {
		return false
	}
	item, err := store.GetByIP(context.Background(), ip)
	return err == nil && item.SubnetID == target
}
