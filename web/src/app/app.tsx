import { useEffect } from "react"
import { useTheme } from "next-themes"
import { Navigate, Route, Routes } from "react-router-dom"

import { AuthGate } from "@/app/auth-gate"
import { DashboardDataProvider } from "@/app/dashboard-context"
import { useDashboardData } from "@/hooks/use-dashboard-data"
import { AppShell } from "@/components/layout/app-shell"
import { AddressesPage } from "@/pages/addresses-page"
import { AuditPage } from "@/pages/audit-page"
import { DiscoveryPage } from "@/pages/discovery-page"
import { LeasesPage } from "@/pages/leases-page"
import { OverviewPage } from "@/pages/overview-page"
import { ReservationsPage } from "@/pages/reservations-page"
import { SettingsPage } from "@/pages/settings-page"
import { SubnetsPage } from "@/pages/subnets-page"

export default function App() {
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
    </DashboardDataProvider>
  )
}
