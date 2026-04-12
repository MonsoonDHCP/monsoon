# Monsoon — DHCP + IPAM Server

> Status note (2026-04-12): this document is broader than the current shipped build and should be read as product intent, not an exact implementation contract.
> Current build reality:
> - Auth: `local` only. LDAP config exists in schema/plans but is not implemented.
> - Sessions: persisted on the local node; not shared across HA peers.
> - Discovery: active scan orchestration plus lease/reservation-aware conflict reporting. True passive rogue-DHCP sensing is not implemented.
> - Storage: WAL + snapshot backed engine with sorted in-memory structures, not the fully realized page-backed B+Tree described below.
> - Restore: available in both CLI and REST (`POST /api/v1/system/restore`).
> - Config update: `PUT /api/v1/system/config` now merges into the current config instead of replacing omitted fields with defaults.

## Project Identity

| Field | Value |
|-------|-------|
| **Name** | Monsoon |
| **Tagline** | "Every Address. Accounted For." |
| **Domain** | monsoondhcp.com |
| **Repository** | github.com/monsoondhcp/monsoon |
| **License** | Apache 2.0 |
| **Language** | Go (pure, #NOFORKANYMORE) |
| **Binary** | `monsoon` (single binary, multi-platform) |
| **Replaces** | ISC DHCP + Kea + phpIPAM + NetBox IPAM |

---

## 1. Problem Statement

Network IP address management is fragmented across multiple tools:

- **ISC DHCP**: Legacy C daemon, config-file driven, no API, no UI, EOL announced.
- **Kea (ISC)**: Modern replacement but requires PostgreSQL/MySQL, Stork UI is separate deployment, complex multi-process architecture.
- **phpIPAM / NetBox**: IPAM-only tools (no DHCP), require PHP/Python + database stack, no lease awareness.
- **Infoblox / BlueCat**: Enterprise solutions, expensive licensing, vendor lock-in.

**The gap**: No single open-source tool provides unified DHCP serving + IP address management with a modern Web UI, REST API, and zero infrastructure dependencies.

**Monsoon fills this gap**: One binary that serves DHCP leases AND tracks every IP address across your network, with an embedded web dashboard, REST/gRPC APIs, and AI-ready MCP integration.

---

## 2. Architecture Overview

```
┌─────────────────────────────────────────────────────────┐
│                    MONSOON BINARY                        │
│                                                         │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐              │
│  │  DHCPv4  │  │  DHCPv6  │  │   IPAM   │              │
│  │  Engine  │  │  Engine  │  │  Engine   │              │
│  │ RFC 2131 │  │ RFC 8415 │  │          │              │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘              │
│       │              │             │                     │
│  ┌────▼──────────────▼─────────────▼─────┐              │
│  │          Lease & Address Store          │              │
│  │    (Unified State Machine + WAL)       │              │
│  └────────────────┬──────────────────────┘              │
│                   │                                      │
│  ┌────────────────▼──────────────────────┐              │
│  │         Embedded Storage Engine        │              │
│  │     B+Tree Index + WAL + Snapshots    │              │
│  └───────────────────────────────────────┘              │
│                                                         │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────┐ │
│  │ REST API │  │   gRPC   │  │ WebSocket│  │  MCP   │ │
│  │  Server  │  │  Server  │  │  Server  │  │ Server │ │
│  └──────────┘  └──────────┘  └──────────┘  └────────┘ │
│                                                         │
│  ┌─────────────────────────────────────────────────┐   │
│  │            Embedded Web Dashboard                │   │
│  │     (Vanilla JS + CSS — embedded in binary)     │   │
│  └─────────────────────────────────────────────────┘   │
│                                                         │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐              │
│  │  DDNS    │  │ Discovery│  │  HA /    │              │
│  │  Client  │  │  Scanner │  │ Failover │              │
│  └──────────┘  └──────────┘  └──────────┘              │
└─────────────────────────────────────────────────────────┘
```

### 2.1 Design Principles

1. **Single Binary**: Everything embedded — DHCP server, IPAM engine, web dashboard, storage. `./monsoon` and done.
2. **Zero External Dependencies**: #NOFORKANYMORE. Only `golang.org/x/crypto`, `golang.org/x/sys`, `gopkg.in/yaml.v3`.
3. **Unified State**: DHCP leases and IPAM records share one truth source. A lease event automatically updates IPAM state.
4. **Multi-Platform**: Linux (primary), macOS, Windows. Cross-compiled via `GOOS/GOARCH`.
5. **API-First**: Every feature accessible via REST API. Web UI consumes the same API.
6. **Observable**: Structured logging, Prometheus metrics endpoint, health checks.

---

## 3. DHCP Server Engine

### 3.1 DHCPv4 (RFC 2131/2132)

#### 3.1.1 Protocol Support

| RFC | Title | Status |
|-----|-------|--------|
| RFC 2131 | Dynamic Host Configuration Protocol | Full |
| RFC 2132 | DHCP Options and BOOTP Vendor Extensions | Full |
| RFC 3046 | DHCP Relay Agent Information Option (Option 82) | Full |
| RFC 4361 | Node-specific Client Identifiers for DHCPv4 | Full |
| RFC 6842 | Client Identifier Option in DHCP Server Replies | Full |
| RFC 2136 | Dynamic Updates in the DNS (DDNS) | Full |
| RFC 3442 | Classless Static Route Option (Option 121) | Full |
| RFC 4039 | Rapid Commit Option | Full |
| RFC 3004 | User Class Option | Full |
| RFC 3011 | IPv4 Subnet Selection Option | Full |

#### 3.1.2 DHCP Message Flow

```
Client                    Server
  │                          │
  │──── DHCPDISCOVER ───────►│  (broadcast, src=0.0.0.0)
  │                          │
  │◄──── DHCPOFFER ─────────│  (offer IP from pool)
  │                          │
  │──── DHCPREQUEST ────────►│  (request offered IP)
  │                          │
  │◄──── DHCPACK ───────────│  (confirm lease)
  │                          │
  │  ... lease duration ...  │
  │                          │
  │──── DHCPREQUEST ────────►│  (renew at T1=50%)
  │◄──── DHCPACK ───────────│
  │                          │
  │──── DHCPRELEASE ────────►│  (client done)
  │                          │
```

#### 3.1.3 Lease State Machine

```
          DISCOVER
              │
              ▼
         ┌─────────┐
         │  FREE    │◄──────── expiry timer
         └────┬─────┘          │
              │ OFFER          │
              ▼                │
         ┌─────────┐          │
         │ OFFERED  │──────────┤ (offer timeout)
         └────┬─────┘          │
              │ REQUEST+ACK    │
              ▼                │
         ┌─────────┐          │
    ┌───►│  BOUND   │──────────┤ (lease expiry)
    │    └────┬─────┘          │
    │         │ RENEW          │
    │         ▼                │
    │    ┌─────────┐          │
    │    │RENEWING  │──────────┤ (rebind timeout)
    │    └────┬─────┘          │
    │         │ ACK            │
    │         │                │
    └─────────┘                │
                               │
         ┌─────────┐          │
         │RELEASED  │──────────┘
         └─────────┘
              │
         ┌─────────┐
         │DECLINED  │ (conflict detected)
         └─────────┘
              │
         ┌─────────┐
         │QUARANTINE│ (grace period before reuse)
         └─────────┘
```

#### 3.1.4 Lease Storage Fields

```go
type Lease struct {
    IP            net.IP        // Assigned IP address
    MAC           net.HardwareAddr // Client MAC
    ClientID      []byte        // Option 61 client identifier
    Hostname      string        // Option 12 hostname
    State         LeaseState    // Current state
    StartTime     time.Time     // Lease start
    Duration      time.Duration // Lease duration
    T1            time.Duration // Renewal time (default 50%)
    T2            time.Duration // Rebind time (default 87.5%)
    ExpiryTime    time.Time     // Absolute expiry
    SubnetID      string        // Parent subnet reference
    RelayAddr     net.IP        // GIADDR if relayed
    RelayInfo     []byte        // Option 82 data
    CircuitID     string        // Option 82 sub-option 1
    RemoteID      string        // Option 82 sub-option 2
    VendorClass   string        // Option 60
    UserClass     string        // Option 77
    LastSeen      time.Time     // Last DHCP interaction
    DDNSForward   string        // Forward DNS name registered
    DDNSReverse   string        // Reverse PTR registered
    Tags          map[string]string // Custom metadata
    CreatedAt     time.Time
    UpdatedAt     time.Time
}
```

#### 3.1.5 Option Engine

Standard DHCP options plus custom option definitions:

```yaml
# monsoon.yaml - DHCP option templates
option_templates:
  office_standard:
    - option: 3    # Router
      value: "{{.Gateway}}"
    - option: 6    # DNS Servers
      value: "8.8.8.8,8.8.4.4"
    - option: 15   # Domain Name
      value: "office.local"
    - option: 42   # NTP Servers
      value: "pool.ntp.org"
    - option: 121  # Classless Static Routes
      value: "10.0.0.0/8,10.0.0.1"
    
  pxe_boot:
    - option: 66   # TFTP Server
      value: "pxe.office.local"
    - option: 67   # Boot Filename
      value: "pxelinux.0"

  voip_phones:
    - option: 66   # TFTP Server
      value: "phone-config.local"
    - option: 150  # TFTP Server Address (Cisco)
      value: "10.0.1.5"
    - option: 176  # Avaya IP Phone
      value: "HTTPSRVR=10.0.1.5"
```

#### 3.1.6 Client Classification

Match clients to pools/options based on:

```yaml
classifications:
  - name: "cisco_phones"
    match:
      vendor_class: "Cisco Systems, Inc. IP Phone*"
    pool: "voip_pool"
    options: "voip_phones"
    
  - name: "pxe_clients"
    match:
      option_93: "0"  # Client System Architecture (x86 BIOS)
    pool: "pxe_pool"
    options: "pxe_boot"
    
  - name: "known_hosts"
    match:
      mac_prefix: "AA:BB:CC"
    pool: "trusted_pool"
    lease_time: "24h"
    
  - name: "unknown"
    match:
      default: true
    pool: "guest_pool"
    lease_time: "1h"
```

### 3.2 DHCPv6 (RFC 8415)

#### 3.2.1 Protocol Support

| RFC | Title | Status |
|-----|-------|--------|
| RFC 8415 | DHCPv6 (consolidated) | Full |
| RFC 3633 | IPv6 Prefix Delegation | Full |
| RFC 3736 | Stateless DHCPv6 | Full |
| RFC 4704 | DHCPv6 Client FQDN Option | Full |
| RFC 8156 | DHCPv6 Failover Protocol | Full |

#### 3.2.2 DHCPv6 Message Types

- **Solicit / Advertise / Request / Reply**: Standard 4-message exchange
- **Confirm**: Client verifies address still valid after link change
- **Renew / Rebind**: Lease extension
- **Release / Decline**: Address return / conflict
- **Information-Request**: Stateless configuration only
- **Relay-Forward / Relay-Reply**: Relay agent support
- **Reconfigure**: Server-initiated client reconfiguration

#### 3.2.3 Prefix Delegation (PD)

```yaml
prefix_delegation:
  - pool: "customer_prefixes"
    prefix: "2001:db8::/32"
    delegation_length: 48    # Each customer gets a /48
    valid_lifetime: "86400s"
    preferred_lifetime: "43200s"
    
  - pool: "home_routers"
    prefix: "fd00::/32"
    delegation_length: 56    # Each home router gets a /56
```

### 3.3 Relay Agent Support

```
┌──────────┐     ┌──────────┐     ┌──────────┐
│  Client   │────►│  Relay   │────►│ Monsoon  │
│ (VLAN 10) │     │  Agent   │     │  Server  │
└──────────┘     └──────────┘     └──────────┘
                  GIADDR=10.0.10.1
                  Option 82:
                    Circuit-ID: "Gi0/1"
                    Remote-ID: "switch01.office"
```

Monsoon uses GIADDR to select the correct subnet/pool:

```yaml
subnets:
  - cidr: "10.0.10.0/24"
    gateway: "10.0.10.1"        # Matches GIADDR
    pool: "10.0.10.10-10.0.10.200"
    relay_match:
      giaddr: "10.0.10.1"
      circuit_id_prefix: "Gi0/"  # Optional: per-port matching
```

---

## 4. IPAM Engine

### 4.1 Data Model

#### 4.1.1 Hierarchical Subnet Tree

```
Root
├── 10.0.0.0/8 (RFC 1918 Private)
│   ├── 10.0.0.0/16 (HQ Network)
│   │   ├── 10.0.1.0/24 (Server VLAN)
│   │   ├── 10.0.2.0/24 (Desktop VLAN)
│   │   ├── 10.0.3.0/24 (VoIP VLAN)
│   │   └── 10.0.10.0/24 (Guest WiFi)
│   └── 10.1.0.0/16 (Branch Office)
│       ├── 10.1.1.0/24 (Branch Servers)
│       └── 10.1.2.0/24 (Branch Desktops)
├── 172.16.0.0/12 (RFC 1918 Private)
│   └── 172.16.0.0/16 (Lab Network)
└── 192.168.0.0/16 (RFC 1918 Private)
    └── 192.168.1.0/24 (Home Office VPN)
```

#### 4.1.2 Core IPAM Objects

```go
// Subnet represents a managed network segment
type Subnet struct {
    ID              string            // Unique identifier
    CIDR            string            // e.g., "10.0.1.0/24"
    Network         net.IPNet         // Parsed network
    ParentID        string            // Parent subnet ID (hierarchy)
    Name            string            // Human-friendly name
    Description     string            // Purpose description
    VLANID          *int              // Associated VLAN (optional)
    Gateway         net.IP            // Default gateway
    DNSServers      []net.IP          // DNS servers for this subnet
    DHCPEnabled     bool              // Serve DHCP on this subnet?
    DHCPPoolStart   net.IP            // DHCP range start
    DHCPPoolEnd     net.IP            // DHCP range end
    LeaseTime       time.Duration     // Default lease duration
    OptionTemplate  string            // DHCP option template name
    Location        string            // Physical/logical location
    Tags            map[string]string // Custom metadata
    Utilization     float64           // Calculated: used/total %
    TotalAddresses  int64             // Total usable addresses
    UsedAddresses   int64             // Currently used
    CreatedAt       time.Time
    UpdatedAt       time.Time
}

// IPAddress represents a single tracked address
type IPAddress struct {
    IP              net.IP            // The address
    SubnetID        string            // Parent subnet
    State           IPState           // Current state
    Type            IPType            // Assignment type
    MAC             net.HardwareAddr  // Associated MAC (if known)
    Hostname        string            // DNS hostname
    FQDN            string            // Fully qualified domain name
    Description     string            // Purpose/owner note
    LeaseID         string            // DHCP lease reference (if active)
    LastSeen        time.Time         // Last ARP/ping response
    LastScanTime    time.Time         // Last discovery scan
    PTRRecord       string            // Reverse DNS PTR
    SwitchPort      string            // Connected switch port (if known)
    Owner           string            // Department/person
    Tags            map[string]string // Custom metadata
    CreatedAt       time.Time
    UpdatedAt       time.Time
}

// IPState represents the lifecycle state of an IP
type IPState int

const (
    IPStateAvailable   IPState = iota // Free to assign
    IPStateReserved                   // Manually reserved
    IPStateDHCP                       // Assigned via DHCP lease
    IPStateStatic                     // Manually assigned (no DHCP)
    IPStateQuarantined                // Recently released, cooling down
    IPStateAbandoned                  // No response in multiple scans
    IPStateConflict                   // Duplicate detected
)

// IPType represents how the address was assigned
type IPType int

const (
    IPTypeDynamic    IPType = iota // DHCP dynamic
    IPTypeReserved                 // DHCP reservation (MAC→IP)
    IPTypeStatic                   // Manual static assignment
    IPTypeInfra                    // Infrastructure (gateway, DNS, etc.)
    IPTypeVIP                      // Virtual IP / floating IP
)

// VLAN represents a virtual LAN
type VLAN struct {
    ID          int               // VLAN ID (1-4094)
    Name        string            // VLAN name
    Description string
    SubnetIDs   []string          // Associated subnets
    Location    string
    Tags        map[string]string
}

// DHCPReservation is a fixed MAC→IP mapping
type DHCPReservation struct {
    MAC         net.HardwareAddr
    IP          net.IP
    SubnetID    string
    Hostname    string
    Description string
    Options     []DHCPOption      // Per-host DHCP options
    Enabled     bool
}
```

### 4.2 Subnet Management

#### 4.2.1 CIDR Calculator

Built-in CIDR operations:
- **Split**: Divide a /24 into two /25s, four /26s, etc.
- **Merge**: Combine adjacent subnets into a larger block
- **Supernet**: Find the smallest supernet containing given subnets
- **Available**: Find free address ranges within a subnet
- **Next Available**: Get next free IP in a subnet (for auto-assignment)
- **Overlap Detection**: Check if two subnets overlap

#### 4.2.2 Capacity Planning

```
Subnet: 10.0.1.0/24 (Server VLAN)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Total Addresses : 254 (usable)
Used            : 187 (73.6%)
Available       : 52 (20.5%)
Reserved        : 12 (4.7%)
Quarantined     : 3 (1.2%)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠ Warning: 73.6% utilized — consider expanding
  Projected exhaustion: ~45 days at current growth
  Recommendation: Split to /23 or create overflow subnet
```

### 4.3 Network Discovery

#### 4.3.1 Discovery Methods

1. **ARP Scan**: Send ARP requests across subnet, collect MAC→IP mappings
2. **ICMP Ping Sweep**: ICMP echo request to each address
3. **TCP Connect Probe**: Check common ports (22, 80, 443, 3389) for hosts that block ICMP
4. **SNMP Walk**: Query switches for MAC address tables and port mappings (v2c/v3)
5. **Passive DHCP**: Learn from DHCP DISCOVER/REQUEST traffic (zero-probe)
6. **DNS Reverse Lookup**: PTR record queries for discovered IPs

#### 4.3.2 Discovery Scheduler

```yaml
discovery:
  enabled: true
  schedules:
    - name: "server_vlan_frequent"
      subnets: ["10.0.1.0/24"]
      interval: "5m"
      methods: ["arp", "ping"]
      
    - name: "all_networks_daily"
      subnets: ["10.0.0.0/16"]
      interval: "24h"
      methods: ["arp", "ping", "tcp_probe", "dns_reverse"]
      tcp_ports: [22, 80, 443, 3389, 8080]
      
    - name: "passive_always"
      subnets: ["*"]
      method: "passive_dhcp"
      interval: "continuous"
```

#### 4.3.3 Conflict Detection

- **Duplicate IP**: Two different MACs claiming the same IP → alert + mark as `Conflict`
- **Rogue DHCP**: Detect unauthorized DHCP servers on the network
- **Orphaned Leases**: DHCP lease active but no network response → mark as `Abandoned`
- **Static/DHCP Mismatch**: Static IP record but DHCP lease exists for different host

### 4.4 Audit Trail

Every change to IPAM state is logged:

```go
type AuditEntry struct {
    ID          string    // Unique entry ID
    Timestamp   time.Time // When
    Actor       string    // Who (user, "dhcp-engine", "discovery", "api:token:xxx")
    Action      string    // What (create, update, delete, assign, release, reserve)
    ObjectType  string    // Which type (subnet, ip, lease, reservation, vlan)
    ObjectID    string    // Which object
    OldValue    string    // Previous state (JSON)
    NewValue    string    // New state (JSON)
    Source      string    // How (web-ui, rest-api, grpc, dhcp-protocol, discovery)
    ClientIP    net.IP    // Request source IP
    Notes       string    // Optional human note
}
```

---

## 5. Embedded Storage Engine

### 5.1 Architecture

Custom embedded key-value store optimized for DHCP/IPAM workloads:

```
┌─────────────────────────────────┐
│         Write Path              │
│  Write → WAL → MemTable → Flush│
└─────────────┬───────────────────┘
              │
┌─────────────▼───────────────────┐
│         Read Path               │
│  MemTable → B+Tree Index → Data│
└─────────────────────────────────┘
              │
┌─────────────▼───────────────────┐
│         Persistence             │
│  WAL + B+Tree Pages + Snapshots │
└─────────────────────────────────┘
```

### 5.2 Storage Layout

```
/var/lib/monsoon/
├── data/
│   ├── leases.db         # B+Tree: lease records
│   ├── subnets.db        # B+Tree: subnet definitions
│   ├── addresses.db      # B+Tree: IP address records
│   ├── reservations.db   # B+Tree: DHCP reservations
│   ├── vlans.db          # B+Tree: VLAN definitions
│   └── audit.db          # Append-only: audit log
├── wal/
│   ├── 000001.wal        # Write-ahead log segments
│   └── 000002.wal
├── snapshots/
│   └── 20250410T120000.snap
└── config/
    └── monsoon.yaml
```

### 5.3 Indexes

- **Lease by IP**: Primary lookup for DHCP operations
- **Lease by MAC**: Find all leases for a client
- **Lease by Expiry**: Time-sorted index for expiry processing
- **IP by Subnet**: Range scan for subnet utilization
- **IP by State**: Filter by available/reserved/etc.
- **IP by MAC**: Reverse lookup
- **Subnet by CIDR**: Prefix tree for longest-prefix match
- **Audit by Time**: Time-range queries for audit trail

### 5.4 Consistency & Recovery

- **WAL**: All mutations written to WAL before applying. fsync on commit.
- **Checkpoints**: Periodic flush from WAL to B+Tree pages.
- **Snapshots**: Full consistent snapshot for backup/restore.
- **Crash Recovery**: Replay WAL from last checkpoint on startup.

---

## 6. API Layer

### 6.1 REST API

Base URL: `http://localhost:8067/api/v1`

#### 6.1.1 Subnet Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/subnets` | List all subnets (tree or flat) |
| POST | `/subnets` | Create subnet |
| GET | `/subnets/{id}` | Get subnet details |
| PUT | `/subnets/{id}` | Update subnet |
| DELETE | `/subnets/{id}` | Delete subnet |
| GET | `/subnets/{id}/addresses` | List IPs in subnet |
| GET | `/subnets/{id}/available` | List available IPs |
| POST | `/subnets/{id}/next-available` | Get next free IP |
| GET | `/subnets/{id}/utilization` | Usage statistics |
| POST | `/subnets/{id}/split` | Split subnet |
| POST | `/subnets/{id}/scan` | Trigger discovery scan |

#### 6.1.2 IP Address Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/addresses` | Search/filter addresses |
| GET | `/addresses/{ip}` | Get IP details |
| POST | `/addresses` | Create/reserve IP |
| PUT | `/addresses/{ip}` | Update IP record |
| DELETE | `/addresses/{ip}` | Release IP |
| GET | `/addresses/{ip}/history` | IP assignment history |

#### 6.1.3 Lease Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/leases` | List active leases |
| GET | `/leases/{ip}` | Get lease details |
| DELETE | `/leases/{ip}` | Force-release lease |
| GET | `/leases/expiring` | Leases expiring soon |
| GET | `/leases/stats` | Lease statistics |

#### 6.1.4 Reservation Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/reservations` | List all reservations |
| POST | `/reservations` | Create reservation |
| GET | `/reservations/{mac}` | Get reservation by MAC |
| PUT | `/reservations/{mac}` | Update reservation |
| DELETE | `/reservations/{mac}` | Delete reservation |

#### 6.1.5 VLAN Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/vlans` | List VLANs |
| POST | `/vlans` | Create VLAN |
| GET | `/vlans/{id}` | Get VLAN |
| PUT | `/vlans/{id}` | Update VLAN |
| DELETE | `/vlans/{id}` | Delete VLAN |

#### 6.1.6 Discovery Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/discovery/scan` | Trigger manual scan |
| GET | `/discovery/results` | Latest scan results |
| GET | `/discovery/conflicts` | Detected conflicts |
| GET | `/discovery/rogue` | Persisted rogue findings, if any |

#### 6.1.7 Audit Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/audit` | Query audit log |
| GET | `/audit/export` | Export audit as CSV/JSON |

#### 6.1.8 System Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/system/health` | Health check |
| GET | `/system/config` | Current configuration |
| PUT | `/system/config` | Update configuration (hot reload) |
| POST | `/system/backup` | Create snapshot |
| POST | `/system/restore` | Restore from snapshot |
| GET | `/system/metrics` | Prometheus metrics |
| GET | `/system/info` | Version, uptime, stats |

### 6.2 gRPC API

```protobuf
syntax = "proto3";
package monsoon.v1;

service SubnetService {
    rpc ListSubnets(ListSubnetsRequest) returns (ListSubnetsResponse);
    rpc CreateSubnet(CreateSubnetRequest) returns (Subnet);
    rpc GetSubnet(GetSubnetRequest) returns (Subnet);
    rpc UpdateSubnet(UpdateSubnetRequest) returns (Subnet);
    rpc DeleteSubnet(DeleteSubnetRequest) returns (Empty);
    rpc GetSubnetUtilization(GetSubnetRequest) returns (UtilizationResponse);
}

service LeaseService {
    rpc ListLeases(ListLeasesRequest) returns (ListLeasesResponse);
    rpc GetLease(GetLeaseRequest) returns (Lease);
    rpc ReleaseLease(ReleaseLeaseRequest) returns (Empty);
    rpc WatchLeases(WatchLeasesRequest) returns (stream LeaseEvent);
}

service AddressService {
    rpc SearchAddresses(SearchAddressesRequest) returns (SearchAddressesResponse);
    rpc GetAddress(GetAddressRequest) returns (IPAddress);
    rpc ReserveAddress(ReserveAddressRequest) returns (IPAddress);
    rpc ReleaseAddress(ReleaseAddressRequest) returns (Empty);
    rpc NextAvailable(NextAvailableRequest) returns (IPAddress);
}

service DiscoveryService {
    rpc TriggerScan(TriggerScanRequest) returns (ScanResponse);
    rpc GetConflicts(GetConflictsRequest) returns (ConflictsResponse);
    rpc WatchDiscovery(WatchDiscoveryRequest) returns (stream DiscoveryEvent);
}
```

### 6.3 WebSocket Events

Endpoint: `ws://localhost:8067/ws`

```json
// Lease events
{"type": "lease.created",   "data": {"ip": "10.0.1.50", "mac": "AA:BB:CC:DD:EE:FF", "hostname": "laptop-01"}}
{"type": "lease.renewed",   "data": {"ip": "10.0.1.50", "remaining": "23h45m"}}
{"type": "lease.expired",   "data": {"ip": "10.0.1.50", "mac": "AA:BB:CC:DD:EE:FF"}}
{"type": "lease.released",  "data": {"ip": "10.0.1.50"}}

// Discovery events  
{"type": "discovery.started",   "data": {"subnet": "10.0.1.0/24", "method": "arp"}}
{"type": "discovery.host_found","data": {"ip": "10.0.1.99", "mac": "11:22:33:44:55:66"}}
{"type": "discovery.conflict",  "data": {"ip": "10.0.1.50", "macs": ["AA:BB:...", "11:22:..."]}}
{"type": "discovery.completed", "data": {"subnet": "10.0.1.0/24", "found": 45}}

// IPAM events
{"type": "address.reserved",   "data": {"ip": "10.0.1.200", "owner": "admin"}}
{"type": "subnet.exhaustion",  "data": {"subnet": "10.0.1.0/24", "utilization": 0.95}}
{"type": "subnet.created",     "data": {"cidr": "10.0.2.0/24", "name": "New VLAN"}}
```

### 6.4 MCP Server

Model Context Protocol integration for AI-assisted network management:

```
Tools:
├── monsoon_list_subnets       — List all subnets with utilization
├── monsoon_get_subnet         — Get subnet details
├── monsoon_create_subnet      — Create new subnet
├── monsoon_find_available_ip  — Find next available IP in subnet
├── monsoon_reserve_ip         — Reserve an IP address
├── monsoon_list_leases        — List active DHCP leases
├── monsoon_get_lease          — Get lease details by IP
├── monsoon_search_by_mac      — Find all records for a MAC address
├── monsoon_search_by_hostname — Find host by name
├── monsoon_subnet_utilization — Get capacity/usage stats
├── monsoon_run_discovery      — Trigger network scan
├── monsoon_get_conflicts      — List detected conflicts
├── monsoon_audit_query        — Search audit log
├── monsoon_get_health         — System health status
└── monsoon_plan_subnet        — AI-assisted subnet planning
    (input: "I need 500 addresses for IoT devices")
    (output: suggested CIDR, gateway, DHCP range)
```

### 6.5 Webhook Notifications

```yaml
webhooks:
  - name: "slack_alerts"
    url: "https://hooks.slack.com/services/xxx"
    events: 
      - "subnet.exhaustion"
      - "discovery.conflict"
      - "discovery.rogue_dhcp"
      - "lease.conflict"
    format: "slack"
    
  - name: "generic_webhook"
    url: "https://monitoring.internal/webhook"
    events: ["*"]
    format: "json"
    headers:
      Authorization: "Bearer {{.Token}}"
    retry:
      max_attempts: 3
      backoff: "exponential"
```

---

## 7. Web Dashboard

### 7.1 Technology

- **Vanilla JavaScript**: No React/Vue/Angular. ES2020+ modules.
- **Vanilla CSS**: Custom properties (CSS variables) for theming. No Tailwind.
- **Embedded**: All assets compiled into the Go binary via `embed.FS`.
- **API Consumer**: Dashboard uses the same REST API + WebSocket as external clients.
- **SSE/WebSocket**: Real-time updates for lease activity, discovery, alerts.

### 7.2 Dashboard Pages

#### 7.2.1 Overview (Dashboard Home)

```
┌─────────────────────────────────────────────────────────┐
│  MONSOON DASHBOARD                            [🌙/☀️]  │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐  │
│  │  TOTAL   │ │  ACTIVE  │ │ AVAILABLE│ │ CONFLICTS│  │
│  │ SUBNETS  │ │  LEASES  │ │    IPs   │ │ DETECTED │  │
│  │   24     │ │  1,847   │ │  3,291   │ │    2     │  │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘  │
│                                                         │
│  TOP SUBNETS BY UTILIZATION                             │
│  ┌─────────────────────────────────────────────────┐   │
│  │ 10.0.1.0/24  Server VLAN    ████████████░░  89% │   │
│  │ 10.0.2.0/24  Desktop VLAN   ███████████░░░  78% │   │
│  │ 10.0.10.0/24 Guest WiFi     █████░░░░░░░░  35% │   │
│  │ 10.0.3.0/24  VoIP VLAN      ████░░░░░░░░░  28% │   │
│  └─────────────────────────────────────────────────┘   │
│                                                         │
│  RECENT LEASE ACTIVITY (live)              [View All →] │
│  ┌─────────────────────────────────────────────────┐   │
│  │ 🟢 10.0.2.45   AA:BB:CC:DD:EE:01  laptop-03    │   │
│  │ 🔄 10.0.1.102  11:22:33:44:55:66  db-server-02 │   │
│  │ 🔴 10.0.10.88  FF:EE:DD:CC:BB:AA  (expired)    │   │
│  │ 🟢 10.0.2.67   AA:BB:CC:DD:EE:02  desktop-15   │   │
│  └─────────────────────────────────────────────────┘   │
│                                                         │
│  ALERTS                                                 │
│  ⚠️  Subnet 10.0.1.0/24 at 89% capacity               │
│  🔴  IP conflict: 10.0.2.33 (2 MACs detected)          │
│  ⚠️  Rogue DHCP server detected: 10.0.2.200            │
└─────────────────────────────────────────────────────────┘
```

#### 7.2.2 Subnet Tree View

Hierarchical subnet browser with expand/collapse:
- Visual subnet tree (parent → children)
- Inline utilization bars
- Click to drill into any subnet
- Drag-and-drop subnet reorganization
- Right-click context menu (split, merge, delete)

#### 7.2.3 IP Grid View (Visual IP Map)

```
10.0.1.0/24 — Server VLAN
┌──┬──┬──┬──┬──┬──┬──┬──┬──┬──┬──┬──┬──┬──┬──┬──┐
│.0│.1│.2│.3│.4│.5│.6│.7│.8│.9│10│11│12│13│14│15│
│██│🟡│🔵│🔵│🟢│🟢│🟢│🟢│🟢│🟢│🔵│🔵│🟢│🟢│🟢│🟢│
├──┼──┼──┼──┼──┼──┼──┼──┼──┼──┼──┼──┼──┼──┼──┼──┤
│16│17│18│19│20│21│22│23│24│25│26│27│28│29│30│31│
│🟢│🟢│🟢│🟢│░░│░░│░░│░░│░░│🔵│🔴│░░│░░│░░│░░│░░│
...

Legend: ██ Network  🟡 Gateway  🔵 Reserved  🟢 DHCP Active  
        🔴 Conflict  ⬜ Available  ░░ Unused
        
Hover: Shows MAC, hostname, lease time, last seen
Click: Opens IP detail panel
```

#### 7.2.4 Lease Browser

- Filterable/searchable table of all active leases
- Columns: IP, MAC, Hostname, State, Start, Expires, Remaining, Subnet
- Real-time countdown timers
- Bulk actions: release, convert to reservation
- Export: CSV, JSON

#### 7.2.5 Reservations Manager

- CRUD for DHCP reservations (MAC → IP fixed mappings)
- Import from CSV
- Bulk create from discovery results

#### 7.2.6 Discovery & Scanning

- Manual scan trigger with progress indicator
- Results table with new/known/changed/missing classification
- Conflict resolution workflow
- Rogue DHCP server alerts

#### 7.2.7 Audit Log Viewer

- Time-range filtered audit log
- Filter by actor, action, object type
- JSON diff viewer for changes
- Export to CSV

#### 7.2.8 Settings

- DHCP configuration editor (YAML with validation)
- Subnet/pool management
- User/API token management
- Discovery schedule configuration
- Webhook configuration
- Backup/restore
- System info & diagnostics

### 7.3 Theming

```css
:root {
    /* Monsoon Dark Theme */
    --bg-primary: #0f1419;
    --bg-secondary: #1a2332;
    --bg-tertiary: #243042;
    --text-primary: #e8edf4;
    --text-secondary: #8899aa;
    --accent: #00b4d8;          /* Monsoon Blue — water/rain */
    --accent-hover: #0096c7;
    --success: #2dd4bf;
    --warning: #fbbf24;
    --error: #f87171;
    --info: #60a5fa;
    --border: #2a3a4a;
    
    /* IP State Colors */
    --ip-available: #2dd4bf;
    --ip-dhcp: #00b4d8;
    --ip-reserved: #a78bfa;
    --ip-static: #60a5fa;
    --ip-conflict: #f87171;
    --ip-quarantine: #fbbf24;
    --ip-abandoned: #6b7280;
}

:root[data-theme="light"] {
    --bg-primary: #f8fafc;
    --bg-secondary: #ffffff;
    --bg-tertiary: #f1f5f9;
    --text-primary: #1e293b;
    --text-secondary: #64748b;
    --accent: #0284c7;
    --border: #e2e8f0;
}
```

---

## 8. Configuration

### 8.1 Configuration File

```yaml
# /etc/monsoon/monsoon.yaml

# Server Configuration
server:
  hostname: "monsoon-01"
  data_dir: "/var/lib/monsoon"
  log_level: "info"            # debug, info, warn, error
  log_format: "json"           # json, text
  
# DHCP Engine
dhcp:
  v4:
    enabled: true
    listen: "0.0.0.0:67"
    interface: "eth0"           # Bind to specific interface
    authoritative: true         # Authoritative for configured subnets
    
  v6:
    enabled: false
    listen: "[::]:547"
    interface: "eth0"
    
  default_lease_time: "12h"
  max_lease_time: "24h"
  
  # DDNS
  ddns:
    enabled: false
    forward_zone: "office.local"
    reverse_zone: "1.0.10.in-addr.arpa"
    dns_server: "10.0.1.2:53"
    tsig_key: "dhcp-key"
    tsig_secret: "base64secret=="
    tsig_algorithm: "hmac-sha256"

# Subnets & Pools
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
      options:
        template: "office_standard"
    reservations:
      - mac: "AA:BB:CC:DD:EE:01"
        ip: "10.0.1.10"
        hostname: "web-server-01"
      - mac: "AA:BB:CC:DD:EE:02"
        ip: "10.0.1.11"
        hostname: "db-server-01"

  - cidr: "10.0.2.0/24"
    name: "Desktop VLAN"
    vlan: 20
    gateway: "10.0.2.1"
    dns: ["10.0.1.2"]
    dhcp:
      enabled: true
      pool_start: "10.0.2.10"
      pool_end: "10.0.2.240"
      lease_time: "4h"

  - cidr: "10.0.10.0/24"
    name: "Guest WiFi"
    vlan: 100
    gateway: "10.0.10.1"
    dns: ["8.8.8.8", "8.8.4.4"]
    dhcp:
      enabled: true
      pool_start: "10.0.10.10"
      pool_end: "10.0.10.250"
      lease_time: "1h"
      max_lease_time: "2h"
      classifications:
        default: "guest_pool"

# IPAM Configuration
ipam:
  discovery:
    enabled: true
    default_interval: "1h"
    methods: ["arp", "ping"]
    conflict_detection: true
    rogue_dhcp_detection: true
    quarantine_period: "15m"    # Cooldown for released IPs
    abandoned_threshold: "7d"   # Mark abandoned after 7 days no response
    
  auto_create_reverse_dns: false
  track_mac_vendors: true       # OUI lookup for MAC vendor identification

# API Configuration
api:
  rest:
    enabled: true
    listen: ":8067"
    cors_origins: ["*"]
    rate_limit: 100             # requests/second
    
  grpc:
    enabled: true
    listen: ":9067"
    
  websocket:
    enabled: true
    # Shares port with REST server
    
  mcp:
    enabled: true
    listen: ":7067"

# Web Dashboard
dashboard:
  enabled: true
  # Served on REST API port under /
  base_path: "/"
  
# Authentication
auth:
  enabled: true
  type: "local"                 # current build supports only local
  
  local:
    admin_username: "admin"
    admin_password_hash: ""     # Set on first run
    
  ldap:                         # planned schema; not implemented in current build
    server: "ldap://ldap.office.local:389"
    base_dn: "dc=office,dc=local"
    bind_dn: "cn=monsoon,ou=services,dc=office,dc=local"
    bind_password: ""
    user_filter: "(uid={{.Username}})"
    group_filter: "(memberUid={{.Username}})"
    admin_group: "cn=netadmins,ou=groups,dc=office,dc=local"
    
  api_tokens:
    enabled: true               # API key authentication for REST/gRPC
    
  session:
    duration: "24h"
    cookie_name: "monsoon_session"
    secure: true

# High Availability
ha:
  enabled: false
  mode: "active-passive"        # current build effectively targets basic active-passive
  peer_address: "10.0.1.6:8068"
  heartbeat_interval: "1s"
  failover_timeout: "10s"
  lease_sync: true              # Synchronize lease database with peer
  shared_secret: ""             # Peer authentication

# Metrics & Monitoring
metrics:
  prometheus:
    enabled: true
    path: "/metrics"            # On REST API port

# Webhooks
webhooks: []

# Backup
backup:
  auto:
    enabled: true
    interval: "6h"
    retention: 7                # Keep last 7 backups
    path: "/var/lib/monsoon/backups"
```

### 8.2 CLI Flags

```
monsoon [flags]

Flags:
  -c, --config string    Configuration file path (default "/etc/monsoon/monsoon.yaml")
  -d, --data-dir string  Data directory (default "/var/lib/monsoon")
  -v, --version          Print version and exit
      --init             Initialize configuration file
      --check-config     Validate configuration and exit
      --export-config    Export current configuration to stdout
      --backup           Create backup and exit
      --restore string   Restore from backup file
      --migrate          Run data migrations
      --debug            Enable debug logging
```

### 8.3 Environment Variables

All configuration values can be overridden via environment variables with `MONSOON_` prefix:

```bash
MONSOON_SERVER_HOSTNAME=monsoon-01
MONSOON_DHCP_V4_ENABLED=true
MONSOON_DHCP_V4_LISTEN=0.0.0.0:67
MONSOON_API_REST_LISTEN=:8067
MONSOON_AUTH_ENABLED=true
MONSOON_LOG_LEVEL=debug
```

---

## 9. High Availability & Failover

### 9.1 Active-Passive Failover

```
┌─────────────┐         ┌─────────────┐
│  Primary     │◄───────►│  Secondary  │
│  (Active)    │  Lease  │  (Standby)  │
│              │  Sync   │             │
│  Serves DHCP │         │  Monitors   │
│  Serves API  │         │  primary    │
│  Serves UI   │         │  heartbeat  │
└──────┬───────┘         └──────┬──────┘
       │     VIP: 10.0.1.5     │
       │◄──────────────────────►│
       │  (floats to active)    │
```

### 9.2 Lease Synchronization

- Primary streams all lease mutations to secondary via TCP
- Binary protocol with sequence numbers
- Secondary maintains identical lease database
- On failover, secondary takes over with zero lease loss
- Split-brain protection via MCLAG-style fencing or shared witness

### 9.3 Load-Sharing Mode (DHCPv4)

- Both servers active, each serving half the pool
- Uses `split-scope` approach: Primary serves .10-.128, Secondary serves .129-.250
- Leases synchronized bidirectionally
- Either server can serve any lease after failover grace period

---

## 10. Security

### 10.1 Authentication & Authorization

- **Web Dashboard**: Session-based auth with secure cookies
- **REST API**: Bearer token (API keys) or session cookie
- **gRPC**: mTLS or token metadata
- **RBAC Roles**:
  - `admin`: Full access
  - `operator`: Manage subnets, leases, reservations (no system settings)
  - `viewer`: Read-only access
  - `api`: Programmatic access (scoped by subnet)

### 10.2 DHCP Security

- **Rogue Server Detection**: Passive monitoring for unauthorized DHCP responses
- **MAC Filtering**: Optional allow/deny lists per subnet
- **Rate Limiting**: Per-MAC request rate limiting to prevent DHCP starvation attacks
- **Lease Limit**: Maximum leases per MAC address

### 10.3 Network Security

- **TLS**: HTTPS for dashboard and REST API (auto-generated self-signed or custom cert)
- **ACME**: Optional Let's Encrypt for public-facing instances
- **Interface Binding**: DHCP listens only on configured interfaces
- **API Rate Limiting**: Configurable per-client rate limits

---

## 11. Observability

### 11.1 Prometheus Metrics

```
# DHCP metrics
monsoon_dhcp_requests_total{type="discover|offer|request|ack|nak|release|decline"}
monsoon_dhcp_leases_active{subnet="10.0.1.0/24"}
monsoon_dhcp_leases_total{subnet="10.0.1.0/24",state="bound|expired|released"}
monsoon_dhcp_response_duration_seconds{type="discover|request"}
monsoon_dhcp_pool_utilization{subnet="10.0.1.0/24"}
monsoon_dhcp_pool_available{subnet="10.0.1.0/24"}

# IPAM metrics
monsoon_ipam_addresses_total{subnet="10.0.1.0/24",state="available|dhcp|reserved|static|conflict"}
monsoon_ipam_subnets_total
monsoon_ipam_discovery_scans_total{subnet="10.0.1.0/24"}
monsoon_ipam_conflicts_total

# System metrics
monsoon_storage_size_bytes
monsoon_storage_operations_total{type="read|write"}
monsoon_api_requests_total{method="GET|POST|PUT|DELETE",path="/api/v1/..."}
monsoon_api_response_duration_seconds
monsoon_websocket_connections_active
monsoon_ha_heartbeat_latency_seconds
monsoon_ha_lease_sync_lag_seconds
```

### 11.2 Structured Logging

```json
{
  "time": "2025-04-10T14:30:00Z",
  "level": "info",
  "msg": "DHCP lease created",
  "component": "dhcpv4",
  "ip": "10.0.1.50",
  "mac": "AA:BB:CC:DD:EE:FF",
  "hostname": "laptop-03",
  "subnet": "10.0.1.0/24",
  "lease_duration": "8h0m0s",
  "relay": "10.0.1.1",
  "latency_us": 245
}
```

### 11.3 Health Check

```json
GET /api/v1/system/health

{
  "status": "healthy",
  "components": {
    "dhcpv4": {"status": "running", "leases_active": 1847},
    "dhcpv6": {"status": "disabled"},
    "storage": {"status": "healthy", "size_mb": 124},
    "api": {"status": "running", "uptime": "7d12h"},
    "discovery": {"status": "running", "last_scan": "2025-04-10T14:00:00Z"},
    "ha": {"status": "primary", "peer": "connected", "sync_lag": "0ms"}
  },
  "version": "1.0.0",
  "uptime": "7d12h30m"
}
```

---

## 12. Deployment

### 12.1 Quick Start

```bash
# Download
curl -fsSL https://monsoondhcp.com/install.sh | sh

# Initialize (creates config, sets admin password)
monsoon --init

# Run
monsoon -c /etc/monsoon/monsoon.yaml
```

### 12.2 Systemd Service

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
RestartSec=5
LimitNOFILE=65536
AmbientCapabilities=CAP_NET_BIND_SERVICE CAP_NET_RAW
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/var/lib/monsoon

[Install]
WantedBy=multi-user.target
```

### 12.3 Docker

```dockerfile
FROM scratch
COPY monsoon /monsoon
EXPOSE 67/udp 547/udp 8067 9067 7067
VOLUME /var/lib/monsoon
ENTRYPOINT ["/monsoon"]
```

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

> **Note**: `--net=host` required for DHCP broadcast packet handling.

### 12.4 Platform Support

| Platform | Architecture | DHCP | IPAM | Web UI |
|----------|-------------|------|------|--------|
| Linux | amd64, arm64 | ✅ | ✅ | ✅ |
| macOS | amd64, arm64 | ⚠️ (limited, no raw socket) | ✅ | ✅ |
| Windows | amd64 | ⚠️ (limited) | ✅ | ✅ |
| FreeBSD | amd64 | ✅ | ✅ | ✅ |

> DHCP server functionality requires raw socket access (Linux/FreeBSD primary targets). macOS/Windows can run IPAM-only mode with external DHCP lease import.

---

## 13. Migration & Import

### 13.1 Import Sources

| Source | Format | What's Imported |
|--------|--------|-----------------|
| ISC DHCP | dhcpd.conf + dhcpd.leases | Subnets, pools, reservations, active leases |
| Kea | kea-dhcp4.conf + lease DB | Subnets, pools, reservations, leases |
| phpIPAM | MySQL dump or REST API export | Subnets, addresses, VLANs |
| NetBox | REST API export | Prefixes, addresses, VLANs |
| CSV | Generic CSV | Any: subnets, IPs, reservations, VLANs |

### 13.2 Migration CLI

```bash
# Import from ISC DHCP
monsoon migrate --from isc-dhcp \
  --config /etc/dhcp/dhcpd.conf \
  --leases /var/lib/dhcp/dhcpd.leases

# Import from phpIPAM
monsoon migrate --from phpipam \
  --api-url http://phpipam.local/api \
  --api-token "xxx"

# Import from CSV
monsoon migrate --from csv \
  --subnets subnets.csv \
  --addresses addresses.csv \
  --reservations reservations.csv
```

---

## 14. Competitive Comparison

| Feature | Monsoon | ISC DHCP | Kea | phpIPAM | NetBox |
|---------|---------|----------|-----|---------|--------|
| DHCP Server | ✅ | ✅ | ✅ | ❌ | ❌ |
| IPAM | ✅ | ❌ | ❌ | ✅ | ✅ |
| Unified DHCP+IPAM | ✅ | ❌ | ❌ | ❌ | ❌ |
| Single Binary | ✅ | ✅ | ❌ (multi-process) | ❌ (PHP+MySQL) | ❌ (Python+PostgreSQL) |
| Zero Dependencies | ✅ | ✅ | ❌ (PostgreSQL/MySQL) | ❌ | ❌ |
| Web Dashboard | ✅ (embedded) | ❌ | ⚠️ (Stork, separate) | ✅ | ✅ |
| REST API | ✅ | ❌ | ✅ | ✅ | ✅ |
| gRPC API | ✅ | ❌ | ❌ | ❌ | ❌ |
| WebSocket Events | ✅ | ❌ | ❌ | ❌ | ❌ |
| MCP Server | ✅ | ❌ | ❌ | ❌ | ❌ |
| Network Discovery | ✅ | ❌ | ❌ | ✅ | ❌ |
| DHCPv6 | ✅ | ✅ | ✅ | ❌ | ❌ |
| HA/Failover | ✅ | ⚠️ (limited) | ✅ | ❌ | ❌ |
| Visual IP Map | ✅ | ❌ | ❌ | ✅ | ❌ |
| Audit Trail | ✅ | ❌ | ✅ | ✅ | ✅ |
| Hot Config Reload | ✅ | ❌ | ✅ | N/A | N/A |

---

## 15. Non-Goals (v1)

These are explicitly out of scope for initial release:

- **RADIUS/802.1X**: Not a NAC solution
- **DNS Server**: Use NothingDNS for authoritative DNS
- **Network Configuration Management**: Not Ansible/Nornir
- **Full DCIM**: Not a data center inventory (use NetBox for that)
- **Multi-Tenant SaaS**: Self-hosted only
- **SNMP-based switch management**: Read-only SNMP walk for discovery only
