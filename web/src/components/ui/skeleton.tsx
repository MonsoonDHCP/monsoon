import { cn } from "@/lib/utils"

type SkeletonProps = {
  className?: string
}

export function Skeleton({ className }: SkeletonProps) {
  return <div className={cn("animate-pulse rounded-xl bg-muted/70", className)} aria-hidden="true" />
}
