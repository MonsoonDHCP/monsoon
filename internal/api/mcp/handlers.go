package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
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

const defaultProtocolVersion = "2025-06-18"

var promptNumberPattern = regexp.MustCompile(`\d+`)

type HandlerDeps struct {
	LeaseStore      lease.Store
	IPAMEngine      *ipam.Engine
	DiscoveryEngine *discovery.Engine
	AuditLogger     *audit.Logger
	EventBroker     *events.Broker
	Version         string
	StartedAt       time.Time
	DHCPv4Enabled   bool
	DHCPv4Listen    string
	DHCPv4Running   func() bool
	MCPListen       string
}

type Toolset struct {
	deps     HandlerDeps
	tools    []ToolDefinition
	handlers map[string]toolHandler
}

type toolHandler func(context.Context, map[string]any) (CallToolResult, error)

type CallToolResult struct {
	Content           []ToolContent  `json:"content"`
	StructuredContent map[string]any `json:"structuredContent,omitempty"`
	IsError           bool           `json:"isError,omitempty"`
}

type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type paramError struct {
	message string
}

func (e paramError) Error() string {
	return e.message
}

func NewToolset(deps HandlerDeps) *Toolset {
	t := &Toolset{
		deps:  deps,
		tools: DefaultTools(),
	}
	t.handlers = map[string]toolHandler{
		"monsoon_list_subnets":       t.listSubnets,
		"monsoon_get_subnet":         t.getSubnet,
		"monsoon_create_subnet":      t.createSubnet,
		"monsoon_find_available_ip":  t.findAvailableIP,
		"monsoon_reserve_ip":         t.reserveIP,
		"monsoon_list_leases":        t.listLeases,
		"monsoon_get_lease":          t.getLease,
		"monsoon_search_by_mac":      t.searchByMAC,
		"monsoon_search_by_hostname": t.searchByHostname,
		"monsoon_subnet_utilization": t.subnetUtilization,
		"monsoon_run_discovery":      t.runDiscovery,
		"monsoon_get_conflicts":      t.getConflicts,
		"monsoon_audit_query":        t.auditQuery,
		"monsoon_get_health":         t.getHealth,
		"monsoon_plan_subnet":        t.planSubnet,
	}
	return t
}

func (t *Toolset) List() []ToolDefinition {
	return append([]ToolDefinition(nil), t.tools...)
}

func (t *Toolset) Call(ctx context.Context, name string, args map[string]any) (CallToolResult, error) {
	handler, ok := t.handlers[strings.TrimSpace(name)]
	if !ok {
		return CallToolResult{}, paramError{message: "unknown tool: " + strings.TrimSpace(name)}
	}
	if args == nil {
		args = map[string]any{}
	}
	result, err := handler(ctx, args)
	if err == nil {
		return ensureContent(result), nil
	}
	if _, ok := err.(paramError); ok {
		return CallToolResult{}, err
	}
	return toolError(err), nil
}

func ensureContent(result CallToolResult) CallToolResult {
	if len(result.Content) == 0 {
		result.Content = []ToolContent{{Type: "text", Text: "ok"}}
	}
	return result
}

func toolResult(text string, structured map[string]any) CallToolResult {
	result := CallToolResult{
		Content: []ToolContent{{Type: "text", Text: text}},
	}
	if structured != nil {
		result.StructuredContent = structured
	}
	return result
}

func toolResultJSON(value map[string]any) CallToolResult {
	raw, _ := json.MarshalIndent(value, "", "  ")
	return CallToolResult{
		Content:           []ToolContent{{Type: "text", Text: string(raw)}},
		StructuredContent: value,
	}
}

func toolError(err error) CallToolResult {
	return CallToolResult{
		Content: []ToolContent{{
			Type: "text",
			Text: err.Error(),
		}},
		StructuredContent: map[string]any{
			"error": err.Error(),
		},
		IsError: true,
	}
}

func (t *Toolset) listSubnets(ctx context.Context, _ map[string]any) (CallToolResult, error) {
	if t.deps.IPAMEngine == nil {
		return CallToolResult{}, fmt.Errorf("ipam engine is unavailable")
	}
	summaries, err := t.deps.IPAMEngine.ListSummaries(ctx)
	if err != nil {
		return CallToolResult{}, err
	}
	return toolResultJSON(map[string]any{
		"subnets": summaries,
		"count":   len(summaries),
	}), nil
}

func (t *Toolset) getSubnet(ctx context.Context, args map[string]any) (CallToolResult, error) {
	if t.deps.IPAMEngine == nil {
		return CallToolResult{}, fmt.Errorf("ipam engine is unavailable")
	}
	cidr, err := requireString(args, "cidr")
	if err != nil {
		return CallToolResult{}, err
	}
	subnet, err := t.deps.IPAMEngine.GetSubnet(ctx, cidr)
	if err != nil {
		return CallToolResult{}, err
	}
	utilization, err := t.computeUtilization(ctx, subnet)
	if err != nil {
		return CallToolResult{}, err
	}
	return toolResultJSON(map[string]any{
		"subnet":      subnet,
		"utilization": utilization,
	}), nil
}

func (t *Toolset) createSubnet(ctx context.Context, args map[string]any) (CallToolResult, error) {
	if t.deps.IPAMEngine == nil {
		return CallToolResult{}, fmt.Errorf("ipam engine is unavailable")
	}
	if err := requireRole(ctx, auth.DefaultRoleOperator); err != nil {
		return CallToolResult{}, err
	}

	input := ipam.UpsertSubnetInput{}
	var err error
	input.CIDR, err = requireString(args, "cidr")
	if err != nil {
		return CallToolResult{}, err
	}
	input.Name = optionalString(args, "name")
	input.Gateway = optionalString(args, "gateway")
	input.DNS, err = optionalStringSlice(args, "dns")
	if err != nil {
		return CallToolResult{}, err
	}
	if input.VLAN, err = optionalInt(args, "vlan", 0); err != nil {
		return CallToolResult{}, err
	}
	if input.DHCPEnable, err = optionalBool(args, "dhcp_enabled", false); err != nil {
		return CallToolResult{}, err
	}
	input.PoolStart = optionalString(args, "pool_start")
	input.PoolEnd = optionalString(args, "pool_end")
	if input.LeaseSec, err = optionalInt64(args, "lease_time_sec", 0); err != nil {
		return CallToolResult{}, err
	}

	subnet, err := t.deps.IPAMEngine.UpsertSubnet(ctx, input)
	if err != nil {
		return CallToolResult{}, err
	}
	if t.deps.EventBroker != nil {
		t.deps.EventBroker.Publish(events.Event{Type: "subnet.upserted", Data: map[string]any{"cidr": subnet.CIDR}})
	}
	t.logAudit(ctx, audit.Entry{
		Actor:      actorFromContext(ctx),
		Action:     "mcp.subnet.upsert",
		ObjectType: "subnet",
		ObjectID:   subnet.CIDR,
		Source:     "mcp",
		After: map[string]any{
			"name":         subnet.Name,
			"gateway":      subnet.Gateway,
			"vlan":         subnet.VLAN,
			"dhcp_enabled": subnet.DHCP.Enabled,
		},
	})
	return toolResultJSON(map[string]any{
		"subnet": subnet,
	}), nil
}

func (t *Toolset) findAvailableIP(ctx context.Context, args map[string]any) (CallToolResult, error) {
	if t.deps.IPAMEngine == nil {
		return CallToolResult{}, fmt.Errorf("ipam engine is unavailable")
	}
	cidr, err := requireString(args, "subnet_cidr")
	if err != nil {
		return CallToolResult{}, err
	}
	subnet, err := t.deps.IPAMEngine.GetSubnet(ctx, cidr)
	if err != nil {
		return CallToolResult{}, err
	}
	if !subnet.DHCP.Enabled {
		return CallToolResult{}, fmt.Errorf("dhcp is disabled for subnet %s", subnet.CIDR)
	}
	addresses, err := t.deps.IPAMEngine.ListAddresses(ctx, ipam.AddressFilter{SubnetCIDR: subnet.CIDR})
	if err != nil {
		return CallToolResult{}, err
	}
	for _, item := range addresses {
		if item.State == ipam.IPStateAvailable {
			return toolResultJSON(map[string]any{
				"subnet_cidr":    subnet.CIDR,
				"available_ip":   item.IP,
				"address_record": item,
			}), nil
		}
	}
	return CallToolResult{}, fmt.Errorf("no available IP found in subnet %s", subnet.CIDR)
}

func (t *Toolset) reserveIP(ctx context.Context, args map[string]any) (CallToolResult, error) {
	if t.deps.IPAMEngine == nil {
		return CallToolResult{}, fmt.Errorf("ipam engine is unavailable")
	}
	if err := requireRole(ctx, auth.DefaultRoleOperator); err != nil {
		return CallToolResult{}, err
	}
	mac, err := requireString(args, "mac")
	if err != nil {
		return CallToolResult{}, err
	}
	ipAddr, err := requireString(args, "ip")
	if err != nil {
		return CallToolResult{}, err
	}
	record, err := t.deps.IPAMEngine.UpsertReservation(ctx, ipam.UpsertReservationInput{
		MAC:        mac,
		IP:         ipAddr,
		Hostname:   optionalString(args, "hostname"),
		SubnetCIDR: optionalString(args, "subnet_cidr"),
	})
	if err != nil {
		return CallToolResult{}, err
	}
	if t.deps.EventBroker != nil {
		t.deps.EventBroker.Publish(events.Event{Type: "reservation.upserted", Data: map[string]any{"mac": record.MAC, "ip": record.IP}})
	}
	t.logAudit(ctx, audit.Entry{
		Actor:      actorFromContext(ctx),
		Action:     "mcp.reservation.upsert",
		ObjectType: "reservation",
		ObjectID:   record.MAC,
		Source:     "mcp",
		After: map[string]any{
			"ip":     record.IP,
			"subnet": record.SubnetCIDR,
		},
	})
	return toolResultJSON(map[string]any{
		"reservation": record,
	}), nil
}

func (t *Toolset) listLeases(ctx context.Context, args map[string]any) (CallToolResult, error) {
	if t.deps.LeaseStore == nil {
		return CallToolResult{}, fmt.Errorf("lease store is unavailable")
	}
	includeInactive, err := optionalBool(args, "include_inactive", false)
	if err != nil {
		return CallToolResult{}, err
	}
	state := optionalString(args, "state")
	subnet := optionalString(args, "subnet_cidr")

	leases, err := t.deps.LeaseStore.ListAll(ctx)
	if err != nil {
		return CallToolResult{}, err
	}
	filtered := make([]lease.Lease, 0, len(leases))
	for _, item := range leases {
		if subnet != "" && item.SubnetID != subnet {
			continue
		}
		if state != "" && !strings.EqualFold(string(item.State), state) {
			continue
		}
		if state == "" && !includeInactive && !isActiveLease(item.State) {
			continue
		}
		filtered = append(filtered, item)
	}
	sort.Slice(filtered, func(i, j int) bool { return compareIPString(filtered[i].IP, filtered[j].IP) < 0 })
	return toolResultJSON(map[string]any{
		"leases": leasesToAny(filtered),
		"count":  len(filtered),
	}), nil
}

func (t *Toolset) getLease(ctx context.Context, args map[string]any) (CallToolResult, error) {
	if t.deps.LeaseStore == nil {
		return CallToolResult{}, fmt.Errorf("lease store is unavailable")
	}
	ipAddr, err := requireString(args, "ip")
	if err != nil {
		return CallToolResult{}, err
	}
	item, err := t.deps.LeaseStore.GetByIP(ctx, ipAddr)
	if err != nil {
		return CallToolResult{}, err
	}
	return toolResultJSON(map[string]any{
		"lease": item,
	}), nil
}

func (t *Toolset) searchByMAC(ctx context.Context, args map[string]any) (CallToolResult, error) {
	if t.deps.IPAMEngine == nil && t.deps.LeaseStore == nil {
		return CallToolResult{}, fmt.Errorf("search backends are unavailable")
	}
	mac, err := requireString(args, "mac")
	if err != nil {
		return CallToolResult{}, err
	}
	mac = strings.ToUpper(strings.TrimSpace(mac))
	var reservation any
	if t.deps.IPAMEngine != nil {
		if item, lookupErr := t.deps.IPAMEngine.GetReservationByMAC(ctx, mac); lookupErr == nil {
			reservation = item
		}
	}
	leases := []lease.Lease{}
	if t.deps.LeaseStore != nil {
		leases, _ = t.deps.LeaseStore.GetByMAC(ctx, mac)
	}
	addresses := []ipam.AddressRecord{}
	if t.deps.IPAMEngine != nil {
		all, listErr := t.deps.IPAMEngine.ListAddresses(ctx, ipam.AddressFilter{Query: mac})
		if listErr == nil {
			for _, item := range all {
				if strings.EqualFold(item.MAC, mac) {
					addresses = append(addresses, item)
				}
			}
		}
	}
	return toolResultJSON(map[string]any{
		"mac":         mac,
		"reservation": reservation,
		"leases":      leasesToAny(leases),
		"addresses":   addressesToAny(addresses),
		"found":       reservation != nil || len(leases) > 0 || len(addresses) > 0,
	}), nil
}

func (t *Toolset) searchByHostname(ctx context.Context, args map[string]any) (CallToolResult, error) {
	hostname, err := requireString(args, "hostname")
	if err != nil {
		return CallToolResult{}, err
	}
	query := strings.ToLower(strings.TrimSpace(hostname))
	if query == "" {
		return CallToolResult{}, paramError{message: "hostname is required"}
	}

	reservations := []ipam.Reservation{}
	addresses := []ipam.AddressRecord{}
	leases := []lease.Lease{}

	if t.deps.IPAMEngine != nil {
		items, listErr := t.deps.IPAMEngine.ListReservations(ctx)
		if listErr == nil {
			for _, item := range items {
				if strings.Contains(strings.ToLower(item.Hostname), query) {
					reservations = append(reservations, item)
				}
			}
		}
		all, listErr := t.deps.IPAMEngine.ListAddresses(ctx, ipam.AddressFilter{Query: query})
		if listErr == nil {
			for _, item := range all {
				if strings.Contains(strings.ToLower(item.Hostname), query) {
					addresses = append(addresses, item)
				}
			}
		}
	}
	if t.deps.LeaseStore != nil {
		all, listErr := t.deps.LeaseStore.ListAll(ctx)
		if listErr == nil {
			for _, item := range all {
				if strings.Contains(strings.ToLower(item.Hostname), query) {
					leases = append(leases, item)
				}
			}
		}
	}

	return toolResultJSON(map[string]any{
		"hostname_query": query,
		"reservations":   reservations,
		"leases":         leasesToAny(leases),
		"addresses":      addressesToAny(addresses),
		"found":          len(reservations) > 0 || len(leases) > 0 || len(addresses) > 0,
	}), nil
}

func (t *Toolset) subnetUtilization(ctx context.Context, args map[string]any) (CallToolResult, error) {
	if t.deps.IPAMEngine == nil {
		return CallToolResult{}, fmt.Errorf("ipam engine is unavailable")
	}
	cidr := optionalString(args, "subnet_cidr")
	if cidr != "" {
		subnet, err := t.deps.IPAMEngine.GetSubnet(ctx, cidr)
		if err != nil {
			return CallToolResult{}, err
		}
		utilization, err := t.computeUtilization(ctx, subnet)
		if err != nil {
			return CallToolResult{}, err
		}
		return toolResultJSON(utilization), nil
	}

	subnets, err := t.deps.IPAMEngine.ListSubnets(ctx)
	if err != nil {
		return CallToolResult{}, err
	}
	items := make([]map[string]any, 0, len(subnets))
	for _, subnet := range subnets {
		utilization, utilErr := t.computeUtilization(ctx, subnet)
		if utilErr != nil {
			return CallToolResult{}, utilErr
		}
		items = append(items, utilization)
	}
	return toolResultJSON(map[string]any{
		"subnets": items,
		"count":   len(items),
	}), nil
}

func (t *Toolset) runDiscovery(ctx context.Context, args map[string]any) (CallToolResult, error) {
	if t.deps.DiscoveryEngine == nil {
		return CallToolResult{}, fmt.Errorf("discovery engine is unavailable")
	}
	if err := requireRole(ctx, auth.DefaultRoleOperator); err != nil {
		return CallToolResult{}, err
	}
	subnets, err := optionalStringSlice(args, "subnets")
	if err != nil {
		return CallToolResult{}, err
	}
	scanID, err := t.deps.DiscoveryEngine.TriggerScan(ctx, discovery.ScanRequest{
		Reason:  optionalString(args, "reason"),
		Subnets: subnets,
	})
	if err != nil {
		return CallToolResult{}, err
	}
	if t.deps.EventBroker != nil {
		t.deps.EventBroker.Publish(events.Event{Type: "discovery.scan_queued", Data: map[string]any{"scan_id": scanID}})
	}
	t.logAudit(ctx, audit.Entry{
		Actor:      actorFromContext(ctx),
		Action:     "mcp.discovery.scan",
		ObjectType: "discovery_scan",
		ObjectID:   scanID,
		Source:     "mcp",
		After: map[string]any{
			"subnets": subnets,
			"reason":  optionalString(args, "reason"),
		},
	})
	return toolResultJSON(map[string]any{
		"scan_id": scanID,
		"status":  "queued",
	}), nil
}

func (t *Toolset) getConflicts(ctx context.Context, _ map[string]any) (CallToolResult, error) {
	if t.deps.DiscoveryEngine == nil {
		return CallToolResult{}, fmt.Errorf("discovery engine is unavailable")
	}
	conflicts, err := t.deps.DiscoveryEngine.LatestConflicts(ctx)
	if err != nil {
		return CallToolResult{}, err
	}
	return toolResultJSON(map[string]any{
		"conflicts": conflicts,
		"count":     len(conflicts),
	}), nil
}

func (t *Toolset) auditQuery(ctx context.Context, args map[string]any) (CallToolResult, error) {
	if t.deps.AuditLogger == nil {
		return CallToolResult{}, fmt.Errorf("audit logger is unavailable")
	}
	limit, err := optionalInt(args, "limit", 50)
	if err != nil {
		return CallToolResult{}, err
	}
	from, err := optionalTime(args, "from")
	if err != nil {
		return CallToolResult{}, err
	}
	to, err := optionalTime(args, "to")
	if err != nil {
		return CallToolResult{}, err
	}
	entries, err := t.deps.AuditLogger.Query(ctx, audit.QueryFilter{
		Actor:      optionalString(args, "actor"),
		Action:     optionalString(args, "action"),
		ObjectType: optionalString(args, "object_type"),
		ObjectID:   optionalString(args, "object_id"),
		Source:     optionalString(args, "source"),
		Query:      optionalString(args, "q"),
		From:       from,
		To:         to,
		Limit:      limit,
	})
	if err != nil {
		return CallToolResult{}, err
	}
	return toolResultJSON(map[string]any{
		"entries": entries,
		"count":   len(entries),
	}), nil
}

func (t *Toolset) getHealth(ctx context.Context, _ map[string]any) (CallToolResult, error) {
	running := false
	if t.deps.DHCPv4Running != nil {
		running = t.deps.DHCPv4Running()
	}
	now := time.Now().UTC()
	uptime := "0s"
	if !t.deps.StartedAt.IsZero() && now.After(t.deps.StartedAt) {
		uptime = now.Sub(t.deps.StartedAt).Round(time.Second).String()
	}
	result := map[string]any{
		"status":     "healthy",
		"version":    t.deps.Version,
		"started_at": t.deps.StartedAt,
		"uptime":     uptime,
		"components": map[string]any{
			"dhcpv4": map[string]any{
				"enabled": t.deps.DHCPv4Enabled,
				"listen":  t.deps.DHCPv4Listen,
				"running": running,
			},
			"mcp": map[string]any{
				"listen":           t.deps.MCPListen,
				"protocol_version": defaultProtocolVersion,
			},
		},
	}
	if t.deps.DiscoveryEngine != nil {
		result["discovery"] = t.deps.DiscoveryEngine.Status(ctx)
	}
	return toolResultJSON(result), nil
}

func (t *Toolset) planSubnet(ctx context.Context, args map[string]any) (CallToolResult, error) {
	required, err := optionalInt(args, "required_addresses", 0)
	if err != nil {
		return CallToolResult{}, err
	}
	if required <= 0 {
		required = extractRequiredAddresses(optionalString(args, "prompt"))
	}
	if required <= 0 {
		return CallToolResult{}, paramError{message: "required_addresses or a numeric prompt is required"}
	}
	growthPercent, err := optionalInt(args, "growth_percent", 15)
	if err != nil {
		return CallToolResult{}, err
	}
	parentCIDR := optionalString(args, "parent_cidr")
	if parentCIDR == "" {
		parentCIDR = "10.0.0.0/8"
	}
	parent, err := netip.ParsePrefix(parentCIDR)
	if err != nil {
		return CallToolResult{}, paramError{message: "invalid parent_cidr: " + err.Error()}
	}
	parent = parent.Masked()
	if !parent.Addr().Is4() {
		return CallToolResult{}, paramError{message: "only IPv4 subnet planning is currently supported"}
	}

	targetUsable := int(math.Ceil(float64(required) * (1 + float64(growthPercent)/100.0)))
	prefixBits := smallestIPv4Prefix(targetUsable)
	if prefixBits < 0 {
		return CallToolResult{}, fmt.Errorf("unable to calculate subnet size for %d addresses", targetUsable)
	}
	if prefixBits < parent.Bits() {
		return CallToolResult{}, fmt.Errorf("parent subnet %s is too small for %d addresses", parent.String(), targetUsable)
	}

	suggested, err := t.findCandidatePrefix(ctx, parent, prefixBits)
	if err != nil {
		return CallToolResult{}, err
	}

	gateway, poolStart, poolEnd := defaultPlanAddresses(suggested)
	result := map[string]any{
		"prompt":                optionalString(args, "prompt"),
		"name":                  optionalString(args, "name"),
		"vlan":                  optionalIntOrZero(args, "vlan"),
		"required_addresses":    required,
		"target_usable_hosts":   targetUsable,
		"suggested_cidr":        suggested.String(),
		"parent_cidr":           parent.String(),
		"usable_hosts":          usableHosts(suggested),
		"gateway":               gateway,
		"dhcp_pool_start":       poolStart,
		"dhcp_pool_end":         poolEnd,
		"growth_percent":        growthPercent,
		"planning_protocol":     "ipv4",
		"existing_subnet_count": t.countSubnets(ctx),
	}
	return toolResultJSON(result), nil
}

func (t *Toolset) computeUtilization(ctx context.Context, subnet ipam.Subnet) (map[string]any, error) {
	addresses, err := t.deps.IPAMEngine.ListAddresses(ctx, ipam.AddressFilter{SubnetCIDR: subnet.CIDR})
	if err != nil {
		return nil, err
	}
	filtered := addresses
	total := len(filtered)
	if subnet.DHCP.Enabled {
		if start, end, ok := poolRange(subnet); ok {
			filtered = make([]ipam.AddressRecord, 0, len(addresses))
			for _, item := range addresses {
				addr, parseErr := netip.ParseAddr(item.IP)
				if parseErr != nil {
					continue
				}
				if addr.Compare(start) < 0 || addr.Compare(end) > 0 {
					continue
				}
				filtered = append(filtered, item)
			}
			total = hostSpan(start, end)
		}
	}

	stateBreakdown := map[string]int{}
	used := 0
	for _, item := range filtered {
		stateBreakdown[string(item.State)]++
		if item.State != ipam.IPStateAvailable {
			used++
		}
	}
	if total <= 0 {
		total = len(filtered)
	}
	available := total - used
	if available < 0 {
		available = 0
	}
	utilizationPct := 0
	if total > 0 {
		utilizationPct = int(math.Round(float64(used) / float64(total) * 100))
	}
	return map[string]any{
		"subnet_cidr":         subnet.CIDR,
		"name":                subnet.Name,
		"dhcp_enabled":        subnet.DHCP.Enabled,
		"pool_start":          subnet.DHCP.PoolStart,
		"pool_end":            subnet.DHCP.PoolEnd,
		"capacity_total":      total,
		"capacity_used":       used,
		"capacity_available":  available,
		"utilization_percent": utilizationPct,
		"state_breakdown":     stateBreakdown,
	}, nil
}

func (t *Toolset) findCandidatePrefix(ctx context.Context, parent netip.Prefix, prefixBits int) (netip.Prefix, error) {
	subnets := []ipam.Subnet{}
	if t.deps.IPAMEngine != nil {
		subnets, _ = t.deps.IPAMEngine.ListSubnets(ctx)
	}
	blockSize := prefixAddressCount(prefixBits)
	parentCount := ipam.AddressCount(parent)
	for offset := uint64(0); offset+blockSize <= parentCount; offset += blockSize {
		addr, ok := ipam.NthAddress(parent, offset)
		if !ok {
			break
		}
		candidate := netip.PrefixFrom(addr, prefixBits).Masked()
		if !ipam.Contains(parent, candidate) {
			continue
		}
		overlap := false
		for _, existing := range subnets {
			existingPrefix, err := netip.ParsePrefix(existing.CIDR)
			if err != nil {
				continue
			}
			if ipam.Overlaps(candidate, existingPrefix) {
				overlap = true
				break
			}
		}
		if !overlap {
			return candidate, nil
		}
	}
	return netip.Prefix{}, fmt.Errorf("no free subnet with /%d was found inside %s", prefixBits, parent.String())
}

func (t *Toolset) countSubnets(ctx context.Context) int {
	if t.deps.IPAMEngine == nil {
		return 0
	}
	subnets, err := t.deps.IPAMEngine.ListSubnets(ctx)
	if err != nil {
		return 0
	}
	return len(subnets)
}

func (t *Toolset) logAudit(ctx context.Context, entry audit.Entry) {
	if t.deps.AuditLogger == nil {
		return
	}
	_ = t.deps.AuditLogger.Log(ctx, entry)
}

func requireRole(ctx context.Context, required string) error {
	identity, ok := restapi.IdentityFromContext(ctx)
	if !ok {
		return nil
	}
	if auth.HasRole(required, identity.Role) {
		return nil
	}
	return fmt.Errorf("forbidden: %s role required", required)
}

func actorFromContext(ctx context.Context) string {
	if identity, ok := restapi.IdentityFromContext(ctx); ok {
		return identity.Username
	}
	return "anonymous"
}

func requireString(args map[string]any, key string) (string, error) {
	value := optionalString(args, key)
	if value == "" {
		return "", paramError{message: key + " is required"}
	}
	return value, nil
}

func optionalString(args map[string]any, key string) string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return ""
	}
	switch typed := raw.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func optionalStringSlice(args map[string]any, key string) ([]string, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil, nil
	}
	items, ok := raw.([]any)
	if !ok {
		if typed, ok := raw.([]string); ok {
			return append([]string(nil), typed...), nil
		}
		return nil, paramError{message: key + " must be an array of strings"}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		str, ok := item.(string)
		if !ok {
			return nil, paramError{message: key + " must be an array of strings"}
		}
		if trimmed := strings.TrimSpace(str); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out, nil
}

func optionalBool(args map[string]any, key string, fallback bool) (bool, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return fallback, nil
	}
	value, ok := raw.(bool)
	if !ok {
		return false, paramError{message: key + " must be a boolean"}
	}
	return value, nil
}

func optionalInt(args map[string]any, key string, fallback int) (int, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return fallback, nil
	}
	switch typed := raw.(type) {
	case float64:
		return int(typed), nil
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case json.Number:
		value, err := typed.Int64()
		if err != nil {
			return 0, paramError{message: key + " must be an integer"}
		}
		return int(value), nil
	case string:
		value, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0, paramError{message: key + " must be an integer"}
		}
		return value, nil
	default:
		return 0, paramError{message: key + " must be an integer"}
	}
}

func optionalIntOrZero(args map[string]any, key string) int {
	value, err := optionalInt(args, key, 0)
	if err != nil {
		return 0
	}
	return value
}

func optionalInt64(args map[string]any, key string, fallback int64) (int64, error) {
	value, err := optionalInt(args, key, int(fallback))
	return int64(value), err
}

func optionalTime(args map[string]any, key string) (time.Time, error) {
	raw := optionalString(args, key)
	if raw == "" {
		return time.Time{}, nil
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, paramError{message: key + " must be RFC3339"}
	}
	return value, nil
}

func leasesToAny(items []lease.Lease) []lease.Lease {
	return append([]lease.Lease(nil), items...)
}

func addressesToAny(items []ipam.AddressRecord) []ipam.AddressRecord {
	return append([]ipam.AddressRecord(nil), items...)
}

func isActiveLease(state lease.LeaseState) bool {
	switch state {
	case lease.StateBound, lease.StateRenewing, lease.StateOffered:
		return true
	default:
		return false
	}
}

func extractRequiredAddresses(prompt string) int {
	match := promptNumberPattern.FindString(prompt)
	if match == "" {
		return 0
	}
	value, err := strconv.Atoi(match)
	if err != nil {
		return 0
	}
	return value
}

func smallestIPv4Prefix(requiredUsable int) int {
	if requiredUsable <= 0 {
		return -1
	}
	for bits := 30; bits >= 1; bits-- {
		prefix := netip.PrefixFrom(netip.MustParseAddr("10.0.0.0"), bits)
		if usableHosts(prefix) >= requiredUsable {
			return bits
		}
	}
	return -1
}

func usableHosts(prefix netip.Prefix) int {
	count := int(ipam.AddressCount(prefix))
	if count <= 2 {
		return 0
	}
	return count - 2
}

func defaultPlanAddresses(prefix netip.Prefix) (gateway string, poolStart string, poolEnd string) {
	total := int(ipam.AddressCount(prefix))
	if total < 4 {
		return "", "", ""
	}
	gw, _ := ipam.NthAddress(prefix, 1)
	lastUsable, _ := ipam.NthAddress(prefix, uint64(total-2))
	startOffset := uint64(10)
	if usableHosts(prefix) <= 20 {
		startOffset = 2
	}
	start, ok := ipam.NthAddress(prefix, startOffset)
	if !ok || lastUsable.Compare(start) < 0 {
		start = gw.Next()
	}
	return gw.String(), start.String(), lastUsable.String()
}

func poolRange(subnet ipam.Subnet) (netip.Addr, netip.Addr, bool) {
	start, err := netip.ParseAddr(strings.TrimSpace(subnet.DHCP.PoolStart))
	if err != nil {
		return netip.Addr{}, netip.Addr{}, false
	}
	end, err := netip.ParseAddr(strings.TrimSpace(subnet.DHCP.PoolEnd))
	if err != nil {
		return netip.Addr{}, netip.Addr{}, false
	}
	return start, end, start.IsValid() && end.IsValid() && start.Compare(end) <= 0
}

func hostSpan(start, end netip.Addr) int {
	if !start.IsValid() || !end.IsValid() || start.Compare(end) > 0 {
		return 0
	}
	count := 1
	for current := start; current.Compare(end) < 0; current = current.Next() {
		count++
	}
	return count
}

func prefixAddressCount(bits int) uint64 {
	return 1 << (32 - bits)
}

func compareIPString(a, b string) int {
	addrA, errA := netip.ParseAddr(a)
	addrB, errB := netip.ParseAddr(b)
	if errA == nil && errB == nil {
		return addrA.Compare(addrB)
	}
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

var _ = storage.ErrNotFound
