import { Skeleton } from "@/components/ui/skeleton"

interface StatsGridSkeletonProps {
  cards?: number
}

export function StatsGridSkeleton({ cards = 4 }: StatsGridSkeletonProps) {
  return (
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      {Array.from({ length: cards }).map((_, index) => (
        <div key={index} className="rounded-2xl border border-border/70 bg-card p-6 shadow-sm">
          <Skeleton className="h-4 w-24" />
          <Skeleton className="mt-4 h-9 w-16" />
          <Skeleton className="mt-4 h-3 w-32" />
        </div>
      ))}
    </div>
  )
}
