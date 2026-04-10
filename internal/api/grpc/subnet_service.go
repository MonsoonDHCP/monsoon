package grpc

import (
	"context"
	"strings"

	"github.com/monsoondhcp/monsoon/internal/auth"
	"github.com/monsoondhcp/monsoon/internal/events"
	"github.com/monsoondhcp/monsoon/internal/ipam"
)

func (h *Handler) registerSubnetService() {
	h.methods["/monsoon.v1.SubnetService/ListSubnets"] = methodDesc{
		requiredRole: auth.DefaultRoleViewer,
		decode:       decodeEmpty,
		unary:        h.listSubnets,
	}
	h.methods["/monsoon.v1.SubnetService/CreateSubnet"] = methodDesc{
		requiredRole: auth.DefaultRoleOperator,
		decode:       decodeSubnetMutationRequest,
		unary:        h.createSubnet,
	}
	h.methods["/monsoon.v1.SubnetService/GetSubnet"] = methodDesc{
		requiredRole: auth.DefaultRoleViewer,
		decode:       decodeCIDRRequest,
		unary:        h.getSubnet,
	}
	h.methods["/monsoon.v1.SubnetService/UpdateSubnet"] = methodDesc{
		requiredRole: auth.DefaultRoleOperator,
		decode:       decodeSubnetMutationRequest,
		unary:        h.updateSubnet,
	}
	h.methods["/monsoon.v1.SubnetService/DeleteSubnet"] = methodDesc{
		requiredRole: auth.DefaultRoleOperator,
		decode:       decodeCIDRRequest,
		unary:        h.deleteSubnet,
	}
	h.methods["/monsoon.v1.SubnetService/GetSubnetUtilization"] = methodDesc{
		requiredRole: auth.DefaultRoleViewer,
		decode:       decodeCIDRRequest,
		unary:        h.getSubnetUtilization,
	}
}

func (h *Handler) listSubnets(ctx context.Context, _ any) (protoMarshaler, error) {
	if h.deps.IPAMEngine == nil {
		return nil, grpcError(codeFailedPrecondition, "ipam engine is not configured")
	}
	subnets, err := h.deps.IPAMEngine.ListSubnets(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]subnetMessage, 0, len(subnets))
	for _, item := range subnets {
		items = append(items, newSubnetMessage(item))
	}
	return listSubnetsResponse{Items: items}, nil
}

func (h *Handler) createSubnet(ctx context.Context, raw any) (protoMarshaler, error) {
	return h.upsertSubnet(ctx, raw.(subnetMutationRequest))
}

func (h *Handler) updateSubnet(ctx context.Context, raw any) (protoMarshaler, error) {
	return h.upsertSubnet(ctx, raw.(subnetMutationRequest))
}

func (h *Handler) upsertSubnet(ctx context.Context, req subnetMutationRequest) (protoMarshaler, error) {
	if h.deps.IPAMEngine == nil {
		return nil, grpcError(codeFailedPrecondition, "ipam engine is not configured")
	}
	result, err := h.deps.IPAMEngine.UpsertSubnet(ctx, ipam.UpsertSubnetInput{
		CIDR:       req.CIDR,
		Name:       req.Name,
		VLAN:       req.VLAN,
		Gateway:    req.Gateway,
		DNS:        append([]string(nil), req.DNS...),
		DHCPEnable: req.DHCPEnable,
		PoolStart:  req.PoolStart,
		PoolEnd:    req.PoolEnd,
		LeaseSec:   req.LeaseSec,
	})
	if err != nil {
		return nil, grpcError(codeInvalidArgument, err.Error())
	}
	if h.deps.EventBroker != nil {
		h.deps.EventBroker.Publish(events.Event{Type: "subnet.upserted", Data: map[string]any{"cidr": result.CIDR, "name": result.Name}})
	}
	msg := newSubnetMessage(result)
	return msg, nil
}

func (h *Handler) getSubnet(ctx context.Context, raw any) (protoMarshaler, error) {
	if h.deps.IPAMEngine == nil {
		return nil, grpcError(codeFailedPrecondition, "ipam engine is not configured")
	}
	req := raw.(cidrRequest)
	item, err := h.deps.IPAMEngine.GetSubnet(ctx, req.CIDR)
	if err != nil {
		return nil, grpcError(codeNotFound, "subnet not found")
	}
	msg := newSubnetMessage(item)
	return msg, nil
}

func (h *Handler) deleteSubnet(ctx context.Context, raw any) (protoMarshaler, error) {
	if h.deps.IPAMEngine == nil {
		return nil, grpcError(codeFailedPrecondition, "ipam engine is not configured")
	}
	req := raw.(cidrRequest)
	cidr := strings.TrimSpace(req.CIDR)
	if cidr == "" {
		return nil, grpcError(codeInvalidArgument, "cidr is required")
	}
	if err := h.deps.IPAMEngine.DeleteSubnet(ctx, cidr); err != nil {
		return nil, grpcError(codeInternal, err.Error())
	}
	if h.deps.EventBroker != nil {
		h.deps.EventBroker.Publish(events.Event{Type: "subnet.deleted", Data: map[string]any{"cidr": cidr}})
	}
	return emptyMessage{}, nil
}

func (h *Handler) getSubnetUtilization(ctx context.Context, raw any) (protoMarshaler, error) {
	if h.deps.IPAMEngine == nil {
		return nil, grpcError(codeFailedPrecondition, "ipam engine is not configured")
	}
	req := raw.(cidrRequest)
	target := strings.TrimSpace(req.CIDR)
	if target == "" {
		return nil, grpcError(codeInvalidArgument, "cidr is required")
	}
	items, err := h.deps.IPAMEngine.ListSummaries(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.CIDR != target {
			continue
		}
		return utilizationResponse{
			CIDR:         item.CIDR,
			Name:         item.Name,
			ActiveLeases: item.ActiveLeases,
			TotalLeases:  item.TotalLeases,
			Utilization:  item.Utilization,
		}, nil
	}
	return nil, grpcError(codeNotFound, "subnet not found")
}
