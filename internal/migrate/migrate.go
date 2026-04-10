package migrate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monsoondhcp/monsoon/internal/ipam"
	"github.com/monsoondhcp/monsoon/internal/lease"
)

const (
	SourceCSV = "csv"

	ConflictOverwrite = "overwrite"
	ConflictSkip      = "skip"
)

var ErrUnsupportedSource = errors.New("unsupported migration source")

type Runner struct {
	ipam   *ipam.Engine
	leases lease.Store
}

type Options struct {
	Source         string
	DryRun         bool
	ConflictPolicy string
	CSV            CSVOptions
}

type CSVOptions struct {
	SubnetsPath      string
	AddressesPath    string
	ReservationsPath string
	LeasesPath       string
}

type Report struct {
	Source      string       `json:"source"`
	DryRun      bool         `json:"dry_run"`
	StartedAt   time.Time    `json:"started_at"`
	CompletedAt time.Time    `json:"completed_at"`
	Files       []FileReport `json:"files"`
	Warnings    []string     `json:"warnings,omitempty"`
}

type FileReport struct {
	Kind    string     `json:"kind"`
	Path    string     `json:"path"`
	Rows    int        `json:"rows"`
	Applied int        `json:"applied"`
	Skipped int        `json:"skipped"`
	Errors  []RowError `json:"errors,omitempty"`
}

type RowError struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}

func NewRunner(ipamEngine *ipam.Engine, leaseStore lease.Store) *Runner {
	return &Runner{
		ipam:   ipamEngine,
		leases: leaseStore,
	}
}

func (r *Runner) Run(ctx context.Context, opts Options) (Report, error) {
	source := strings.ToLower(strings.TrimSpace(opts.Source))
	if source == "" && (opts.CSV.SubnetsPath != "" || opts.CSV.AddressesPath != "" || opts.CSV.ReservationsPath != "" || opts.CSV.LeasesPath != "") {
		source = SourceCSV
	}
	report := Report{
		Source:    source,
		DryRun:    opts.DryRun,
		StartedAt: time.Now().UTC(),
	}
	defer func() {
		report.CompletedAt = time.Now().UTC()
	}()

	if r == nil || r.ipam == nil || r.leases == nil {
		report.Warnings = append(report.Warnings, "migration dependencies are incomplete")
		return report, fmt.Errorf("migration runner dependencies are incomplete")
	}

	policy := strings.ToLower(strings.TrimSpace(opts.ConflictPolicy))
	if policy == "" {
		policy = ConflictOverwrite
	}
	switch policy {
	case ConflictOverwrite, ConflictSkip:
	default:
		return report, fmt.Errorf("invalid conflict policy: %s", opts.ConflictPolicy)
	}

	switch report.Source {
	case SourceCSV:
		csvReport, err := r.runCSV(ctx, opts.CSV, opts.DryRun, policy)
		report.Files = csvReport.Files
		report.Warnings = append(report.Warnings, csvReport.Warnings...)
		report.CompletedAt = time.Now().UTC()
		if err != nil {
			return report, err
		}
		if hasRowErrors(report.Files) {
			return report, fmt.Errorf("migration completed with %d row errors", countRowErrors(report.Files))
		}
		return report, nil
	default:
		report.CompletedAt = time.Now().UTC()
		return report, fmt.Errorf("%w: %s", ErrUnsupportedSource, opts.Source)
	}
}

func hasRowErrors(files []FileReport) bool {
	return countRowErrors(files) > 0
}

func countRowErrors(files []FileReport) int {
	total := 0
	for _, file := range files {
		total += len(file.Errors)
	}
	return total
}
