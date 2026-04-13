package migrate

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/monsoondhcp/monsoon/internal/ipam"
)

type phpipamRunReport struct {
	Files    []FileReport
	Warnings []string
}

type phpipamClient struct {
	baseURL string
	token   string
	client  *http.Client
}

type phpipamEnvelope[T any] struct {
	Code    int  `json:"code"`
	Success bool `json:"success"`
	Data    T    `json:"data"`
}

type phpipamSection struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type phpipamSubnet struct {
	ID             string `json:"id"`
	Subnet         any    `json:"subnet"`
	Mask           any    `json:"mask"`
	Description    string `json:"description"`
	SectionID      string `json:"sectionId"`
	MasterSubnetID string `json:"masterSubnetId"`
	VLANID         string `json:"vlanId"`
}

type phpipamAddress struct {
	ID          string `json:"id"`
	SubnetID    string `json:"subnetId"`
	IPAddress   any    `json:"ip"`
	Hostname    string `json:"hostname"`
	Description string `json:"description"`
	MAC         string `json:"mac"`
	Tag         any    `json:"tag"`
}

type phpipamVLAN struct {
	ID          string `json:"id"`
	Number      any    `json:"number"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type phpipamTag struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
}

func (r *Runner) runPHPIPAM(ctx context.Context, apiURL string, apiToken string, dryRun bool, conflictPolicy string) (phpipamRunReport, error) {
	report := phpipamRunReport{}
	client, err := newPHPIPAMClient(apiURL, apiToken)
	if err != nil {
		return report, err
	}

	sections, err := client.fetchSections(ctx)
	if err != nil {
		return report, err
	}
	subnets, err := client.fetchSubnets(ctx)
	if err != nil {
		return report, err
	}
	vlans, err := client.fetchVLANs(ctx)
	if err != nil {
		return report, err
	}
	tags, err := client.fetchTags(ctx)
	if err != nil {
		return report, err
	}
	addresses, err := client.fetchAddresses(ctx)
	if err != nil {
		return report, err
	}

	subnetReport, subnetWarnings, subnetIndex, err := r.importPHPIPAMSubnets(ctx, apiURL, sections, subnets, vlans, dryRun, conflictPolicy)
	report.Files = append(report.Files, subnetReport)
	report.Warnings = append(report.Warnings, subnetWarnings...)
	if err != nil {
		return report, err
	}

	addressReport, addressWarnings, err := r.importPHPIPAMAddresses(ctx, apiURL, addresses, subnetIndex, tags, dryRun, conflictPolicy)
	report.Files = append(report.Files, addressReport)
	report.Warnings = append(report.Warnings, addressWarnings...)
	if err != nil {
		return report, err
	}

	return report, nil
}

func newPHPIPAMClient(apiURL string, apiToken string) (*phpipamClient, error) {
	base := strings.TrimRight(strings.TrimSpace(apiURL), "/")
	if base == "" {
		return nil, fmt.Errorf("phpipam migration requires --api-url")
	}
	token := strings.TrimSpace(apiToken)
	if token == "" {
		return nil, fmt.Errorf("phpipam migration requires --api-token")
	}
	return &phpipamClient{
		baseURL: base,
		token:   token,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

func (c *phpipamClient) fetchSections(ctx context.Context) ([]phpipamSection, error) {
	return phpipamGET[[]phpipamSection](ctx, c, c.baseURL+"/sections/")
}

func (c *phpipamClient) fetchSubnets(ctx context.Context) ([]phpipamSubnet, error) {
	return phpipamGET[[]phpipamSubnet](ctx, c, c.baseURL+"/subnets/")
}

func (c *phpipamClient) fetchAddresses(ctx context.Context) ([]phpipamAddress, error) {
	return phpipamGET[[]phpipamAddress](ctx, c, c.baseURL+"/addresses/all/")
}

func (c *phpipamClient) fetchVLANs(ctx context.Context) ([]phpipamVLAN, error) {
	result, err := phpipamGET[[]phpipamVLAN](ctx, c, c.baseURL+"/vlan/")
	if err == nil {
		return result, nil
	}
	return phpipamGET[[]phpipamVLAN](ctx, c, c.baseURL+"/tools/vlans/")
}

func (c *phpipamClient) fetchTags(ctx context.Context) ([]phpipamTag, error) {
	return phpipamGET[[]phpipamTag](ctx, c, c.baseURL+"/addresses/tags/")
}

func phpipamGET[T any](ctx context.Context, client *phpipamClient, url string) (T, error) {
	var zero T
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return zero, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("token", client.token)
	req.Header.Set("phpipam-token", client.token)

	res, err := client.client.Do(req)
	if err != nil {
		return zero, err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return zero, fmt.Errorf("phpipam api request failed: %s", res.Status)
	}

	var envelope phpipamEnvelope[T]
	if err := json.NewDecoder(res.Body).Decode(&envelope); err != nil {
		return zero, err
	}
	if !envelope.Success && envelope.Code != 200 {
		return zero, fmt.Errorf("phpipam api request failed with code %d", envelope.Code)
	}
	return envelope.Data, nil
}

func (r *Runner) importPHPIPAMSubnets(
	ctx context.Context,
	apiURL string,
	sections []phpipamSection,
	subnets []phpipamSubnet,
	vlans []phpipamVLAN,
	dryRun bool,
	conflictPolicy string,
) (FileReport, []string, map[string]string, error) {
	report := FileReport{
		Kind: "phpipam_subnets",
		Path: apiURL,
		Rows: len(subnets),
	}

	sectionByID := make(map[string]phpipamSection, len(sections))
	for _, section := range sections {
		sectionByID[strings.TrimSpace(section.ID)] = section
	}
	vlanByID := make(map[string]phpipamVLAN, len(vlans))
	for _, vlan := range vlans {
		vlanByID[strings.TrimSpace(vlan.ID)] = vlan
	}

	subnetIndex := make(map[string]string, len(subnets))
	for idx, item := range subnets {
		if ctx.Err() != nil {
			report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: ctx.Err().Error()})
			break
		}
		payload, cidr, err := buildPHPIPAMSubnetImport(item, sectionByID[item.SectionID], vlanByID[item.VLANID])
		if err != nil {
			report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: err.Error()})
			continue
		}
		subnetIndex[strings.TrimSpace(item.ID)] = cidr

		if conflictPolicy == ConflictSkip {
			if _, err := r.ipam.GetSubnet(ctx, cidr); err == nil {
				report.Skipped++
				continue
			}
		}

		if dryRun {
			if _, err := r.ipam.ValidateSubnet(ctx, payload); err != nil {
				report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: err.Error()})
				continue
			}
			report.Applied++
			continue
		}
		if _, err := r.ipam.UpsertSubnet(ctx, payload); err != nil {
			report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: err.Error()})
			continue
		}
		report.Applied++
	}

	if len(report.Errors) > 0 {
		return report, nil, subnetIndex, fmt.Errorf("phpipam subnet import reported %d row errors", len(report.Errors))
	}
	return report, nil, subnetIndex, nil
}

func (r *Runner) importPHPIPAMAddresses(
	ctx context.Context,
	apiURL string,
	addresses []phpipamAddress,
	subnetIndex map[string]string,
	tags []phpipamTag,
	dryRun bool,
	conflictPolicy string,
) (FileReport, []string, error) {
	report := FileReport{
		Kind: "phpipam_addresses",
		Path: apiURL,
		Rows: len(addresses),
	}

	tagMap := make(map[string]string, len(tags))
	for _, tag := range tags {
		label := strings.TrimSpace(tag.Type)
		if label == "" {
			label = strings.TrimSpace(tag.Name)
		}
		tagMap[strings.TrimSpace(tag.ID)] = strings.ToLower(label)
	}

	for idx, item := range addresses {
		if ctx.Err() != nil {
			report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: ctx.Err().Error()})
			break
		}
		payload, err := buildPHPIPAMAddressImport(item, subnetIndex, tagMap)
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
		return report, nil, fmt.Errorf("phpipam address import reported %d row errors", len(report.Errors))
	}
	return report, nil, nil
}

func buildPHPIPAMSubnetImport(item phpipamSubnet, section phpipamSection, vlan phpipamVLAN) (ipam.UpsertSubnetInput, string, error) {
	addr, err := parsePHPIPAMIPv4(item.Subnet)
	if err != nil {
		return ipam.UpsertSubnetInput{}, "", fmt.Errorf("invalid phpipam subnet: %w", err)
	}
	mask, err := parsePHPIPAMInt(item.Mask)
	if err != nil {
		return ipam.UpsertSubnetInput{}, "", fmt.Errorf("invalid phpipam mask: %w", err)
	}
	if mask < 0 || mask > 32 {
		return ipam.UpsertSubnetInput{}, "", fmt.Errorf("invalid phpipam mask: %d", mask)
	}
	cidr := netip.PrefixFrom(addr, mask).Masked().String()

	name := strings.TrimSpace(item.Description)
	if name == "" {
		name = strings.TrimSpace(section.Name)
	}
	if name == "" {
		name = cidr
	}

	vlanID := 0
	if parsed, err := parsePHPIPAMInt(vlan.Number); err == nil {
		vlanID = parsed
	}

	return ipam.UpsertSubnetInput{
		CIDR:       cidr,
		Name:       name,
		VLAN:       vlanID,
		DHCPEnable: false,
	}, cidr, nil
}

func buildPHPIPAMAddressImport(item phpipamAddress, subnetIndex map[string]string, tagMap map[string]string) (ipam.UpsertAddressInput, error) {
	addr, err := parsePHPIPAMIPv4(item.IPAddress)
	if err != nil {
		return ipam.UpsertAddressInput{}, fmt.Errorf("invalid phpipam address: %w", err)
	}
	subnetCIDR := strings.TrimSpace(subnetIndex[strings.TrimSpace(item.SubnetID)])
	if subnetCIDR == "" {
		return ipam.UpsertAddressInput{}, fmt.Errorf("unknown phpipam subnet id %s", item.SubnetID)
	}

	hostname := strings.TrimSpace(item.Hostname)
	if hostname == "" {
		hostname = strings.TrimSpace(item.Description)
	}
	source := "phpipam"
	if strings.TrimSpace(item.Description) != "" {
		source = "phpipam:" + strings.TrimSpace(item.Description)
	}

	tagKey := strings.TrimSpace(anyToString(item.Tag))
	return ipam.UpsertAddressInput{
		IP:         addr.String(),
		SubnetCIDR: subnetCIDR,
		State:      mapPHPIPAMTag(tagMap[tagKey]),
		MAC:        strings.TrimSpace(item.MAC),
		Hostname:   hostname,
		Source:     source,
	}, nil
}

func parsePHPIPAMIPv4(value any) (netip.Addr, error) {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return netip.Addr{}, fmt.Errorf("empty ipv4 value")
		}
		if ip := net.ParseIP(trimmed).To4(); ip != nil {
			return netip.AddrFrom4([4]byte{ip[0], ip[1], ip[2], ip[3]}), nil
		}
		parsed, err := strconv.ParseUint(trimmed, 10, 32)
		if err != nil {
			return netip.Addr{}, err
		}
		if parsed > math.MaxUint32 {
			return netip.Addr{}, fmt.Errorf("ipv4 numeric value out of range")
		}
		// #nosec G115 -- parsed is bounded to uint32 range above.
		return uint32ToAddr(uint32(parsed)), nil
	case float64:
		if typed < 0 || typed > float64(math.MaxUint32) || math.Trunc(typed) != typed {
			return netip.Addr{}, fmt.Errorf("ipv4 numeric value out of range")
		}
		// #nosec G115 -- typed is range-checked and integral above.
		return uint32ToAddr(uint32(typed)), nil
	case int:
		if typed < 0 || uint64(typed) > math.MaxUint32 {
			return netip.Addr{}, fmt.Errorf("ipv4 numeric value out of range")
		}
		// #nosec G115 -- typed is bounded to uint32 range above.
		return uint32ToAddr(uint32(typed)), nil
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return netip.Addr{}, err
		}
		if parsed < 0 || parsed > math.MaxUint32 {
			return netip.Addr{}, fmt.Errorf("ipv4 numeric value out of range")
		}
		// #nosec G115 -- parsed is bounded to uint32 range above.
		return uint32ToAddr(uint32(parsed)), nil
	default:
		return netip.Addr{}, fmt.Errorf("unsupported ipv4 value type %T", value)
	}
}

func parsePHPIPAMInt(value any) (int, error) {
	switch typed := value.(type) {
	case string:
		return strconv.Atoi(strings.TrimSpace(typed))
	case float64:
		return int(typed), nil
	case int:
		return typed, nil
	case json.Number:
		parsed, err := typed.Int64()
		return int(parsed), err
	default:
		return 0, fmt.Errorf("unsupported int value type %T", value)
	}
}

func anyToString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.Itoa(int(typed))
	case int:
		return strconv.Itoa(typed)
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

func uint32ToAddr(value uint32) netip.Addr {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], value)
	return netip.AddrFrom4(buf)
}

func mapPHPIPAMTag(label string) ipam.IPState {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "offline", "reserved":
		return ipam.IPStateReserved
	case "used", "active":
		return ipam.IPStateDHCP
	case "deprecated":
		return ipam.IPStateQuarantined
	default:
		return ipam.IPStateAvailable
	}
}
