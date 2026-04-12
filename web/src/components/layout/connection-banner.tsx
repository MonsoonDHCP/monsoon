import { AlertTriangle, WifiOff, X } from "lucide-react"
import { useEffect, useState } from "react"

import { useDashboard } from "@/app/dashboard-context"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

export function ConnectionBanner() {
  const { liveConnection, reload } = useDashboard()
  const [dismissed, setDismissed] = useState(false)

  useEffect(() => {
    setDismissed(false)
  }, [liveConnection])

  if (dismissed || liveConnection === "websocket") {
    return null
  }

  const isOffline = liveConnection === "offline"

  return (
    <div
      aria-live="polite"
      className={cn(
        "border-b px-4 py-2 text-sm sm:px-6 lg:px-8",
        isOffline
          ? "border-destructive/40 bg-destructive/10 text-foreground"
          : "border-warning/40 bg-warning/10 text-foreground",
      )}
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          {isOffline ? <WifiOff className="size-4 text-destructive" /> : <AlertTriangle className="size-4 text-warning" />}
          <p>
            {isOffline
              ? "Realtime connection is offline. Updates may lag until the stream reconnects."
              : "Realtime socket is unavailable. The dashboard is using event-stream fallback mode."}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button size="sm" variant="outline" onClick={() => void reload()}>
            Retry sync
          </Button>
          <Button size="icon" variant="ghost" aria-label="Dismiss connection banner" onClick={() => setDismissed(true)}>
            <X className="size-4" />
          </Button>
        </div>
      </div>
    </div>
  )
}
