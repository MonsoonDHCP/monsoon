# Monsoon — Implementation Guide

> Status note (2026-04-12): this guide contains intended architecture and historical plan, not a file-by-file description of the current implementation.
> Important current-build differences:
> - Web UI is React/Vite/Tailwind in `web/src`, not the older vanilla JS/CSS structure shown below.
> - Storage is WAL + snapshot + sorted in-memory structures; it is not a full page-backed B+Tree engine.
> - LDAP auth is not implemented.
> - Discovery does not yet include true passive rogue-DHCP sensing.
> - Sessions are persisted locally in storage and survive restart on the same node.
> - Backup restore exists in both CLI and REST.

## Project Structure

```
monsoon/
├── cmd/
│   └── monsoon/
│       └── main.go                 # Entry point, CLI flags, signal handling
├── internal/
│   ├── config/
│   │   ├── config.go               # Configuration struct + YAML parsing
│   │   ├── defaults.go             # Default values
│   │   ├── validate.go             # Configuration validation
│   │   ├── env.go                  # Environment variable overrides
│   │   └── reload.go               # Hot-reload via SIGHUP
│   ├── dhcpv4/
│   │   ├── server.go               # DHCPv4 UDP listener + dispatcher
│   │   ├── handler.go              # DISCOVER/OFFER/REQUEST/ACK handler
│   │   ├── packet.go               # DHCPv4 packet encode/decode (RFC 2131)
│   │   ├── options.go              # DHCP option encode/decode (RFC 2132)
│   │   ├── option_types.go         # Option type definitions (1-255)
│   │   ├── relay.go                # Relay agent (Option 82) processing
│   │   ├── classification.go       # Client classification engine
│   │   ├── pool.go                 # Address pool allocation logic
│   │   ├── rapid_commit.go         # RFC 4039 rapid commit support
│   │   └── broadcast.go            # Raw socket broadcast handling
│   ├── dhcpv6/
│   │   ├── server.go               # DHCPv6 UDP listener + dispatcher
│   │   ├── handler.go              # Solicit/Advertise/Request/Reply handler
│   │   ├── packet.go               # DHCPv6 packet encode/decode (RFC 8415)
│   │   ├── options.go              # DHCPv6 option encode/decode
│   │   ├── duid.go                 # DUID generation and parsing
│   │   ├── pd.go                   # Prefix delegation (RFC 3633)
│   │   ├── relay.go                # Relay-Forward / Relay-Reply
│   │   └── pool.go                 # IPv6 address/prefix pool
│   ├── lease/
│   │   ├── store.go                # Lease store interface
│   │   ├── state.go                # Lease state machine
│   │   ├── expiry.go               # Lease expiry timer/sweeper
│   │   ├── sync.go                 # Lease HA synchronization
│   │   └── types.go                # Lease data types
│   ├── ipam/
│   │   ├── engine.go               # IPAM core engine
│   │   ├── subnet.go               # Subnet CRUD + tree operations
│   │   ├── address.go              # IP address CRUD + state tracking
│   │   ├── reservation.go          # DHCP reservation management
│   │   ├── vlan.go                 # VLAN management
│   │   ├── cidr.go                 # CIDR calculator (split/merge/supernet)
│   │   ├── capacity.go             # Utilization calculation + forecasting
│   │   └── types.go                # IPAM data types
│   ├── discovery/
│   │   ├── engine.go               # Discovery orchestrator
│   │   ├── arp.go                  # ARP scan implementation
│   │   ├── ping.go                 # ICMP ping sweep
│   │   ├── tcp.go                  # TCP connect probe
│   │   ├── dns.go                  # Reverse DNS lookup
│   │   ├── passive.go              # Planned passive DHCP listener
│   │   ├── conflict.go             # Conflict detection logic
│   │   ├── rogue.go                # Planned rogue DHCP server detection
│   │   ├── oui.go                  # MAC vendor OUI lookup (embedded DB)
│   │   ├── scheduler.go            # Scan scheduler (cron-like)
│   │   └── types.go                # Discovery result types
│   ├── ddns/
│   │   ├── client.go               # DNS UPDATE client (RFC 2136)
│   │   ├── tsig.go                 # TSIG authentication
│   │   └── types.go                # DDNS types
│   ├── storage/
│   │   ├── engine.go               # Storage engine interface
│   │   ├── btree.go                # Simplified sorted in-memory tree
│   │   ├── wal.go                  # Write-ahead log
│   │   ├── page.go                 # Page manager (4KB pages)
│   │   ├── snapshot.go             # Snapshot create/restore
│   │   ├── index.go                # Secondary index management
│   │   ├── iterator.go             # Range scan iterator
│   │   ├── codec.go                # Binary serialization
│   │   └── types.go                # Storage types
│   ├── api/
│   │   ├── rest/
│   │   │   ├── server.go           # HTTP server setup + middleware
│   │   │   ├── router.go           # Route registration
│   │   │   ├── middleware.go        # Auth, CORS, rate limit, logging
│   │   │   ├── subnet_handler.go   # Subnet endpoints
│   │   │   ├── address_handler.go  # Address endpoints
│   │   │   ├── lease_handler.go    # Lease endpoints
│   │   │   ├── reservation_handler.go # Reservation endpoints
│   │   │   ├── vlan_handler.go     # VLAN endpoints
│   │   │   ├── discovery_handler.go # Discovery endpoints
│   │   │   ├── audit_handler.go    # Audit log endpoints
│   │   │   ├── system_handler.go   # System/health endpoints
│   │   │   ├── auth_handler.go     # Login/logout/token endpoints
│   │   │   └── response.go         # JSON response helpers
│   │   ├── grpc/
│   │   │   ├── server.go           # gRPC server setup
│   │   │   ├── subnet_service.go   # Subnet gRPC service
│   │   │   ├── lease_service.go    # Lease gRPC service
│   │   │   ├── address_service.go  # Address gRPC service
│   │   │   └── discovery_service.go # Discovery gRPC service
│   │   ├── websocket/
│   │   │   ├── hub.go              # WebSocket connection hub
│   │   │   ├── client.go           # WebSocket client handler
│   │   │   └── events.go           # Event types + serialization
│   │   └── mcp/
│   │       ├── server.go           # MCP server (JSON-RPC 2.0 / SSE)
│   │       ├── tools.go            # MCP tool definitions
│   │       └── handlers.go         # MCP tool handlers
│   ├── auth/
│   │   ├── auth.go                 # Authentication interface
│   │   ├── local.go                # Local user store (bcrypt)
│   │   ├── ldap.go                 # Planned LDAP authentication
│   │   ├── token.go                # API token management
│   │   ├── session.go              # Session management
│   │   └── rbac.go                 # Role-based access control
│   ├── ha/
│   │   ├── failover.go             # Active-passive failover
│   │   ├── heartbeat.go            # Peer heartbeat
│   │   ├── sync.go                 # Lease sync protocol
│   │   └── election.go             # Leader election
│   ├── audit/
│   │   ├── logger.go               # Audit log writer
│   │   ├── query.go                # Audit log queries
│   │   └── types.go                # Audit entry types
│   ├── metrics/
│   │   └── prometheus.go           # Prometheus metrics registry
│   ├── webhook/
│   │   ├── dispatcher.go           # Webhook event dispatcher
│   │   ├── delivery.go             # HTTP delivery with retry
│   │   └── types.go                # Webhook types
│   ├── migrate/
│   │   ├── migrate.go              # Migration orchestrator
│   │   ├── isc_dhcp.go             # ISC DHCP config/lease parser
│   │   ├── kea.go                  # Kea config/lease parser
│   │   ├── phpipam.go              # phpIPAM API importer
│   │   ├── netbox.go               # NetBox API importer
│   │   └── csv.go                  # Generic CSV importer
│   └── dashboard/
│       └── embed.go                # go:embed for web dashboard files
├── web/                            # Web Dashboard (React/Vite/TypeScript)
│   ├── index.html                  # SPA shell
│   ├── css/
│   │   ├── variables.css           # CSS custom properties (theme)
│   │   ├── base.css                # Reset + typography
│   │   ├── layout.css              # Grid/flexbox layout
│   │   ├── components.css          # Buttons, forms, tables, cards
│   │   └── pages.css               # Page-specific styles
│   ├── js/
│   │   ├── app.js                  # App initialization + router
│   │   ├── api.js                  # REST API client
│   │   ├── ws.js                   # WebSocket client
│   │   ├── auth.js                 # Auth state management
│   │   ├── router.js               # Client-side SPA router
│   │   ├── components/
│   │   │   ├── sidebar.js          # Navigation sidebar
│   │   │   ├── header.js           # Top bar + user menu
│   │   │   ├── table.js            # Data table component
│   │   │   ├── modal.js            # Modal dialog
│   │   │   ├── toast.js            # Toast notifications
│   │   │   ├── chart.js            # Utilization charts (SVG)
│   │   │   ├── tree.js             # Subnet tree component
│   │   │   ├── ipgrid.js           # IP grid visualization
│   │   │   └── form.js             # Form builder
│   │   └── pages/
│   │       ├── dashboard.js        # Overview page
│   │       ├── subnets.js          # Subnet management
│   │       ├── subnet-detail.js    # Single subnet view + IP grid
│   │       ├── leases.js           # Lease browser
│   │       ├── reservations.js     # Reservation manager
│   │       ├── vlans.js            # VLAN management
│   │       ├── discovery.js        # Discovery & scanning
│   │       ├── audit.js            # Audit log viewer
│   │       └── settings.js         # Settings pages
│   └── assets/
│       ├── favicon.ico
│       └── logo.svg
├── proto/
│   └── monsoon/
│       └── v1/
│           ├── subnet.proto
│           ├── lease.proto
│           ├── address.proto
│           └── discovery.proto
├── scripts/
│   ├── install.sh                  # Installer script
│   └── migrate-isc.sh             # ISC DHCP migration helper
├── go.mod
├── go.sum
├── Makefile
├── Dockerfile
├── SPECIFICATION.md
├── IMPLEMENTATION.md
├── TASKS.md
├── BRANDING.md
└── README.md
```

---

## Technology Decisions

### Dependencies (Strict #NOFORKANYMORE)

| Dependency | Purpose | Justification |
|-----------|---------|---------------|
| `golang.org/x/sys` | Raw socket, Linux capabilities | Required for DHCP broadcast packets |
| `golang.org/x/crypto` | bcrypt (auth), HMAC-SHA256 (TSIG) | Cryptographic operations |
| `gopkg.in/yaml.v3` | YAML config parsing | Configuration format |
| **stdlib only** | Everything else | HTTP server, JSON, TLS, gRPC (custom), etc. |

### Key Implementation Notes

**No `net/http` third-party router**: Use stdlib `http.ServeMux` (Go 1.22+ with method+path patterns).

**No gRPC framework**: Implement gRPC over HTTP/2 using stdlib `net/http` with `h2c` (unencrypted) or TLS. Binary protobuf encoding done manually — no `protoc` generated code. Define message types in Go structs with custom marshal/unmarshal.

**No database driver**: Custom B+Tree storage engine. All data persisted in binary format with WAL.

**No DHCP library**: Full DHCPv4/v6 packet parsing from scratch. UDP raw sockets via `golang.org/x/sys/unix`.

**No DNS library**: DDNS UPDATE messages constructed manually per RFC 2136.

---

## Module Specifications

### M1: Configuration (internal/config)

**config.go**: Define `Config` struct mirroring YAML structure. Unmarshal with `gopkg.in/yaml.v3`. Validate all fields post-parse. Support hot-reload on SIGHUP: re-read file, validate, atomic swap via `sync.RWMutex`.

**env.go**: Walk struct fields with reflection, generate `MONSOON_` prefixed env var names. Environment overrides applied after YAML parse but before validation.

**validate.go**: Check CIDR validity, port ranges, no overlapping subnets, pool ranges within subnet, gateway within subnet, lease times positive, etc.

### M2: Storage Engine (internal/storage)

**B+Tree**: Historical design intent. The current build uses a simpler sorted in-memory tree abstraction plus WAL/snapshot persistence rather than a full page-backed B+Tree.

**WAL**: Append-only log of mutations. Each entry: `[length:4][crc32:4][type:1][key_len:2][key][value_len:4][value]`. fsync after each transaction. Segment rotation at 64MB.

**Snapshot**: Consistent read of the current tree state. Serialize to a single file with header + sorted key-value pairs. Used for backup, restore, and HA initial sync.

**Indexes**: Multiple logical trees for different access patterns. Composite keys support secondary lookup patterns such as `subnet_id + ip`.

### M3: DHCPv4 Engine (internal/dhcpv4)

**server.go**: Open raw UDP socket on port 67. Use `SO_BINDTODEVICE` for interface binding. Read loop: receive packet → decode → dispatch to handler → encode response → send (unicast or broadcast based on flags).

**packet.go**: Full RFC 2131 BOOTP/DHCP packet structure. 236-byte fixed header + variable options. Magic cookie `0x63825363`. Parse and serialize all fields.

**options.go**: Encode/decode DHCP options (Tag-Length-Value format). Support for all standard options (1-255). Option overloading in `sname` and `file` fields. Vendor-specific options (option 43).

**handler.go**: State machine:
- DISCOVER → allocate IP from pool → send OFFER
- REQUEST → verify offer/renew → send ACK or NAK  
- RELEASE → mark lease released → update IPAM
- DECLINE → mark IP as conflict → quarantine
- INFORM → send options only (no lease)

**pool.go**: Free IP allocation strategy:
1. Check if client has existing lease (by MAC/client-id)
2. Check if client has reservation
3. Check requested IP (option 50) availability
4. Scan pool for first available IP
5. Bitmap-based free IP tracking for O(1) allocation

**relay.go**: When GIADDR ≠ 0.0.0.0, packet came via relay. Use GIADDR to select subnet. Preserve and forward Option 82 sub-options. Send unicast reply to relay address.

### M4: DHCPv6 Engine (internal/dhcpv6)

Similar to DHCPv4 but:
- UDP port 547 (server), 546 (client)
- Uses DUID (DHCP Unique Identifier) instead of MAC for client identification
- Link-local multicast for solicitation (ff02::1:2)
- Support for IA_NA (non-temporary addresses), IA_TA (temporary), IA_PD (prefix delegation)
- Different option format: `[option_code:2][option_len:2][option_data]`

### M5: IPAM Engine (internal/ipam)

**subnet.go**: Hierarchical subnet tree using prefix trie. Operations:
- Create: Validate CIDR, check no overlap, find parent, insert into tree
- Split: Divide /N into two /(N+1) subnets, redistribute IPs
- Merge: Combine adjacent same-size subnets, validate prerequisites
- Utilization: Count IPs by state, calculate percentages

**address.go**: IP address lifecycle management. State transitions triggered by:
- DHCP events (lease create/renew/expire/release)
- Manual operations (reserve/unreserve via API)
- Discovery results (found/not-found)
- Conflict detection (duplicate MAC)

**cidr.go**: Pure CIDR arithmetic:
- `Contains(parent, child net.IPNet) bool`
- `Split(cidr net.IPNet) (net.IPNet, net.IPNet)`
- `Merge(a, b net.IPNet) (net.IPNet, bool)`
- `NextAvailable(subnet net.IPNet, used []net.IP) net.IP`
- `Overlaps(a, b net.IPNet) bool`
- `AddressCount(cidr net.IPNet) int64`
- `NthAddress(cidr net.IPNet, n int64) net.IP`

### M6: Discovery Engine (internal/discovery)

**arp.go**: Construct ARP request packets. Send via raw socket (`AF_PACKET` + `ETH_P_ARP`). Listen for ARP replies. Timeout after configurable wait. Collect IP→MAC mappings.

**ping.go**: ICMP echo request via raw socket (`IPPROTO_ICMP`). Send to each IP in subnet. Configurable concurrency (default 64 goroutines). Timeout per host. Collect responding IPs.

**tcp.go**: TCP connect() probe on configurable ports. Non-blocking with timeout. Detect services by banner grab (SSH, HTTP, etc.).

**conflict.go**: Compare discovery results with IPAM state. Flag:
- New IPs not in IPAM (unknown hosts)
- Known IPs not responding (potentially abandoned)
- Multiple MACs for same IP (conflict)
- DHCP lease but no network presence (orphaned)

**rogue.go**: Planned passive listener on DHCP client port. The current build does not yet implement true passive rogue-DHCP detection.

### M7: REST API (internal/api/rest)

**server.go**: `http.NewServeMux()` with Go 1.22+ patterns. Middleware chain: logging → recovery → CORS → rate-limit → auth → handler. TLS optional via `crypto/tls`.

**response.go**: Standard JSON envelope:
```json
{
  "data": {},
  "meta": {"page": 1, "per_page": 50, "total": 150},
  "error": null
}
```

Pagination via `?page=1&per_page=50`. Filtering via query params: `?state=active&subnet=10.0.1.0/24`. Sorting: `?sort=ip&order=asc`.

### M8: WebSocket Hub (internal/api/websocket)

**hub.go**: Central hub managing all WebSocket connections. Event bus pattern: internal components publish events → hub broadcasts to subscribed clients.

Clients can subscribe to specific event types:
```json
{"action": "subscribe", "events": ["lease.*", "discovery.conflict"]}
```

### M9: MCP Server (internal/api/mcp)

JSON-RPC 2.0 over SSE transport. Tool definitions expose IPAM operations with rich descriptions for AI consumption. Each tool maps to an IPAM engine method.

### M10: Web Dashboard (web/)

**SPA Architecture**: Single `index.html` loads `app.js` which initializes router. Hash-based routing (`#/subnets`, `#/leases`, etc.). Each page module exports `render()` and `destroy()`.

**IP Grid (ipgrid.js)**: Canvas-based or SVG grid rendering. 16×16 grid for /24 (256 cells). Color-coded by IP state. Hover tooltip with IP details. Click opens detail panel.

**Subnet Tree (tree.js)**: Expandable tree view using nested `<ul>/<li>`. Indent by hierarchy level. Inline utilization bar per subnet. Lazy-load children on expand.

**Real-time updates**: WebSocket connection from `ws.js`. Received events update in-memory state and re-render affected components. Toast notifications for alerts.

### M11: Authentication (internal/auth)

**local.go**: bcrypt password hashing. User store in embedded storage. `--init` writes config only; first admin is created via bootstrap or pre-seeded bcrypt hash. API tokens are random and stored hashed.

**session.go**: Random session ID. Server-side session store persisted in storage for same-node restart continuity. Secure cookie: `HttpOnly`, `SameSite=Strict`, optional `Secure` flag.

**rbac.go**: Three roles: admin, operator, viewer. Permission matrix checked per endpoint. API tokens scoped by role + optional subnet filter.

### M12: HA/Failover (internal/ha)

**heartbeat.go**: TCP connection between primary and secondary. Periodic heartbeat (default 1s). If no heartbeat for `failover_timeout` (default 10s), secondary promotes to primary.

**sync.go**: Binary protocol for lease replication:
1. Initial sync: Full snapshot transfer
2. Incremental sync: WAL entries streamed in real-time
3. Sequence numbers for consistency tracking

**election.go**: Simple leader election: lowest configured priority wins. Fencing via shared witness (optional) to prevent split-brain.

### M13: Audit (internal/audit)

**logger.go**: Append-only log using dedicated storage B+Tree. Key: timestamp (nanosecond precision). Value: serialized `AuditEntry`. Automatic compaction of entries older than configured retention period.

### M14: Migration (internal/migrate)

**isc_dhcp.go**: Parse ISC DHCP config grammar (recursive descent parser). Extract subnet declarations, pool ranges, fixed-address (reservations), option statements. Parse `dhcpd.leases` file for active leases.

**csv.go**: Generic CSV importer with column mapping. Auto-detect headers. Validate CIDR/IP/MAC formats. Report errors per row.

---

## Build & Release

### Makefile Targets

```makefile
VERSION := $(shell git describe --tags --always)
LDFLAGS := -s -w -X main.version=$(VERSION)

build:         ## Build for current platform
    go build -ldflags "$(LDFLAGS)" -o bin/monsoon ./cmd/monsoon

release:       ## Cross-compile all platforms
    GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/monsoon-linux-amd64 ./cmd/monsoon
    GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/monsoon-linux-arm64 ./cmd/monsoon
    GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/monsoon-darwin-amd64 ./cmd/monsoon
    GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/monsoon-darwin-arm64 ./cmd/monsoon
    GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/monsoon-windows-amd64.exe ./cmd/monsoon
    GOOS=freebsd GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/monsoon-freebsd-amd64 ./cmd/monsoon

test:          ## Run tests
    go test -race -cover ./...

lint:          ## Run linter
    go vet ./...

clean:         ## Clean build artifacts
    rm -rf bin/

embed:         ## Embed web dashboard assets
    # Assets in web/ are embedded via go:embed directives in internal/dashboard/embed.go
```

### Binary Size Target

- Target: < 20MB (single binary with embedded dashboard)
- Use `-ldflags "-s -w"` for stripped binary
- Compress embedded assets with gzip
- No CGO required (`CGO_ENABLED=0`)

---

## Testing Strategy

### Unit Tests

- **Packet encode/decode**: Round-trip DHCPv4/v6 packets
- **CIDR calculator**: All operations with edge cases
- **Lease state machine**: All valid and invalid transitions
- **Option engine**: Standard option types + custom options
- **B+Tree**: Insert, delete, range scan, concurrent access
- **WAL**: Write, crash recovery, compaction

### Integration Tests

- **DHCP flow**: Full DISCOVER→OFFER→REQUEST→ACK with in-memory server
- **Lease lifecycle**: Create→renew→expire→reuse
- **Discovery**: ARP/ping scan with mock network
- **API**: All REST endpoints with httptest
- **HA**: Failover simulation with two instances

### Benchmark Tests

- DHCP packet processing throughput (target: >10,000 leases/sec)
- B+Tree operations under load
- API request latency (target: <5ms p99)
- WebSocket broadcast fanout
