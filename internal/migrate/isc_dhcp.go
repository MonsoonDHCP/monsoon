package migrate

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/monsoondhcp/monsoon/internal/ipam"
	"github.com/monsoondhcp/monsoon/internal/lease"
)

type iscRunReport struct {
	Files    []FileReport
	Warnings []string
}

type iscConfigData struct {
	Subnets []iscSubnet
	Hosts   []iscHost
}

type iscSubnet struct {
	Network      string
	Netmask      string
	LeaseTimeSec int64
	Ranges       []iscRange
	Gateway      string
	DNS          []string
	Reservations []iscHost
}

type iscRange struct {
	Start string
	End   string
}

type iscHost struct {
	Name         string
	MAC          string
	FixedAddress string
	Hostname     string
	SubnetCIDR   string
}

type iscLeaseRecord struct {
	IP         string
	MAC        string
	Hostname   string
	State      lease.LeaseState
	StartTime  time.Time
	ExpiryTime time.Time
	SubnetID   string
}

type iscParser struct {
	tokens []string
	pos    int
	data   iscConfigData
}

func (r *Runner) runISCDHCP(ctx context.Context, configPath string, leasesPath string, dryRun bool, conflictPolicy string) (iscRunReport, error) {
	report := iscRunReport{}
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return report, fmt.Errorf("isc-dhcp migration requires --source-config")
	}

	configReport, warnings, parsedConfig, err := r.importISCDHCPConfig(ctx, configPath, dryRun, conflictPolicy)
	report.Files = append(report.Files, configReport)
	report.Warnings = append(report.Warnings, warnings...)
	if err != nil {
		return report, err
	}

	if strings.TrimSpace(leasesPath) != "" {
		leaseReport, leaseWarnings, leaseErr := r.importISCDHCPLeases(ctx, leasesPath, parsedConfig, dryRun, conflictPolicy)
		report.Files = append(report.Files, leaseReport)
		report.Warnings = append(report.Warnings, leaseWarnings...)
		if leaseErr != nil {
			return report, leaseErr
		}
	}

	return report, nil
}

func (r *Runner) importISCDHCPConfig(ctx context.Context, path string, dryRun bool, conflictPolicy string) (FileReport, []string, iscConfigData, error) {
	report := FileReport{
		Kind: "isc_dhcp_config",
		Path: path,
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		report.Errors = append(report.Errors, RowError{Row: 0, Message: err.Error()})
		return report, nil, iscConfigData{}, err
	}

	parsed, err := parseISCDHCPConfig(raw)
	if err != nil {
		report.Errors = append(report.Errors, RowError{Row: 0, Message: err.Error()})
		return report, nil, iscConfigData{}, err
	}

	report.Rows = len(parsed.Subnets)
	warnings := []string{}
	for idx, subnet := range parsed.Subnets {
		if ctx.Err() != nil {
			report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: ctx.Err().Error()})
			break
		}

		payload, reservations, subnetWarnings, buildErr := buildISCSubnetImport(subnet)
		warnings = append(warnings, subnetWarnings...)
		if buildErr != nil {
			report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: buildErr.Error()})
			continue
		}

		if conflictPolicy == ConflictSkip {
			if _, err := r.ipam.GetSubnet(ctx, payload.CIDR); err == nil {
				report.Skipped++
				continue
			}
		}

		if dryRun {
			if _, err := r.ipam.ValidateSubnet(ctx, payload); err != nil {
				report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: err.Error()})
				continue
			}
			reservationFailed := false
			for _, reservation := range reservations {
				if _, err := r.ipam.ValidateReservation(ctx, reservation); err != nil {
					report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: err.Error()})
					reservationFailed = true
					break
				}
			}
			if reservationFailed {
				continue
			}
			report.Applied++
			continue
		}

		if _, err := r.ipam.UpsertSubnet(ctx, payload); err != nil {
			report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: err.Error()})
			continue
		}
		reservationFailed := false
		for _, reservation := range reservations {
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
		return report, warnings, parsed, fmt.Errorf("isc config import reported %d row errors", len(report.Errors))
	}
	return report, warnings, parsed, nil
}

func (r *Runner) importISCDHCPLeases(ctx context.Context, path string, config iscConfigData, dryRun bool, conflictPolicy string) (FileReport, []string, error) {
	report := FileReport{
		Kind: "isc_dhcp_leases",
		Path: path,
	}

	records, err := parseISCDHCPLeases(path, config.Subnets)
	if err != nil {
		report.Errors = append(report.Errors, RowError{Row: 0, Message: err.Error()})
		return report, nil, err
	}
	report.Rows = len(records)

	for idx, record := range records {
		if ctx.Err() != nil {
			report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: ctx.Err().Error()})
			break
		}

		if record.State != lease.StateBound && record.State != lease.StateRenewing {
			report.Skipped++
			continue
		}
		if conflictPolicy == ConflictSkip {
			if _, err := r.leases.GetByIP(ctx, record.IP); err == nil {
				report.Skipped++
				continue
			}
		}

		item := lease.Lease{
			IP:         record.IP,
			MAC:        record.MAC,
			Hostname:   record.Hostname,
			State:      record.State,
			StartTime:  record.StartTime,
			ExpiryTime: record.ExpiryTime,
			Duration:   record.ExpiryTime.Sub(record.StartTime),
			SubnetID:   record.SubnetID,
		}
		if item.Duration <= 0 {
			item.Duration = 12 * time.Hour
		}

		if dryRun {
			if err := validateLeaseRecord(item); err != nil {
				report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: err.Error()})
				continue
			}
			report.Applied++
			continue
		}
		if err := r.leases.Upsert(ctx, item); err != nil {
			report.Errors = append(report.Errors, RowError{Row: idx + 1, Message: err.Error()})
			continue
		}
		report.Applied++
	}

	if len(report.Errors) > 0 {
		return report, nil, fmt.Errorf("isc lease import reported %d row errors", len(report.Errors))
	}
	return report, nil, nil
}

func parseISCDHCPConfig(raw []byte) (iscConfigData, error) {
	parser := &iscParser{
		tokens: tokenizeISCConfig(string(raw)),
	}
	if err := parser.parseStatements(nil); err != nil {
		return iscConfigData{}, err
	}
	assignGlobalHosts(&parser.data)
	return parser.data, nil
}

func (p *iscParser) parseStatements(currentSubnet *iscSubnet) error {
	for p.pos < len(p.tokens) {
		token := p.peek()
		if token == "}" {
			p.pos++
			return nil
		}
		switch token {
		case "subnet":
			if err := p.parseSubnet(); err != nil {
				return err
			}
		case "host":
			host, err := p.parseHost(currentSubnet)
			if err != nil {
				return err
			}
			if currentSubnet != nil {
				currentSubnet.Reservations = append(currentSubnet.Reservations, host)
			} else {
				p.data.Hosts = append(p.data.Hosts, host)
			}
		case "group", "shared-network", "pool":
			if err := p.parseWrapper(currentSubnet); err != nil {
				return err
			}
		case "option":
			if err := p.parseOption(currentSubnet); err != nil {
				return err
			}
		case "range":
			if err := p.parseRange(currentSubnet); err != nil {
				return err
			}
		case "default-lease-time":
			if err := p.parseLeaseTime(currentSubnet); err != nil {
				return err
			}
		default:
			p.skipStatementOrBlock()
		}
	}
	return nil
}

func (p *iscParser) parseSubnet() error {
	if _, err := p.consume("subnet"); err != nil {
		return err
	}
	network, err := p.next()
	if err != nil {
		return err
	}
	if _, err := p.consume("netmask"); err != nil {
		return err
	}
	netmask, err := p.next()
	if err != nil {
		return err
	}
	if _, err := p.consume("{"); err != nil {
		return err
	}
	subnet := iscSubnet{
		Network: network,
		Netmask: netmask,
	}
	if err := p.parseStatements(&subnet); err != nil {
		return err
	}
	p.data.Subnets = append(p.data.Subnets, subnet)
	return nil
}

func (p *iscParser) parseWrapper(currentSubnet *iscSubnet) error {
	start, _ := p.next()
	for p.pos < len(p.tokens) && p.peek() != "{" && p.peek() != ";" {
		p.pos++
	}
	if p.pos >= len(p.tokens) {
		return fmt.Errorf("unterminated %s block", start)
	}
	if p.peek() == ";" {
		p.pos++
		return nil
	}
	p.pos++
	return p.parseStatements(currentSubnet)
}

func (p *iscParser) parseHost(currentSubnet *iscSubnet) (iscHost, error) {
	if _, err := p.consume("host"); err != nil {
		return iscHost{}, err
	}
	name, err := p.next()
	if err != nil {
		return iscHost{}, err
	}
	if _, err := p.consume("{"); err != nil {
		return iscHost{}, err
	}
	host := iscHost{Name: name}
	if currentSubnet != nil {
		if cidr, cidrErr := subnetCIDR(currentSubnet.Network, currentSubnet.Netmask); cidrErr == nil {
			host.SubnetCIDR = cidr
		}
	}

	for p.pos < len(p.tokens) {
		token := p.peek()
		if token == "}" {
			p.pos++
			break
		}
		switch token {
		case "hardware":
			p.pos++
			if p.peek() == "ethernet" {
				p.pos++
			}
			value, valueErr := p.next()
			if valueErr != nil {
				return iscHost{}, valueErr
			}
			host.MAC = value
			if _, err := p.consume(";"); err != nil {
				return iscHost{}, err
			}
		case "fixed-address":
			p.pos++
			value, valueErr := p.next()
			if valueErr != nil {
				return iscHost{}, valueErr
			}
			host.FixedAddress = value
			if _, err := p.consume(";"); err != nil {
				return iscHost{}, err
			}
		case "option":
			p.pos++
			nameToken, nameErr := p.next()
			if nameErr != nil {
				return iscHost{}, nameErr
			}
			values := p.collectUntilSemicolon()
			if strings.EqualFold(nameToken, "host-name") && len(values) > 0 {
				host.Hostname = values[0]
			}
		default:
			p.skipStatementOrBlock()
		}
	}

	if host.Hostname == "" {
		host.Hostname = host.Name
	}
	return host, nil
}

func (p *iscParser) parseOption(currentSubnet *iscSubnet) error {
	if _, err := p.consume("option"); err != nil {
		return err
	}
	name, err := p.next()
	if err != nil {
		return err
	}
	values := p.collectUntilSemicolon()
	if currentSubnet == nil {
		return nil
	}
	switch strings.ToLower(name) {
	case "routers":
		if len(values) > 0 {
			currentSubnet.Gateway = values[0]
		}
	case "domain-name-servers":
		currentSubnet.DNS = append([]string(nil), values...)
	}
	return nil
}

func (p *iscParser) parseRange(currentSubnet *iscSubnet) error {
	if _, err := p.consume("range"); err != nil {
		return err
	}
	if currentSubnet == nil {
		p.collectUntilSemicolon()
		return nil
	}
	start, err := p.next()
	if err != nil {
		return err
	}
	if strings.EqualFold(start, "dynamic-bootp") {
		start, err = p.next()
		if err != nil {
			return err
		}
	}
	end, err := p.next()
	if err != nil {
		return err
	}
	currentSubnet.Ranges = append(currentSubnet.Ranges, iscRange{Start: start, End: end})
	_, err = p.consume(";")
	return err
}

func (p *iscParser) parseLeaseTime(currentSubnet *iscSubnet) error {
	if _, err := p.consume("default-lease-time"); err != nil {
		return err
	}
	value, err := p.next()
	if err != nil {
		return err
	}
	seconds, parseErr := strconv.ParseInt(value, 10, 64)
	if parseErr != nil {
		return parseErr
	}
	if currentSubnet != nil {
		currentSubnet.LeaseTimeSec = seconds
	}
	_, err = p.consume(";")
	return err
}

func (p *iscParser) peek() string {
	if p.pos >= len(p.tokens) {
		return ""
	}
	return p.tokens[p.pos]
}

func (p *iscParser) next() (string, error) {
	if p.pos >= len(p.tokens) {
		return "", fmt.Errorf("unexpected end of input")
	}
	token := p.tokens[p.pos]
	p.pos++
	return token, nil
}

func (p *iscParser) consume(expected string) (string, error) {
	token, err := p.next()
	if err != nil {
		return "", err
	}
	if token != expected {
		return "", fmt.Errorf("expected %q, got %q", expected, token)
	}
	return token, nil
}

func (p *iscParser) collectUntilSemicolon() []string {
	values := make([]string, 0, 4)
	for p.pos < len(p.tokens) {
		token := p.peek()
		p.pos++
		if token == ";" {
			break
		}
		if token == "," {
			continue
		}
		values = append(values, token)
	}
	return values
}

func (p *iscParser) skipStatementOrBlock() {
	depth := 0
	for p.pos < len(p.tokens) {
		token := p.tokens[p.pos]
		p.pos++
		switch token {
		case "{":
			depth++
		case "}":
			if depth == 0 {
				return
			}
			depth--
			if depth == 0 {
				return
			}
		case ";":
			if depth == 0 {
				return
			}
		}
	}
}

func tokenizeISCConfig(input string) []string {
	tokens := make([]string, 0, len(input)/4)
	var current strings.Builder
	inString := false

	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}

	for idx := 0; idx < len(input); idx++ {
		ch := input[idx]
		if inString {
			if ch == '"' {
				flush()
				inString = false
				continue
			}
			current.WriteByte(ch)
			continue
		}
		switch ch {
		case '"':
			flush()
			inString = true
		case '#':
			flush()
			for idx < len(input) && input[idx] != '\n' {
				idx++
			}
		case '{', '}', ';', ',':
			flush()
			tokens = append(tokens, string(ch))
		case ' ', '\t', '\r', '\n':
			flush()
		default:
			current.WriteByte(ch)
		}
	}
	flush()
	return tokens
}

func assignGlobalHosts(data *iscConfigData) {
	if data == nil {
		return
	}
	for _, host := range data.Hosts {
		addr, err := netip.ParseAddr(strings.TrimSpace(host.FixedAddress))
		if err != nil {
			continue
		}
		for index := range data.Subnets {
			cidr, cidrErr := subnetCIDR(data.Subnets[index].Network, data.Subnets[index].Netmask)
			if cidrErr != nil {
				continue
			}
			prefix, parseErr := netip.ParsePrefix(cidr)
			if parseErr != nil || !prefix.Contains(addr) {
				continue
			}
			host.SubnetCIDR = cidr
			data.Subnets[index].Reservations = append(data.Subnets[index].Reservations, host)
			break
		}
	}
}

func buildISCSubnetImport(subnet iscSubnet) (ipam.UpsertSubnetInput, []ipam.UpsertReservationInput, []string, error) {
	cidr, err := subnetCIDR(subnet.Network, subnet.Netmask)
	if err != nil {
		return ipam.UpsertSubnetInput{}, nil, nil, err
	}

	poolStart, poolEnd, warnings, err := mergeISCRanges(subnet.Ranges)
	if err != nil {
		return ipam.UpsertSubnetInput{}, nil, nil, err
	}
	if subnet.LeaseTimeSec <= 0 {
		subnet.LeaseTimeSec = int64((12 * time.Hour).Seconds())
	}

	payload := ipam.UpsertSubnetInput{
		CIDR:       cidr,
		Name:       cidr,
		Gateway:    subnet.Gateway,
		DNS:        append([]string(nil), subnet.DNS...),
		DHCPEnable: poolStart != "" && poolEnd != "",
		PoolStart:  poolStart,
		PoolEnd:    poolEnd,
		LeaseSec:   subnet.LeaseTimeSec,
	}

	reservations := make([]ipam.UpsertReservationInput, 0, len(subnet.Reservations))
	for _, host := range subnet.Reservations {
		if strings.TrimSpace(host.MAC) == "" || strings.TrimSpace(host.FixedAddress) == "" {
			continue
		}
		reservations = append(reservations, ipam.UpsertReservationInput{
			MAC:        host.MAC,
			IP:         host.FixedAddress,
			Hostname:   host.Hostname,
			SubnetCIDR: cidr,
		})
	}
	return payload, reservations, warnings, nil
}

func subnetCIDR(network string, netmask string) (string, error) {
	ip := net.ParseIP(strings.TrimSpace(network)).To4()
	maskIP := net.ParseIP(strings.TrimSpace(netmask)).To4()
	if ip == nil || maskIP == nil {
		return "", fmt.Errorf("invalid subnet definition %s netmask %s", network, netmask)
	}
	mask := net.IPMask(maskIP)
	ones, bits := mask.Size()
	if bits != 32 {
		return "", fmt.Errorf("invalid netmask %s", netmask)
	}
	return netip.PrefixFrom(netip.AddrFrom4([4]byte{ip[0], ip[1], ip[2], ip[3]}), ones).Masked().String(), nil
}

func mergeISCRanges(ranges []iscRange) (string, string, []string, error) {
	if len(ranges) == 0 {
		return "", "", nil, nil
	}
	type ipRange struct {
		start netip.Addr
		end   netip.Addr
	}
	items := make([]ipRange, 0, len(ranges))
	for _, item := range ranges {
		start, err := netip.ParseAddr(strings.TrimSpace(item.Start))
		if err != nil {
			return "", "", nil, err
		}
		end, err := netip.ParseAddr(strings.TrimSpace(item.End))
		if err != nil {
			return "", "", nil, err
		}
		items = append(items, ipRange{start: start, end: end})
	}
	for i := 0; i < len(items)-1; i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].start.Compare(items[i].start) < 0 {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	start := items[0].start
	end := items[0].end
	warnings := []string{}
	for idx := 1; idx < len(items); idx++ {
		next := items[idx]
		if end.Next().IsValid() && next.start.Compare(end.Next()) <= 0 {
			if next.end.Compare(end) > 0 {
				end = next.end
			}
			continue
		}
		return "", "", nil, fmt.Errorf("multiple non-contiguous ISC ranges are not supported")
	}
	if len(items) > 1 {
		warnings = append(warnings, "multiple ISC ranges were merged into a single Monsoon DHCP range")
	}
	return start.String(), end.String(), warnings, nil
}

func parseISCDHCPLeases(path string, subnets []iscSubnet) ([]iscLeaseRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	records := make([]iscLeaseRecord, 0, 64)
	var current *iscLeaseRecord
	for scanner.Scan() {
		line := stripInlineComment(scanner.Text())
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "lease ") && strings.HasSuffix(line, "{") {
			ip := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "lease "), "{"))
			current = &iscLeaseRecord{
				IP:    ip,
				State: lease.StateBound,
			}
			continue
		}
		if current == nil {
			continue
		}
		if line == "}" {
			current.SubnetID = inferISCSubnetForIP(current.IP, subnets)
			records = append(records, *current)
			current = nil
			continue
		}

		line = strings.TrimSuffix(line, ";")
		switch {
		case strings.HasPrefix(line, "binding state "):
			current.State = mapISCBindingState(strings.TrimSpace(strings.TrimPrefix(line, "binding state ")))
		case strings.HasPrefix(line, "hardware ethernet "):
			current.MAC = strings.TrimSpace(strings.TrimPrefix(line, "hardware ethernet "))
		case strings.HasPrefix(line, "client-hostname "):
			current.Hostname = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "client-hostname ")), `"`)
		case strings.HasPrefix(line, "starts "):
			if ts, tsErr := parseISCTimestamp(strings.TrimSpace(strings.TrimPrefix(line, "starts "))); tsErr == nil {
				current.StartTime = ts
			}
		case strings.HasPrefix(line, "ends "):
			if ts, tsErr := parseISCTimestamp(strings.TrimSpace(strings.TrimPrefix(line, "ends "))); tsErr == nil {
				current.ExpiryTime = ts
			}
		case strings.HasPrefix(line, "cltt "):
			if ts, tsErr := parseISCTimestamp(strings.TrimSpace(strings.TrimPrefix(line, "cltt "))); tsErr == nil && current.StartTime.IsZero() {
				current.StartTime = ts
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func mapISCBindingState(value string) lease.LeaseState {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "active":
		return lease.StateBound
	case "free":
		return lease.StateReleased
	case "expired":
		return lease.StateExpired
	case "released":
		return lease.StateReleased
	case "abandoned":
		return lease.StateDeclined
	case "backup":
		return lease.StateRenewing
	default:
		return lease.StateBound
	}
}

func parseISCTimestamp(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "never") {
		return time.Time{}, nil
	}
	parts := strings.Fields(value)
	if len(parts) == 3 {
		return time.Parse("2006/01/02 15:04:05", parts[1]+" "+parts[2])
	}
	if len(parts) == 2 {
		return time.Parse("2006/01/02 15:04:05", parts[0]+" "+parts[1])
	}
	return time.Time{}, fmt.Errorf("invalid ISC timestamp: %s", value)
}

func inferISCSubnetForIP(ip string, subnets []iscSubnet) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return ""
	}
	for _, subnet := range subnets {
		cidr, cidrErr := subnetCIDR(subnet.Network, subnet.Netmask)
		if cidrErr != nil {
			continue
		}
		prefix, parseErr := netip.ParsePrefix(cidr)
		if parseErr != nil {
			continue
		}
		if prefix.Contains(addr) {
			return cidr
		}
	}
	return ""
}

func stripInlineComment(input string) string {
	inString := false
	var out strings.Builder
	for idx := 0; idx < len(input); idx++ {
		ch := input[idx]
		if ch == '"' {
			inString = !inString
			out.WriteByte(ch)
			continue
		}
		if ch == '#' && !inString {
			break
		}
		out.WriteByte(ch)
	}
	return out.String()
}
