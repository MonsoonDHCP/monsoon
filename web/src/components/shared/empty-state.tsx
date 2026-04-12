import type { ReactNode } from "react"
import type { LucideIcon } from "lucide-react"

import { cn } from "@/lib/utils"

type EmptyStateProps = {
  icon: LucideIcon
  title: string
  description: string
  action?: ReactNode
  className?: string
}

export function EmptyState({ icon: Icon, title, description, action, className }: EmptyStateProps) {
  return (
    <div
      className={cn(
        "flex flex-col items-start gap-3 rounded-2xl border border-dashed border-border bg-muted/30 p-5 text-sm",
        className,
      )}
    >
      <div className="grid size-11 place-items-center rounded-2xl bg-accent text-accent-foreground">
        <Icon className="size-5" />
      </div>
      <div className="space-y-1">
        <p className="text-base font-semibold tracking-tight text-foreground">{title}</p>
        <p className="leading-relaxed text-muted-foreground">{description}</p>
      </div>
      {action}
    </div>
  )
}
