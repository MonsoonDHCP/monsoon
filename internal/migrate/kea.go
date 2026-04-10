package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"sort"
	"strings"

	"github.com/monsoondhcp/monsoon/internal/ipam"
)

type keaRunReport struct {
	Files    []FileReport
	Warnings []string
}

type keaConfigFile struct {
	DHCP4 keaDHCP4 `json:"Dhcp4"`
}

type keaDHCP4 struct {
	ValidLifetime  int64              `json:"valid-lifetime"`
	Subnet4        []keaSubnet4       `json:"subnet4"`
	SharedNetworks []keaSharedNetwork `json:"shared-networks"`
}

type keaSharedNetwork struct {
	Name    string       `json:"name"`
	Subnet4 []keaSubnet4 `json:"subnet4"`
}

type keaSubnet4 struct {
	ID            int               `json:"id"`
	Subnet        string            `json:"subnet"`
	ValidLifetime int64             `json:"valid-lifetime"`
	Pools         []json.RawMessage `json:"pools"`
	Reservations  []keaReservation  `json:"reservations"`
	OptionData    []keaOption       `json:"option-data"`
	UserContext   map[string]any    `json:"user-context"`
}

type keaReservation struct {
	HWAddress string `json:"hw-address"`
	IPAddress string `json:"ip-address"`
	Hostname  string `json:"hostname"`
}

type keaOption struct {
	Name string `json:"name"`
	Data string `json:"data"`
}

type keaPool struct {
	Pool string `json:"pool"`
}

func (r *Runner) runKea(ctx context.Context, configPath string, leasesPath string, dryRun bool, conflictPolicy string) (keaRunReport, error) {
	report := keaRunReport{}
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return report, fmt.Errorf("kea migration requires --source-config")
	}

	configReport, warnings, err := r.importKeaConfig(ctx, configPath, dryRun, conflictPolicy)
	report.Files = append(report.Files, configReport)
	report.Warnings = append(report.Warnings, warnings...)
	if err != nil {
		return report, err
	}

	if strings.TrimSpace(leasesPath) != "" {
		leaseReport, leaseErr := r.importCSVFile(ctx, "kea_leases", leasesPath, dryRun, conflictPolicy, r.importLeaseRow)
		report.Files = append(report.Files, leaseReport)
		if leaseErr != nil {
			report.Warnings = append(report.Warnings, leaseErr.Error())
			return report, leaseErr
		}
	}

	return report, nil
}

func (r *Runner) importKeaConfig(ctx context.Context, path string, dryRun bool, conflictPolicy string) (FileReport, []string, error) {
	report := FileReport{
		Kind: "kea_config",
		Path: path,
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		report.Errors = append(report.Errors, RowError{Row: 0, Message: err.Error()})
		return report, nil, err
	}

	var parsed keaConfigFile
	if err := json.Unmarshal(stripJSONComments(raw), &parsed); err != nil {
		report.Errors = append(report.Errors, RowError{Row: 0, Message: err.Error()})
		return report, nil, err
	}

	subnets := flattenKeaSubnets(parsed.DHCP4)
	report.Rows = len(subnets)

	var warnings []string
	for idx, subnet := range subnets {
		if ctx.Err() != nil {
			report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: ctx.Err().Error()})
			break
		}

		payload, reservationInputs, subnetWarnings, buildErr := buildKeaSubnetImport(parsed.DHCP4.ValidLifetime, subnet)
		warnings = append(warnings, subnetWarnings...)
		if buildErr != nil {
			report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: buildErr.Error()})
			continue
		}

		skipped := false
		if conflictPolicy == ConflictSkip {
			if _, err := r.ipam.GetSubnet(ctx, payload.CIDR); err == nil {
				skipped = true
			}
		}
		if skipped {
			report.Skipped++
			continue
		}

		if dryRun {
			if _, err := r.ipam.ValidateSubnet(ctx, payload); err != nil {
				report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: err.Error()})
				continue
			}
			for _, reservation := range reservationInputs {
				if _, err := r.ipam.ValidateReservation(ctx, reservation); err != nil {
					report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: err.Error()})
					break
				}
			}
			report.Applied++
			continue
		}

		if _, err := r.ipam.UpsertSubnet(ctx, payload); err != nil {
			report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: err.Error()})
			continue
		}
		reservationFailed := false
		for _, reservation := range reservationInputs {
			if _, err := r.ipam.UpsertReservation(ctx, reservation); err != nil {
				report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: err.Error()})
				reservationFailed = true
				break
			}
		}
		if reservationFailed {
			continue
		}
		report.Applied++
	}

	if len(report.Errors) > 0 {
		return report, warnings, fmt.Errorf("kea config import reported %d row errors", len(report.Errors))
	}
	return report, warnings, nil
}

func flattenKeaSubnets(dhcp4 keaDHCP4) []keaSubnet4 {
	out := append([]keaSubnet4(nil), dhcp4.Subnet4...)
	for _, network := range dhcp4.SharedNetworks {
		for _, subnet := range network.Subnet4 {
			if strings.TrimSpace(subnet.Subnet) == "" {
				continue
			}
			if _, ok := subnet.UserContext["shared_network"]; !ok && strings.TrimSpace(network.Name) != "" {
				if subnet.UserContext == nil {
					subnet.UserContext = map[string]any{}
				}
				subnet.UserContext["shared_network"] = network.Name
			}
			out = append(out, subnet)
		}
	}
	return out
}

func buildKeaSubnetImport(defaultLifetime int64, subnet keaSubnet4) (ipam.UpsertSubnetInput, []ipam.UpsertReservationInput, []string, error) {
	cidr := strings.TrimSpace(subnet.Subnet)
	if cidr == "" {
		return ipam.UpsertSubnetInput{}, nil, nil, fmt.Errorf("subnet is required")
	}

	poolStart, poolEnd, poolWarnings, err := normalizeKeaPools(subnet.Pools)
	if err != nil {
		return ipam.UpsertSubnetInput{}, nil, nil, fmt.Errorf("subnet %s: %w", cidr, err)
	}

	gateway, dns := extractKeaOptions(subnet.OptionData)
	leaseSec := subnet.ValidLifetime
	if leaseSec <= 0 {
		leaseSec = defaultLifetime
	}
	if leaseSec <= 0 {
		leaseSec = int64((12 * 60 * 60))
	}

	name := inferKeaSubnetName(subnet)
	result := ipam.UpsertSubnetInput{
		CIDR:       cidr,
		Name:       name,
		VLAN:       0,
		Gateway:    gateway,
		DNS:        dns,
		DHCPEnable: poolStart != "" && poolEnd != "",
		PoolStart:  poolStart,
		PoolEnd:    poolEnd,
		LeaseSec:   leaseSec,
	}

	reservations := make([]ipam.UpsertReservationInput, 0, len(subnet.Reservations))
	for _, item := range subnet.Reservations {
		if strings.TrimSpace(item.HWAddress) == "" || strings.TrimSpace(item.IPAddress) == "" {
			continue
		}
		reservations = append(reservations, ipam.UpsertReservationInput{
			MAC:        item.HWAddress,
			IP:         item.IPAddress,
			Hostname:   item.Hostname,
			SubnetCIDR: cidr,
		})
	}

	return result, reservations, poolWarnings, nil
}

func inferKeaSubnetName(subnet keaSubnet4) string {
	if subnet.UserContext != nil {
		if value, ok := subnet.UserContext["name"].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
		if value, ok := subnet.UserContext["shared_network"].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value) + " " + strings.TrimSpace(subnet.Subnet)
		}
	}
	if subnet.ID > 0 {
		return fmt.Sprintf("Kea Subnet %d", subnet.ID)
	}
	return strings.TrimSpace(subnet.Subnet)
}

func normalizeKeaPools(rawPools []json.RawMessage) (string, string, []string, error) {
	if len(rawPools) == 0 {
		return "", "", nil, nil
	}

	type ipRange struct {
		start netip.Addr
		end   netip.Addr
	}
	ranges := make([]ipRange, 0, len(rawPools))
	for _, raw := range rawPools {
		poolString := ""
		var direct string
		if err := json.Unmarshal(raw, &direct); err == nil {
			poolString = direct
		} else {
			var wrapped keaPool
			if err := json.Unmarshal(raw, &wrapped); err != nil {
				return "", "", nil, fmt.Errorf("invalid pool definition")
			}
			poolString = wrapped.Pool
		}
		start, end, err := parseKeaPool(poolString)
		if err != nil {
			return "", "", nil, err
		}
		ranges = append(ranges, ipRange{start: start, end: end})
	}

	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].start.Compare(ranges[j].start) < 0
	})

	mergedStart := ranges[0].start
	mergedEnd := ranges[0].end
	warnings := []string{}
	for idx := 1; idx < len(ranges); idx++ {
		current := ranges[idx]
		nextAddr := mergedEnd.Next()
		if nextAddr.IsValid() && current.start.Compare(nextAddr) <= 0 {
			if current.end.Compare(mergedEnd) > 0 {
				mergedEnd = current.end
			}
			continue
		}
		return "", "", nil, fmt.Errorf("multiple non-contiguous pools are not supported")
	}
	if len(ranges) > 1 {
		warnings = append(warnings, "multiple Kea pools were merged into a single Monsoon DHCP range")
	}
	return mergedStart.String(), mergedEnd.String(), warnings, nil
}

func parseKeaPool(value string) (netip.Addr, netip.Addr, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return netip.Addr{}, netip.Addr{}, fmt.Errorf("empty pool value")
	}
	if strings.Contains(value, "-") {
		parts := strings.SplitN(value, "-", 2)
		start, err := netip.ParseAddr(strings.TrimSpace(parts[0]))
		if err != nil {
			return netip.Addr{}, netip.Addr{}, fmt.Errorf("invalid pool start: %w", err)
		}
		end, err := netip.ParseAddr(strings.TrimSpace(parts[1]))
		if err != nil {
			return netip.Addr{}, netip.Addr{}, fmt.Errorf("invalid pool end: %w", err)
		}
		if start.Compare(end) > 0 {
			return netip.Addr{}, netip.Addr{}, fmt.Errorf("pool start must be <= pool end")
		}
		return start, end, nil
	}

	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return netip.Addr{}, netip.Addr{}, fmt.Errorf("unsupported pool format: %s", value)
	}
	start := prefix.Masked().Addr()
	if !start.Is4() {
		return netip.Addr{}, netip.Addr{}, fmt.Errorf("only DHCPv4 pools are supported")
	}
	end := lastIPv4InPrefix(prefix)
	return start, end, nil
}

func extractKeaOptions(options []keaOption) (string, []string) {
	var gateway string
	var dns []string
	for _, option := range options {
		name := strings.ToLower(strings.TrimSpace(option.Name))
		switch name {
		case "routers":
			parts := splitList(option.Data)
			if len(parts) > 0 {
				gateway = parts[0]
			}
		case "domain-name-servers":
			dns = splitList(option.Data)
		}
	}
	return gateway, dns
}

func lastIPv4InPrefix(prefix netip.Prefix) netip.Addr {
	base := prefix.Masked().Addr().As4()
	value := uint32(base[0])<<24 | uint32(base[1])<<16 | uint32(base[2])<<8 | uint32(base[3])
	hostBits := 32 - prefix.Bits()
	mask := uint32(0)
	if hostBits >= 32 {
		mask = ^uint32(0)
	} else if hostBits > 0 {
		mask = (uint32(1) << uint(hostBits)) - 1
	}
	end := value | mask
	return netip.AddrFrom4([4]byte{byte(end >> 24), byte(end >> 16), byte(end >> 8), byte(end)})
}

func stripJSONComments(input []byte) []byte {
	const (
		stateNormal = iota
		stateString
		stateLineComment
		stateBlockComment
	)

	out := make([]byte, 0, len(input))
	state := stateNormal
	escaped := false
	for idx := 0; idx < len(input); idx++ {
		ch := input[idx]
		next := byte(0)
		if idx+1 < len(input) {
			next = input[idx+1]
		}

		switch state {
		case stateNormal:
			if ch == '"' {
				state = stateString
				out = append(out, ch)
				continue
			}
			if ch == '/' && next == '/' {
				state = stateLineComment
				idx++
				continue
			}
			if ch == '/' && next == '*' {
				state = stateBlockComment
				idx++
				continue
			}
			out = append(out, ch)
		case stateString:
			out = append(out, ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				state = stateNormal
			}
		case stateLineComment:
			if ch == '\n' {
				state = stateNormal
				out = append(out, ch)
			}
		case stateBlockComment:
			if ch == '*' && next == '/' {
				state = stateNormal
				idx++
			}
		}
	}
	return out
}
