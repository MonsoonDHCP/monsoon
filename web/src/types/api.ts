export type HealthResponse = {
  status: string
  version: string
  components?: {
    dhcpv4?: {
      enabled?: boolean
      listen?: string
      running?: boolean
    }
  }
}

export type Lease = {
  ip: string
  mac: string
  hostname?: string
  state: string
  start_time?: string
  expiry_time?: string
  subnet_id?: string
}

export type SubnetSummary = {
  cidr: string
  name: string
  vlan: number
  active_leases: number
  total_leases: number
  utilization: number
}

export type Subnet = {
  cidr: string
  name: string
  vlan: number
  gateway: string
  dns: string[]
  dhcp: {
    enabled: boolean
    pool_start: string
    pool_end: string
    lease_time_sec: number
  }
  created_at?: string
  updated_at?: string
}

export type UpsertSubnetPayload = {
  cidr: string
  name: string
  vlan: number
  gateway: string
  dns: string[]
  dhcp_enabled: boolean
  pool_start: string
  pool_end: string
  lease_time_sec: number
}

export type DiscoveryStatus = {
  sensor_online: boolean
  last_scan_at: string
  rogue_detected: boolean
  active_conflicts: number
  next_scheduled_scan: string
  scanning?: boolean
  latest_scan_id?: string
}

export type DiscoveryScanResponse = {
  status: string
  scan_id: string
  estimated_in: string
}

export type DiscoveryConflict = {
  ip: string
  macs: string[]
  severity: string
  note?: string
}

export type RogueServer = {
  ip: string
  mac?: string
  source?: string
  detected: string
}

export type DiscoveryHost = {
  ip: string
  mac?: string
  hostname?: string
  subnet?: string
  state: string
  seen_at: string
}

export type DiscoveryResult = {
  scan_id: string
  status: string
  reason?: string
  subnets?: string[]
  started_at: string
  completed_at?: string
  duration_ms: number
  total_hosts: number
  new_hosts: number
  known_hosts: number
  missing_hosts: number
  changed_hosts: number
  conflicts: DiscoveryConflict[]
  rogue_servers: RogueServer[]
  hosts?: DiscoveryHost[]
}

export type Reservation = {
  mac: string
  ip: string
  hostname?: string
  subnet_cidr: string
  created_at?: string
  updated_at?: string
}

export type UpsertReservationPayload = {
  mac: string
  ip: string
  hostname: string
  subnet_cidr?: string
}

export type AddressState = "available" | "dhcp" | "reserved" | "conflict" | "quarantined"

export type AddressRecord = {
  ip: string
  subnet_cidr?: string
  state: AddressState
  mac?: string
  hostname?: string
  lease_state?: string
  source?: string
  updated_at?: string
}

export type UISettings = {
  theme: "light" | "dark" | "system"
  density: "compact" | "comfortable"
  auto_refresh: boolean
}

export type AuthIdentity = {
  username: string
  role: "admin" | "operator" | "viewer" | string
  auth_type: "session" | "token" | string
  token_id?: string
}

export type AuthToken = {
  id: string
  name: string
  role: string
  prefix: string
  created_at: string
  expires_at?: string
  last_used_at?: string
  description?: string
}

export type AuditEntry = {
  id: string
  timestamp: string
  actor: string
  action: string
  object_type: string
  object_id: string
  source: string
  before?: Record<string, unknown>
  after?: Record<string, unknown>
  meta?: Record<string, unknown>
}

export type ApiEnvelope<T> = {
  data?: T
  meta?: Record<string, unknown>
  error?: {
    code: string
    message: string
  }
}
