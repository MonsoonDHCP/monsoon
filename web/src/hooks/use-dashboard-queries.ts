import { useQuery, type QueryClient } from "@tanstack/react-query"

import {
  fetchAddresses,
  fetchAuditEntries,
  fetchAuthTokens,
  fetchCurrentUser,
  fetchDiscoveryConflicts,
  fetchDiscoveryProgress,
  fetchDiscoveryResults,
  fetchDiscoveryRogueServers,
  fetchDiscoveryStatus,
  fetchHealth,
  fetchLeases,
  fetchRawSubnets,
  fetchReservations,
  fetchSubnets,
  fetchSystemBackups,
  fetchSystemConfig,
  fetchSystemInfo,
  fetchUISettings,
} from "@/lib/api"

export const dashboardQueryKeys = {
  health: ["dashboard", "health"] as const,
  systemInfo: ["dashboard", "system-info"] as const,
  systemConfig: ["dashboard", "system-config"] as const,
  backups: ["dashboard", "backups"] as const,
  leases: ["dashboard", "leases"] as const,
  subnets: ["dashboard", "subnets"] as const,
  rawSubnets: ["dashboard", "subnets", "raw"] as const,
  reservations: ["dashboard", "reservations"] as const,
  discoveryStatus: ["dashboard", "discovery", "status"] as const,
  discoveryProgress: ["dashboard", "discovery", "progress"] as const,
  discoveryResults: ["dashboard", "discovery", "results"] as const,
  discoveryConflicts: ["dashboard", "discovery", "conflicts"] as const,
  rogueServers: ["dashboard", "discovery", "rogue"] as const,
  settings: ["dashboard", "settings", "ui"] as const,
  auditEntries: ["dashboard", "audit"] as const,
  currentUser: ["dashboard", "auth", "me"] as const,
  authTokens: ["dashboard", "auth", "tokens"] as const,
  addresses: (subnet?: string) => ["dashboard", "addresses", subnet ?? "all"] as const,
}

type QueryOptions = {
  enabled?: boolean
}

export function useHealthQuery() {
  return useQuery({
    queryKey: dashboardQueryKeys.health,
    queryFn: fetchHealth,
    retry: false,
  })
}

export function useSystemInfoQuery({ enabled = true }: QueryOptions = {}) {
  return useQuery({
    queryKey: dashboardQueryKeys.systemInfo,
    queryFn: fetchSystemInfo,
    enabled,
    retry: false,
  })
}

export function useSystemConfigQuery({ enabled = true }: QueryOptions = {}) {
  return useQuery({
    queryKey: dashboardQueryKeys.systemConfig,
    queryFn: fetchSystemConfig,
    enabled,
    retry: false,
  })
}

export function useBackupsQuery({ enabled = true }: QueryOptions = {}) {
  return useQuery({
    queryKey: dashboardQueryKeys.backups,
    queryFn: fetchSystemBackups,
    enabled,
    retry: false,
  })
}

export function useLeasesQuery({ enabled = true }: QueryOptions = {}) {
  return useQuery({
    queryKey: dashboardQueryKeys.leases,
    queryFn: fetchLeases,
    enabled,
    retry: false,
  })
}

export function useSubnetsQuery({ enabled = true }: QueryOptions = {}) {
  return useQuery({
    queryKey: dashboardQueryKeys.subnets,
    queryFn: fetchSubnets,
    enabled,
    retry: false,
  })
}

export function useRawSubnetsQuery({ enabled = true }: QueryOptions = {}) {
  return useQuery({
    queryKey: dashboardQueryKeys.rawSubnets,
    queryFn: fetchRawSubnets,
    enabled,
    retry: false,
  })
}

export function useReservationsQuery({ enabled = true }: QueryOptions = {}) {
  return useQuery({
    queryKey: dashboardQueryKeys.reservations,
    queryFn: fetchReservations,
    enabled,
    retry: false,
  })
}

export function useDiscoveryStatusQuery({ enabled = true }: QueryOptions = {}) {
  return useQuery({
    queryKey: dashboardQueryKeys.discoveryStatus,
    queryFn: fetchDiscoveryStatus,
    enabled,
    retry: false,
  })
}

export function useDiscoveryProgressQuery({ enabled = true }: QueryOptions = {}) {
  return useQuery({
    queryKey: dashboardQueryKeys.discoveryProgress,
    queryFn: fetchDiscoveryProgress,
    enabled,
    retry: false,
  })
}

export function useDiscoveryResultsQuery(limit = 30, { enabled = true }: QueryOptions = {}) {
  return useQuery({
    queryKey: [...dashboardQueryKeys.discoveryResults, limit] as const,
    queryFn: () => fetchDiscoveryResults(limit),
    enabled,
    retry: false,
  })
}

export function useDiscoveryConflictsQuery({ enabled = true }: QueryOptions = {}) {
  return useQuery({
    queryKey: dashboardQueryKeys.discoveryConflicts,
    queryFn: fetchDiscoveryConflicts,
    enabled,
    retry: false,
  })
}

export function useRogueServersQuery({ enabled = true }: QueryOptions = {}) {
  return useQuery({
    queryKey: dashboardQueryKeys.rogueServers,
    queryFn: fetchDiscoveryRogueServers,
    enabled,
    retry: false,
  })
}

export function useUISettingsQuery({ enabled = true }: QueryOptions = {}) {
  return useQuery({
    queryKey: dashboardQueryKeys.settings,
    queryFn: fetchUISettings,
    enabled,
    retry: false,
  })
}

export function useAuditEntriesQuery(limit = 200, { enabled = true }: QueryOptions = {}) {
  return useQuery({
    queryKey: [...dashboardQueryKeys.auditEntries, limit] as const,
    queryFn: () => fetchAuditEntries({ limit }),
    enabled,
    retry: false,
  })
}

export function useCurrentUserQuery({ enabled = true }: QueryOptions = {}) {
  return useQuery({
    queryKey: dashboardQueryKeys.currentUser,
    queryFn: fetchCurrentUser,
    enabled,
    retry: false,
  })
}

export function useAuthTokensQuery(enabled: boolean) {
  return useQuery({
    queryKey: dashboardQueryKeys.authTokens,
    queryFn: fetchAuthTokens,
    enabled,
    retry: false,
  })
}

export function useAddressesQuery(subnet?: string, options: { enabled?: boolean; refetchInterval?: number | false } = {}) {
  return useQuery({
    queryKey: dashboardQueryKeys.addresses(subnet),
    queryFn: () => fetchAddresses(subnet),
    enabled: options.enabled ?? true,
    refetchInterval: options.refetchInterval,
    retry: false,
  })
}

export async function invalidateDashboardQueries(queryClient: QueryClient) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.health }),
    queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.systemInfo }),
    queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.systemConfig }),
    queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.backups }),
    queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.leases }),
    queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.subnets }),
    queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.rawSubnets }),
    queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.reservations }),
    queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.discoveryStatus }),
    queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.discoveryProgress }),
    queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.discoveryResults }),
    queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.discoveryConflicts }),
    queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.rogueServers }),
    queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.settings }),
    queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.auditEntries }),
    queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.currentUser }),
    queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.authTokens }),
    queryClient.invalidateQueries({ queryKey: ["dashboard", "addresses"] }),
  ])
}
