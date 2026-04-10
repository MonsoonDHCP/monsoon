import type {
  AuditEntry,
  AuthIdentity,
  AuthToken,
  AddressRecord,
  ApiEnvelope,
  DiscoveryConflict,
  DiscoveryProgress,
  DiscoveryResult,
  RogueServer,
  DiscoveryScanResponse,
  DiscoveryStatus,
  HealthResponse,
  Lease,
  Reservation,
  Subnet,
  SubnetSummary,
  UISettings,
  UpsertReservationPayload,
  UpsertSubnetPayload,
} from "@/types/api"

const JSON_HEADERS = {
  "Content-Type": "application/json",
}

export class ApiError extends Error {
  status: number
  code?: string

  constructor(message: string, status: number, code?: string) {
    super(message)
    this.name = "ApiError"
    this.status = status
    this.code = code
  }
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    credentials: "include",
    ...init,
    headers: {
      ...JSON_HEADERS,
      ...(init?.headers ?? {}),
    },
  })

  const contentType = response.headers.get("content-type") ?? ""
  const isJSON = contentType.includes("application/json")
  const body = isJSON ? ((await response.json()) as ApiEnvelope<T>) : undefined

  if (!response.ok || body?.error) {
    const message = body?.error?.message ?? `HTTP ${response.status}`
    throw new ApiError(message, response.status, body?.error?.code)
  }

  return body?.data as T
}

export function fetchHealth() {
  return request<HealthResponse>("/api/v1/system/health")
}

export function fetchLeases() {
  return request<Lease[]>("/api/v1/leases")
}

export function releaseLease(ip: string) {
  return request<{ status: string; ip: string }>(`/api/v1/leases/${ip}/release`, {
    method: "POST",
  })
}

export function convertLeaseToReservation(ip: string) {
  return request<Reservation>(`/api/v1/leases/${ip}/reservation`, {
    method: "POST",
  })
}

export function fetchSubnets() {
  return request<SubnetSummary[]>("/api/v1/subnets")
}

export function fetchRawSubnets() {
  return request<Subnet[]>("/api/v1/subnets/raw")
}

export function upsertSubnet(payload: UpsertSubnetPayload) {
  return request<Subnet>("/api/v1/subnets", {
    method: "POST",
    body: JSON.stringify(payload),
  })
}

export function deleteSubnet(cidr: string) {
  const query = new URLSearchParams({ cidr })
  return request<{ status: string; cidr: string }>(`/api/v1/subnets?${query.toString()}`, {
    method: "DELETE",
  })
}

export function fetchDiscoveryStatus() {
  return request<DiscoveryStatus>("/api/v1/discovery/status")
}

export function triggerDiscoveryScan() {
  return request<DiscoveryScanResponse>("/api/v1/discovery/scan", {
    method: "POST",
  })
}

export function fetchDiscoveryResults(limit = 20) {
  return request<DiscoveryResult[]>(`/api/v1/discovery/results?limit=${limit}`)
}

export function fetchDiscoveryConflicts() {
  return request<DiscoveryConflict[]>("/api/v1/discovery/conflicts")
}

export function fetchDiscoveryRogueServers() {
  return request<RogueServer[]>("/api/v1/discovery/rogue")
}

export function fetchDiscoveryProgress() {
  return request<DiscoveryProgress>("/api/v1/discovery/progress")
}

export function fetchReservations() {
  return request<Reservation[]>("/api/v1/reservations")
}

export function upsertReservation(payload: UpsertReservationPayload) {
  return request<Reservation>("/api/v1/reservations", {
    method: "POST",
    body: JSON.stringify(payload),
  })
}

export function deleteReservation(mac: string) {
  const query = new URLSearchParams({ mac })
  return request<{ status: string; mac: string }>(`/api/v1/reservations?${query.toString()}`, {
    method: "DELETE",
  })
}

export function fetchAddresses(subnet?: string) {
  const query = new URLSearchParams()
  if (subnet) {
    query.set("subnet", subnet)
  }
  const suffix = query.toString()
  return request<AddressRecord[]>(`/api/v1/addresses${suffix ? `?${suffix}` : ""}`)
}

export function fetchUISettings() {
  return request<UISettings>("/api/v1/settings/ui")
}

export function updateUISettings(payload: UISettings) {
  return request<UISettings>("/api/v1/settings/ui", {
    method: "PUT",
    body: JSON.stringify(payload),
  })
}

export function bootstrapAuth(username: string, password: string) {
  return request<{ status: string; username: string }>("/api/v1/auth/bootstrap", {
    method: "POST",
    body: JSON.stringify({ username, password }),
  })
}

export function login(username: string, password: string) {
  return request<AuthIdentity>("/api/v1/auth/login", {
    method: "POST",
    body: JSON.stringify({ username, password }),
  })
}

export function logout() {
  return request<{ status: string }>("/api/v1/auth/logout", {
    method: "POST",
  })
}

export function fetchCurrentUser() {
  return request<AuthIdentity>("/api/v1/auth/me")
}

export function fetchAuthTokens() {
  return request<AuthToken[]>("/api/v1/auth/tokens")
}

export function fetchAuditEntries(params?: { q?: string; actor?: string; action?: string; object_type?: string; limit?: number }) {
  const query = new URLSearchParams()
  if (params?.q) query.set("q", params.q)
  if (params?.actor) query.set("actor", params.actor)
  if (params?.action) query.set("action", params.action)
  if (params?.object_type) query.set("object_type", params.object_type)
  if (params?.limit) query.set("limit", String(params.limit))
  const suffix = query.toString()
  return request<AuditEntry[]>(`/api/v1/audit${suffix ? `?${suffix}` : ""}`)
}

export function createAuthToken(payload: { name: string; role: string; expires_in_hours?: number; description?: string }) {
  return request<{ token: AuthToken; secret: string }>("/api/v1/auth/tokens", {
    method: "POST",
    body: JSON.stringify(payload),
  })
}

export function revokeAuthToken(id: string) {
  return request<{ status: string; id: string }>(`/api/v1/auth/tokens/${encodeURIComponent(id)}`, {
    method: "DELETE",
  })
}
