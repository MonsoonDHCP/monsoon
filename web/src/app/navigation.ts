import type { LucideIcon } from "lucide-react"
import { BarChart3, ClipboardList, Compass, LifeBuoy, Network, Settings2, ShieldCheck, Waypoints } from "lucide-react"

type NavigationItem = {
  to: string
  label: string
  icon: LucideIcon
  preload: () => Promise<unknown>
}

export const navItems: NavigationItem[] = [
  { to: "/", label: "Overview", icon: BarChart3, preload: () => import("@/pages/overview-page") },
  { to: "/subnets", label: "Subnets", icon: Network, preload: () => import("@/pages/subnets-page") },
  { to: "/addresses", label: "Addresses", icon: Waypoints, preload: () => import("@/pages/addresses-page") },
  { to: "/reservations", label: "Reservations", icon: ShieldCheck, preload: () => import("@/pages/reservations-page") },
  { to: "/leases", label: "Leases", icon: Compass, preload: () => import("@/pages/leases-page") },
  { to: "/discovery", label: "Discovery", icon: LifeBuoy, preload: () => import("@/pages/discovery-page") },
  { to: "/audit", label: "Audit", icon: ClipboardList, preload: () => import("@/pages/audit-page") },
  { to: "/settings", label: "Settings", icon: Settings2, preload: () => import("@/pages/settings-page") },
] as const
