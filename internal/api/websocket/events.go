package websocket

import (
	"time"

	"github.com/monsoondhcp/monsoon/internal/discovery"
	"github.com/monsoondhcp/monsoon/internal/events"
)

type EventMessage struct {
	Type      string         `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	Data      map[string]any `json:"data,omitempty"`
}

func NormalizeEvent(evt events.Event) []EventMessage {
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now().UTC()
	}
	out := []EventMessage{{
		Type:      evt.Type,
		Timestamp: evt.Timestamp,
		Data:      cloneMap(evt.Data),
	}}

	switch evt.Type {
	case "subnet.upserted":
		out = append(out, EventMessage{
			Type:      "subnet.created",
			Timestamp: evt.Timestamp,
			Data:      cloneMap(evt.Data),
		})
	case "reservation.upserted":
		data := cloneMap(evt.Data)
		if _, ok := data["owner"]; !ok {
			data["owner"] = "system"
		}
		out = append(out, EventMessage{
			Type:      "address.reserved",
			Timestamp: evt.Timestamp,
			Data:      data,
		})
	case "discovery.scan_queued":
		out = append(out, EventMessage{
			Type:      "discovery.started",
			Timestamp: evt.Timestamp,
			Data:      cloneMap(evt.Data),
		})
	case "discovery.scan_completed":
		data := cloneMap(evt.Data)
		out = append(out, EventMessage{
			Type:      "discovery.completed",
			Timestamp: evt.Timestamp,
			Data: map[string]any{
				"scan_id": data["scan_id"],
				"found":   data["total_hosts"],
				"subnet":  firstString(data["subnet"], data["subnet_cidr"]),
			},
		})
		if rawConflicts, ok := data["conflicts"].([]discovery.Conflict); ok {
			for _, item := range rawConflicts {
				out = append(out, EventMessage{
					Type:      "discovery.conflict",
					Timestamp: evt.Timestamp,
					Data: map[string]any{
						"ip":   item.IP,
						"macs": append([]string(nil), item.MACs...),
					},
				})
			}
		}
		if count, ok := data["conflicts"].(int); ok && count > 0 {
			out = append(out, EventMessage{
				Type:      "discovery.conflict",
				Timestamp: evt.Timestamp,
				Data: map[string]any{
					"count": count,
				},
			})
		}
	}

	return out
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func firstString(values ...any) string {
	for _, value := range values {
		if s, ok := value.(string); ok && s != "" {
			return s
		}
	}
	return ""
}
