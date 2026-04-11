package grpc

import (
	"context"
	"encoding/json"

	restapi "github.com/monsoondhcp/monsoon/internal/api/rest"
)

func (h *Handler) registerSystemService() {
	h.methods["/monsoon.v1.SystemService/GetHealth"] = methodDesc{
		decode: decodeEmpty,
		unary:  h.getHealth,
	}
	h.methods["/monsoon.v1.SystemService/GetReadiness"] = methodDesc{
		decode: decodeEmpty,
		unary:  h.getReadiness,
	}
}

func (h *Handler) getHealth(ctx context.Context, _ any) (protoMarshaler, error) {
	return h.systemHealth(ctx)
}

func (h *Handler) getReadiness(ctx context.Context, _ any) (protoMarshaler, error) {
	return h.systemHealth(ctx)
}

func (h *Handler) systemHealth(ctx context.Context) (protoMarshaler, error) {
	payload, ready := restapi.BuildSystemHealthPayload(ctx, restapi.RouterDeps{
		DiscoveryEngine:  h.deps.DiscoveryEngine,
		DiscoveryEnabled: h.deps.DiscoveryEnabled,
		Version:          h.deps.Version,
		StartedAt:        h.deps.StartedAt,
		StorageReady:     h.deps.StorageReady,
		DHCPv4Enabled:    h.deps.DHCPv4Enabled,
		DHCPv4Listen:     h.deps.DHCPv4Listen,
		DHCPv4Running:    h.deps.DHCPv4Running,
		HAEnabled:        h.deps.HAEnabled,
		HAStatus:         h.deps.HAStatus,
	})
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, grpcError(codeInternal, err.Error())
	}
	return systemHealthResponse{
		Status:      mustString(payload["status"]),
		Ready:       ready,
		Version:     mustString(payload["version"]),
		Uptime:      mustString(payload["uptime"]),
		PayloadJSON: string(raw),
	}, nil
}
