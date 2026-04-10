import { ClipboardList, Search } from "lucide-react"
import { useMemo, useState } from "react"

import { useDashboard } from "@/app/dashboard-context"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

export function AuditPage() {
  const { auditEntries } = useDashboard()
  const [query, setQuery] = useState("")

  const filtered = useMemo(() => {
    const needle = query.toLowerCase().trim()
    if (!needle) return auditEntries
    return auditEntries.filter((item) =>
      [item.actor, item.action, item.object_type, item.object_id, item.source].join(" ").toLowerCase().includes(needle),
    )
  }, [auditEntries, query])

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Audit Trail</h2>
        <p className="text-sm text-muted-foreground">Track every operational change in a searchable timeline.</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <ClipboardList className="size-4 text-cyan-400" />
            Audit log
          </CardTitle>
          <CardDescription>{filtered.length} records</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex items-center gap-2 rounded-xl border border-border/70 bg-muted/30 px-3 py-2">
            <Search className="size-4 text-muted-foreground" />
            <input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              className="h-8 w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
              placeholder="Filter by actor, action, object..."
            />
          </div>

          <div className="space-y-2">
            {filtered.map((item) => (
              <div key={item.id} className="rounded-xl border border-border/70 bg-background/60 p-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <p className="text-sm font-medium">{item.action}</p>
                  <Badge variant="outline">{item.source}</Badge>
                </div>
                <p className="mt-1 text-xs text-muted-foreground">
                  {item.timestamp} | actor: {item.actor} | {item.object_type}:{item.object_id}
                </p>
              </div>
            ))}
            {filtered.length === 0 && <p className="text-sm text-muted-foreground">No audit entries found.</p>}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
