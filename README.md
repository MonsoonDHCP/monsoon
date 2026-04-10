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
- **Capacity Planning** — Utilization %, exhaustion forecast, automatic alerts
- **Network Discovery** — ARP scan, ICMP ping sweep, TCP probe, SNMP walk, passive DHCP learning
- **Conflict Detection** — Duplicate IP, rogue DHCP, orphaned lease, static/DHCP mismatches
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
- **Hot Reload** — Configuration refresh via `SIGHUP`
- **HA / Failover** — Active-passive or split-scope load sharing, TCP lease synchronization
- **Prometheus Metrics** — DHCP, IPAM, storage, API, WebSocket, HA metrics
- **Structured Logging** — Component-level JSON logs
- **Migration Tools** — ISC DHCP, Kea, phpIPAM, NetBox, generic CSV import

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

### Backend

```bash
go run ./cmd/monsoon --config ./configs/monsoon.yaml
```

Endpoints:
- Health — `GET http://localhost:8067/api/v1/system/health`
- Metrics — `GET http://localhost:8067/metrics`
- REST API — `http://localhost:8067/api/v1/`

Serve the React dashboard from the same backend port:

```bash
go run ./cmd/monsoon --config ./configs/monsoon.yaml --web-dist ./web/dist
```

### Frontend (development)

```bash
cd web
npm install
npm run dev
```

### Frontend build

```bash
cd web
npm run build
```

### CLI Flags

```
monsoon [flags]

  -c, --config string    Configuration file path
  -d, --data-dir string  Data directory
  -v, --version          Print version and exit
      --init             Initialize configuration and admin password
      --check-config     Validate configuration and exit
      --export-config    Export current configuration to stdout
      --backup           Create backup and exit
      --restore string   Restore from backup file
      --migrate          Run data migrations
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

### Subnets
| Method | Path | Description |
|--------|------|-------------|
| GET | `/subnets` | List all (tree or flat) |
| POST | `/subnets` | Create subnet |
| GET | `/subnets/{id}` | Get details |
| PUT | `/subnets/{id}` | Update |
| DELETE | `/subnets/{id}` | Delete |
| GET | `/subnets/{id}/utilization` | Usage statistics |
| POST | `/subnets/{id}/next-available` | Next free IP |
| POST | `/subnets/{id}/split` | Split subnet |

### Leases
| Method | Path | Description |
|--------|------|-------------|
| GET | `/leases` | List active leases |
| GET | `/leases/{ip}` | Lease details |
| DELETE | `/leases/{ip}` | Force-release |
| GET | `/leases/expiring` | Leases expiring soon |

### Addresses & Reservations
| Method | Path | Description |
|--------|------|-------------|
| GET | `/addresses` | Search/filter IPs |
| GET | `/addresses/{ip}/history` | Assignment history |
| GET | `/reservations` | List reservations |
| POST | `/reservations` | MAC → IP fixed mapping |

### Discovery & Audit
| Method | Path | Description |
|--------|------|-------------|
| POST | `/discovery/scan` | Trigger manual scan |
| GET | `/discovery/conflicts` | Detected conflicts |
| GET | `/discovery/rogue-dhcp` | Rogue DHCP servers |
| GET | `/audit` | Query audit log |

### System
| Method | Path | Description |
|--------|------|-------------|
| GET | `/system/health` | Health check |
| GET/PUT | `/system/config` | Configuration (hot reload) |
| POST | `/system/backup` | Create snapshot |
| GET | `/system/metrics` | Prometheus metrics |

### Real-time (WebSocket)

`ws://localhost:8067/ws` — live `lease.*`, `discovery.*`, `address.*`, `subnet.*` events.

```json
{"type": "lease.created", "data": {"ip": "10.0.1.50", "mac": "AA:BB:CC:DD:EE:FF", "hostname": "laptop-01"}}
{"type": "discovery.conflict", "data": {"ip": "10.0.1.50", "macs": ["AA:BB:...", "11:22:..."]}}
{"type": "subnet.exhaustion", "data": {"subnet": "10.0.1.0/24", "utilization": 0.95}}
```

For the complete endpoint list, see [.project/SPECIFICATION.md](.project/SPECIFICATION.md).

---

## Configuration

Monsoon is configured via a YAML file, and every value can be overridden via `MONSOON_*` environment variables.

```yaml
server:
  hostname: "monsoon-01"
  data_dir: "/var/lib/monsoon"
  log_level: "info"

dhcp:
  v4:
    enabled: true
    listen: "0.0.0.0:67"
    interface: "eth0"
    authoritative: true
  default_lease_time: "12h"
  max_lease_time: "24h"

subnets:
  - cidr: "10.0.1.0/24"
    name: "Server VLAN"
    vlan: 10
    gateway: "10.0.1.1"
    dns: ["10.0.1.2", "8.8.8.8"]
    dhcp:
      enabled: true
      pool_start: "10.0.1.50"
      pool_end: "10.0.1.200"
      lease_time: "8h"
    reservations:
      - mac: "AA:BB:CC:DD:EE:01"
        ip: "10.0.1.10"
        hostname: "web-server-01"

ipam:
  discovery:
    enabled: true
    default_interval: "1h"
    methods: ["arp", "ping"]
    conflict_detection: true
    rogue_dhcp_detection: true

api:
  rest:  { enabled: true, listen: ":8067" }
  grpc:  { enabled: true, listen: ":9067" }
  mcp:   { enabled: true, listen: ":7067" }

metrics:
  prometheus:
    enabled: true
    path: "/metrics"
```

Environment variable examples:

```bash
MONSOON_SERVER_HOSTNAME=monsoon-01
MONSOON_DHCP_V4_LISTEN=0.0.0.0:67
MONSOON_API_REST_LISTEN=:8067
MONSOON_LOG_LEVEL=debug
```

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
- Storage foundation: WAL + sorted KV tree + snapshots + engine facade
- DHCPv4 packet/options/handler/server baseline
- Lease state/store/expiry sweeper
- REST API shell + lease endpoints + middleware
- Storage-backed IPAM subnet engine with overlap validation and config seeding
- Subnet CRUD API (`GET/POST/PUT/DELETE /api/v1/subnets`, `GET /api/v1/subnets/raw`)
- Discovery status/scan API (`GET /api/v1/discovery/status`, `POST /api/v1/discovery/scan`)
- Persistent UI settings API (`GET/PUT /api/v1/settings/ui`)
- React dashboard shell and responsive pages

Notes:
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
