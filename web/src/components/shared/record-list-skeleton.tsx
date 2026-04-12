import { Skeleton } from "@/components/ui/skeleton"

interface RecordListSkeletonProps {
  rows?: number
}

export function RecordListSkeleton({ rows = 4 }: RecordListSkeletonProps) {
  return (
    <div className="space-y-2" aria-hidden="true">
      {Array.from({ length: rows }).map((_, index) => (
        <div key={index} className="rounded-xl border border-border/70 bg-background/60 p-3">
          <div className="flex items-center justify-between gap-3">
            <Skeleton className="h-3 w-24" />
            <Skeleton className="h-5 w-16 rounded-full" />
          </div>
          <Skeleton className="mt-3 h-4 w-32" />
          <Skeleton className="mt-2 h-3 w-48" />
        </div>
      ))}
    </div>
  )
}
