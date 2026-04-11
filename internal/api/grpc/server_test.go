package grpc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	restapi "github.com/monsoondhcp/monsoon/internal/api/rest"
	"github.com/monsoondhcp/monsoon/internal/auth"
	"github.com/monsoondhcp/monsoon/internal/discovery"
	"github.com/monsoondhcp/monsoon/internal/events"
	"github.com/monsoondhcp/monsoon/internal/ipam"
	"github.com/monsoondhcp/monsoon/internal/lease"
	"github.com/monsoondhcp/monsoon/internal/storage"
)

func TestGRPCUnarySubnetAndAddressRPCs(t *testing.T) {
	stack := newTestStack(t)
	defer stack.close(t)

	payload, trailer, protoMajor := stack.invokeUnary(t, "/monsoon.v1.SubnetService/CreateSubnet", subnetMutationRequest{
		CIDR:       "10.0.1.0/24",
		Name:       "Users",
		VLAN:       100,
		Gateway:    "10.0.1.1",
		DNS:        []string{"1.1.1.1", "8.8.8.8"},
		DHCPEnable: true,
		PoolStart:  "10.0.1.10",
		PoolEnd:    "10.0.1.20",
		LeaseSec:   3600,
	})
	if protoMajor != 2 {
		t.Fatalf("expected HTTP/2, got %d", protoMajor)
	}
	if got := trailer.Get("Grpc-Status"); got != "0" {
		t.Fatalf("expected grpc success, got %q", got)
	}
	var created subnetMessage
	if err := created.unmarshalProto(payload); err != nil {
		t.Fatalf("unmarshal subnet: %v", err)
	}
	if created.CIDR != "10.0.1.0/24" || created.Name != "Users" {
		t.Fatalf("unexpected subnet response: %+v", created)
	}

	payload, trailer, _ = stack.invokeUnary(t, "/monsoon.v1.SubnetService/GetSubnet", cidrRequest{CIDR: "10.0.1.0/24"})
	if got := trailer.Get("Grpc-Status"); got != "0" {
		t.Fatalf("expected grpc success, got %q", got)
	}
	var gotSubnet subnetMessage
	if err := gotSubnet.unmarshalProto(payload); err != nil {
		t.Fatalf("unmarshal get subnet: %v", err)
	}
	if gotSubnet.Gateway != "10.0.1.1" || gotSubnet.VLAN != 100 {
		t.Fatalf("unexpected get subnet response: %+v", gotSubnet)
	}

	payload, trailer, _ = stack.invokeUnary(t, "/monsoon.v1.AddressService/NextAvailable", nextAvailableRequest{SubnetCIDR: "10.0.1.0/24"})
	if got := trailer.Get("Grpc-Status"); got != "0" {
		t.Fatalf("expected grpc success, got %q", got)
	}
	var addr ipAddressMessage
	if err := addr.unmarshalProto(payload); err != nil {
		t.Fatalf("unmarshal address: %v", err)
	}
	if addr.IP != "10.0.1.10" || addr.State != string(ipam.IPStateAvailable) {
		t.Fatalf("unexpected address response: %+v", addr)
	}
}

func TestGRPCStreams(t *testing.T) {
	stack := newTestStack(t)
	defer stack.close(t)

	_, err := stack.ipam.UpsertSubnet(context.Background(), ipam.UpsertSubnetInput{
		CIDR:       "10.0.2.0/24",
		Name:       "Lab",
		DHCPEnable: true,
		PoolStart:  "10.0.2.10",
		PoolEnd:    "10.0.2.20",
		LeaseSec:   3600,
	})
	if err != nil {
		t.Fatalf("seed subnet: %v", err)
	}
	if err := stack.leaseStore.Upsert(context.Background(), lease.Lease{
		IP:         "10.0.2.11",
		MAC:        "AA:BB:CC:DD:EE:FF",
		Hostname:   "client-1",
		State:      lease.StateBound,
		SubnetID:   "10.0.2.0/24",
		StartTime:  time.Now().UTC(),
		Duration:   time.Hour,
		ExpiryTime: time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed lease: %v", err)
	}

	leaseResp := stack.openStream(t, "/monsoon.v1.LeaseService/WatchLeases", watchLeasesRequest{SubnetCIDR: "10.0.2.0/24"})
	time.Sleep(100 * time.Millisecond)
	_, trailer, _ := stack.invokeUnary(t, "/monsoon.v1.LeaseService/ReleaseLease", ipRequest{IP: "10.0.2.11"})
	if got := trailer.Get("Grpc-Status"); got != "0" {
		t.Fatalf("expected grpc success, got %q", got)
	}
	frame, err := readFrame(leaseResp.Body)
	if err != nil {
		t.Fatalf("read lease stream frame: %v", err)
	}
	var leaseEvent leaseEventMessage
	if err := leaseEvent.unmarshalProto(frame); err != nil {
		t.Fatalf("unmarshal lease event: %v", err)
	}
	if leaseEvent.Type != "lease.released" || leaseEvent.IP != "10.0.2.11" {
		t.Fatalf("unexpected lease event: %+v", leaseEvent)
	}
	_ = leaseResp.Body.Close()

	discoveryResp := stack.openStream(t, "/monsoon.v1.DiscoveryService/WatchDiscovery", watchDiscoveryRequest{})
	time.Sleep(100 * time.Millisecond)
	stack.broker.Publish(events.Event{
		Type: "discovery.scan_completed",
		Data: map[string]any{
			"scan_id":     "scan-1",
			"subnet":      "10.0.2.0/24",
			"total_hosts": 5,
			"conflicts": []discovery.Conflict{{
				IP:       "10.0.2.99",
				MACs:     []string{"AA:AA:AA:AA:AA:AA", "BB:BB:BB:BB:BB:BB"},
				Severity: "high",
			}},
		},
	})
	frame, err = readFrame(discoveryResp.Body)
	if err != nil {
		t.Fatalf("read discovery completed frame: %v", err)
	}
	var completed discoveryEventMessage
	if err := completed.unmarshalProto(frame); err != nil {
		t.Fatalf("unmarshal discovery completed event: %v", err)
	}
	if completed.Type != "discovery.completed" || completed.ScanID != "scan-1" || completed.Found != 5 {
		t.Fatalf("unexpected completed event: %+v", completed)
	}

	frame, err = readFrame(discoveryResp.Body)
	if err != nil {
		t.Fatalf("read discovery conflict frame: %v", err)
	}
	var conflict discoveryEventMessage
	if err := conflict.unmarshalProto(frame); err != nil {
		t.Fatalf("unmarshal discovery conflict event: %v", err)
	}
	if conflict.Type != "discovery.conflict" || conflict.IP != "10.0.2.99" || len(conflict.MACs) != 2 {
		t.Fatalf("unexpected conflict event: %+v", conflict)
	}
	_ = discoveryResp.Body.Close()
}

func TestAuthorizeHonorsAuthEnforcement(t *testing.T) {
	if err := authorize(context.Background(), auth.DefaultRoleAdmin); err != nil {
		t.Fatalf("expected auth-disabled context to allow request, got %v", err)
	}

	err := authorize(restapi.WithAuthEnforcement(context.Background(), true), auth.DefaultRoleAdmin)
	if err == nil {
		t.Fatalf("expected enforced auth context to reject missing identity")
	}
	status, ok := err.(statusError)
	if !ok {
		t.Fatalf("expected statusError, got %T", err)
	}
	if status.code != codeUnauthenticated {
		t.Fatalf("expected unauthenticated code, got %d", status.code)
	}

	ctx := restapi.WithIdentity(restapi.WithAuthEnforcement(context.Background(), true), auth.Identity{
		Username: "viewer",
		Role:     auth.DefaultRoleViewer,
	})
	err = authorize(ctx, auth.DefaultRoleAdmin)
	if err == nil {
		t.Fatalf("expected viewer to be denied")
	}
	status, ok = err.(statusError)
	if !ok {
		t.Fatalf("expected statusError, got %T", err)
	}
	if status.code != codePermissionDenied {
		t.Fatalf("expected permission denied code, got %d", status.code)
	}
}

func TestGRPCSystemHealthRPCs(t *testing.T) {
	stack := newTestStack(t)
	defer stack.close(t)

	payload, trailer, _ := stack.invokeUnary(t, "/monsoon.v1.SystemService/GetHealth", emptyMessage{})
	if got := trailer.Get("Grpc-Status"); got != "0" {
		t.Fatalf("expected grpc success, got %q", got)
	}
	var health systemHealthResponse
	if err := health.unmarshalProto(payload); err != nil {
		t.Fatalf("unmarshal health response: %v", err)
	}
	if health.Status != "healthy" || !health.Ready {
		t.Fatalf("unexpected health response: %+v", health)
	}
	payloadMap := map[string]any{}
	if err := json.Unmarshal([]byte(health.PayloadJSON), &payloadMap); err != nil {
		t.Fatalf("decode payload json: %v", err)
	}
	if payloadMap["status"] != "healthy" {
		t.Fatalf("unexpected payload status: %+v", payloadMap)
	}

	stack.handlerDeps.DHCPv4Enabled = true
	stack.handlerDeps.DHCPv4Listen = ":67"
	stack.handlerDeps.DHCPv4Running = func() bool { return false }
	stack.rebuildHandler()

	payload, trailer, _ = stack.invokeUnary(t, "/monsoon.v1.SystemService/GetReadiness", emptyMessage{})
	if got := trailer.Get("Grpc-Status"); got != "0" {
		t.Fatalf("expected grpc success for readiness rpc, got %q", got)
	}
	var readiness systemHealthResponse
	if err := readiness.unmarshalProto(payload); err != nil {
		t.Fatalf("unmarshal readiness response: %v", err)
	}
	if readiness.Status != "degraded" || readiness.Ready {
		t.Fatalf("unexpected readiness response: %+v", readiness)
	}
}

type testStack struct {
	server      *Server
	baseURL     string
	client      *http.Client
	engine      *storage.Engine
	ipam        *ipam.Engine
	leaseStore  lease.Store
	broker      *events.Broker
	handlerDeps HandlerDeps
}

func newTestStack(t *testing.T) *testStack {
	t.Helper()

	dir := t.TempDir()
	engine, err := storage.OpenEngine(dir, []string{
		"leases",
		"leases_by_mac",
		"leases_by_expiry",
		"leases_by_subnet",
		"leases_by_client",
		"subnets",
		"addresses",
		"reservations",
		"discovery_scans",
		"discovery_meta",
	})
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	leaseStore := lease.NewStore(engine)
	ipamEngine := ipam.NewEngine(engine, leaseStore)
	discoveryEngine := discovery.NewEngine(engine, leaseStore, ipamEngine, time.Hour)
	broker := events.NewBroker(16)

	handlerDeps := HandlerDeps{
		LeaseStore:       leaseStore,
		IPAMEngine:       ipamEngine,
		DiscoveryEngine:  discoveryEngine,
		DiscoveryEnabled: true,
		Version:          "test",
		StartedAt:        time.Now().UTC().Add(-time.Minute),
		StorageReady: func(ctx context.Context) error {
			return engine.Tx(func(tx *storage.Tx) error {
				return ctx.Err()
			})
		},
		EventBroker: broker,
	}
	handler := NewHandler(handlerDeps).Handler()
	server := NewServer("127.0.0.1:0", handler)
	go func() {
		_ = server.Start()
	}()

	for i := 0; i < 50; i++ {
		if server.listener != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if server.listener == nil {
		t.Fatal("grpc server did not start")
	}

	protocols := &http.Protocols{}
	protocols.SetUnencryptedHTTP2(true)
	client := &http.Client{
		Transport: &http.Transport{
			Protocols:             protocols,
			ResponseHeaderTimeout: 5 * time.Second,
		},
	}

	return &testStack{
		server:      server,
		baseURL:     "http://" + server.listener.Addr().String(),
		client:      client,
		engine:      engine,
		ipam:        ipamEngine,
		leaseStore:  leaseStore,
		broker:      broker,
		handlerDeps: handlerDeps,
	}
}

func (s *testStack) rebuildHandler() {
	s.server.httpServer.Handler = NewHandler(s.handlerDeps).Handler()
}

func (s *testStack) close(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.server.Shutdown(ctx)
	if err := s.engine.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}
}

func (s *testStack) invokeUnary(t *testing.T, path string, req protoMarshaler) ([]byte, http.Header, int) {
	t.Helper()
	httpReq, err := http.NewRequest(http.MethodPost, s.baseURL+path, bytes.NewReader(encodeGRPCFrame(req.marshalProto())))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/grpc+proto")
	resp, err := s.client.Do(httpReq)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	payload := []byte(nil)
	if len(body) > 0 {
		payload, err = decodeGRPCFrame(body)
		if err != nil {
			t.Fatalf("decode grpc frame: %v", err)
		}
	}
	return payload, resp.Trailer, resp.ProtoMajor
}

func (s *testStack) openStream(t *testing.T, path string, req protoMarshaler) *http.Response {
	t.Helper()
	httpReq, err := http.NewRequest(http.MethodPost, s.baseURL+path, bytes.NewReader(encodeGRPCFrame(req.marshalProto())))
	if err != nil {
		t.Fatalf("new stream request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/grpc+proto")

	type result struct {
		resp *http.Response
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := s.client.Do(httpReq)
		done <- result{resp: resp, err: err}
	}()

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("open stream: %v", res.err)
		}
		return res.resp
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not establish")
		return nil
	}
}

func readFrame(r io.Reader) ([]byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	length := int(uint32(header[1])<<24 | uint32(header[2])<<16 | uint32(header[3])<<8 | uint32(header[4]))
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}
