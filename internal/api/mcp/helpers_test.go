package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/monsoondhcp/monsoon/internal/ipam"
	"github.com/monsoondhcp/monsoon/internal/lease"
)

func TestToolResultAndArgumentHelpers(t *testing.T) {
	content := ensureContent(CallToolResult{})
	if len(content.Content) != 1 || content.Content[0].Text != "ok" {
		t.Fatalf("ensureContent failed: %+v", content)
	}
	result := toolResultJSON(map[string]any{"status": "ok"})
	if result.StructuredContent["status"] != "ok" || !strings.Contains(result.Content[0].Text, "\"status\": \"ok\"") {
		t.Fatalf("toolResultJSON mismatch: %+v", result)
	}
	errResult := toolError(errors.New("boom"))
	if !errResult.IsError || errResult.StructuredContent["error"] != "boom" {
		t.Fatalf("toolError mismatch: %+v", errResult)
	}

	args := map[string]any{
		"name":      " monsoon ",
		"slice":     []any{" a ", "b"},
		"typed":     []string{" x ", "y"},
		"enabled":   true,
		"count":     json.Number("12"),
		"count64":   "34",
		"timestamp": "2026-01-02T03:04:05Z",
	}
	if value, err := requireString(args, "name"); err != nil || value != "monsoon" {
		t.Fatalf("requireString = %q err=%v", value, err)
	}
	if _, err := requireString(map[string]any{}, "missing"); err == nil {
		t.Fatalf("expected missing required string to fail")
	}
	if items, err := optionalStringSlice(args, "slice"); err != nil || len(items) != 2 || items[0] != "a" {
		t.Fatalf("optionalStringSlice(any) = %#v err=%v", items, err)
	}
	if items, err := optionalStringSlice(args, "typed"); err != nil || len(items) != 2 || items[0] != " x " {
		t.Fatalf("optionalStringSlice(string) = %#v err=%v", items, err)
	}
	if _, err := optionalStringSlice(map[string]any{"bad": []any{"ok", 7}}, "bad"); err == nil {
		t.Fatalf("expected invalid string slice error")
	}
	if value, err := optionalBool(args, "enabled", false); err != nil || !value {
		t.Fatalf("optionalBool = %v err=%v", value, err)
	}
	if _, err := optionalBool(map[string]any{"enabled": "true"}, "enabled", false); err == nil {
		t.Fatalf("expected invalid bool error")
	}
	if value, err := optionalInt(args, "count", 0); err != nil || value != 12 {
		t.Fatalf("optionalInt = %d err=%v", value, err)
	}
	if value := optionalIntOrZero(map[string]any{"count": "bad"}, "count"); value != 0 {
		t.Fatalf("optionalIntOrZero = %d, want 0", value)
	}
	if value, err := optionalInt64(args, "count64", 0); err != nil || value != 34 {
		t.Fatalf("optionalInt64 = %d err=%v", value, err)
	}
	if ts, err := optionalTime(args, "timestamp"); err != nil || ts.Format(time.RFC3339) != "2026-01-02T03:04:05Z" {
		t.Fatalf("optionalTime = %v err=%v", ts, err)
	}
	if _, err := optionalTime(map[string]any{"timestamp": "nope"}, "timestamp"); err == nil {
		t.Fatalf("expected invalid RFC3339 error")
	}
}

func TestPlanningAndSliceHelpers(t *testing.T) {
	if got := extractRequiredAddresses("need 500 addresses"); got != 500 {
		t.Fatalf("extractRequiredAddresses = %d, want 500", got)
	}
	if got := extractRequiredAddresses("no numbers here"); got != 0 {
		t.Fatalf("expected zero extracted addresses, got %d", got)
	}
	if bits := smallestIPv4Prefix(500); bits != 23 {
		t.Fatalf("smallestIPv4Prefix = %d, want 23", bits)
	}
	if bits := smallestIPv4Prefix(0); bits != -1 {
		t.Fatalf("smallestIPv4Prefix invalid input = %d, want -1", bits)
	}
	prefix := netip.MustParsePrefix("10.0.0.0/24")
	if usable := usableHosts(prefix); usable != 254 {
		t.Fatalf("usableHosts = %d, want 254", usable)
	}
	gw, poolStart, poolEnd := defaultPlanAddresses(prefix)
	if gw != "10.0.0.1" || poolStart != "10.0.0.10" || poolEnd != "10.0.0.254" {
		t.Fatalf("defaultPlanAddresses mismatch: %s %s %s", gw, poolStart, poolEnd)
	}
	small := netip.MustParsePrefix("10.0.0.0/31")
	gw, poolStart, poolEnd = defaultPlanAddresses(small)
	if gw != "" || poolStart != "" || poolEnd != "" {
		t.Fatalf("/30 plan should be empty, got %s %s %s", gw, poolStart, poolEnd)
	}
	if span := hostSpan(netip.MustParseAddr("10.0.0.10"), netip.MustParseAddr("10.0.0.12")); span != 3 {
		t.Fatalf("hostSpan = %d, want 3", span)
	}
	if count := prefixAddressCount(24); count != 256 {
		t.Fatalf("prefixAddressCount = %d, want 256", count)
	}
	if compareIPString("10.0.0.2", "10.0.0.10") >= 0 {
		t.Fatalf("expected numeric ip ordering")
	}
	if !isActiveLease(lease.StateBound) || isActiveLease(lease.StateReleased) {
		t.Fatalf("isActiveLease mismatch")
	}

	leases := []lease.Lease{{IP: "10.0.0.10"}}
	leasesCopy := leasesToAny(leases)
	leasesCopy[0].IP = "10.0.0.11"
	if leases[0].IP != "10.0.0.10" {
		t.Fatalf("leasesToAny should copy slice header")
	}
	addresses := []ipam.AddressRecord{{IP: "10.0.0.20"}}
	addressesCopy := addressesToAny(addresses)
	addressesCopy[0].IP = "10.0.0.21"
	if addresses[0].IP != "10.0.0.20" {
		t.Fatalf("addressesToAny should copy slice header")
	}
}

func TestServerProtocolAndOriginHelpers(t *testing.T) {
	var params toolCallParams
	raw, _ := json.Marshal(map[string]any{
		"name":      "tool",
		"arguments": map[string]any{"limit": 5},
	})
	if err := decodeParams(raw, &params); err != nil || params.Name != "tool" || params.Arguments["limit"].(json.Number).String() != "5" {
		t.Fatalf("decodeParams = %+v err=%v", params, err)
	}
	if err := decodeParams(nil, &params); err != nil {
		t.Fatalf("decodeParams empty should succeed: %v", err)
	}
	if version := negotiateProtocolVersion("2024-11-05"); version != defaultProtocolVersion {
		t.Fatalf("unexpected negotiated version: %s", version)
	}
	if version := (&session{}).protocolVersionOrDefault(); version != defaultProtocolVersion {
		t.Fatalf("unexpected default session protocol version: %s", version)
	}

	req := httptest.NewRequest("GET", "http://monsoon.local/sse", nil)
	req.Host = "monsoon.local:8080"
	req.Header.Set("Origin", "http://monsoon.local")
	if !allowOrigin(req) {
		t.Fatalf("same-host origin should be allowed")
	}
	req.Header.Set("Origin", "http://localhost:3000")
	if !allowOrigin(req) {
		t.Fatalf("localhost origin should be allowed")
	}
	req.Header.Set("Origin", "http://evil.example")
	if allowOrigin(req) {
		t.Fatalf("unexpected foreign origin allowance")
	}
	req.Header.Set("Origin", "://bad")
	if allowOrigin(req) {
		t.Fatalf("invalid origin should be rejected")
	}
	if host := requestHostName("monsoon.local:8080"); host != "monsoon.local" {
		t.Fatalf("requestHostName = %q, want monsoon.local", host)
	}
	if host := normalizeHost("[::1]"); host != "::1" {
		t.Fatalf("normalizeHost = %q, want ::1", host)
	}

	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)
	if err := writeSSE(writer, "message", "line-1\nline-2"); err != nil {
		t.Fatalf("writeSSE: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush sse: %v", err)
	}
	payload := buf.String()
	if !strings.Contains(payload, "event: message\n") || !strings.Contains(payload, "data: line-1\n") || !strings.Contains(payload, "data: line-2\n\n") {
		t.Fatalf("unexpected sse payload: %q", payload)
	}
}
