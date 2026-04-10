import { createContext, type ReactNode, useContext } from "react"

import type { useDashboardData } from "@/hooks/use-dashboard-data"

type DashboardData = ReturnType<typeof useDashboardData>

const DashboardContext = createContext<DashboardData | null>(null)

export function DashboardDataProvider({ value, children }: { value: DashboardData; children: ReactNode }) {
  return <DashboardContext.Provider value={value}>{children}</DashboardContext.Provider>
}

export function useDashboard() {
  const value = useContext(DashboardContext)
  if (!value) {
    throw new Error("useDashboard must be used inside DashboardDataProvider")
  }
  return value
}
