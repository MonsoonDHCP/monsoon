package migrate

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/monsoondhcp/monsoon/internal/ipam"
	"github.com/monsoondhcp/monsoon/internal/lease"
)

type csvRunReport struct {
	Files    []FileReport
	Warnings []string
}

type csvRow map[string]string

func (r *Runner) runCSV(ctx context.Context, opts CSVOptions, dryRun bool, conflictPolicy string) (csvRunReport, error) {
	files := []struct {
		kind string
		path string
		run  func(context.Context, csvRow, bool, string) (bool, error)
	}{
		{kind: "subnets", path: opts.SubnetsPath, run: r.importSubnetRow},
		{kind: "addresses", path: opts.AddressesPath, run: r.importAddressRow},
		{kind: "reservations", path: opts.ReservationsPath, run: r.importReservationRow},
		{kind: "leases", path: opts.LeasesPath, run: r.importLeaseRow},
	}

	report := csvRunReport{}
	enabled := 0
	for _, file := range files {
		if strings.TrimSpace(file.path) == "" {
			continue
		}
		enabled++
		result, err := r.importCSVFile(ctx, file.kind, file.path, dryRun, conflictPolicy, file.run)
		report.Files = append(report.Files, result)
		if err != nil {
			report.Warnings = append(report.Warnings, err.Error())
		}
	}

	if enabled == 0 {
		return report, fmt.Errorf("csv migration requires at least one input file")
	}
	return report, nil
}

func (r *Runner) importCSVFile(
	ctx context.Context,
	kind string,
	path string,
	dryRun bool,
	conflictPolicy string,
	importer func(context.Context, csvRow, bool, string) (bool, error),
) (FileReport, error) {
	rows, err := readCSVRows(path)
	fileReport := FileReport{
		Kind: kind,
		Path: path,
		Rows: len(rows),
	}
	if err != nil {
		fileReport.Errors = append(fileReport.Errors, RowError{Row: 0, Message: err.Error()})
		return fileReport, err
	}

	for idx, row := range rows {
		if ctx.Err() != nil {
			fileReport.Errors = append(fileReport.Errors, RowError{Row: idx + 2, Message: ctx.Err().Error()})
			break
		}
		skipped, rowErr := importer(ctx, row, dryRun, conflictPolicy)
		if rowErr != nil {
			fileReport.Errors = append(fileReport.Errors, RowError{Row: idx + 2, Message: rowErr.Error()})
			continue
		}
		if skipped {
			fileReport.Skipped++
		} else {
			fileReport.Applied++
		}
	}

	if len(fileReport.Errors) > 0 {
		return fileReport, fmt.Errorf("%s import reported %d row errors", kind, len(fileReport.Errors))
	}
	return fileReport, nil
}

func (r *Runner) importSubnetRow(ctx context.Context, row csvRow, dryRun bool, conflictPolicy string) (bool, error) {
	cidr := field(row, "cidr", "subnet", "prefix")
	if cidr == "" {
		return false, fmt.Errorf("cidr is required")
	}
	payload := ipam.UpsertSubnetInput{
		CIDR:       cidr,
		Name:       field(row, "name"),
		VLAN:       intField(row, "vlan", "vlan_id"),
		Gateway:    field(row, "gateway"),
		DNS:        splitList(field(row, "dns", "dns_servers")),
		DHCPEnable: boolField(row, "dhcp_enabled", "dhcp", "enabled"),
		PoolStart:  field(row, "pool_start", "range_start"),
		PoolEnd:    field(row, "pool_end", "range_end"),
		LeaseSec:   durationSecondsField(row, "lease_time_sec", "lease_seconds", "lease_duration", "lease_time"),
	}
	if payload.PoolStart != "" || payload.PoolEnd != "" {
		payload.DHCPEnable = true
	}
	if conflictPolicy == ConflictSkip {
		if _, err := r.ipam.GetSubnet(ctx, payload.CIDR); err == nil {
			return true, nil
		}
	}
	if dryRun {
		_, err := r.ipam.ValidateSubnet(context.Background(), payload)
		return false, err
	}
	_, err := r.ipam.UpsertSubnet(ctx, payload)
	return false, err
}

func (r *Runner) importAddressRow(ctx context.Context, row csvRow, dryRun bool, conflictPolicy string) (bool, error) {
	ip := field(row, "ip", "address")
	if ip == "" {
		return false, fmt.Errorf("ip is required")
	}
	payload := ipam.UpsertAddressInput{
		IP:         ip,
		SubnetCIDR: field(row, "subnet_cidr", "subnet", "cidr"),
		State:      ipam.IPState(strings.ToLower(field(row, "state"))),
		MAC:        field(row, "mac"),
		Hostname:   field(row, "hostname", "dns_name"),
		Source:     field(row, "source"),
		UpdatedAt:  timeField(row, "updated_at", "last_seen"),
	}
	if conflictPolicy == ConflictSkip {
		if _, err := r.ipam.GetStoredAddress(ctx, payload.IP); err == nil {
			return true, nil
		}
	}
	if dryRun {
		_, err := r.ipam.ValidateAddress(ctx, payload)
		return false, err
	}
	_, err := r.ipam.UpsertAddress(ctx, payload)
	return false, err
}

func (r *Runner) importReservationRow(ctx context.Context, row csvRow, dryRun bool, conflictPolicy string) (bool, error) {
	payload := ipam.UpsertReservationInput{
		MAC:        field(row, "mac"),
		IP:         field(row, "ip", "address"),
		Hostname:   field(row, "hostname", "name"),
		SubnetCIDR: field(row, "subnet_cidr", "subnet", "cidr"),
	}
	if payload.MAC == "" || payload.IP == "" {
		return false, fmt.Errorf("mac and ip are required")
	}
	if conflictPolicy == ConflictSkip {
		if _, err := r.ipam.GetReservationByMAC(ctx, payload.MAC); err == nil {
			return true, nil
		}
	}
	if dryRun {
		_, err := r.ipam.ValidateReservation(ctx, payload)
		return false, err
	}
	_, err := r.ipam.UpsertReservation(ctx, payload)
	return false, err
}

func (r *Runner) importLeaseRow(ctx context.Context, row csvRow, dryRun bool, conflictPolicy string) (bool, error) {
	ip := field(row, "ip", "address", "ip_address")
	mac := field(row, "mac", "hw_address")
	if ip == "" || mac == "" {
		return false, fmt.Errorf("ip and mac are required")
	}
	if _, err := netip.ParseAddr(ip); err != nil {
		return false, fmt.Errorf("invalid ip: %w", err)
	}

	if conflictPolicy == ConflictSkip {
		if _, err := r.leases.GetByIP(ctx, ip); err == nil {
			return true, nil
		}
	}

	now := time.Now().UTC()
	startTime := timeField(row, "start_time", "starts_at", "created_at", "cltt")
	if startTime.IsZero() {
		startTime = now
	}
	duration := durationField(row, "duration", "duration_sec", "lease_time", "lease_duration", "valid_lft")
	expiryTime := timeField(row, "expiry_time", "expires_at", "end_time")
	if duration <= 0 && !expiryTime.IsZero() && expiryTime.After(startTime) {
		duration = expiryTime.Sub(startTime)
	}
	if expiryTime.IsZero() {
		if expireEpoch := field(row, "expire"); expireEpoch != "" {
			if seconds, err := strconv.ParseInt(expireEpoch, 10, 64); err == nil {
				expiryTime = time.Unix(seconds, 0).UTC()
			}
		}
	}
	if duration <= 0 {
		duration = 12 * time.Hour
	}
	if expiryTime.IsZero() {
		expiryTime = startTime.Add(duration)
	}

	record := lease.Lease{
		IP:          ip,
		MAC:         mac,
		Hostname:    field(row, "hostname", "name"),
		State:       leaseStateField(row, "state"),
		StartTime:   startTime,
		Duration:    duration,
		ExpiryTime:  expiryTime,
		SubnetID:    field(row, "subnet_id", "subnet_cidr", "subnet"),
		RelayAddr:   field(row, "relay_addr"),
		CircuitID:   field(row, "circuit_id"),
		RemoteID:    field(row, "remote_id"),
		VendorClass: field(row, "vendor_class"),
		UserClass:   field(row, "user_class"),
		LastSeen:    timeField(row, "last_seen", "updated_at"),
		CreatedAt:   timeField(row, "created_at"),
		UpdatedAt:   timeField(row, "updated_at"),
	}

	if dryRun {
		return false, validateLeaseRecord(record)
	}
	return false, r.leases.Upsert(ctx, record)
}

func readCSVRows(path string) ([]csvRow, error) {
	// #nosec G304 -- migration source path is explicitly provided by operator.
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return []csvRow{}, nil
		}
		return nil, err
	}
	for idx := range header {
		header[idx] = normalizeHeader(header[idx])
	}

	rows := make([]csvRow, 0, 64)
	for {
		record, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				return rows, nil
			}
			return nil, err
		}
		row := make(csvRow, len(header))
		for idx, key := range header {
			value := ""
			if idx < len(record) {
				value = strings.TrimSpace(record[idx])
			}
			row[key] = value
		}
		rows = append(rows, row)
	}
}

func normalizeHeader(in string) string {
	replacer := strings.NewReplacer(" ", "_", "-", "_", ".", "_", "/", "_")
	return strings.ToLower(strings.TrimSpace(replacer.Replace(in)))
}

func field(row csvRow, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(row[normalizeHeader(key)]); value != "" {
			return value
		}
	}
	return ""
}

func splitList(in string) []string {
	in = strings.TrimSpace(in)
	if in == "" {
		return nil
	}
	fields := strings.FieldsFunc(in, func(r rune) bool {
		return r == ',' || r == ';' || r == '|'
	})
	out := make([]string, 0, len(fields))
	for _, item := range fields {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func boolField(row csvRow, keys ...string) bool {
	value := strings.ToLower(field(row, keys...))
	switch value {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func intField(row csvRow, keys ...string) int {
	value := field(row, keys...)
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}

func durationSecondsField(row csvRow, keys ...string) int64 {
	value := field(row, keys...)
	if value == "" {
		return 0
	}
	if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
		return parsed
	}
	if duration, err := time.ParseDuration(value); err == nil {
		return int64(duration.Seconds())
	}
	return 0
}

func durationField(row csvRow, keys ...string) time.Duration {
	value := field(row, keys...)
	if value == "" {
		return 0
	}
	if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Duration(parsed) * time.Second
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0
	}
	return duration
}

func timeField(row csvRow, keys ...string) time.Time {
	value := field(row, keys...)
	if value == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	if unixSeconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Unix(unixSeconds, 0).UTC()
	}
	return time.Time{}
}

func leaseStateField(row csvRow, key string) lease.LeaseState {
	value := strings.ToLower(field(row, key))
	switch value {
	case "0":
		return lease.StateBound
	case "1":
		return lease.StateDeclined
	case "2":
		return lease.StateExpired
	}
	switch lease.LeaseState(value) {
	case lease.StateFree,
		lease.StateOffered,
		lease.StateBound,
		lease.StateRenewing,
		lease.StateReleased,
		lease.StateDeclined,
		lease.StateQuarantined,
		lease.StateExpired:
		return lease.LeaseState(value)
	default:
		return lease.StateBound
	}
}

func validateLeaseRecord(record lease.Lease) error {
	if _, err := netip.ParseAddr(strings.TrimSpace(record.IP)); err != nil {
		return fmt.Errorf("invalid ip: %w", err)
	}
	if strings.TrimSpace(record.MAC) == "" {
		return fmt.Errorf("mac is required")
	}
	record.NormalizeDefaults(time.Now().UTC(), 12*time.Hour)
	return nil
}
