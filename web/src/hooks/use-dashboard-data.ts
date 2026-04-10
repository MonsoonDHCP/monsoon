import { useCallback, useEffect, useMemo, useState } from "react"

import {
  bootstrapAuth,
  createAuthToken,
  convertLeaseToReservation,
  fetchAuditEntries,
  fetchAuthTokens,
  deleteReservation,
  fetchAddresses,
  fetchCurrentUser,
  deleteSubnet,
  fetchDiscoveryStatus,
  fetchHealth,
  fetchLeases,
  fetchReservations,
  fetchRawSubnets,
  fetchSubnets,
  fetchUISettings,
  login,
  logout,
  releaseLease,
  revokeAuthToken,
  triggerDiscoveryScan,
  updateUISettings,
  upsertReservation,
  upsertSubnet,
} from "@/lib/api"
import type {
  AuthIdentity,
  AuthToken,
  AuditEntry,
  AddressRecord,
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

type DashboardState = {
  health: HealthResponse | null
  leases: Lease[]
  subnets: SubnetSummary[]
  subnetRecords: Subnet[]
  addresses: AddressRecord[]
  reservations: Reservation[]
  discovery: DiscoveryStatus | null
  auditEntries: AuditEntry[]
  settings: UISettings | null
  currentUser: AuthIdentity | null
  authTokens: AuthToken[]
  tokenSecret: string | null
  loading: boolean
  error: string | null
  reload: () => Promise<void>
  release: (ip: string) => Promise<void>
  reserveLease: (ip: string) => Promise<void>
  triggerScan: () => Promise<void>
  saveSettings: (settings: UISettings) => Promise<void>
  saveSubnet: (payload: UpsertSubnetPayload) => Promise<void>
  removeSubnet: (cidr: string) => Promise<void>
  saveReservation: (payload: UpsertReservationPayload) => Promise<void>
  removeReservation: (mac: string) => Promise<void>
  loadAddressesForSubnet: (subnetCIDR?: string) => Promise<AddressRecord[]>
  loginWithPassword: (username: string, password: string) => Promise<void>
  bootstrapAndLogin: (username: string, password: string) => Promise<void>
  logoutCurrentUser: () => Promise<void>
  createToken: (payload: { name: string; role: string; expires_in_hours?: number; description?: string }) => Promise<void>
  revokeToken: (id: string) => Promise<void>
}

export function useDashboardData(): DashboardState {
  const [health, setHealth] = useState<HealthResponse | null>(null)
  const [leases, setLeases] = useState<Lease[]>([])
  const [subnets, setSubnets] = useState<SubnetSummary[]>([])
  const [subnetRecords, setSubnetRecords] = useState<Subnet[]>([])
  const [addresses, setAddresses] = useState<AddressRecord[]>([])
  const [reservations, setReservations] = useState<Reservation[]>([])
  const [discovery, setDiscovery] = useState<DiscoveryStatus | null>(null)
  const [auditEntries, setAuditEntries] = useState<AuditEntry[]>([])
  const [settings, setSettings] = useState<UISettings | null>(null)
  const [currentUser, setCurrentUser] = useState<AuthIdentity | null>(null)
  const [authTokens, setAuthTokens] = useState<AuthToken[]>([])
  const [tokenSecret, setTokenSecret] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      setError(null)
      const [healthData, leaseData, subnetData, subnetRawData, addressData, reservationData, discoveryData, settingsData, auditData] = await Promise.all([
        fetchHealth(),
        fetchLeases(),
        fetchSubnets(),
        fetchRawSubnets(),
        fetchAddresses(),
        fetchReservations(),
        fetchDiscoveryStatus(),
        fetchUISettings(),
        fetchAuditEntries({ limit: 200 }),
      ])
      setHealth(healthData)
      setLeases(leaseData)
      setSubnets(subnetData)
      setSubnetRecords(subnetRawData)
      setAddresses(addressData)
      setReservations(reservationData)
      setDiscovery(discoveryData)
      setSettings(settingsData)
      setAuditEntries(auditData)

      try {
        const me = await fetchCurrentUser()
        setCurrentUser(me)
        if (me.role === "admin") {
          const tokens = await fetchAuthTokens()
          setAuthTokens(tokens)
        } else {
          setAuthTokens([])
        }
      } catch {
        setCurrentUser(null)
        setAuthTokens([])
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : "Unknown error"
      setError(message)
    } finally {
      setLoading(false)
    }
  }, [])

  const release = useCallback(
    async (ip: string) => {
      await releaseLease(ip)
      await load()
    },
    [load],
  )

  const triggerScan = useCallback(async () => {
    await triggerDiscoveryScan()
    await load()
  }, [load])

  const reserveLease = useCallback(
    async (ip: string) => {
      await convertLeaseToReservation(ip)
      await load()
    },
    [load],
  )

  const saveSettings = useCallback(
    async (next: UISettings) => {
      const saved = await updateUISettings(next)
      setSettings(saved)
    },
    [setSettings],
  )

  const saveSubnet = useCallback(
    async (payload: UpsertSubnetPayload) => {
      await upsertSubnet(payload)
      await load()
    },
    [load],
  )

  const removeSubnet = useCallback(
    async (cidr: string) => {
      await deleteSubnet(cidr)
      await load()
    },
    [load],
  )

  const saveReservation = useCallback(
    async (payload: UpsertReservationPayload) => {
      await upsertReservation(payload)
      await load()
    },
    [load],
  )

  const removeReservation = useCallback(
    async (mac: string) => {
      await deleteReservation(mac)
      await load()
    },
    [load],
  )

  const loadAddressesForSubnet = useCallback(async (subnetCIDR?: string) => {
    const rows = await fetchAddresses(subnetCIDR)
    setAddresses(rows)
    return rows
  }, [])

  const loginWithPassword = useCallback(
    async (username: string, password: string) => {
      await login(username, password)
      await load()
    },
    [load],
  )

  const bootstrapAndLogin = useCallback(
    async (username: string, password: string) => {
      await bootstrapAuth(username, password)
      await login(username, password)
      await load()
    },
    [load],
  )

  const logoutCurrentUser = useCallback(async () => {
    await logout()
    setCurrentUser(null)
    setAuthTokens([])
    setTokenSecret(null)
  }, [])

  const createToken = useCallback(
    async (payload: { name: string; role: string; expires_in_hours?: number; description?: string }) => {
      const result = await createAuthToken(payload)
      setTokenSecret(result.secret)
      await load()
    },
    [load],
  )

  const revokeToken = useCallback(
    async (id: string) => {
      await revokeAuthToken(id)
      await load()
    },
    [load],
  )

  useEffect(() => {
    let timer: ReturnType<typeof setInterval> | null = null
    void load()
    if (settings?.auto_refresh ?? true) {
      timer = setInterval(() => {
        void load()
      }, 15000)
    }
    return () => {
      if (timer) {
        clearInterval(timer)
      }
    }
  }, [load, settings?.auto_refresh])

  useEffect(() => {
    const source = new EventSource("/api/v1/events")
    const refresh = () => {
      void load()
    }
    source.addEventListener("lease.released", refresh)
    source.addEventListener("subnet.upserted", refresh)
    source.addEventListener("subnet.deleted", refresh)
    source.addEventListener("reservation.upserted", refresh)
    source.addEventListener("reservation.deleted", refresh)
    source.addEventListener("discovery.scan_queued", refresh)
    source.addEventListener("settings.ui_updated", refresh)
    return () => {
      source.removeEventListener("lease.released", refresh)
      source.removeEventListener("subnet.upserted", refresh)
      source.removeEventListener("subnet.deleted", refresh)
      source.removeEventListener("reservation.upserted", refresh)
      source.removeEventListener("reservation.deleted", refresh)
      source.removeEventListener("discovery.scan_queued", refresh)
      source.removeEventListener("settings.ui_updated", refresh)
      source.close()
    }
  }, [load])

  return useMemo(
    () => ({
      health,
      leases,
      subnets,
      subnetRecords,
      addresses,
      reservations,
      discovery,
      auditEntries,
      settings,
      currentUser,
      authTokens,
      tokenSecret,
      loading,
      error,
      reload: load,
      release,
      reserveLease,
      triggerScan,
      saveSettings,
      saveSubnet,
      removeSubnet,
      saveReservation,
      removeReservation,
      loadAddressesForSubnet,
      loginWithPassword,
      bootstrapAndLogin,
      logoutCurrentUser,
      createToken,
      revokeToken,
    }),
    [
      health,
      leases,
      subnets,
      subnetRecords,
      addresses,
      reservations,
      discovery,
      auditEntries,
      settings,
      currentUser,
      authTokens,
      tokenSecret,
      loading,
      error,
      load,
      release,
      reserveLease,
      triggerScan,
      saveSettings,
      saveSubnet,
      removeSubnet,
      saveReservation,
      removeReservation,
      loadAddressesForSubnet,
      loginWithPassword,
      bootstrapAndLogin,
      logoutCurrentUser,
      createToken,
      revokeToken,
    ],
  )
}
