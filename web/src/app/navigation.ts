import { BarChart3, ClipboardList, Compass, LifeBuoy, Network, Settings2, ShieldCheck, Waypoints } from "lucide-react"

export const navItems = [
  { to: "/", label: "Overview", icon: BarChart3 },
  { to: "/subnets", label: "Subnets", icon: Network },
  { to: "/addresses", label: "Addresses", icon: Waypoints },
  { to: "/reservations", label: "Reservations", icon: ShieldCheck },
  { to: "/leases", label: "Leases", icon: Compass },
  { to: "/discovery", label: "Discovery", icon: LifeBuoy },
  { to: "/audit", label: "Audit", icon: ClipboardList },
  { to: "/settings", label: "Settings", icon: Settings2 },
] as const
