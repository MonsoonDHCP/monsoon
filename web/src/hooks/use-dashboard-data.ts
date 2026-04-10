import { useCallback, useEffect, useMemo, useState } from "react"

import {
  isApiError,
  bootstrapAuth,
  createAuthToken,
  convertLeaseToReservation,
  fetchAuditEntries,
  fetchAuthTokens,
  deleteReservation,
  fetchAddresses,
  fetchCurrentUser,
  deleteSubnet,
  fetchDiscoveryConflicts,
  fetchDiscoveryProgress,
  fetchDiscoveryResults,
  fetchDiscoveryRogueServers,
  fetchDiscoveryStatus,
  fetchHealth,
  fetchSystemBackups,
  fetchSystemInfo,
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
  createSystemBackup,
} from "@/lib/api"
import type {
  AuthIdentity,
  AuthToken,
  AuditEntry,
  AddressRecord,
  DiscoveryStatus,
  DiscoveryConflict,
  DiscoveryProgress,
  DiscoveryResult,
  RogueServer,
  HealthResponse,
  Lease,
  Reservation,
  Subnet,
  SubnetSummary,
  SystemBackup,
  SystemInfo,
  UISettings,
  UpsertReservationPayload,
  UpsertSubnetPayload,
} from "@/types/api"

type DashboardState = {
  health: HealthResponse | null
  systemInfo: SystemInfo | null
  backups: SystemBackup[]
  leases: Lease[]
  subnets: SubnetSummary[]
  subnetRecords: Subnet[]
  addresses: AddressRecord[]
  reservations: Reservation[]
  discovery: DiscoveryStatus | null
  discoveryResults: DiscoveryResult[]
  discoveryProgress: DiscoveryProgress | null
  discoveryConflicts: DiscoveryConflict[]
  rogueServers: RogueServer[]
  auditEntries: AuditEntry[]
  settings: UISettings | null
  currentUser: AuthIdentity | null
  authTokens: AuthToken[]
  tokenSecret: string | null
  authRequired: boolean
  canMutate: boolean
  isAdmin: boolean
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
  createBackup: () => Promise<void>
  refreshBackups: () => Promise<void>
}

export function useDashboardData(): DashboardState {
  const [health, setHealth] = useState<HealthResponse | null>(null)
  const [systemInfo, setSystemInfo] = useState<SystemInfo | null>(null)
  const [backups, setBackups] = useState<SystemBackup[]>([])
  const [leases, setLeases] = useState<Lease[]>([])
  const [subnets, setSubnets] = useState<SubnetSummary[]>([])
  const [subnetRecords, setSubnetRecords] = useState<Subnet[]>([])
  const [addresses, setAddresses] = useState<AddressRecord[]>([])
  const [reservations, setReservations] = useState<Reservation[]>([])
  const [discovery, setDiscovery] = useState<DiscoveryStatus | null>(null)
  const [discoveryResults, setDiscoveryResults] = useState<DiscoveryResult[]>([])
  const [discoveryProgress, setDiscoveryProgress] = useState<DiscoveryProgress | null>(null)
  const [discoveryConflicts, setDiscoveryConflicts] = useState<DiscoveryConflict[]>([])
  const [rogueServers, setRogueServers] = useState<RogueServer[]>([])
  const [auditEntries, setAuditEntries] = useState<AuditEntry[]>([])
  const [settings, setSettings] = useState<UISettings | null>(null)
  const [currentUser, setCurrentUser] = useState<AuthIdentity | null>(null)
  const [authTokens, setAuthTokens] = useState<AuthToken[]>([])
  const [tokenSecret, setTokenSecret] = useState<string | null>(null)
  const [authRequired, setAuthRequired] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)
      const healthData = await fetchHealth()
      setHealth(healthData)

      const [systemInfoData, backupsData, leaseData, subnetData, subnetRawData, addressData, reservationData, discoveryData, discoveryProgressData, discoveryResultsData, discoveryConflictsData, rogueServersData, settingsData, auditData] = await Promise.all([
        fetchSystemInfo(),
        fetchSystemBackups(),
        fetchLeases(),
        fetchSubnets(),
        fetchRawSubnets(),
        fetchAddresses(),
        fetchReservations(),
        fetchDiscoveryStatus(),
        fetchDiscoveryProgress(),
        fetchDiscoveryResults(30),
        fetchDiscoveryConflicts(),
        fetchDiscoveryRogueServers(),
        fetchUISettings(),
        fetchAuditEntries({ limit: 200 }),
      ])
      setLeases(leaseData)
      setSystemInfo(systemInfoData)
      setBackups(backupsData)
      setSubnets(subnetData)
      setSubnetRecords(subnetRawData)
      setAddresses(addressData)
      setReservations(reservationData)
      setDiscovery(discoveryData)
      setDiscoveryProgress(discoveryProgressData)
      setDiscoveryResults(discoveryResultsData)
      setDiscoveryConflicts(discoveryConflictsData)
      setRogueServers(rogueServersData)
      setSettings(settingsData)
      setAuditEntries(auditData)
      setAuthRequired(false)

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
      if (isApiError(err) && err.status === 401) {
        setAuthRequired(true)
        setCurrentUser(null)
        setAuthTokens([])
        setLeases([])
        setSystemInfo(null)
        setBackups([])
        setSubnets([])
        setSubnetRecords([])
        setAddresses([])
        setReservations([])
        setDiscovery(null)
        setDiscoveryProgress(null)
        setDiscoveryResults([])
        setDiscoveryConflicts([])
        setRogueServers([])
        setAuditEntries([])
        setSettings(null)
        setError("Authentication required. Please sign in.")
      } else {
        const message = err instanceof Error ? err.message : "Unknown error"
        setError(message)
      }
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
      setError(null)
      try {
        await login(username, password)
        await load()
      } catch (err) {
        const message = err instanceof Error ? err.message : "Login failed"
        setError(message)
        throw err
      }
    },
    [load],
  )

  const bootstrapAndLogin = useCallback(
    async (username: string, password: string) => {
      setError(null)
      try {
        await bootstrapAuth(username, password)
        await login(username, password)
        await load()
      } catch (err) {
        const message = err instanceof Error ? err.message : "Bootstrap failed"
        setError(message)
        throw err
      }
    },
    [load],
  )

  const logoutCurrentUser = useCallback(async () => {
    await logout()
    await load()
    setTokenSecret(null)
  }, [load])

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

  const refreshBackups = useCallback(async () => {
    const rows = await fetchSystemBackups()
    setBackups(rows)
  }, [])

  const createBackup = useCallback(async () => {
    await createSystemBackup()
    await refreshBackups()
  }, [refreshBackups])

  useEffect(() => {
    let timer: ReturnType<typeof setInterval> | null = null
    void load()
    if (!authRequired && (settings?.auto_refresh ?? true)) {
      timer = setInterval(() => {
        void load()
      }, 15000)
    }
    return () => {
      if (timer) {
        clearInterval(timer)
      }
    }
  }, [authRequired, load, settings?.auto_refresh])

  useEffect(() => {
    if (authRequired) {
      return
    }
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
    source.addEventListener("discovery.scan_completed", refresh)
    source.addEventListener("settings.ui_updated", refresh)
    return () => {
      source.removeEventListener("lease.released", refresh)
      source.removeEventListener("subnet.upserted", refresh)
      source.removeEventListener("subnet.deleted", refresh)
      source.removeEventListener("reservation.upserted", refresh)
      source.removeEventListener("reservation.deleted", refresh)
      source.removeEventListener("discovery.scan_queued", refresh)
      source.removeEventListener("discovery.scan_completed", refresh)
      source.removeEventListener("settings.ui_updated", refresh)
      source.close()
    }
  }, [authRequired, load])

  return useMemo(
    () => {
      const role = currentUser?.role ?? ""
      const isAdmin = role === "admin"
      const canMutate = !authRequired || role === "admin" || role === "operator"

      return {
        health,
        systemInfo,
        backups,
        leases,
        subnets,
        subnetRecords,
        addresses,
        reservations,
        discovery,
        discoveryResults,
        discoveryProgress,
        discoveryConflicts,
        rogueServers,
        auditEntries,
        settings,
        currentUser,
        authTokens,
        tokenSecret,
        authRequired,
        canMutate,
        isAdmin,
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
        createBackup,
        refreshBackups,
      }
    },
    [
      health,
      systemInfo,
      backups,
      leases,
      subnets,
      subnetRecords,
      addresses,
      reservations,
      discovery,
      discoveryResults,
      discoveryProgress,
      discoveryConflicts,
      rogueServers,
      auditEntries,
      settings,
      currentUser,
      authTokens,
      tokenSecret,
      authRequired,
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
      createBackup,
      refreshBackups,
    ],
  )
}
