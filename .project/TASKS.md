# Monsoon — Task Breakdown

## Implementation Phases

| Phase | Name | Duration | Tasks |
|-------|------|----------|-------|
| 1 | Foundation | 2 weeks | #1–#15 |
| 2 | DHCPv4 Core | 2 weeks | #16–#30 |
| 3 | IPAM Engine | 2 weeks | #31–#44 |
| 4 | REST API | 1.5 weeks | #45–#57 |
| 5 | Web Dashboard | 2 weeks | #58–#72 |
| 6 | Discovery & DDNS | 1.5 weeks | #73–#83 |
| 7 | Auth & Security | 1 week | #84–#91 |
| 8 | DHCPv6 | 1.5 weeks | #92–#101 |
| 9 | Advanced Features | 1.5 weeks | #102–#113 |
| 10 | HA & Failover | 1 week | #114–#121 |
| 11 | Migration & Import | 1 week | #122–#129 |
| 12 | Polish & Release | 1 week | #130–#140 |

**Total: ~17 weeks / ~140 tasks**

---

## Phase 1: Foundation (Tasks #1–#15)

### Project Setup
- [ ] #1 — Initialize Go module (`go mod init github.com/monsoondhcp/monsoon`), create directory structure per IMPLEMENTATION.md, add Makefile with build/test/lint/release targets
- [ ] #2 — Implement `internal/config/config.go`: Define complete `Config` struct matching monsoon.yaml schema, YAML unmarshaling with `gopkg.in/yaml.v3`, default values
- [ ] #3 — Implement `internal/config/validate.go`: Validate all config fields — CIDR validity, no subnet overlaps, pool ranges within subnet, gateway within subnet, positive lease times, valid port ranges
- [ ] #4 — Implement `internal/config/env.go`: Environment variable override system — reflect over Config struct fields, generate `MONSOON_` prefixed names, apply overrides after YAML parse
- [ ] #5 — Implement `internal/config/reload.go`: SIGHUP handler for hot-reload — re-read config file, validate, atomic swap via `sync.RWMutex`

### Storage Engine
- [ ] #6 — Implement `internal/storage/wal.go`: Write-ahead log — append entries `[length:4][crc32:4][type:1][key_len:2][key][value_len:4][value]`, fsync per transaction, segment rotation at 64MB, crash recovery replay
- [ ] #7 — Implement `internal/storage/page.go`: 4KB page manager — page allocation, free-list, page read/write, buffer pool with LRU eviction
- [ ] #8 — Implement `internal/storage/btree.go`: B+Tree core — order-256, insert, delete, point lookup, leaf page chain for range scan, page splits/merges, concurrent reads via `sync.RWMutex`
- [ ] #9 — Implement `internal/storage/iterator.go`: Range scan iterator — forward/reverse iteration, prefix scan, seek to key, cursor-based pagination
- [ ] #10 — Implement `internal/storage/index.go`: Secondary index manager — create/drop indexes, composite key construction, index-based lookup routing, automatic index maintenance on insert/update/delete
- [ ] #11 — Implement `internal/storage/codec.go`: Binary serialization — encode/decode Go structs to `[]byte` for storage, use `encoding/binary` + varint for compact representation
- [ ] #12 — Implement `internal/storage/snapshot.go`: Consistent snapshot creation — serialize entire B+Tree state to file with header + sorted key-value pairs, restore from snapshot file
- [ ] #13 — Implement `internal/storage/engine.go`: Storage engine facade — manage multiple B+Tree instances (leases, subnets, addresses, reservations, vlans, audit), transaction support across trees, startup/shutdown lifecycle

### Core Infrastructure
- [ ] #14 — Implement `cmd/monsoon/main.go`: Entry point — CLI flag parsing (config path, data-dir, version, init, check-config, debug), signal handling (SIGTERM graceful shutdown, SIGHUP reload), component lifecycle orchestration
- [ ] #15 — Implement `internal/metrics/prometheus.go`: Prometheus metrics registry — define all metric types (counters, gauges, histograms) from SPECIFICATION §11.1, expose `/metrics` endpoint in text exposition format

---

## Phase 2: DHCPv4 Core (Tasks #16–#30)

### Packet Processing
- [ ] #16 — Implement `internal/dhcpv4/packet.go`: DHCPv4 packet struct — 236-byte fixed header (op, htype, hlen, hops, xid, secs, flags, ciaddr, yiaddr, siaddr, giaddr, chaddr, sname, file), magic cookie `0x63825363`, encode/decode methods
- [ ] #17 — Implement `internal/dhcpv4/options.go`: DHCP option TLV encode/decode — parse option 53 (message type), option 50 (requested IP), option 54 (server identifier), option 51 (lease time), option 55 (parameter request list), option 61 (client identifier)
- [ ] #18 — Implement `internal/dhcpv4/option_types.go`: Full option type definitions — all standard options 1–255 with type info (IP, IP list, uint8, uint16, uint32, string, binary), encode/decode per type, human-readable names
- [ ] #19 — Implement `internal/dhcpv4/broadcast.go`: Raw UDP socket handling — open socket on port 67 with `SO_BINDTODEVICE`, `SO_BROADCAST`, `SO_REUSEADDR`, read/write DHCP packets, broadcast vs unicast reply logic based on flags field and giaddr

### Lease Management
- [ ] #20 — Implement `internal/lease/types.go`: Lease data types — `Lease` struct per SPECIFICATION §3.1.4, `LeaseState` enum (Free, Offered, Bound, Renewing, Released, Declined, Quarantined, Expired)
- [ ] #21 — Implement `internal/lease/state.go`: Lease state machine — valid transitions (Free→Offered→Bound→Renewing→Bound, Bound→Released→Free, etc.), transition validation, event emission on state change
- [ ] #22 — Implement `internal/lease/store.go`: Lease store — CRUD operations using storage engine, indexes by IP (primary), MAC (secondary), expiry time (sorted), subnet ID (range)
- [ ] #23 — Implement `internal/lease/expiry.go`: Lease expiry sweeper — background goroutine with timer heap, process expired leases (Bound→Expired→Quarantined→Free), configurable quarantine period, emit expiry events

### DHCP Server Logic
- [ ] #24 — Implement `internal/dhcpv4/pool.go`: Address pool allocator — bitmap-based free IP tracking per subnet, allocation strategy: (1) existing lease by MAC/client-id, (2) reservation check, (3) requested IP if available, (4) next free from bitmap. O(1) allocation.
- [ ] #25 — Implement `internal/dhcpv4/handler.go` DISCOVER handler: Receive DISCOVER → select subnet (by interface or giaddr) → allocate IP from pool → create Offered lease → construct OFFER packet with options → send response
- [ ] #26 — Implement `internal/dhcpv4/handler.go` REQUEST handler: Receive REQUEST → validate (selecting, renewing, rebinding, init-reboot per RFC 2131 §4.3.2) → create/update Bound lease → construct ACK → send. Send NAK on invalid request.
- [ ] #27 — Implement `internal/dhcpv4/handler.go` RELEASE/DECLINE/INFORM handlers: RELEASE → transition lease to Released → notify IPAM. DECLINE → mark IP as Conflict → quarantine. INFORM → send options without lease.
- [ ] #28 — Implement `internal/dhcpv4/classification.go`: Client classification engine — match rules (vendor class, MAC prefix, option values, relay circuit-id) → select pool + option template per matched class
- [ ] #29 — Implement `internal/dhcpv4/relay.go`: Relay agent support — detect GIADDR ≠ 0, select subnet by GIADDR match, parse Option 82 (Circuit ID, Remote ID), preserve relay info in lease record, unicast reply to relay address
- [ ] #30 — Implement `internal/dhcpv4/server.go`: DHCPv4 server orchestrator — start UDP listener, packet read loop, dispatch to handler by message type, graceful shutdown, error handling, metrics collection

---

## Phase 3: IPAM Engine (Tasks #31–#44)

### Core IPAM
- [ ] #31 — Implement `internal/ipam/types.go`: IPAM data types — `Subnet`, `IPAddress`, `VLAN`, `DHCPReservation` structs per SPECIFICATION §4.1.2, `IPState` and `IPType` enums
- [ ] #32 — Implement `internal/ipam/cidr.go`: CIDR calculator — `Contains`, `Split`, `Merge`, `NextAvailable`, `Overlaps`, `AddressCount`, `NthAddress`, `Supernet`, `BroadcastAddr`, `NetworkAddr` functions with full IPv4/IPv6 support
- [ ] #33 — Implement `internal/ipam/subnet.go` CRUD: Subnet create/read/update/delete — validate CIDR, check no overlap with siblings, auto-detect parent in hierarchy, store in B+Tree with prefix key, maintain parent-child relationships
- [ ] #34 — Implement `internal/ipam/subnet.go` tree operations: Hierarchical subnet tree — build tree from flat storage, get children, get ancestors, get siblings, tree-walk for utilization rollup
- [ ] #35 — Implement `internal/ipam/subnet.go` split/merge: Split subnet into two halves — create child subnets, redistribute IP records. Merge adjacent same-size subnets — validate adjacency, combine IP records, remove children.
- [ ] #36 — Implement `internal/ipam/address.go` CRUD: IP address create/read/update/delete — validate IP within subnet, enforce state transitions, update subnet utilization counters on change
- [ ] #37 — Implement `internal/ipam/address.go` state tracking: IP state lifecycle — Available↔Reserved, Available→DHCP (on lease), DHCP→Quarantined→Available (on expiry), detect Conflict, mark Abandoned
- [ ] #38 — Implement `internal/ipam/address.go` auto-sync with DHCP: Event listener for DHCP lease events — on lease create: create/update IPAddress as DHCP state. On lease expire: transition to Quarantined. On release: transition to Available after quarantine.
- [ ] #39 — Implement `internal/ipam/reservation.go`: DHCP reservation CRUD — MAC→IP fixed mapping, validate IP within subnet and not in DHCP pool range (or in pool but marked reserved), per-host option overrides
- [ ] #40 — Implement `internal/ipam/vlan.go`: VLAN management — CRUD for VLAN records (ID 1-4094), associate subnets with VLANs, validate VLAN ID uniqueness
- [ ] #41 — Implement `internal/ipam/capacity.go`: Utilization calculation — count IPs by state per subnet, calculate percentages, roll up to parent subnets, exhaustion prediction (linear regression on usage history)
- [ ] #42 — Implement `internal/ipam/engine.go`: IPAM engine facade — initialize all stores, wire DHCP event listener, provide unified interface for API layer, start background tasks (utilization updates, threshold alerts)

### Audit
- [ ] #43 — Implement `internal/audit/types.go` + `internal/audit/logger.go`: Audit entry struct, append-only logger — capture actor, action, object, old/new values as JSON, source (web-ui/api/dhcp/discovery), write to dedicated B+Tree with timestamp key
- [ ] #44 — Implement `internal/audit/query.go`: Audit log queries — filter by time range, actor, action, object type/ID, pagination, export to JSON/CSV format

---

## Phase 4: REST API (Tasks #45–#57)

### API Infrastructure
- [ ] #45 — Implement `internal/api/rest/server.go`: HTTP server setup — `http.NewServeMux` (Go 1.22+), TLS configuration, graceful shutdown with context, static file serving for dashboard
- [ ] #46 — Implement `internal/api/rest/router.go`: Route registration — register all endpoint handlers per SPECIFICATION §6.1, method+path patterns
- [ ] #47 — Implement `internal/api/rest/middleware.go`: Middleware chain — structured request logging, panic recovery, CORS headers, rate limiting (token bucket per IP), request ID generation
- [ ] #48 — Implement `internal/api/rest/response.go`: JSON response helpers — standard envelope `{data, meta, error}`, pagination meta, error formatting with codes, content negotiation

### API Endpoints
- [ ] #49 — Implement `internal/api/rest/subnet_handler.go`: Subnet endpoints — List (tree/flat), Create, Get, Update, Delete, List addresses, Available IPs, Next available, Utilization, Split, Trigger scan
- [ ] #50 — Implement `internal/api/rest/address_handler.go`: Address endpoints — Search/filter (by state, subnet, MAC, hostname), Get by IP, Create/reserve, Update, Release, History
- [ ] #51 — Implement `internal/api/rest/lease_handler.go`: Lease endpoints — List active (filterable), Get by IP, Force-release, Expiring soon, Statistics
- [ ] #52 — Implement `internal/api/rest/reservation_handler.go`: Reservation endpoints — List, Create, Get by MAC, Update, Delete
- [ ] #53 — Implement `internal/api/rest/vlan_handler.go`: VLAN endpoints — CRUD operations
- [ ] #54 — Implement `internal/api/rest/discovery_handler.go`: Discovery endpoints — Trigger scan, Results, Conflicts, Rogue DHCP list
- [ ] #55 — Implement `internal/api/rest/audit_handler.go`: Audit endpoints — Query with filters, Export CSV/JSON
- [ ] #56 — Implement `internal/api/rest/system_handler.go`: System endpoints — Health check, Config view/update, Backup/restore, Prometheus metrics, Version/uptime info
- [ ] #57 — Implement `internal/api/rest/auth_handler.go`: Auth endpoints — Login (POST), Logout, Current user, API token CRUD, Password change

---

## Phase 5: Web Dashboard (Tasks #58–#72)

### Dashboard Infrastructure
- [ ] #58 — Create `web/index.html`: SPA shell — minimal HTML with `<div id="app">`, CSS imports, JS module imports, dark/light theme toggle, responsive viewport meta
- [ ] #59 — Create `web/css/variables.css` + `web/css/base.css`: CSS custom properties (colors, spacing, typography per SPECIFICATION §7.3), reset styles, base typography, scrollbar styling
- [ ] #60 — Create `web/css/layout.css` + `web/css/components.css`: Sidebar + header + content grid layout, responsive breakpoints. Component styles: buttons, inputs, selects, tables, cards, badges, progress bars, modals, toasts, tabs
- [ ] #61 — Implement `web/js/router.js`: Hash-based SPA router — route definitions, page load/unload lifecycle, active link highlighting, 404 fallback, browser back/forward support
- [ ] #62 — Implement `web/js/api.js`: REST API client — fetch wrapper with auth header injection, error handling, pagination helpers, base URL configuration
- [ ] #63 — Implement `web/js/ws.js`: WebSocket client — auto-reconnect with exponential backoff, event subscription system, message parsing, connection state indicator
- [ ] #64 — Implement `web/js/auth.js`: Auth state management — login form, session storage, auth header injection, logout, redirect to login on 401

### Dashboard Pages
- [ ] #65 — Implement `web/js/pages/dashboard.js`: Overview page — stat cards (total subnets, active leases, available IPs, conflicts), top subnets by utilization bar chart, live lease activity feed (WebSocket), alert list
- [ ] #66 — Implement `web/js/components/tree.js` + `web/js/pages/subnets.js`: Subnet tree view — expandable/collapsible tree, inline utilization bars, click to navigate to subnet detail, context menu (split/delete)
- [ ] #67 — Implement `web/js/components/ipgrid.js`: IP grid visualization component — 16×16 grid for /24 subnets (dynamic sizing for other prefix lengths), color-coded cells by IP state, hover tooltip (IP, MAC, hostname, state), click to select
- [ ] #68 — Implement `web/js/pages/subnet-detail.js`: Single subnet view — subnet info header, IP grid visualization, address table below grid, create reservation button, DHCP pool indicator overlay on grid
- [ ] #69 — Implement `web/js/pages/leases.js`: Lease browser — sortable/filterable table, real-time countdown timers for expiry, bulk actions (release, convert to reservation), search by IP/MAC/hostname, export button
- [ ] #70 — Implement `web/js/pages/reservations.js`: Reservation manager — table with CRUD modals, import from CSV button, bulk create from discovery
- [ ] #71 — Implement `web/js/pages/discovery.js`: Discovery page — scan trigger button with progress bar, results table (new/known/changed/missing), conflict list with resolution actions, rogue DHCP alerts
- [ ] #72 — Implement `web/js/pages/audit.js` + `web/js/pages/settings.js`: Audit log viewer (time-range filter, JSON diff viewer). Settings page (config editor, user management, webhook config, backup/restore, system info)

---

## Phase 6: Discovery & DDNS (Tasks #73–#83)

### Network Discovery
- [ ] #73 — Implement `internal/discovery/arp.go`: ARP scanner — construct ARP request packets (Ethernet frame + ARP payload), send via raw socket (`AF_PACKET`), listen for replies, timeout handling, collect IP→MAC results
- [ ] #74 — Implement `internal/discovery/ping.go`: ICMP ping sweep — raw ICMP socket, concurrent ping with goroutine pool (default 64), configurable timeout per host, ICMP sequence tracking, collect responding IPs
- [ ] #75 — Implement `internal/discovery/tcp.go`: TCP connect probe — non-blocking connect with timeout on configurable ports, optional banner grab (first 256 bytes), service identification (SSH, HTTP, HTTPS, RDP)
- [ ] #76 — Implement `internal/discovery/dns.go`: Reverse DNS lookup — PTR queries for discovered IPs via stdlib `net.LookupAddr`, batch queries with concurrency limit
- [ ] #77 — Implement `internal/discovery/passive.go`: Passive DHCP listener — sniff DHCP traffic on network, learn client MACs and hostnames from DISCOVER/REQUEST packets without active probing
- [ ] #78 — Implement `internal/discovery/oui.go`: MAC vendor OUI lookup — embedded OUI database (IEEE MA-L), parse 3-byte OUI prefix from MAC, return vendor name (e.g., "Apple, Inc.", "Dell Inc.")
- [ ] #79 — Implement `internal/discovery/conflict.go`: Conflict detection logic — compare discovery results with IPAM state, detect: duplicate IPs (multiple MACs), unknown hosts, orphaned leases, static/DHCP mismatches
- [ ] #80 — Implement `internal/discovery/rogue.go`: Rogue DHCP detection — listen for DHCPOFFER packets from non-configured servers, alert on detection with source MAC/IP
- [ ] #81 — Implement `internal/discovery/scheduler.go`: Scan scheduler — parse schedule config (interval-based), manage scan lifecycle, prevent concurrent scans on same subnet, emit results to IPAM engine
- [ ] #82 — Implement `internal/discovery/engine.go`: Discovery engine orchestrator — coordinate all discovery methods, merge results, update IPAM, emit events (WebSocket + webhooks)

### DDNS
- [ ] #83 — Implement `internal/ddns/client.go` + `internal/ddns/tsig.go`: DNS UPDATE client (RFC 2136) — construct UPDATE messages for A/AAAA record add/delete on lease create/expire, PTR record management, TSIG authentication (HMAC-SHA256)

---

## Phase 7: Auth & Security (Tasks #84–#91)

- [ ] #84 — Implement `internal/auth/local.go`: Local user store — user CRUD in storage engine, bcrypt password hashing, admin user creation on `--init`
- [ ] #85 — Implement `internal/auth/token.go`: API token management — generate random 32-byte hex tokens, store hashed (SHA-256), CRUD operations, token scoping (role + subnet filter)
- [ ] #86 — Implement `internal/auth/session.go`: Session management — random 32-byte session ID, in-memory store with TTL, secure cookie (`HttpOnly`, `SameSite=Strict`), periodic persistence to storage
- [ ] #87 — Implement `internal/auth/rbac.go`: RBAC — role definitions (admin, operator, viewer), permission matrix per endpoint, middleware that checks role before handler execution
- [ ] #88 — Implement `internal/auth/ldap.go`: LDAP authentication — connect to LDAP server, bind + search for user, group membership check for role mapping, TLS/StartTLS support
- [ ] #89 — Implement `internal/api/rest/middleware.go` auth middleware: Extract session cookie or Bearer token from request, validate, inject user context, return 401/403 on failure
- [ ] #90 — Implement TLS configuration: Auto-generate self-signed cert on first run, custom cert/key path config, optional ACME (Let's Encrypt) with embedded ACME client
- [ ] #91 — Implement DHCP security features: Per-MAC rate limiting (configurable requests/second), MAC allow/deny lists per subnet, maximum leases per MAC, DHCP starvation attack mitigation

---

## Phase 8: DHCPv6 (Tasks #92–#101)

- [ ] #92 — Implement `internal/dhcpv6/packet.go`: DHCPv6 packet encode/decode — message type (1 byte), transaction ID (3 bytes), options (type:2 + length:2 + data format per RFC 8415)
- [ ] #93 — Implement `internal/dhcpv6/options.go`: DHCPv6 option encode/decode — Client Identifier (1), Server Identifier (2), IA_NA (3), IA_TA (4), IA_PD (25), Status Code (13), Rapid Commit (14), DNS Servers (23), Domain List (24)
- [ ] #94 — Implement `internal/dhcpv6/duid.go`: DUID generation and parsing — DUID-LLT (link-layer + time), DUID-EN (enterprise number), DUID-LL (link-layer), DUID-UUID
- [ ] #95 — Implement `internal/dhcpv6/pool.go`: IPv6 address pool — allocate from configured range, bitmap tracking for /64 pools, prefix delegation pool for IA_PD
- [ ] #96 — Implement `internal/dhcpv6/handler.go` Solicit/Advertise: Receive Solicit on multicast ff02::1:2 port 547 → allocate address → send Advertise with IA_NA/IA_PD options
- [ ] #97 — Implement `internal/dhcpv6/handler.go` Request/Reply: Validate Request matches Advertise → create binding → send Reply with confirmed addresses/prefixes
- [ ] #98 — Implement `internal/dhcpv6/handler.go` Renew/Rebind/Release/Decline: Renew → extend lifetime. Rebind → re-verify. Release → free address. Decline → mark conflict.
- [ ] #99 — Implement `internal/dhcpv6/handler.go` Information-Request: Stateless mode — return configuration options (DNS servers, domain) without address assignment
- [ ] #100 — Implement `internal/dhcpv6/pd.go`: Prefix delegation — allocate /48, /56, or /64 prefixes from configured pool, track delegated prefixes, support sub-delegation
- [ ] #101 — Implement `internal/dhcpv6/relay.go` + `internal/dhcpv6/server.go`: Relay-Forward/Relay-Reply message handling, nested relay support. Server orchestrator: UDP listener on port 547, dispatch loop, graceful shutdown.

---

## Phase 9: Advanced Features (Tasks #102–#113)

### WebSocket & Events
- [x] #102 — Implement `internal/api/websocket/hub.go`: WebSocket hub — connection registry, topic-based subscription, broadcast to subscribers, connection cleanup on disconnect
- [x] #103 — Implement `internal/api/websocket/client.go`: WebSocket client handler — upgrade HTTP connection, read subscription messages, write events, ping/pong keepalive
- [x] #104 — Implement `internal/api/websocket/events.go`: Event types — all events from SPECIFICATION §6.3 (lease.*, discovery.*, address.*, subnet.*), JSON serialization

### gRPC
- [ ] #105 — Implement `internal/api/grpc/server.go`: gRPC server over HTTP/2 — custom binary protobuf encoding (no protoc), service registration, unary + server-streaming RPCs, mTLS support
- [ ] #106 — Implement gRPC service handlers: SubnetService, LeaseService, AddressService, DiscoveryService — map to IPAM engine methods, streaming for WatchLeases/WatchDiscovery

### MCP Server
- [x] #107 — Implement `internal/api/mcp/server.go`: MCP server — JSON-RPC 2.0 over SSE transport, tool listing, tool invocation dispatch
- [x] #108 — Implement `internal/api/mcp/tools.go` + `internal/api/mcp/handlers.go`: All 15 MCP tools per SPECIFICATION §6.4 — rich descriptions for AI consumption, input validation, response formatting

### Webhooks
- [ ] #109 — Implement `internal/webhook/dispatcher.go`: Event dispatcher — subscribe to internal events, filter by configured event types per webhook, queue for delivery
- [ ] #110 — Implement `internal/webhook/delivery.go`: HTTP delivery — POST with JSON/Slack payload format, exponential backoff retry (max 3 attempts), timeout handling, delivery status tracking

### Dashboard Embed
- [ ] #111 — Implement `internal/dashboard/embed.go`: `go:embed web/*` directive — embed all dashboard assets into binary, serve via `http.FileServer` with proper MIME types, gzip compression for JS/CSS
- [ ] #112 — Create `web/assets/logo.svg` + `web/assets/favicon.ico`: Monsoon logo SVG, favicon

### Rapid Commit
- [ ] #113 — Implement `internal/dhcpv4/rapid_commit.go`: RFC 4039 — two-message exchange (DISCOVER with Rapid Commit option → immediate ACK, skip OFFER/REQUEST) for fast lease acquisition

---

## Phase 10: HA & Failover (Tasks #114–#121)

- [ ] #114 — Implement `internal/ha/heartbeat.go`: Peer heartbeat — TCP connection, periodic ping/pong (default 1s), loss detection with configurable timeout (default 10s)
- [ ] #115 — Implement `internal/ha/sync.go` initial sync: Full snapshot transfer — primary sends complete lease database snapshot to secondary on first connect
- [ ] #116 — Implement `internal/ha/sync.go` incremental sync: WAL streaming — primary streams WAL entries to secondary in real-time, sequence number tracking for consistency
- [ ] #117 — Implement `internal/ha/election.go`: Leader election — priority-based (lower wins), handle network partition, fencing mechanism to prevent split-brain
- [ ] #118 — Implement `internal/ha/failover.go` active-passive: Failover orchestrator — detect primary failure, promote secondary, take over VIP (gratuitous ARP), resume DHCP serving
- [ ] #119 — Implement `internal/ha/failover.go` load-sharing: Split-scope mode — configure non-overlapping pool ranges per peer, bidirectional lease sync, graceful takeover of partner's range on failure
- [ ] #120 — Add HA status to dashboard: Peer status indicator, sync lag display, manual failover trigger button, HA configuration page
- [ ] #121 — Add HA metrics: `monsoon_ha_heartbeat_latency_seconds`, `monsoon_ha_lease_sync_lag_seconds`, `monsoon_ha_failover_total`, `monsoon_ha_peer_state`

---

## Phase 11: Migration & Import (Tasks #122–#129)

- [ ] #122 — Implement `internal/migrate/isc_dhcp.go` config parser: Recursive descent parser for ISC DHCP `dhcpd.conf` — extract subnet declarations, pool ranges, fixed-address (reservations), option statements, group/class definitions
- [ ] #123 — Implement `internal/migrate/isc_dhcp.go` lease parser: Parse `dhcpd.leases` file — extract active leases with IP, MAC, start/end times, hostname, binding state
- [ ] #124 — Implement `internal/migrate/kea.go`: Parse Kea JSON config (`kea-dhcp4.conf`) — extract subnets, pools, reservations, options. Import leases from Kea lease database (CSV or memfile format).
- [ ] #125 — Implement `internal/migrate/phpipam.go`: phpIPAM REST API importer — authenticate, fetch sections → subnets → addresses → VLANs, map to Monsoon data model
- [ ] #126 — Implement `internal/migrate/netbox.go`: NetBox REST API importer — fetch prefixes → IP addresses → VLANs → tenants, map to Monsoon data model
- [ ] #127 — Implement `internal/migrate/csv.go`: Generic CSV importer — column mapping configuration, auto-detect headers, validate CIDR/IP/MAC formats, per-row error reporting, support for subnets/addresses/reservations CSV types
- [ ] #128 — Implement `internal/migrate/migrate.go`: Migration orchestrator — CLI subcommand routing, dry-run mode (validate without importing), progress reporting, conflict resolution (skip/overwrite)
- [ ] #129 — Implement migration CLI commands: `monsoon migrate --from {isc-dhcp|kea|phpipam|netbox|csv}` with source-specific flags

---

## Phase 12: Polish & Release (Tasks #130–#140)

### Testing
- [ ] #130 — Write unit tests for DHCPv4 packet encode/decode — round-trip tests, malformed packet handling, all option types, edge cases (max options, overloading)
- [ ] #131 — Write unit tests for storage engine — B+Tree operations under concurrent load, WAL crash recovery simulation, snapshot consistency
- [ ] #132 — Write unit tests for IPAM — CIDR calculator edge cases (IPv4/IPv6), subnet tree operations, IP state transitions, capacity calculations
- [ ] #133 — Write integration tests — full DHCP DORA flow with in-memory server, lease lifecycle, discovery with mock network, REST API with httptest
- [ ] #134 — Write benchmark tests — DHCP packet processing throughput, B+Tree operation latency, API request latency

### Documentation & Release
- [ ] #135 — Write `README.md`: Project overview, quick start guide, feature highlights, configuration example, API overview, migration guide, comparison table
- [ ] #136 — Create `scripts/install.sh`: Installer script — detect OS/arch, download correct binary from GitHub releases, create directories, generate default config, create systemd service
- [ ] #137 — Create `Dockerfile`: Multi-stage build (Go builder → scratch), expose ports, volume for data, document `--net=host` requirement
- [ ] #138 — Cross-compile release binaries: Linux (amd64/arm64), macOS (amd64/arm64), Windows (amd64), FreeBSD (amd64) — automated via Makefile `release` target
- [ ] #139 — Create GitHub Actions CI/CD: Build + test on push, release binaries on tag, Docker image build + push
- [ ] #140 — Final review: Verify all features per SPECIFICATION, performance benchmarks, security audit, documentation completeness
