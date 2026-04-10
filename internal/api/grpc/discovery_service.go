package grpc

import (
	"context"
	"strings"

	wsapi "github.com/monsoondhcp/monsoon/internal/api/websocket"
	"github.com/monsoondhcp/monsoon/internal/auth"
	"github.com/monsoondhcp/monsoon/internal/discovery"
	"github.com/monsoondhcp/monsoon/internal/events"
)

func (h *Handler) registerDiscoveryService() {
	h.methods["/monsoon.v1.DiscoveryService/TriggerScan"] = methodDesc{
		requiredRole: auth.DefaultRoleOperator,
		decode:       decodeTriggerScanRequest,
		unary:        h.triggerScan,
	}
	h.methods["/monsoon.v1.DiscoveryService/GetConflicts"] = methodDesc{
		requiredRole: auth.DefaultRoleViewer,
		decode:       decodeGetConflictsRequest,
		unary:        h.getConflicts,
	}
	h.methods["/monsoon.v1.DiscoveryService/WatchDiscovery"] = methodDesc{
		requiredRole: auth.DefaultRoleViewer,
		decode:       decodeWatchDiscoveryRequest,
		stream:       h.watchDiscovery,
	}
}

func (h *Handler) triggerScan(ctx context.Context, raw any) (protoMarshaler, error) {
	if h.deps.DiscoveryEngine == nil {
		return nil, grpcError(codeFailedPrecondition, "discovery engine is not configured")
	}
	req := raw.(triggerScanRequest)
	scanID, err := h.deps.DiscoveryEngine.TriggerScan(ctx, discovery.ScanRequest{
		Reason:  req.Reason,
		Subnets: append([]string(nil), req.Subnets...),
	})
	if err != nil {
		return nil, grpcError(codeFailedPrecondition, err.Error())
	}
	if h.deps.EventBroker != nil {
		h.deps.EventBroker.Publish(events.Event{Type: "discovery.scan_queued", Data: map[string]any{"scan_id": scanID}})
	}
	return scanResponse{
		Status:      "queued",
		ScanID:      scanID,
		EstimatedIn: "15s",
	}, nil
}

func (h *Handler) getConflicts(ctx context.Context, _ any) (protoMarshaler, error) {
	if h.deps.DiscoveryEngine == nil {
		return nil, grpcError(codeFailedPrecondition, "discovery engine is not configured")
	}
	conflicts, err := h.deps.DiscoveryEngine.LatestConflicts(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]conflictMessage, 0, len(conflicts))
	for _, item := range conflicts {
		items = append(items, newConflictMessage(item))
	}
	return conflictsResponse{Items: items}, nil
}

func (h *Handler) watchDiscovery(ctx context.Context, _ any, stream *serverStream) error {
	if h.deps.EventBroker == nil {
		return grpcError(codeFailedPrecondition, "event broker is not configured")
	}
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
				if !strings.HasPrefix(item.Type, "discovery.") {
					continue
				}
				if strings.Contains(item.Type, "scan_") {
					continue
				}
				msg := discoveryEventMessage{
					Type:       item.Type,
					ScanID:     mustString(item.Data["scan_id"]),
					Subnet:     firstNonEmpty(mustString(item.Data["subnet"]), mustString(item.Data["subnet_cidr"])),
					IP:         mustString(item.Data["ip"]),
					Found:      firstNonZero(mustInt(item.Data["found"]), mustInt(item.Data["total_hosts"])),
					MACs:       mustStrings(item.Data["macs"]),
					OccurredAt: item.Timestamp.Unix(),
					Note:       mustString(item.Data["note"]),
				}
				if err := stream.Send(msg); err != nil {
					return err
				}
			}
		}
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
