import { Bell, Menu, RefreshCw } from "lucide-react"

import { useDashboard } from "@/app/dashboard-context"
import { ThemeToggle } from "@/components/layout/theme-toggle"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetTitle, SheetTrigger } from "@/components/ui/sheet"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { Sidebar } from "@/components/layout/sidebar"

export function Topbar() {
  const { reload, loading } = useDashboard()

  return (
    <header className="sticky top-0 z-30 flex h-16 items-center justify-between border-b border-border/70 bg-background/75 px-4 backdrop-blur-xl lg:px-6">
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
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="outline" size="icon" onClick={() => void reload()} disabled={loading}>
                <RefreshCw className={loading ? "size-4 animate-spin" : "size-4"} />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Refresh data</TooltipContent>
          </Tooltip>
        </TooltipProvider>

        <Button variant="outline" size="icon">
          <Bell className="size-4" />
        </Button>

        <ThemeToggle />

        <Avatar className="size-9 border border-border/70">
          <AvatarFallback className="bg-primary/15 text-primary">MO</AvatarFallback>
        </Avatar>
      </div>
    </header>
  )
}
