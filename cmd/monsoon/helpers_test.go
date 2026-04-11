package main

import (
	"bytes"
	"io"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/monsoondhcp/monsoon/internal/config"
	"github.com/monsoondhcp/monsoon/internal/migrate"
)

func TestPickServerIdentifierPrefersGatewayThenListenThenLoopback(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Subnets = []config.SubnetConfig{{Gateway: "10.0.0.1"}}
	if got := pickServerIdentifier(cfg); !got.Equal(net.IPv4(10, 0, 0, 1).To4()) {
		t.Fatalf("expected gateway ip, got %v", got)
	}

	cfg.Subnets = nil
	cfg.DHCP.V4.Listen = "192.0.2.5:67"
	if got := pickServerIdentifier(cfg); !got.Equal(net.ParseIP("192.0.2.5").To4()) {
		t.Fatalf("expected listen host ip, got %v", got)
	}

	cfg.DHCP.V4.Listen = ":67"
	if got := pickServerIdentifier(cfg); !got.Equal(net.IPv4(127, 0, 0, 1).To4()) {
		t.Fatalf("expected loopback fallback, got %v", got)
	}
}

func TestPickServerDUIDAndFirstSubnet(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.Hostname = "node-a"
	first := pickServerDUID(cfg)
	second := pickServerDUID(cfg)
	if len(first) == 0 || !bytes.Equal(first, second) {
		t.Fatalf("expected deterministic duid bytes, got %x vs %x", first, second)
	}

	if got := firstSubnet(nil); got != "" {
		t.Fatalf("expected empty first subnet, got %q", got)
	}
	if got := firstSubnet([]string{" 10.0.0.0/24 ", "10.0.1.0/24"}); got != "10.0.0.0/24" {
		t.Fatalf("unexpected first subnet %q", got)
	}
}

func TestPrintMigrationReport(t *testing.T) {
	report := migrate.Report{
		Source: "csv",
		DryRun: true,
		Files: []migrate.FileReport{{
			Kind:    "subnets",
			Path:    "subnets.csv",
			Rows:    2,
			Applied: 1,
			Skipped: 1,
			Errors: []migrate.RowError{{
				Row:     2,
				Message: "duplicate",
			}},
		}},
		Warnings: []string{"warn"},
	}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	os.Stdout = w
	printMigrationReport(report)
	_ = w.Close()
	os.Stdout = oldStdout

	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read report output: %v", err)
	}
	output := string(body)
	for _, fragment := range []string{"migration source=csv", "subnets: path=subnets.csv", "row 2: duplicate", "warning: warn"} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("expected output to contain %q, got %q", fragment, output)
		}
	}
}
