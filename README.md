# Monsoon — DHCP + IPAM

> **Every Address. Accounted For.**

Monsoon is an open-source network platform that unifies a **DHCP server** and **IP Address Management (IPAM)** in a single Go binary. It aims to replace the ISC DHCP + Kea + phpIPAM + NetBox IPAM stack with one process and zero external dependencies.

| | |
|---|---|
| **Language** | Pure Go (`go1.26.2`, `#NOFORKANYMORE`) |
| **Binary** | `monsoon` — single binary, cross-platform |
| **License** | Apache 2.0 |
| **Repository** | github.com/monsoondhcp/monsoon |
| **Domain** | monsoondhcp.com |

---

## Why Monsoon?

Network IP management today is fragmented across multiple tools:

- **ISC DHCP** — Legacy C daemon, no API, no UI, EOL announced.
- **Kea** — Modern replacement but requires PostgreSQL/MySQL, Stork UI is a separate deployment, complex multi-process architecture.
- **phpIPAM / NetBox** — IPAM-only, no DHCP, requires PHP/Python + database stack.
- **Infoblox / BlueCat** — Enterprise, expensive licensing, vendor lock-in.

**The gap:** no single open-source tool provides unified DHCP serving + IP address management with a modern web UI, REST/gRPC APIs, and zero infrastructure dependencies.

**Monsoon fills this gap:** one binary with embedded storage, embedded web dashboard, real-time events, and AI-ready MCP integration.

## Supported In This Build

- Local authentication only. `auth.type` must be `local` in the current binary.
- Storage-backed browser sessions persist across restart on the same node.
- `PUT /api/v1/system/config` uses merge-on-write semantics and does not reset omitted fields to defaults.
- Snapshot operations are available through CLI backup/restore and REST backup/list/restore endpoints.
- Discovery currently provides scheduled/manual scans, lease/reservation-backed conflict reporting, and limited active probe enrichment.
- True passive rogue-DHCP sensing is not implemented yet, so the rogue feed may legitimately be empty.
- HA exists as a basic heartbeat/lease-sync feature and should still be treated as limited-scope operational support rather than hardened clustering.

---

## Key Features

### DHCP Engine
- **DHCPv4** — Full RFC 2131/2132 support, Option 82 relay info (RFC 3046), Rapid Commit (RFC 4039), Classless Static Route (RFC 3442)
- **DHCPv6** — RFC 8415, Prefix Delegation (RFC 3633), stateless mode (RFC 3736), failover (RFC 8156)
- **Lease State Machine** — FREE → OFFERED → BOUND → RENEWING → RELEASED, with dedicated `QUARANTINE` and `CONFLICT` states
- **Client Classification** — Pool/option selection via vendor class, MAC prefix, Option 93 matching
- **DDNS** — RFC 2136 forward/reverse updates, TSIG-signed mutations
- **Relay Agent** — GIADDR-based subnet selection, per-port matching via Circuit/Remote ID

### IPAM Engine
- **Hierarchical Subnet Tree** — `/8 → /16 → /24` parent/child navigation across RFC 1918 space
- **CIDR Tools** — Split, merge, supernet, next-available, overlap detection
- **Capacity Planning** — Subnet summaries and DHCP-pool utilization based on configured pool sizes
- **Network Discovery** — Scheduled/manual scans with ARP, ping, TCP, DNS, and Monsoon-owned lease/reservation state
- **Conflict Detection** — Duplicate IP/MAC inconsistencies derived from lease and reservation state
- **Audit Trail** — Full record of every change with actor, action, old/new values

### Embedded Storage
- **Custom KV Store** — B+Tree index + Write-Ahead Log + periodic snapshots
- **Critical Indexes** — Lease by IP/MAC/Expiry, IP by Subnet/State/MAC, CIDR longest-prefix
- **Crash Recovery** — WAL replay from last checkpoint, fsync-on-commit

### API Layer
- **REST API** — Subnet, address, lease, reservation, VLAN, discovery, audit, system endpoints
- **gRPC** — SubnetService, LeaseService, AddressService, DiscoveryService with streaming watch
- **WebSocket** — Live lease, discovery, IPAM, and subnet events
- **MCP Server** — AI-assistant tools (subnet planning, IP search, conflict querying)
- **Webhooks** — Slack and generic JSON formats, retry with exponential backoff

### Web Dashboard
- React 19 + Tailwind CSS 4.1 + shadcn/ui + lucide-react
- Dark/light theme (`next-themes`)
- Dashboard, subnet tree, visual IP grid, lease browser, reservations, discovery, audit, settings

### Operations
- **Hot Reload** - `SIGHUP` live-applies REST/auth safety controls: CORS allowlist, trusted proxies, general/auth rate limits, auth enforcement, and secure session cookie
- **Restart Signaling** - Reloaded changes to listeners, TLS, DHCP/discovery/HA runtime, auth backend/session lifetime, metrics path, and webhooks are accepted but reported as restart-required
- **HA / Failover** - Basic active-passive heartbeat and lease synchronization for controlled environments
- **Backup & Restore** - CLI snapshot restore plus REST backup/list/restore operations
- **Prometheus Metrics** - DHCP, IPAM, storage, API, WebSocket, HA metrics
- **Structured Logging** - Component-level JSON logs
- **Migration Tools** - ISC DHCP, Kea, phpIPAM, NetBox, generic CSV import

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    MONSOON BINARY                       │
│                                                         │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐               │
│  │  DHCPv4  │  │  DHCPv6  │  │   IPAM   │               │
│  │  Engine  │  │  Engine  │  │  Engine  │               │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘               │
│       │             │             │                     │
│  ┌────▼─────────────▼─────────────▼─────┐               │
│  │       Lease & Address Store          │               │
│  │   (Unified State Machine + WAL)      │               │
│  └────────────────┬─────────────────────┘               │
│                   │                                     │
│  ┌────────────────▼─────────────────────┐               │
│  │        Embedded Storage Engine       │               │
│  │    B+Tree Index + WAL + Snapshots    │               │
│  └──────────────────────────────────────┘               │
│                                                         │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────┐      │
│  │ REST API │ │   gRPC   │ │WebSocket │ │  MCP   │      │
│  └──────────┘ └──────────┘ └──────────┘ └────────┘      │
│                                                         │
│  ┌─────────────────────────────────────────────────┐    │
│  │              React Web Dashboard                │    │
│  └─────────────────────────────────────────────────┘    │
│                                                         │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐               │
│  │   DDNS   │  │Discovery │  │   HA /   │               │
│  │  Client  │  │  Scanner │  │ Failover │               │
│  └──────────┘  └──────────┘  └──────────┘               │
└─────────────────────────────────────────────────────────┘
```

### Design Principles

1. **Single Binary** — DHCP, IPAM, storage, and web UI all embedded. `./monsoon` and done.
2. **Zero External Dependencies** — Only `golang.org/x/crypto`, `golang.org/x/sys`, `gopkg.in/yaml.v3`.
3. **Unified State** — DHCP lease events automatically update IPAM state. One source of truth.
4. **Multi-Platform** — Linux (primary), macOS, Windows, FreeBSD. Cross-compiled via `GOOS/GOARCH`.
5. **API-First** — Every feature accessible via REST. The web UI consumes the same API.
6. **Observable** — Structured logs, Prometheus metrics, health check endpoints.

---

## Quick Start

### 1. Initialize a config file

```bash
go run ./cmd/monsoon --init --config ./configs/monsoon.yaml
```

`--init` only writes a default config file. It does not create an admin user or seed a password.

### 2. Start the backend

```bash
go run ./cmd/monsoon --config ./configs/monsoon.yaml --web-dist ./web/dist
```

Default local endpoints:
- Health: `GET http://localhost:8067/api/v1/system/health`
- Readiness: `GET http://localhost:8067/api/v1/system/ready`
- Metrics: `GET http://localhost:8067/metrics`
- REST API: `http://localhost:8067/api/v1/`
- Dashboard: `http://localhost:8067/`

If `api.rest.tls_cert_file` and `api.rest.tls_key_file` are both set, Monsoon serves the REST API, dashboard, SSE, and WebSocket endpoint over HTTPS instead. `api.grpc.*` and `api.mcp.*` expose separate TLS settings for those listeners.

### 3. Complete first-time admin bootstrap

The current build supports **local auth only**. With local auth enabled and `auth.local.admin_password_hash` left empty, Monsoon starts in first-run mode and waits for exactly one bootstrap request:

```bash
curl -X POST http://localhost:8067/api/v1/auth/bootstrap \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"admin\",\"password\":\"change-me-now\"}"
```

The bootstrap route succeeds only when no local users exist yet. After the first admin is created, the same route returns `409 Conflict`.

If you prefer a pre-seeded admin instead of bootstrap, set `auth.local.admin_password_hash` to a bcrypt hash before startup.

### 4. Frontend development

```bash
cd web
npm install
npm run dev
```

When using the Vite dev server, explicitly allow its origin in `api.rest.cors_origins`, for example:

```yaml
api:
  rest:
    cors_origins:
      - http://localhost:5173
```

An empty `cors_origins` list means browser cross-origin requests are not allowed.

### 5. Frontend build

```bash
cd web
npm run build
```

Run the frontend test suite with:

```bash
cd web
npm test
```

### CLI Flags

```
monsoon [flags]

  -c, --config string    Configuration file path
  -d, --data-dir string  Data directory override
      --web-dist string  Web dashboard dist directory
      --version          Print version and exit
      --init             Initialize configuration file and exit
      --check-config     Validate configuration and exit
      --export-config    Export resolved configuration to stdout
      --backup           Create backup snapshot and exit
      --restore string   Restore snapshot file
      --migrate          Run migrations and exit
      --debug            Enable debug logging
```

---
## Frontend Stack

- **React 19** — Modern function components, concurrent features
- **Tailwind CSS 4.1** — Utility-first styling
- **shadcn/ui** — Accessible, composable component primitives
- **lucide-react** — Consistent icon set
- **next-themes** — Dark/light theme switching

---

## API Overview

Base URL: `http://localhost:8067/api/v1`

### Authentication
| Method | Path | Description |
|--------|------|-------------|
| POST | `/auth/bootstrap` | Create the first local admin |
| POST | `/auth/login` | Username/password login |
| POST | `/auth/logout` | Revoke current browser session |
| GET | `/auth/me` | Current identity |
| POST | `/auth/password` | Change local password |
| GET | `/auth/tokens` | List API tokens |
| POST | `/auth/tokens` | Create API token |
| DELETE | `/auth/tokens/{id}` | Revoke API token |

### Subnets, Addresses, Reservations
| Method | Path | Description |
|--------|------|-------------|
| GET | `/subnets` | List subnet summaries |
| GET | `/subnets/raw` | List raw subnet records |
| POST | `/subnets` | Create/update subnet |
| PUT | `/subnets` | Create/update subnet |
| DELETE | `/subnets?cidr=...` | Delete subnet |
| GET | `/addresses` | Search/filter IPs |
| GET | `/addresses/{ip}` | Address details |
| GET | `/reservations` | List reservations |
| GET | `/reservations/{mac}` | Reservation details |
| POST | `/reservations` | Create/update reservation |
| PUT | `/reservations` | Create/update reservation |
| DELETE | `/reservations?mac=...` | Delete reservation |

### Leases
| Method | Path | Description |
|--------|------|-------------|
| GET | `/leases` | List leases |
| GET | `/leases/{ip}` | Lease details |
| POST | `/leases/{ip}/release` | Mark lease as released |
| POST | `/leases/{ip}/reservation` | Convert lease to reservation |

### Discovery & Audit
| Method | Path | Description |
|--------|------|-------------|
| POST | `/discovery/scan` | Trigger manual scan |
| GET | `/discovery/conflicts` | Detected conflicts |
| GET | `/discovery/status` | Discovery status |
| GET | `/discovery/progress` | Current scan progress |
| GET | `/discovery/results` | List scan results |
| GET | `/discovery/results/{id}` | Read one scan result |
| GET | `/discovery/rogue` | Persisted rogue findings, if any |
| GET | `/audit` | Query audit log |

### System
| Method | Path | Description |
|--------|------|-------------|
| GET | `/system/health` | Health check |
| GET | `/system/ready` | Readiness probe |
| GET | `/system/info` | Runtime info plus `config_reload` restart-pending status |
| GET/PUT | `/system/config` | Configuration snapshot/update; response `meta.reload` shows hot-reload vs restart-required state |
| POST | `/system/backup` | Create snapshot |
| GET | `/system/config/export` | Export current config as YAML or JSON |
| GET | `/system/backups` | List snapshots |
| POST | `/system/restore` | Restore snapshot by name/path |

### gRPC Health

- `monsoon.v1.SystemService/GetHealth`
- `monsoon.v1.SystemService/GetReadiness`

### MCP Health

- `monsoon_get_health` now returns the same core readiness model used by the REST and gRPC surfaces.

### Real-time (WebSocket)

`ws://localhost:8067/ws` — live `lease.*`, `discovery.*`, `address.*`, `subnet.*` events.

```json
{"type": "lease.created", "data": {"ip": "10.0.1.50", "mac": "AA:BB:CC:DD:EE:FF", "hostname": "laptop-01"}}
{"type": "discovery.conflict", "data": {"ip": "10.0.1.50", "macs": ["AA:BB:...", "11:22:..."]}}
{"type": "subnet.exhaustion", "data": {"subnet": "10.0.1.0/24", "utilization": 0.95}}
```

For the current implementation, prefer `.project/ANALYSIS.md` or `internal/api/rest/router.go` over the aspirational specification.

---

## Configuration

Monsoon is configured via YAML, and values can be overridden with `MONSOON_*` environment variables.

Security-sensitive defaults to be aware of:
- `api.rest.cors_origins: []` blocks browser cross-origin access until you allow explicit origins.
- `api.rest.trusted_proxies` must list the reverse proxy IPs/CIDRs before Monsoon will honor `X-Forwarded-For`, `X-Forwarded-Proto`, or `X-Forwarded-Host`.
- `api.rest.tls_cert_file` and `api.rest.tls_key_file` must be set together to enable HTTPS.
- `api.grpc.tls_cert_file` / `api.grpc.tls_key_file` and `api.mcp.tls_cert_file` / `api.mcp.tls_key_file` follow the same all-or-nothing rule.
- `api.rest.auth_rate_limit` applies a stricter per-IP limit to login, bootstrap, logout, password, and token mutation routes on top of the general API limiter.
- `auth.session.secure: true` means browsers will only send the session cookie over HTTPS.
- This build validates `auth.type: local`; LDAP configuration from planning docs is not supported by the current binary.
- `auth.local.max_failed_attempts` and `auth.local.lockout_duration` add a user-level temporary lockout after repeated local password failures.
- Password changes revoke existing browser sessions for that user and rotate the current session cookie automatically.
- Browser sessions are persisted in storage and survive same-node restarts, but they are still not shared across HA peers.
- Leaving `auth.local.admin_password_hash` empty no longer creates a default admin account. Use bootstrap or provide a bcrypt hash.
- `POST /api/v1/system/restore` and CLI `--restore` both perform snapshot restore against the storage engine.
- REST responses include defensive headers by default, including CSP, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, and HSTS when the request is served over HTTPS or arrives through an HTTPS reverse proxy.
- `/metrics` now exports auth outcome counters and auth rate-limit counters in addition to the existing HA metrics.
- `/metrics` also exports low-cardinality `monsoon_security_events_total{event,surface}` counters for alerting on auth failures, account lockouts, CSRF rejects, and auth rate limits.
- Session-cookie authenticated unsafe requests are rejected when they arrive as cross-site browser requests; same-origin browser calls and non-browser token clients continue to work.
- REST responses include `X-Request-ID`, and HTTP access logs now capture request ID, status, remote IP, actor, auth type, and structured error codes for correlation.
- `/api/v1/audit` now also records security-relevant auth failures such as invalid logins, temporary account lockouts, CSRF rejections, and auth endpoint rate limits.

```yaml
server:
  hostname: monsoon-01
  data_dir: /var/lib/monsoon
  log_level: info

dhcp:
  v4:
    enabled: true
    listen: 0.0.0.0:67
    interface: eth0
    authoritative: true
  default_lease_time: 12h
  max_lease_time: 24h

api:
  rest:
    enabled: true
    listen: :8067
    cors_origins:
      - http://localhost:5173
    trusted_proxies:
      - 127.0.0.1/32
    rate_limit: 100
    auth_rate_limit: 5
    tls_cert_file: /etc/monsoon/tls/server.crt
    tls_key_file: /etc/monsoon/tls/server.key
  grpc:
    enabled: true
    listen: :9067
    tls_cert_file: /etc/monsoon/tls/server.crt
    tls_key_file: /etc/monsoon/tls/server.key
  mcp:
    enabled: true
    listen: :7067
    tls_cert_file: /etc/monsoon/tls/server.crt
    tls_key_file: /etc/monsoon/tls/server.key

auth:
  enabled: true
  type: local
  local:
    admin_username: admin
    admin_password_hash: ""
    max_failed_attempts: 5
    lockout_duration: 15m
  session:
    duration: 24h
    cookie_name: monsoon_session
    secure: true
```

Environment variable examples:

```bash
MONSOON_SERVER_HOSTNAME=monsoon-01
MONSOON_DHCP_V4_LISTEN=0.0.0.0:67
MONSOON_API_REST_LISTEN=:8067
MONSOON_LOG_LEVEL=debug
```

For same-origin deployments behind a reverse proxy, keep `cors_origins: []`, set `api.rest.trusted_proxies` to that proxy's source IP/CIDR, and terminate TLS at the proxy or directly in Monsoon.

---
## Deployment

### Systemd

```ini
[Unit]
Description=Monsoon DHCP + IPAM Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/monsoon -c /etc/monsoon/monsoon.yaml
ExecReload=/bin/kill -HUP $MAINPID
Restart=always
AmbientCapabilities=CAP_NET_BIND_SERVICE CAP_NET_RAW
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/var/lib/monsoon

[Install]
WantedBy=multi-user.target
```

### Docker

```bash
docker run -d \
  --name monsoon \
  --net=host \
  --cap-add=NET_BIND_SERVICE \
  --cap-add=NET_RAW \
  -v /var/lib/monsoon:/var/lib/monsoon \
  -v /etc/monsoon:/etc/monsoon \
  monsoondhcp/monsoon:latest
```

> **Note:** `--net=host` is required so the container can handle DHCP broadcast packets.

### Platform Support

| Platform | Architecture | DHCP | IPAM | Web UI |
|----------|--------------|------|------|--------|
| Linux    | amd64, arm64 | Full | Full | Full |
| FreeBSD  | amd64        | Full | Full | Full |
| macOS    | amd64, arm64 | Limited (no raw socket) | Full | Full |
| Windows  | amd64        | Limited | Full | Full |

---

## Migration & Import

| Source | Format | What's Imported |
|--------|--------|-----------------|
| ISC DHCP | `dhcpd.conf` + `dhcpd.leases` | Subnets, pools, reservations, active leases |
| Kea | `kea-dhcp4.conf` + lease DB | Subnets, pools, reservations, leases |
| phpIPAM | MySQL dump or REST API | Subnets, addresses, VLANs |
| NetBox | REST API export | Prefixes, addresses, VLANs |
| CSV | Generic CSV | Any object type |

```bash
monsoon migrate --from isc-dhcp \
  --config /etc/dhcp/dhcpd.conf \
  --leases /var/lib/dhcp/dhcpd.leases
```

---

## Comparison

| Feature | Monsoon | ISC DHCP | Kea | phpIPAM | NetBox |
|---------|:-------:|:--------:|:---:|:-------:|:------:|
| DHCP Server | Yes | Yes | Yes | — | — |
| IPAM | Yes | — | — | Yes | Yes |
| Unified DHCP + IPAM | Yes | — | — | — | — |
| Single Binary | Yes | Yes | No (multi-process) | No (PHP+MySQL) | No (Python+PG) |
| Zero Dependencies | Yes | Yes | No | No | No |
| Embedded Web Dashboard | Yes | — | Stork (separate) | Yes | Yes |
| REST API | Yes | — | Yes | Yes | Yes |
| gRPC API | Yes | — | — | — | — |
| WebSocket Events | Yes | — | — | — | — |
| MCP Server | Yes | — | — | — | — |
| Network Discovery | Yes | — | — | Yes | — |
| DHCPv6 | Yes | Yes | Yes | — | — |
| HA / Failover | Yes | Limited | Yes | — | — |
| Visual IP Map | Yes | — | — | Yes | — |
| Audit Trail | Yes | — | Yes | Yes | Yes |
| Hot Config Reload | Yes | — | Yes | N/A | N/A |

---

## Observability

### Prometheus Metrics

```
monsoon_dhcp_requests_total{type="discover|offer|request|ack|nak|release|decline"}
monsoon_dhcp_leases_active{subnet="10.0.1.0/24"}
monsoon_dhcp_pool_utilization{subnet="10.0.1.0/24"}
monsoon_ipam_addresses_total{subnet,state}
monsoon_ipam_conflicts_total
monsoon_api_requests_total{method,path}
monsoon_ha_lease_sync_lag_seconds
```

### Structured Logging

```json
{
  "time": "2026-04-10T14:30:00Z",
  "level": "info",
  "msg": "DHCP lease created",
  "component": "dhcpv4",
  "ip": "10.0.1.50",
  "mac": "AA:BB:CC:DD:EE:FF",
  "hostname": "laptop-03",
  "subnet": "10.0.1.0/24",
  "lease_duration": "8h0m0s",
  "latency_us": 245
}
```

---

## Implementation Status

Current foundation already in place:

- Project/module scaffold and build files
- Configuration schema + defaults + validation
- `MONSOON_*` environment variable override system
- Hot-reload manager triggered by `SIGHUP`
- Live reload currently applies REST/auth runtime controls without restart: CORS allowlist, trusted proxies, general/auth rate limits, auth enforcement, secure session cookie
- Reloaded settings that change listeners, TLS, DHCP/discovery/HA runtime, auth backend/session lifetime, metrics path, or webhook dispatcher are accepted into config and reported as restart-required
- `GET /api/v1/system/info` exposes `config_reload`
- `GET/PUT /api/v1/system/config` include reload status under `meta.reload`
- `PUT /api/v1/system/config` now merges into the existing config snapshot instead of resetting omitted fields
- Storage foundation: WAL + sorted KV tree + snapshots + engine facade
- Storage-backed auth sessions survive restart on the same node
- DHCPv4 packet/options/handler/server baseline
- Lease state/store/expiry sweeper
- REST API shell + lease endpoints + middleware
- Storage-backed IPAM subnet engine with overlap validation and config seeding
- Subnet CRUD API (`GET/POST/PUT/DELETE /api/v1/subnets`, `GET /api/v1/subnets/raw`)
- Discovery status/scan/results API (`GET /api/v1/discovery/status`, `GET /api/v1/discovery/results`, `POST /api/v1/discovery/scan`)
- Persistent UI settings API (`GET/PUT /api/v1/settings/ui`)
- Backup/list/restore API (`GET /api/v1/system/backups`, `POST /api/v1/system/backup`, `POST /api/v1/system/restore`)
- React dashboard shell and responsive pages

Notes:
- Discovery and rogue-DHCP language in older docs/specs is broader than the current implementation. Treat the present feature as active scan orchestration plus lease/reservation-aware conflict reporting.
- DHCPv4 raw-socket support and deeper RFC edge cases are planned in upcoming iterations.
- The UI currently targets Monsoon REST endpoints and is production-buildable.

---

## Non-Goals (v1)

Explicitly out of scope for the initial release:

- **RADIUS / 802.1X** — not a NAC solution
- **Authoritative DNS server** — use a separate tool
- **Network configuration management** — not Ansible/Nornir
- **Full DCIM** — use NetBox for data center inventory
- **Multi-tenant SaaS** — self-hosted only
- **SNMP-based switch management** — read-only walk for discovery only

---

## License

Apache 2.0 — see the `LICENSE` file for details.

---

## Full Specification

For every detail — protocol coverage, lease state machine, storage layout, HA protocol, security model, webhook format — see [.project/SPECIFICATION.md](.project/SPECIFICATION.md).

