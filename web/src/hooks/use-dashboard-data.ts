import { useQueryClient } from "@tanstack/react-query"
import { useCallback, useEffect, useMemo, useState } from "react"
import { toast } from "sonner"

import {
  isApiError,
  bootstrapAuth,
  createAuthToken,
  convertLeaseToReservation,
  createSystemBackup,
  deleteReservation,
  deleteSubnet,
  login,
  logout,
  releaseLease,
  restoreSystemBackup,
  revokeAuthToken,
  triggerDiscoveryScan,
  triggerHAFailover,
  updateSystemConfig,
  updateUISettings,
  upsertReservation,
  upsertSubnet,
} from "@/lib/api"
import { connectLiveSocket, type LiveEvent } from "@/lib/ws"
import {
  dashboardQueryKeys,
  invalidateDashboardQueries,
  useAuditEntriesQuery,
  useAuthTokensQuery,
  useBackupsQuery,
  useCurrentUserQuery,
  useDiscoveryConflictsQuery,
  useDiscoveryProgressQuery,
  useDiscoveryResultsQuery,
  useDiscoveryStatusQuery,
  useHealthQuery,
  useLeasesQuery,
  useRawSubnetsQuery,
  useReservationsQuery,
  useRogueServersQuery,
  useSubnetsQuery,
  useSystemConfigQuery,
  useSystemInfoQuery,
  useUISettingsQuery,
} from "@/hooks/use-dashboard-queries"
import { useDashboardUIStore, type DashboardNotification } from "@/stores/dashboard-ui-store"
import type {
  AuditEntry,
  AuthIdentity,
  AuthToken,
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
  SystemConfig,
  SystemInfo,
  UISettings,
  UpsertReservationPayload,
  UpsertSubnetPayload,
} from "@/types/api"

type DashboardState = {
  health: HealthResponse | null
  systemInfo: SystemInfo | null
  systemConfig: SystemConfig | null
  backups: SystemBackup[]
  leases: Lease[]
  subnets: SubnetSummary[]
  subnetRecords: Subnet[]
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
  liveConnection: "websocket" | "sse" | "offline"
  notifications: DashboardNotification[]
  reload: () => Promise<void>
  release: (ip: string) => Promise<void>
  reserveLease: (ip: string) => Promise<void>
  triggerScan: () => Promise<void>
  saveSettings: (settings: UISettings) => Promise<void>
  saveSubnet: (payload: UpsertSubnetPayload) => Promise<void>
  removeSubnet: (cidr: string) => Promise<void>
  saveReservation: (payload: UpsertReservationPayload) => Promise<void>
  removeReservation: (mac: string) => Promise<void>
  loginWithPassword: (username: string, password: string) => Promise<void>
  bootstrapAndLogin: (username: string, password: string) => Promise<void>
  logoutCurrentUser: () => Promise<void>
  createToken: (payload: { name: string; role: string; expires_in_hours?: number; description?: string }) => Promise<void>
  revokeToken: (id: string) => Promise<void>
  createBackup: () => Promise<void>
  restoreBackup: (payload: { name?: string; path?: string }) => Promise<void>
  refreshBackups: () => Promise<void>
  refreshSystemConfig: () => Promise<void>
  clearNotifications: () => void
  saveSystemConfig: (payload: SystemConfig) => Promise<void>
  requestManualFailover: (reason: string) => Promise<void>
}

function getFirstNonAuthError(errors: Array<unknown>) {
  for (const error of errors) {
    if (isApiError(error) && error.status === 401) {
      continue
    }
    if (error instanceof Error) {
      return error.message
    }
  }
  return null
}

export function useDashboardData(): DashboardState {
  const queryClient = useQueryClient()
  const [authRequired, setAuthRequired] = useState(false)
  const [liveConnection, setLiveConnection] = useState<"websocket" | "sse" | "offline">("offline")

  const notifications = useDashboardUIStore((state) => state.notifications)
  const tokenSecret = useDashboardUIStore((state) => state.tokenSecret)
  const pushNotification = useDashboardUIStore((state) => state.pushNotification)
  const clearNotifications = useDashboardUIStore((state) => state.clearNotifications)
  const setTokenSecret = useDashboardUIStore((state) => state.setTokenSecret)

  const protectedEnabled = !authRequired

  const healthQuery = useHealthQuery()
  const systemInfoQuery = useSystemInfoQuery({ enabled: protectedEnabled })
  const systemConfigQuery = useSystemConfigQuery({ enabled: protectedEnabled })
  const backupsQuery = useBackupsQuery({ enabled: protectedEnabled })
  const leasesQuery = useLeasesQuery({ enabled: protectedEnabled })
  const subnetsQuery = useSubnetsQuery({ enabled: protectedEnabled })
  const rawSubnetsQuery = useRawSubnetsQuery({ enabled: protectedEnabled })
  const reservationsQuery = useReservationsQuery({ enabled: protectedEnabled })
  const discoveryStatusQuery = useDiscoveryStatusQuery({ enabled: protectedEnabled })
  const discoveryProgressQuery = useDiscoveryProgressQuery({ enabled: protectedEnabled })
  const discoveryResultsQuery = useDiscoveryResultsQuery(30, { enabled: protectedEnabled })
  const discoveryConflictsQuery = useDiscoveryConflictsQuery({ enabled: protectedEnabled })
  const rogueServersQuery = useRogueServersQuery({ enabled: protectedEnabled })
  const settingsQuery = useUISettingsQuery({ enabled: protectedEnabled })
  const auditEntriesQuery = useAuditEntriesQuery(200, { enabled: protectedEnabled })
  const currentUserQuery = useCurrentUserQuery({ enabled: protectedEnabled })
  const currentUser = authRequired ? null : (currentUserQuery.data ?? null)
  const authTokensQuery = useAuthTokensQuery(Boolean(protectedEnabled && currentUser?.role === "admin"))

  const protectedErrors = [
    systemInfoQuery.error,
    systemConfigQuery.error,
    backupsQuery.error,
    leasesQuery.error,
    subnetsQuery.error,
    rawSubnetsQuery.error,
    reservationsQuery.error,
    discoveryStatusQuery.error,
    discoveryProgressQuery.error,
    discoveryResultsQuery.error,
    discoveryConflictsQuery.error,
    rogueServersQuery.error,
    settingsQuery.error,
    auditEntriesQuery.error,
  ]

  useEffect(() => {
    if (protectedErrors.some((error) => isApiError(error) && error.status === 401)) {
      setAuthRequired(true)
      setTokenSecret(null)
      return
    }
    if (
      systemInfoQuery.data ||
      systemConfigQuery.data ||
      backupsQuery.data ||
      leasesQuery.data ||
      subnetsQuery.data ||
      rawSubnetsQuery.data ||
      reservationsQuery.data ||
      discoveryStatusQuery.data ||
      settingsQuery.data
    ) {
      setAuthRequired(false)
    }
  }, [
    backupsQuery.data,
    discoveryStatusQuery.data,
    leasesQuery.data,
    rawSubnetsQuery.data,
    reservationsQuery.data,
    setTokenSecret,
    settingsQuery.data,
    subnetsQuery.data,
    systemConfigQuery.data,
    systemInfoQuery.data,
    protectedErrors,
  ])

  const load = useCallback(async () => {
    await invalidateDashboardQueries(queryClient)
  }, [queryClient])

  const withToast = useCallback(async <T,>(work: () => Promise<T>, successMessage: string) => {
    try {
      const result = await work()
      toast.success(successMessage)
      return result
    } catch (error) {
      const message = error instanceof Error ? error.message : "Request failed"
      toast.error(message)
      throw error
    }
  }, [])

  const release = useCallback(
    async (ip: string) => {
      await withToast(
        async () => {
          await releaseLease(ip)
          await load()
        },
        `Lease released for ${ip}`,
      )
    },
    [load, withToast],
  )

  const triggerScan = useCallback(async () => {
    await withToast(
      async () => {
        await triggerDiscoveryScan()
        await load()
      },
      "Discovery scan queued",
    )
  }, [load, withToast])

  const reserveLease = useCallback(
    async (ip: string) => {
      await withToast(
        async () => {
          await convertLeaseToReservation(ip)
          await load()
        },
        `Lease converted to reservation for ${ip}`,
      )
    },
    [load, withToast],
  )

  const saveSettings = useCallback(
    async (next: UISettings) => {
      await withToast(
        async () => {
          const saved = await updateUISettings(next)
          queryClient.setQueryData(dashboardQueryKeys.settings, saved)
        },
        "Preferences saved",
      )
    },
    [queryClient, withToast],
  )

  const saveSubnet = useCallback(
    async (payload: UpsertSubnetPayload) => {
      await withToast(
        async () => {
          await upsertSubnet(payload)
          await load()
        },
        `Subnet saved: ${payload.cidr}`,
      )
    },
    [load, withToast],
  )

  const removeSubnet = useCallback(
    async (cidr: string) => {
      await withToast(
        async () => {
          await deleteSubnet(cidr)
          await load()
        },
        `Subnet deleted: ${cidr}`,
      )
    },
    [load, withToast],
  )

  const saveReservation = useCallback(
    async (payload: UpsertReservationPayload) => {
      await withToast(
        async () => {
          await upsertReservation(payload)
          await load()
        },
        `Reservation saved: ${payload.ip}`,
      )
    },
    [load, withToast],
  )

  const removeReservation = useCallback(
    async (mac: string) => {
      await withToast(
        async () => {
          await deleteReservation(mac)
          await load()
        },
        `Reservation deleted: ${mac}`,
      )
    },
    [load, withToast],
  )

  const loginWithPassword = useCallback(
    async (username: string, password: string) => {
      await withToast(
        async () => {
          await login(username, password)
          setAuthRequired(false)
          await load()
        },
        "Signed in",
      )
    },
    [load, withToast],
  )

  const bootstrapAndLogin = useCallback(
    async (username: string, password: string) => {
      await withToast(
        async () => {
          await bootstrapAuth(username, password)
          await login(username, password)
          setAuthRequired(false)
          await load()
        },
        "Administrator bootstrapped",
      )
    },
    [load, withToast],
  )

  const logoutCurrentUser = useCallback(async () => {
    await withToast(
      async () => {
        await logout()
        setTokenSecret(null)
        setAuthRequired(true)
        queryClient.setQueryData(dashboardQueryKeys.currentUser, null)
        queryClient.setQueryData(dashboardQueryKeys.authTokens, [])
        await load()
      },
      "Signed out",
    )
  }, [load, queryClient, setTokenSecret, withToast])

  const createToken = useCallback(
    async (payload: { name: string; role: string; expires_in_hours?: number; description?: string }) => {
      await withToast(
        async () => {
          const result = await createAuthToken(payload)
          setTokenSecret(result.secret)
          await queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.authTokens })
        },
        "API token created",
      )
    },
    [queryClient, setTokenSecret, withToast],
  )

  const revokeToken = useCallback(
    async (id: string) => {
      await withToast(
        async () => {
          await revokeAuthToken(id)
          await queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.authTokens })
        },
        "API token revoked",
      )
    },
    [queryClient, withToast],
  )

  const refreshBackups = useCallback(async () => {
    await queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.backups })
  }, [queryClient])

  const refreshSystemConfig = useCallback(async () => {
    await queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.systemConfig })
  }, [queryClient])

  const saveSystemConfig = useCallback(
    async (payload: SystemConfig) => {
      await withToast(
        async () => {
          const updated = await updateSystemConfig(payload)
          queryClient.setQueryData(dashboardQueryKeys.systemConfig, updated)
        },
        "Runtime config updated",
      )
    },
    [queryClient, withToast],
  )

  const createBackup = useCallback(async () => {
    await withToast(
      async () => {
        await createSystemBackup()
        await refreshBackups()
      },
      "Backup created",
    )
  }, [refreshBackups, withToast])

  const restoreBackup = useCallback(
    async (payload: { name?: string; path?: string }) => {
      await withToast(
        async () => {
          await restoreSystemBackup(payload)
          await load()
        },
        "Backup restored",
      )
    },
    [load, withToast],
  )

  const requestManualFailover = useCallback(
    async (reason: string) => {
      await withToast(
        async () => {
          await triggerHAFailover(reason)
          await load()
        },
        "Manual failover requested",
      )
    },
    [load, withToast],
  )

  const pushLiveNotification = useCallback(
    (type: string, payload?: Record<string, unknown>) => {
      const suffix =
        typeof payload?.ip === "string"
          ? ` (${payload.ip})`
          : typeof payload?.cidr === "string"
            ? ` (${payload.cidr})`
            : typeof payload?.scan_id === "string"
              ? ` (${payload.scan_id})`
              : ""
      pushNotification({
        type,
        message: `${type}${suffix}`,
        at: new Date().toISOString(),
      })
    },
    [pushNotification],
  )

  const handleLiveEvent = useCallback(
    (event: LiveEvent) => {
      pushLiveNotification(event.type, event.data)
    },
    [pushLiveNotification],
  )

  useEffect(() => {
    let timer: ReturnType<typeof setInterval> | null = null
    void load()
    if (!authRequired && (settingsQuery.data?.auto_refresh ?? true)) {
      timer = setInterval(() => {
        void load()
      }, 15_000)
    }
    return () => {
      if (timer) {
        clearInterval(timer)
      }
    }
  }, [authRequired, load, settingsQuery.data?.auto_refresh])

  useEffect(() => {
    if (authRequired) {
      setLiveConnection("offline")
      return
    }
    let reloadTimer: ReturnType<typeof setTimeout> | null = null
    let fallbackSource: EventSource | null = null
    let fallbackActivated = false

    const scheduleReload = () => {
      if (reloadTimer) {
        clearTimeout(reloadTimer)
      }
      reloadTimer = setTimeout(() => {
        reloadTimer = null
        void load()
      }, 300)
    }

    const activateSSEFallback = () => {
      if (fallbackActivated) {
        return
      }
      fallbackActivated = true
      setLiveConnection("sse")
      fallbackSource = new EventSource("/api/v1/events")
      const parsePayload = (evt: Event): Record<string, unknown> => {
        try {
          const raw = (evt as MessageEvent).data
          if (typeof raw !== "string" || !raw.trim()) {
            return {}
          }
          const decoded = JSON.parse(raw) as { data?: Record<string, unknown> }
          return decoded?.data ?? {}
        } catch {
          return {}
        }
      }
      const makeHandler = (eventType: string) => {
        return (evt: Event) => {
          handleLiveEvent({ type: eventType, data: parsePayload(evt) })
          scheduleReload()
        }
      }

      const handlers = {
        leaseReleased: makeHandler("lease.released"),
        leaseCreated: makeHandler("lease.created"),
        leaseRenewed: makeHandler("lease.renewed"),
        leaseExpired: makeHandler("lease.expired"),
        subnetUpserted: makeHandler("subnet.upserted"),
        subnetCreated: makeHandler("subnet.created"),
        subnetDeleted: makeHandler("subnet.deleted"),
        reservationUpserted: makeHandler("reservation.upserted"),
        reservationDeleted: makeHandler("reservation.deleted"),
        addressReserved: makeHandler("address.reserved"),
        discoveryQueued: makeHandler("discovery.scan_queued"),
        discoveryStarted: makeHandler("discovery.started"),
        discoveryCompleted: makeHandler("discovery.scan_completed"),
        discoveryConflict: makeHandler("discovery.conflict"),
        settingsUpdated: makeHandler("settings.ui_updated"),
        haRoleChanged: makeHandler("ha.role_changed"),
        haFailoverRequested: makeHandler("ha.failover_requested"),
      }

      fallbackSource.addEventListener("lease.released", handlers.leaseReleased)
      fallbackSource.addEventListener("lease.created", handlers.leaseCreated)
      fallbackSource.addEventListener("lease.renewed", handlers.leaseRenewed)
      fallbackSource.addEventListener("lease.expired", handlers.leaseExpired)
      fallbackSource.addEventListener("subnet.upserted", handlers.subnetUpserted)
      fallbackSource.addEventListener("subnet.created", handlers.subnetCreated)
      fallbackSource.addEventListener("subnet.deleted", handlers.subnetDeleted)
      fallbackSource.addEventListener("reservation.upserted", handlers.reservationUpserted)
      fallbackSource.addEventListener("reservation.deleted", handlers.reservationDeleted)
      fallbackSource.addEventListener("address.reserved", handlers.addressReserved)
      fallbackSource.addEventListener("discovery.scan_queued", handlers.discoveryQueued)
      fallbackSource.addEventListener("discovery.started", handlers.discoveryStarted)
      fallbackSource.addEventListener("discovery.scan_completed", handlers.discoveryCompleted)
      fallbackSource.addEventListener("discovery.conflict", handlers.discoveryConflict)
      fallbackSource.addEventListener("settings.ui_updated", handlers.settingsUpdated)
      fallbackSource.addEventListener("ha.role_changed", handlers.haRoleChanged)
      fallbackSource.addEventListener("ha.failover_requested", handlers.haFailoverRequested)
      fallbackSource.onerror = () => {
        setLiveConnection("offline")
      }
    }

    let opened = false
    const socket = connectLiveSocket({
      onOpen: () => {
        opened = true
        setLiveConnection("websocket")
      },
      onClose: () => {
        if (!opened) {
          activateSSEFallback()
          return
        }
        setLiveConnection("offline")
      },
      onError: () => {
        if (!opened) {
          activateSSEFallback()
          return
        }
        setLiveConnection("offline")
      },
      onEvent: (event) => {
        handleLiveEvent(event)
        scheduleReload()
      },
    })

    return () => {
      socket.close()
      fallbackSource?.close()
      setLiveConnection("offline")
      if (reloadTimer) {
        clearTimeout(reloadTimer)
      }
    }
  }, [authRequired, handleLiveEvent, load])

  const loading =
    healthQuery.isPending ||
    (!authRequired &&
      [
        systemInfoQuery,
        systemConfigQuery,
        backupsQuery,
        leasesQuery,
        subnetsQuery,
        rawSubnetsQuery,
        reservationsQuery,
        discoveryStatusQuery,
        discoveryProgressQuery,
        discoveryResultsQuery,
        discoveryConflictsQuery,
        rogueServersQuery,
        settingsQuery,
        auditEntriesQuery,
      ].some((query) => query.isPending))

  const error = authRequired
    ? "Authentication required. Please sign in."
    : getFirstNonAuthError([healthQuery.error, ...protectedErrors, authTokensQuery.error])

  return useMemo(() => {
    const role = currentUser?.role ?? ""
    const isAdmin = role === "admin"
    const canMutate = !authRequired || role === "admin" || role === "operator"

    return {
      health: healthQuery.data ?? null,
      systemInfo: authRequired ? null : (systemInfoQuery.data ?? null),
      systemConfig: authRequired ? null : (systemConfigQuery.data ?? null),
      backups: authRequired ? [] : (backupsQuery.data ?? []),
      leases: authRequired ? [] : (leasesQuery.data ?? []),
      subnets: authRequired ? [] : (subnetsQuery.data ?? []),
      subnetRecords: authRequired ? [] : (rawSubnetsQuery.data ?? []),
      reservations: authRequired ? [] : (reservationsQuery.data ?? []),
      discovery: authRequired ? null : (discoveryStatusQuery.data ?? null),
      discoveryResults: authRequired ? [] : (discoveryResultsQuery.data ?? []),
      discoveryProgress: authRequired ? null : (discoveryProgressQuery.data ?? null),
      discoveryConflicts: authRequired ? [] : (discoveryConflictsQuery.data ?? []),
      rogueServers: authRequired ? [] : (rogueServersQuery.data ?? []),
      auditEntries: authRequired ? [] : (auditEntriesQuery.data ?? []),
      settings: authRequired ? null : (settingsQuery.data ?? null),
      currentUser,
      authTokens: authRequired ? [] : (authTokensQuery.data ?? []),
      tokenSecret,
      authRequired,
      canMutate,
      isAdmin,
      loading,
      error,
      liveConnection,
      notifications,
      reload: load,
      release,
      reserveLease,
      triggerScan,
      saveSettings,
      saveSubnet,
      removeSubnet,
      saveReservation,
      removeReservation,
      loginWithPassword,
      bootstrapAndLogin,
      logoutCurrentUser,
      createToken,
      revokeToken,
      createBackup,
      restoreBackup,
      refreshBackups,
      refreshSystemConfig,
      clearNotifications,
      saveSystemConfig,
      requestManualFailover,
    }
  }, [
    auditEntriesQuery.data,
    authRequired,
    authTokensQuery.data,
    backupsQuery.data,
    bootstrapAndLogin,
    clearNotifications,
    createBackup,
    createToken,
    currentUser,
    discoveryConflictsQuery.data,
    discoveryProgressQuery.data,
    discoveryResultsQuery.data,
    discoveryStatusQuery.data,
    error,
    healthQuery.data,
    leasesQuery.data,
    liveConnection,
    load,
    loading,
    loginWithPassword,
    logoutCurrentUser,
    notifications,
    rawSubnetsQuery.data,
    refreshBackups,
    refreshSystemConfig,
    release,
    removeReservation,
    removeSubnet,
    requestManualFailover,
    reservationsQuery.data,
    reserveLease,
    restoreBackup,
    revokeToken,
    rogueServersQuery.data,
    saveReservation,
    saveSettings,
    saveSubnet,
    saveSystemConfig,
    settingsQuery.data,
    subnetsQuery.data,
    systemConfigQuery.data,
    systemInfoQuery.data,
    tokenSecret,
    triggerScan,
  ])
}
