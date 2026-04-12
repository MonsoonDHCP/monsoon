import { cn } from "@/lib/utils"

type ProgressBarProps = {
  value: number
  label: string
  className?: string
  variant?: "accent" | "success" | "warning" | "danger"
}

function clamp(value: number) {
  return Math.min(100, Math.max(0, Math.round(value)))
}

export function ProgressBar({ value, label, className, variant = "accent" }: ProgressBarProps) {
  return (
    <progress
      max={100}
      value={clamp(value)}
      aria-label={label}
      data-variant={variant}
      className={cn("app-progress", className)}
    />
  )
}
