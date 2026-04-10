package grpc

import (
	"context"
	"strings"

	"github.com/monsoondhcp/monsoon/internal/auth"
	"github.com/monsoondhcp/monsoon/internal/events"
	"github.com/monsoondhcp/monsoon/internal/ipam"
)

func (h *Handler) registerAddressService() {
	h.methods["/monsoon.v1.AddressService/SearchAddresses"] = methodDesc{
		requiredRole: auth.DefaultRoleViewer,
		decode:       decodeSearchAddressesRequest,
		unary:        h.searchAddresses,
	}
	h.methods["/monsoon.v1.AddressService/GetAddress"] = methodDesc{
		requiredRole: auth.DefaultRoleViewer,
		decode:       decodeIPRequest,
		unary:        h.getAddress,
	}
	h.methods["/monsoon.v1.AddressService/ReserveAddress"] = methodDesc{
		requiredRole: auth.DefaultRoleOperator,
		decode:       decodeReserveAddressRequest,
		unary:        h.reserveAddress,
	}
	h.methods["/monsoon.v1.AddressService/ReleaseAddress"] = methodDesc{
		requiredRole: auth.DefaultRoleOperator,
		decode:       decodeIPRequest,
		unary:        h.releaseAddress,
	}
	h.methods["/monsoon.v1.AddressService/NextAvailable"] = methodDesc{
		requiredRole: auth.DefaultRoleViewer,
		decode:       decodeNextAvailableRequest,
		unary:        h.nextAvailable,
	}
}

func (h *Handler) searchAddresses(ctx context.Context, raw any) (protoMarshaler, error) {
	if h.deps.IPAMEngine == nil {
		return nil, grpcError(codeFailedPrecondition, "ipam engine is not configured")
	}
	req := raw.(searchAddressesRequest)
	records, err := h.deps.IPAMEngine.ListAddresses(ctx, ipam.AddressFilter{
		SubnetCIDR: req.SubnetCIDR,
		State:      ipam.IPState(req.State),
		Query:      req.Query,
		Limit:      req.Limit,
	})
	if err != nil {
		return nil, grpcError(codeInvalidArgument, err.Error())
	}
	items := make([]ipAddressMessage, 0, len(records))
	for _, item := range records {
		items = append(items, newIPAddressMessage(item))
	}
	return searchAddressesResponse{Items: items}, nil
}

func (h *Handler) getAddress(ctx context.Context, raw any) (protoMarshaler, error) {
	if h.deps.IPAMEngine == nil {
		return nil, grpcError(codeFailedPrecondition, "ipam engine is not configured")
	}
	req := raw.(ipRequest)
	record, err := h.deps.IPAMEngine.GetAddress(ctx, req.IP)
	if err != nil {
		return nil, grpcError(codeNotFound, "address not found")
	}
	msg := newIPAddressMessage(record)
	return msg, nil
}

func (h *Handler) reserveAddress(ctx context.Context, raw any) (protoMarshaler, error) {
	if h.deps.IPAMEngine == nil {
		return nil, grpcError(codeFailedPrecondition, "ipam engine is not configured")
	}
	req := raw.(reserveAddressRequest)
	if strings.TrimSpace(req.MAC) == "" {
		return nil, grpcError(codeInvalidArgument, "mac is required")
	}
	if strings.TrimSpace(req.IP) == "" {
		return nil, grpcError(codeInvalidArgument, "ip is required")
	}
	record, err := h.deps.IPAMEngine.UpsertReservation(ctx, ipam.UpsertReservationInput{
		MAC:        req.MAC,
		IP:         req.IP,
		Hostname:   req.Hostname,
		SubnetCIDR: req.SubnetCIDR,
	})
	if err != nil {
		return nil, grpcError(codeInvalidArgument, err.Error())
	}
	if h.deps.EventBroker != nil {
		h.deps.EventBroker.Publish(events.Event{Type: "reservation.upserted", Data: map[string]any{"mac": record.MAC, "ip": record.IP, "subnet": record.SubnetCIDR}})
	}
	current, err := h.deps.IPAMEngine.GetAddress(ctx, record.IP)
	if err != nil {
		return nil, err
	}
	msg := newIPAddressMessage(current)
	return msg, nil
}

func (h *Handler) releaseAddress(ctx context.Context, raw any) (protoMarshaler, error) {
	if h.deps.IPAMEngine == nil {
		return nil, grpcError(codeFailedPrecondition, "ipam engine is not configured")
	}
	req := raw.(ipRequest)
	record, err := h.deps.IPAMEngine.GetAddress(ctx, req.IP)
	if err != nil {
		return nil, grpcError(codeNotFound, "address not found")
	}
	if record.MAC != "" {
		_ = h.deps.IPAMEngine.DeleteReservation(ctx, record.MAC)
		if h.deps.EventBroker != nil {
			h.deps.EventBroker.Publish(events.Event{Type: "reservation.deleted", Data: map[string]any{"mac": record.MAC, "ip": record.IP}})
		}
	}
	_ = h.deps.IPAMEngine.DeleteStoredAddress(ctx, record.IP)
	return emptyMessage{}, nil
}

func (h *Handler) nextAvailable(ctx context.Context, raw any) (protoMarshaler, error) {
	if h.deps.IPAMEngine == nil {
		return nil, grpcError(codeFailedPrecondition, "ipam engine is not configured")
	}
	req := raw.(nextAvailableRequest)
	if strings.TrimSpace(req.SubnetCIDR) == "" {
		return nil, grpcError(codeInvalidArgument, "subnet_cidr is required")
	}
	subnet, err := h.deps.IPAMEngine.GetSubnet(ctx, req.SubnetCIDR)
	if err != nil {
		return nil, grpcError(codeNotFound, "subnet not found")
	}
	records, err := h.deps.IPAMEngine.ListAddresses(ctx, ipam.AddressFilter{SubnetCIDR: subnet.CIDR})
	if err != nil {
		return nil, grpcError(codeInvalidArgument, err.Error())
	}
	for _, item := range records {
		if item.State != ipam.IPStateAvailable {
			continue
		}
		msg := newIPAddressMessage(item)
		return msg, nil
	}
	return nil, grpcError(codeNotFound, "no available addresses in subnet")
}
