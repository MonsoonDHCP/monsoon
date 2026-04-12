import { lazy, Suspense, useEffect } from "react"
import { Navigate, Route, Routes } from "react-router-dom"

import { AuthGate } from "@/app/auth-gate"
import { DashboardDataProvider } from "@/app/dashboard-context"
import { PageSkeleton } from "@/components/shared/page-skeleton"
import { useDashboardData } from "@/hooks/use-dashboard-data"
import { useTheme } from "@/hooks/use-theme"
import { AppShell } from "@/components/layout/app-shell"

const OverviewPage = lazy(() => import("@/pages/overview-page").then((module) => ({ default: module.OverviewPage })))
const SubnetsPage = lazy(() => import("@/pages/subnets-page").then((module) => ({ default: module.SubnetsPage })))
const AddressesPage = lazy(() => import("@/pages/addresses-page").then((module) => ({ default: module.AddressesPage })))
const ReservationsPage = lazy(() => import("@/pages/reservations-page").then((module) => ({ default: module.ReservationsPage })))
const LeasesPage = lazy(() => import("@/pages/leases-page").then((module) => ({ default: module.LeasesPage })))
const DiscoveryPage = lazy(() => import("@/pages/discovery-page").then((module) => ({ default: module.DiscoveryPage })))
const AuditPage = lazy(() => import("@/pages/audit-page").then((module) => ({ default: module.AuditPage })))
const SettingsPage = lazy(() => import("@/pages/settings-page").then((module) => ({ default: module.SettingsPage })))

export function App() {
  const data = useDashboardData()
  const { setTheme } = useTheme()

  useEffect(() => {
    if (data.settings?.theme) {
      setTheme(data.settings.theme)
    }
    document.documentElement.setAttribute("data-density", data.settings?.density ?? "comfortable")
  }, [data.settings?.density, data.settings?.theme, setTheme])

  if (data.authRequired && !data.currentUser) {
    return (
      <AuthGate
        busy={data.loading}
        error={data.error}
        onLogin={data.loginWithPassword}
        onBootstrap={data.bootstrapAndLogin}
      />
    )
  }

  return (
    <DashboardDataProvider value={data}>
      <Suspense fallback={<AppShell shellOnly><PageSkeleton /></AppShell>}>
        <Routes>
          <Route element={<AppShell />}>
            <Route path="/" element={<OverviewPage />} />
            <Route path="/subnets" element={<SubnetsPage />} />
            <Route path="/addresses" element={<AddressesPage />} />
            <Route path="/reservations" element={<ReservationsPage />} />
            <Route path="/leases" element={<LeasesPage />} />
            <Route path="/discovery" element={<DiscoveryPage />} />
            <Route path="/audit" element={<AuditPage />} />
            <Route path="/settings" element={<SettingsPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </Routes>
      </Suspense>
    </DashboardDataProvider>
  )
}
