import { Droplets } from "lucide-react"
import { NavLink } from "react-router-dom"

import { navItems } from "@/app/navigation"
import { cn } from "@/lib/utils"

export function Sidebar({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <div className="flex h-full w-full flex-col rounded-2xl border border-border/70 bg-card/85 p-4 shadow-sm backdrop-blur-sm transition-colors duration-150">
      <div className="mb-6 flex items-center gap-3 px-2">
        <div className="grid size-9 place-items-center rounded-xl bg-primary/15 text-primary">
          <Droplets className="size-5" />
        </div>
        <div>
          <p className="text-sm font-medium text-muted-foreground">Monsoon</p>
          <h1 className="text-lg font-semibold tracking-tight">DHCP + IPAM</h1>
        </div>
      </div>

      <nav className="space-y-1">
        {navItems.map((item) => {
          const Icon = item.icon
          return (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.to === "/"}
              onClick={onNavigate}
              onMouseEnter={() => {
                void item.preload()
              }}
              onFocus={() => {
                void item.preload()
              }}
              className={({ isActive }) =>
                cn(
                  "group flex items-center gap-3 rounded-xl px-3 py-2 text-sm font-medium transition-colors",
                  isActive ? "bg-primary/15 text-primary" : "text-muted-foreground hover:bg-accent hover:text-foreground",
                )
              }
            >
              <Icon className="size-4" />
              {item.label}
            </NavLink>
          )
        })}
      </nav>

      <div className="mt-auto rounded-xl border border-border/70 bg-background/70 p-3">
        <p className="text-xs uppercase tracking-wide text-muted-foreground">Status</p>
        <p className="mt-1 text-sm font-semibold text-foreground">All systems operational</p>
      </div>
    </div>
  )
}
