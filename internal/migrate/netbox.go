package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/monsoondhcp/monsoon/internal/ipam"
)

type netboxRunReport struct {
	Files    []FileReport
	Warnings []string
}

type netboxClient struct {
	baseURL string
	token   string
	client  *http.Client
}

type netboxPage[T any] struct {
	Count   int     `json:"count"`
	Next    *string `json:"next"`
	Results []T     `json:"results"`
}

type netboxPrefix struct {
	Prefix      string         `json:"prefix"`
	Description string         `json:"description"`
	Status      netboxStatus   `json:"status"`
	VLAN        *netboxVLAN    `json:"vlan"`
	Role        *netboxNamed   `json:"role"`
	Site        *netboxNamed   `json:"site"`
	Tenant      *netboxNamed   `json:"tenant"`
	VRF         *netboxNamed   `json:"vrf"`
	CustomField map[string]any `json:"custom_fields"`
}

type netboxIPAddress struct {
	Address     string       `json:"address"`
	DNSName     string       `json:"dns_name"`
	Description string       `json:"description"`
	Status      netboxStatus `json:"status"`
}

type netboxStatus struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type netboxVLAN struct {
	VID  int    `json:"vid"`
	Name string `json:"name"`
}

type netboxNamed struct {
	Name string `json:"name"`
}

func (r *Runner) runNetBox(ctx context.Context, apiURL string, apiToken string, dryRun bool, conflictPolicy string) (netboxRunReport, error) {
	report := netboxRunReport{}
	client, err := newNetBoxClient(apiURL, apiToken)
	if err != nil {
		return report, err
	}

	prefixes, err := client.fetchPrefixes(ctx)
	if err != nil {
		return report, err
	}
	prefixReport, prefixWarnings, importedPrefixes, err := r.importNetBoxPrefixes(ctx, apiURL, prefixes, dryRun, conflictPolicy)
	report.Files = append(report.Files, prefixReport)
	report.Warnings = append(report.Warnings, prefixWarnings...)
	if err != nil {
		return report, err
	}

	addresses, err := client.fetchIPAddresses(ctx)
	if err != nil {
		return report, err
	}
	addressReport, addressWarnings, err := r.importNetBoxAddresses(ctx, apiURL, addresses, importedPrefixes, dryRun, conflictPolicy)
	report.Files = append(report.Files, addressReport)
	report.Warnings = append(report.Warnings, addressWarnings...)
	if err != nil {
		return report, err
	}

	return report, nil
}

func newNetBoxClient(apiURL string, apiToken string) (*netboxClient, error) {
	base := strings.TrimRight(strings.TrimSpace(apiURL), "/")
	if base == "" {
		return nil, fmt.Errorf("netbox migration requires --api-url")
	}
	return &netboxClient{
		baseURL: base,
		token:   strings.TrimSpace(apiToken),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

func (c *netboxClient) fetchPrefixes(ctx context.Context) ([]netboxPrefix, error) {
	return fetchNetBoxPaginated[netboxPrefix](ctx, c, c.baseURL+"/api/ipam/prefixes/")
}

func (c *netboxClient) fetchIPAddresses(ctx context.Context) ([]netboxIPAddress, error) {
	return fetchNetBoxPaginated[netboxIPAddress](ctx, c, c.baseURL+"/api/ipam/ip-addresses/")
}

func fetchNetBoxPaginated[T any](ctx context.Context, client *netboxClient, firstURL string) ([]T, error) {
	nextURL := firstURL
	out := make([]T, 0, 64)
	for nextURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		if client.token != "" {
			req.Header.Set("Authorization", "Token "+client.token)
		}
		res, err := client.client.Do(req)
		if err != nil {
			return nil, err
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			_ = res.Body.Close()
			return nil, fmt.Errorf("netbox api request failed: %s", res.Status)
		}
		var page netboxPage[T]
		if err := json.NewDecoder(res.Body).Decode(&page); err != nil {
			_ = res.Body.Close()
			return nil, err
		}
		_ = res.Body.Close()
		out = append(out, page.Results...)
		if page.Next == nil {
			nextURL = ""
		} else {
			nextURL = strings.TrimSpace(*page.Next)
		}
	}
	return out, nil
}

func (r *Runner) importNetBoxPrefixes(ctx context.Context, apiURL string, prefixes []netboxPrefix, dryRun bool, conflictPolicy string) (FileReport, []string, []netip.Prefix, error) {
	report := FileReport{
		Kind: "netbox_prefixes",
		Path: apiURL,
		Rows: len(prefixes),
	}
	warnings := []string{}
	imported := make([]netip.Prefix, 0, len(prefixes))

	for idx, item := range prefixes {
		if ctx.Err() != nil {
			report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: ctx.Err().Error()})
			break
		}
		payload, prefix, buildWarnings, err := buildNetBoxSubnetImport(item)
		warnings = append(warnings, buildWarnings...)
		if err != nil {
			report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: err.Error()})
			continue
		}

		if conflictPolicy == ConflictSkip {
			if _, err := r.ipam.GetSubnet(ctx, payload.CIDR); err == nil {
				report.Skipped++
				imported = append(imported, prefix)
				continue
			}
		}

		if dryRun {
			if _, err := r.ipam.ValidateSubnet(ctx, payload); err != nil {
				report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: err.Error()})
				continue
			}
			report.Applied++
			imported = append(imported, prefix)
			continue
		}

		if _, err := r.ipam.UpsertSubnet(ctx, payload); err != nil {
			report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: err.Error()})
			continue
		}
		report.Applied++
		imported = append(imported, prefix)
	}

	if len(report.Errors) > 0 {
		return report, warnings, imported, fmt.Errorf("netbox prefix import reported %d row errors", len(report.Errors))
	}
	return report, warnings, imported, nil
}

func (r *Runner) importNetBoxAddresses(ctx context.Context, apiURL string, addresses []netboxIPAddress, prefixes []netip.Prefix, dryRun bool, conflictPolicy string) (FileReport, []string, error) {
	report := FileReport{
		Kind: "netbox_ip_addresses",
		Path: apiURL,
		Rows: len(addresses),
	}

	for idx, item := range addresses {
		if ctx.Err() != nil {
			report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: ctx.Err().Error()})
			break
		}
		payload, err := buildNetBoxAddressImport(item, prefixes)
		if err != nil {
			report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: err.Error()})
			continue
		}

		if conflictPolicy == ConflictSkip {
			if _, err := r.ipam.GetStoredAddress(ctx, payload.IP); err == nil {
				report.Skipped++
				continue
			}
		}

		if dryRun {
			if _, err := r.ipam.ValidateAddress(ctx, payload); err != nil {
				report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: err.Error()})
				continue
			}
			report.Applied++
			continue
		}

		if _, err := r.ipam.UpsertAddress(ctx, payload); err != nil {
			report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: err.Error()})
			continue
		}
		report.Applied++
	}

	if len(report.Errors) > 0 {
		return report, nil, fmt.Errorf("netbox address import reported %d row errors", len(report.Errors))
	}
	return report, nil, nil
}

func buildNetBoxSubnetImport(item netboxPrefix) (ipam.UpsertSubnetInput, netip.Prefix, []string, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(item.Prefix))
	if err != nil {
		return ipam.UpsertSubnetInput{}, netip.Prefix{}, nil, fmt.Errorf("invalid prefix %q: %w", item.Prefix, err)
	}
	name := strings.TrimSpace(item.Description)
	if name == "" && item.Role != nil && strings.TrimSpace(item.Role.Name) != "" {
		name = strings.TrimSpace(item.Role.Name)
	}
	if name == "" && item.Site != nil && strings.TrimSpace(item.Site.Name) != "" {
		name = strings.TrimSpace(item.Site.Name) + " " + prefix.String()
	}
	if name == "" {
		name = prefix.String()
	}

	vlanID := 0
	if item.VLAN != nil {
		vlanID = item.VLAN.VID
	}

	warnings := []string{}
	if prefix.Addr().Is6() {
		warnings = append(warnings, fmt.Sprintf("netbox prefix %s imported as IPv6 subnet without DHCP pool", prefix.String()))
	}

	return ipam.UpsertSubnetInput{
		CIDR:       prefix.String(),
		Name:       name,
		VLAN:       vlanID,
		DHCPEnable: false,
	}, prefix, warnings, nil
}

func buildNetBoxAddressImport(item netboxIPAddress, prefixes []netip.Prefix) (ipam.UpsertAddressInput, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(item.Address))
	if err != nil {
		return ipam.UpsertAddressInput{}, fmt.Errorf("invalid netbox address %q: %w", item.Address, err)
	}
	ipAddr := prefix.Addr()
	subnetCIDR := longestContainingPrefix(ipAddr, prefixes)
	if subnetCIDR == "" {
		subnetCIDR = prefix.String()
	}

	sourceName := "netbox"
	if strings.TrimSpace(item.Description) != "" {
		sourceName = "netbox:" + strings.TrimSpace(item.Description)
	}
	hostname := strings.TrimSpace(item.DNSName)
	if hostname == "" {
		hostname = strings.TrimSpace(item.Description)
	}

	return ipam.UpsertAddressInput{
		IP:         ipAddr.String(),
		SubnetCIDR: subnetCIDR,
		State:      mapNetBoxStatus(item.Status.Value),
		Hostname:   hostname,
		Source:     sourceName,
	}, nil
}

func longestContainingPrefix(addr netip.Addr, prefixes []netip.Prefix) string {
	bestBits := -1
	best := ""
	for _, prefix := range prefixes {
		if !prefix.Contains(addr) {
			continue
		}
		if prefix.Bits() > bestBits {
			bestBits = prefix.Bits()
			best = prefix.String()
		}
	}
	return best
}

func mapNetBoxStatus(value string) ipam.IPState {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "dhcp":
		return ipam.IPStateDHCP
	case "deprecated":
		return ipam.IPStateQuarantined
	case "reserved", "active", "slaac":
		return ipam.IPStateReserved
	default:
		return ipam.IPStateAvailable
	}
}
