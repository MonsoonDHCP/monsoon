package grpc

import (
	"fmt"
	"testing"
	"time"

	"github.com/monsoondhcp/monsoon/internal/discovery"
	"github.com/monsoondhcp/monsoon/internal/ipam"
	"github.com/monsoondhcp/monsoon/internal/lease"
)

type stringerValue string

func (s stringerValue) String() string { return string(s) }

func TestGRPCMessageRoundTripsAndDecoders(t *testing.T) {
	health := systemHealthResponse{
		Status:      "ok",
		Ready:       true,
		Version:     "v1",
		Uptime:      "5m",
		PayloadJSON: `{"ready":true}`,
	}
	var decodedHealth systemHealthResponse
	if err := decodedHealth.unmarshalProto(health.marshalProto()); err != nil {
		t.Fatalf("unmarshal health: %v", err)
	}
	if decodedHealth.Status != health.Status || !decodedHealth.Ready || decodedHealth.PayloadJSON != health.PayloadJSON {
		t.Fatalf("unexpected health decode: %+v", decodedHealth)
	}

	subnetReq := subnetMutationRequest{
		CIDR:       " 10.0.0.0/24 ",
		Name:       " Users ",
		VLAN:       20,
		Gateway:    " 10.0.0.1 ",
		DNS:        []string{"1.1.1.1", "8.8.8.8"},
		DHCPEnable: true,
		PoolStart:  " 10.0.0.10 ",
		PoolEnd:    "10.0.0.50 ",
		LeaseSec:   3600,
	}
	value, err := decodeSubnetMutationRequest(subnetReq.marshalProto())
	if err != nil {
		t.Fatalf("decodeSubnetMutationRequest: %v", err)
	}
	decodedSubnetReq := value.(subnetMutationRequest)
	if decodedSubnetReq.CIDR != "10.0.0.0/24" || decodedSubnetReq.Name != "Users" || decodedSubnetReq.VLAN != 20 || len(decodedSubnetReq.DNS) != 2 || !decodedSubnetReq.DHCPEnable {
		t.Fatalf("unexpected subnet mutation decode: %+v", decodedSubnetReq)
	}

	subnetMsg := newSubnetMessage(ipam.Subnet{
		CIDR:    "10.0.0.0/24",
		Name:    "Users",
		VLAN:    20,
		Gateway: "10.0.0.1",
		DNS:     []string{"1.1.1.1"},
		DHCP: ipam.DHCPPool{
			Enabled:      true,
			PoolStart:    "10.0.0.10",
			PoolEnd:      "10.0.0.50",
			LeaseTimeSec: 3600,
		},
		CreatedAt: time.Unix(10, 0),
		UpdatedAt: time.Unix(20, 0),
	})
	var decodedSubnet subnetMessage
	if err := decodedSubnet.unmarshalProto(subnetMsg.marshalProto()); err != nil {
		t.Fatalf("unmarshal subnet: %v", err)
	}
	if decodedSubnet.CIDR != subnetMsg.CIDR || decodedSubnet.CreatedAt != 10 || !decodedSubnet.DHCP {
		t.Fatalf("unexpected subnet decode: %+v", decodedSubnet)
	}

	leaseMsg := newLeaseMessage(lease.Lease{
		IP:         "10.0.0.10",
		MAC:        "AA:BB",
		Hostname:   "host-1",
		State:      lease.StateBound,
		SubnetID:   "10.0.0.0/24",
		RelayAddr:  "10.0.0.1",
		ExpiryTime: time.Unix(30, 0),
		UpdatedAt:  time.Unix(40, 0),
		Duration:   time.Hour,
	})
	var decodedLease leaseMessage
	if err := decodedLease.unmarshalProto(leaseMsg.marshalProto()); err != nil {
		t.Fatalf("unmarshal lease: %v", err)
	}
	if decodedLease.IP != "10.0.0.10" || decodedLease.Duration != 3600 || decodedLease.State != "bound" {
		t.Fatalf("unexpected lease decode: %+v", decodedLease)
	}

	eventMsg := leaseEventMessage{Type: "lease.created", IP: "10.0.0.10", OccurredAt: 50, Lease: &leaseMsg}
	var decodedEvent leaseEventMessage
	if err := decodedEvent.unmarshalProto(eventMsg.marshalProto()); err != nil {
		t.Fatalf("unmarshal lease event: %v", err)
	}
	if decodedEvent.Lease == nil || decodedEvent.Lease.IP != "10.0.0.10" || decodedEvent.OccurredAt != 50 {
		t.Fatalf("unexpected lease event decode: %+v", decodedEvent)
	}

	searchReq := searchAddressesRequest{SubnetCIDR: "10.0.0.0/24", State: "dhcp", Query: "host", Limit: 5}
	value, err = decodeSearchAddressesRequest(searchReq.marshalProto())
	if err != nil {
		t.Fatalf("decodeSearchAddressesRequest: %v", err)
	}
	if got := value.(searchAddressesRequest); got.Query != "host" || got.Limit != 5 {
		t.Fatalf("unexpected search request decode: %+v", got)
	}

	reserveReq := reserveAddressRequest{IP: "10.0.0.10", SubnetCIDR: "10.0.0.0/24", MAC: "AA:BB", Hostname: "printer"}
	value, err = decodeReserveAddressRequest(reserveReq.marshalProto())
	if err != nil {
		t.Fatalf("decodeReserveAddressRequest: %v", err)
	}
	if got := value.(reserveAddressRequest); got.MAC != "AA:BB" || got.Hostname != "printer" {
		t.Fatalf("unexpected reserve request decode: %+v", got)
	}

	nextReq := nextAvailableRequest{SubnetCIDR: "10.0.0.0/24"}
	value, err = decodeNextAvailableRequest(nextReq.marshalProto())
	if err != nil {
		t.Fatalf("decodeNextAvailableRequest: %v", err)
	}
	if value.(nextAvailableRequest).SubnetCIDR != "10.0.0.0/24" {
		t.Fatalf("unexpected next available decode: %+v", value)
	}

	triggerReq := triggerScanRequest{Reason: "manual", Subnets: []string{"10.0.0.0/24", "10.1.0.0/24"}}
	value, err = decodeTriggerScanRequest(triggerReq.marshalProto())
	if err != nil {
		t.Fatalf("decodeTriggerScanRequest: %v", err)
	}
	if got := value.(triggerScanRequest); got.Reason != "manual" || len(got.Subnets) != 2 {
		t.Fatalf("unexpected trigger request decode: %+v", got)
	}

	conflictMsg := newConflictMessage(discovery.Conflict{IP: "10.0.0.10", MACs: []string{"AA", "BB"}, Severity: "high", Note: "dup"})
	discoveryMsg := discoveryEventMessage{Type: "conflict", ScanID: "scan-1", Subnet: "10.0.0.0/24", IP: "10.0.0.10", Found: 2, MACs: []string{"AA", "BB"}, OccurredAt: 60, Note: "dup"}
	var decodedDiscovery discoveryEventMessage
	if err := decodedDiscovery.unmarshalProto(discoveryMsg.marshalProto()); err != nil {
		t.Fatalf("unmarshal discovery event: %v", err)
	}
	if decodedDiscovery.ScanID != "scan-1" || len(decodedDiscovery.MACs) != 2 || conflictMsg.IP != "10.0.0.10" {
		t.Fatalf("unexpected discovery/conflict values: %+v %+v", decodedDiscovery, conflictMsg)
	}

	if _, err := decodeEmpty(nil); err != nil {
		t.Fatalf("decodeEmpty should succeed: %v", err)
	}
	if _, err := decodeCIDRRequest(cidrRequest{CIDR: " 10.0.0.0/24 "}.marshalProto()); err != nil {
		t.Fatalf("decodeCIDRRequest should succeed: %v", err)
	}
	var listLeasesRaw []byte
	listLeasesRaw = appendString(listLeasesRaw, 1, "10.0.0.0/24")
	listLeasesRaw = appendString(listLeasesRaw, 2, "bound")
	listLeasesRaw = appendString(listLeasesRaw, 3, "host")
	listLeasesRaw = appendInt64(listLeasesRaw, 4, 5)
	if _, err := decodeListLeasesRequest(listLeasesRaw); err != nil {
		t.Fatalf("decodeListLeasesRequest should succeed: %v", err)
	}
	if _, err := decodeIPRequest(ipRequest{IP: " 10.0.0.10 "}.marshalProto()); err != nil {
		t.Fatalf("decodeIPRequest should succeed: %v", err)
	}
	if _, err := decodeWatchLeasesRequest(watchLeasesRequest{SubnetCIDR: " 10.0.0.0/24 "}.marshalProto()); err != nil {
		t.Fatalf("decodeWatchLeasesRequest should succeed: %v", err)
	}
	if _, err := decodeGetConflictsRequest(nil); err != nil {
		t.Fatalf("decodeGetConflictsRequest should succeed: %v", err)
	}
	if _, err := decodeWatchDiscoveryRequest(nil); err != nil {
		t.Fatalf("decodeWatchDiscoveryRequest should succeed: %v", err)
	}
}

func TestMustHelpers(t *testing.T) {
	if got := mustString(nil); got != "" {
		t.Fatalf("mustString(nil) = %q", got)
	}
	if got := mustString(stringerValue("value")); got != "value" {
		t.Fatalf("mustString(stringer) = %q", got)
	}
	if got := mustString(12); got != "12" {
		t.Fatalf("mustString(int) = %q", got)
	}

	cases := []struct {
		value any
		want  int
	}{
		{value: int(5), want: 5},
		{value: int32(6), want: 6},
		{value: int64(7), want: 7},
		{value: uint32(8), want: 8},
		{value: uint64(9), want: 9},
		{value: float64(10), want: 10},
		{value: " 11 ", want: 11},
		{value: "bad", want: 0},
	}
	for _, tc := range cases {
		if got := mustInt(tc.value); got != tc.want {
			t.Fatalf("mustInt(%T=%v) = %d, want %d", tc.value, tc.value, got, tc.want)
		}
	}

	if got := mustStrings([]string{"a", "b"}); len(got) != 2 || got[0] != "a" {
		t.Fatalf("mustStrings([]string) = %#v", got)
	}
	if got := mustStrings([]any{" a ", 2, "", fmt.Stringer(stringerValue("x"))}); len(got) != 3 || got[0] != "a" || got[1] != "2" || got[2] != "x" {
		t.Fatalf("mustStrings([]any) = %#v", got)
	}
	if got := mustStrings(123); got != nil {
		t.Fatalf("mustStrings(non-slice) = %#v, want nil", got)
	}
}
