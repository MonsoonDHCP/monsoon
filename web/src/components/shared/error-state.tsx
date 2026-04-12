import type { LucideIcon } from "lucide-react"
import { AlertTriangle, RefreshCw } from "lucide-react"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

interface ErrorStateProps {
  title: string
  description: string
  actionLabel?: string
  icon?: LucideIcon
  onAction?: () => void
  className?: string
}

export function ErrorState({
  title,
  description,
  actionLabel = "Retry",
  icon: Icon = AlertTriangle,
  onAction,
  className,
}: ErrorStateProps) {
  return (
    <div className={cn("rounded-2xl border border-destructive/30 bg-destructive/10 p-5", className)}>
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex gap-3">
          <div className="rounded-xl bg-destructive/15 p-2 text-destructive">
            <Icon className="size-5" />
          </div>
          <div className="space-y-1">
            <h3 className="text-base font-semibold tracking-tight text-foreground">{title}</h3>
            <p className="text-sm leading-relaxed text-muted-foreground">{description}</p>
          </div>
        </div>
        {onAction ? (
          <Button variant="outline" size="sm" onClick={onAction}>
            <RefreshCw className="mr-2 size-4" />
            {actionLabel}
          </Button>
        ) : null}
      </div>
    </div>
  )
}
