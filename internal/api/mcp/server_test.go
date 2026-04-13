package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	restapi "github.com/monsoondhcp/monsoon/internal/api/rest"
	"github.com/monsoondhcp/monsoon/internal/audit"
	"github.com/monsoondhcp/monsoon/internal/auth"
	"github.com/monsoondhcp/monsoon/internal/discovery"
	"github.com/monsoondhcp/monsoon/internal/events"
	"github.com/monsoondhcp/monsoon/internal/ipam"
	"github.com/monsoondhcp/monsoon/internal/lease"
	"github.com/monsoondhcp/monsoon/internal/storage"
)

func TestMCPInitializeListAndCall(t *testing.T) {
	server := newTestMCPServer(t)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	initialize := postRPC(t, ts.URL+messagePath, rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: mustRawJSON(t, map[string]any{
			"protocolVersion": "2025-06-18",
			"clientInfo": map[string]any{
				"name": "test-client",
			},
		}),
	})
	if initialize.Error != nil {
		t.Fatalf("initialize error: %+v", initialize.Error)
	}
	resultMap := decodeResultMap(t, initialize.Result)
	if got := resultMap["protocolVersion"]; got != defaultProtocolVersion {
		t.Fatalf("protocol version mismatch: %v", got)
	}

	toolList := postRPC(t, ts.URL+messagePath, rpcRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/list",
	})
	if toolList.Error != nil {
		t.Fatalf("tools/list error: %+v", toolList.Error)
	}
	toolsResult := decodeResultMap(t, toolList.Result)
	tools, ok := toolsResult["tools"].([]any)
	if !ok || len(tools) != 15 {
		t.Fatalf("expected 15 tools, got %#v", toolsResult["tools"])
	}

	call := postRPC(t, ts.URL+messagePath, rpcRequest{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "tools/call",
		Params: mustRawJSON(t, map[string]any{
			"name": "monsoon_find_available_ip",
			"arguments": map[string]any{
				"subnet_cidr": "10.20.0.0/24",
			},
		}),
	})
	if call.Error != nil {
		t.Fatalf("tool call error: %+v", call.Error)
	}
	callResult := decodeCallResult(t, call.Result)
	if callResult.IsError {
		t.Fatalf("tool returned error: %+v", callResult)
	}
	if callResult.StructuredContent["available_ip"] != "10.20.0.10" {
		t.Fatalf("unexpected available ip: %#v", callResult.StructuredContent)
	}
}

func TestMCPReserveSearchPlanAndAudit(t *testing.T) {
	server := newTestMCPServer(t)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	reserve := postRPC(t, ts.URL+messagePath, rpcRequest{
		JSONRPC: "2.0",
		ID:      4,
		Method:  "tools/call",
		Params: mustRawJSON(t, map[string]any{
			"name": "monsoon_reserve_ip",
			"arguments": map[string]any{
				"mac":         "AA:BB:CC:DD:EE:99",
				"ip":          "10.20.0.99",
				"hostname":    "printer-99",
				"subnet_cidr": "10.20.0.0/24",
			},
		}),
	})
	if reserve.Error != nil {
		t.Fatalf("reserve error: %+v", reserve.Error)
	}
	reserveResult := decodeCallResult(t, reserve.Result)
	if reserveResult.IsError {
		t.Fatalf("reservation failed: %+v", reserveResult)
	}

	search := postRPC(t, ts.URL+messagePath, rpcRequest{
		JSONRPC: "2.0",
		ID:      5,
		Method:  "tools/call",
		Params: mustRawJSON(t, map[string]any{
			"name": "monsoon_search_by_mac",
			"arguments": map[string]any{
				"mac": "AA:BB:CC:DD:EE:99",
			},
		}),
	})
	if search.Error != nil {
		t.Fatalf("search error: %+v", search.Error)
	}
	searchResult := decodeCallResult(t, search.Result)
	if found, _ := searchResult.StructuredContent["found"].(bool); !found {
		t.Fatalf("expected search hit: %+v", searchResult.StructuredContent)
	}

	plan := postRPC(t, ts.URL+messagePath, rpcRequest{
		JSONRPC: "2.0",
		ID:      6,
		Method:  "tools/call",
		Params: mustRawJSON(t, map[string]any{
			"name": "monsoon_plan_subnet",
			"arguments": map[string]any{
				"required_addresses": 500,
				"name":               "IoT",
			},
		}),
	})
	if plan.Error != nil {
		t.Fatalf("plan error: %+v", plan.Error)
	}
	planResult := decodeCallResult(t, plan.Result)
	if planResult.StructuredContent["suggested_cidr"] == "" {
		t.Fatalf("expected suggested cidr: %+v", planResult.StructuredContent)
	}

	auditQuery := postRPC(t, ts.URL+messagePath, rpcRequest{
		JSONRPC: "2.0",
		ID:      7,
		Method:  "tools/call",
		Params: mustRawJSON(t, map[string]any{
			"name": "monsoon_audit_query",
			"arguments": map[string]any{
				"limit": 10,
			},
		}),
	})
	if auditQuery.Error != nil {
		t.Fatalf("audit query error: %+v", auditQuery.Error)
	}
	auditResult := decodeCallResult(t, auditQuery.Result)
	if count, _ := auditResult.StructuredContent["count"].(float64); count < 1 {
		t.Fatalf("expected audit entries: %+v", auditResult.StructuredContent)
	}
}

func TestMCPHealthToolReflectsReadiness(t *testing.T) {
	server := NewServer(HandlerDeps{
		Version:          "test",
		StartedAt:        time.Now().UTC().Add(-time.Minute),
		DiscoveryEnabled: true,
		StorageReady: func(context.Context) error {
			return nil
		},
		DHCPv4Enabled: true,
		DHCPv4Listen:  ":67",
		DHCPv4Running: func() bool {
			return false
		},
		MCPListen: ":7067",
	})
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	call := postRPC(t, ts.URL+messagePath, rpcRequest{
		JSONRPC: "2.0",
		ID:      11,
		Method:  "tools/call",
		Params: mustRawJSON(t, map[string]any{
			"name":      "monsoon_get_health",
			"arguments": map[string]any{},
		}),
	})
	if call.Error != nil {
		t.Fatalf("health tool call error: %+v", call.Error)
	}
	result := decodeCallResult(t, call.Result)
	if result.IsError {
		t.Fatalf("health tool returned error: %+v", result)
	}
	if result.StructuredContent["status"] != "degraded" {
		t.Fatalf("expected degraded mcp health status, got %+v", result.StructuredContent)
	}
	if ready, _ := result.StructuredContent["ready"].(bool); ready {
		t.Fatalf("expected mcp readiness false, got %+v", result.StructuredContent)
	}
}

func TestMCPSSEEndpointAndQueuedResponse(t *testing.T) {
	server := newTestMCPServer(t)
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + ssePath)
	if err != nil {
		t.Fatalf("open sse: %v", err)
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	event, data := readSSEEvent(t, reader)
	if event != "endpoint" {
		t.Fatalf("unexpected first event: %s", event)
	}
	if !strings.Contains(data, "session_id=") {
		t.Fatalf("missing session_id in endpoint: %s", data)
	}

	reqBody, _ := json.Marshal(rpcRequest{
		JSONRPC: "2.0",
		ID:      9,
		Method:  "initialize",
		Params:  mustRawJSON(t, map[string]any{"protocolVersion": "2025-06-18"}),
	})
	msgResp, err := http.Post(ts.URL+data, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("post queued initialize: %v", err)
	}
	defer msgResp.Body.Close()
	if msgResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", msgResp.StatusCode)
	}

	event, data = readSSEEvent(t, reader)
	if event != "message" {
		t.Fatalf("unexpected queued event: %s", event)
	}
	var queued rpcResponse
	if err := json.Unmarshal([]byte(data), &queued); err != nil {
		t.Fatalf("decode queued response: %v", err)
	}
	if queued.Error != nil {
		t.Fatalf("queued response error: %+v", queued.Error)
	}
}

func TestMCPMutationRespectsRole(t *testing.T) {
	server := newTestMCPServer(t)

	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "auth-storage"), []string{"users", "api_tokens", "api_tokens_by_hash"})
	if err != nil {
		t.Fatalf("open auth storage: %v", err)
	}
	defer eng.Close()

	authService := auth.NewService(eng, auth.ServiceOptions{CookieName: "monsoon_session", SessionDuration: time.Hour})
	if err := authService.BootstrapAdmin(context.Background(), "admin", "admin"); err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}
	_, token, err := authService.CreateToken(context.Background(), "viewer", auth.DefaultRoleViewer, nil, "viewer token")
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	handler := restapi.Chain(server.Handler(), restapi.AuthMiddlewareFunc(authService, func() bool { return true }))
	ts := httptest.NewServer(handler)
	defer ts.Close()

	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      10,
		Method:  "tools/call",
		Params: mustRawJSON(t, map[string]any{
			"name": "monsoon_reserve_ip",
			"arguments": map[string]any{
				"mac":         "AA:BB:CC:DD:EE:22",
				"ip":          "10.20.0.22",
				"subnet_cidr": "10.20.0.0/24",
			},
		}),
	}

	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequest(http.MethodPost, ts.URL+messagePath, bytes.NewReader(body))
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("viewer token request failed: %v", err)
	}
	defer httpResp.Body.Close()
	var respBody rpcResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&respBody); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	callResult := decodeCallResult(t, respBody.Result)
	if !callResult.IsError {
		t.Fatalf("expected mutation denial, got %+v", callResult)
	}
}

func TestRequireRoleHonorsAuthEnforcement(t *testing.T) {
	if err := requireRole(context.Background(), auth.DefaultRoleAdmin); err != nil {
		t.Fatalf("expected auth-disabled context to allow request, got %v", err)
	}

	err := requireRole(restapi.WithAuthEnforcement(context.Background(), true), auth.DefaultRoleAdmin)
	if err == nil || !strings.Contains(err.Error(), "authentication required") {
		t.Fatalf("expected authentication error, got %v", err)
	}

	ctx := authenticatedContextForTest(t, auth.DefaultRoleViewer)
	err = requireRole(ctx, auth.DefaultRoleAdmin)
	if err == nil || !strings.Contains(err.Error(), "admin role required") {
		t.Fatalf("expected role error, got %v", err)
	}
}

func newTestMCPServer(t *testing.T) *Server {
	t.Helper()

	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "storage"), []string{
		"subnets",
		"addresses",
		"reservations",
		"leases",
		"audit",
		"discovery_scans",
		"discovery_meta",
	})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	leaseStore := lease.NewStore(eng)
	ipamEngine := ipam.NewEngine(eng, leaseStore)
	if _, err := ipamEngine.UpsertSubnet(context.Background(), ipam.UpsertSubnetInput{
		CIDR:       "10.20.0.0/24",
		Name:       "Servers",
		VLAN:       20,
		Gateway:    "10.20.0.1",
		DNS:        []string{"10.20.0.2"},
		DHCPEnable: true,
		PoolStart:  "10.20.0.10",
		PoolEnd:    "10.20.0.50",
		LeaseSec:   3600,
	}); err != nil {
		t.Fatalf("seed subnet: %v", err)
	}
	if err := leaseStore.Upsert(context.Background(), lease.Lease{
		IP:       "10.20.0.20",
		MAC:      "AA:BB:CC:DD:EE:20",
		Hostname: "app-20",
		State:    lease.StateBound,
		SubnetID: "10.20.0.0/24",
	}); err != nil {
		t.Fatalf("seed lease: %v", err)
	}

	discoveryEngine := discovery.NewEngineWithOptions(eng, leaseStore, ipamEngine, time.Hour, discovery.Options{})
	return NewServer(HandlerDeps{
		LeaseStore:      leaseStore,
		IPAMEngine:      ipamEngine,
		DiscoveryEngine: discoveryEngine,
		AuditLogger:     audit.NewLogger(eng),
		EventBroker:     events.NewBroker(8),
		Version:         "test",
		StartedAt:       time.Now().UTC().Add(-2 * time.Minute),
		DHCPv4Enabled:   true,
		DHCPv4Listen:    ":67",
		DHCPv4Running: func() bool {
			return true
		},
		MCPListen: ":7067",
	})
}

func postRPC(t *testing.T, endpoint string, req rpcRequest) rpcResponse {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal rpc request: %v", err)
	}
	// #nosec G107 -- endpoint is from httptest server under test control.
	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post rpc: %v", err)
	}
	defer resp.Body.Close()
	var out rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode rpc response: %v", err)
	}
	return out
}

func mustRawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal raw json: %v", err)
	}
	return raw
}

func decodeResultMap(t *testing.T, value any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal result map: %v", err)
	}
	return out
}

func decodeCallResult(t *testing.T, value any) CallToolResult {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal call result: %v", err)
	}
	var out CallToolResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal call result: %v", err)
	}
	return out
}

func readSSEEvent(t *testing.T, reader *bufio.Reader) (string, string) {
	t.Helper()
	type lineSet struct {
		event string
		data  []string
	}
	current := lineSet{}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read sse line: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if current.event != "" || len(current.data) > 0 {
				return current.event, strings.Join(current.data, "\n")
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event: ") {
			current.event = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			current.data = append(current.data, strings.TrimPrefix(line, "data: "))
		}
	}
	t.Fatal("timed out waiting for sse event")
	return "", ""
}

func TestDeleteSessionDoesNotCloseMessageChannel(t *testing.T) {
	server := NewServer(HandlerDeps{})
	sess := &session{
		id:       "session-1",
		messages: make(chan rpcResponse, 1),
	}
	server.setSession(sess)
	server.deleteSession(sess.id)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("sending to deleted session channel panicked: %v", r)
		}
	}()

	sess.messages <- rpcResponse{JSONRPC: "2.0"}
}

func authenticatedContextForTest(t *testing.T, role string) context.Context {
	t.Helper()
	eng, err := storage.OpenEngine(filepath.Join(t.TempDir(), "auth-storage"), []string{"users", "api_tokens", "api_tokens_by_hash"})
	if err != nil {
		t.Fatalf("open auth storage: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })

	service := auth.NewService(eng, auth.ServiceOptions{CookieName: "monsoon_session", SessionDuration: time.Hour})
	_, token, err := service.CreateToken(context.Background(), "test-"+role, role, nil, "test token")
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	var captured context.Context
	handler := restapi.AuthMiddlewareFunc(service, func() bool { return true })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Context()
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/internal/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent || captured == nil {
		t.Fatalf("failed to capture authenticated context, code=%d", rr.Code)
	}
	return captured
}
