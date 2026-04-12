import { Bell, LogOut, Menu, RefreshCw } from "lucide-react"

import { useDashboard } from "@/app/dashboard-context"
import { ThemeToggle } from "@/components/layout/theme-toggle"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Sheet, SheetContent, SheetTitle, SheetTrigger } from "@/components/ui/sheet"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { Sidebar } from "@/components/layout/sidebar"

export function Topbar() {
  const { reload, loading, currentUser, logoutCurrentUser, discoveryProgress, notifications, clearNotifications } = useDashboard()
  const initials = currentUser?.username
    ? currentUser.username
        .split(/[._-]/g)
        .filter(Boolean)
        .map((part) => part[0]?.toUpperCase())
        .join("")
        .slice(0, 2)
    : "MO"

  return (
    <header className="sticky top-0 z-[100] flex h-16 items-center justify-between border-b border-border/70 bg-background/80 px-4 backdrop-blur-xl transition-colors duration-150 sm:px-6 lg:px-8">
      <div className="flex items-center gap-2 lg:hidden">
        <Sheet>
          <SheetTrigger asChild>
            <Button variant="outline" size="icon" aria-label="Open navigation">
              <Menu className="size-4" />
            </Button>
          </SheetTrigger>
          <SheetContent side="left" className="p-3">
            <SheetTitle className="sr-only">Navigation</SheetTitle>
            <Sidebar />
          </SheetContent>
        </Sheet>
        <p className="text-sm font-semibold tracking-tight">Monsoon Console</p>
      </div>

      <div className="hidden items-center gap-3 lg:flex">
        <h2 className="text-lg font-semibold tracking-tight">Network Operations Center</h2>
      </div>

      <div className="flex items-center gap-2">
        {discoveryProgress?.in_progress ? (
          <Badge variant="warning" className="hidden sm:inline-flex">
            Scan {discoveryProgress.percent}%
          </Badge>
        ) : null}

        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="outline" size="icon" onClick={() => void reload()} disabled={loading} aria-label="Refresh dashboard data">
                <RefreshCw className={loading ? "size-4 animate-spin" : "size-4"} />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Refresh data</TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="icon" aria-label="Notifications" className="relative">
              <Bell className="size-4" />
              {notifications.length > 0 ? (
                <span className="absolute -right-1 -top-1 inline-flex min-w-4 items-center justify-center rounded-full bg-rose-500 px-1 text-[10px] text-white">
                  {Math.min(notifications.length, 9)}
                </span>
              ) : null}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-96">
            <DropdownMenuLabel className="flex items-center justify-between">
              <span>Notifications</span>
              <Button variant="ghost" size="sm" onClick={clearNotifications} className="h-7 px-2 text-xs">
                Clear
              </Button>
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <div className="max-h-72 space-y-1 overflow-auto p-1">
              {notifications.slice(0, 12).map((item) => (
                <div key={item.id} className="rounded-md border border-border/50 bg-background/60 px-2 py-1.5">
                  <p className="text-xs text-foreground">{item.message}</p>
                  <p className="text-[10px] text-muted-foreground">{new Date(item.at).toLocaleString()}</p>
                </div>
              ))}
              {notifications.length === 0 ? <p className="px-2 py-3 text-xs text-muted-foreground">No recent events.</p> : null}
            </div>
          </DropdownMenuContent>
        </DropdownMenu>

        <ThemeToggle />

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              className="rounded-full transition-colors hover:ring-2 hover:ring-ring/40"
              aria-label="Account menu"
            >
              <Avatar className="size-9 border border-border/70">
                <AvatarFallback className="bg-primary/15 text-primary">{initials}</AvatarFallback>
              </Avatar>
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-60">
            <DropdownMenuLabel className="space-y-1">
              <p className="text-sm leading-none">{currentUser?.username ?? "Session unavailable"}</p>
              <p className="text-xs font-normal text-muted-foreground">role: {currentUser?.role ?? "guest"}</p>
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              disabled={!currentUser}
              onClick={() => {
                void logoutCurrentUser()
              }}
            >
              <LogOut className="mr-2 size-4" />
              Logout
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>
  )
}
