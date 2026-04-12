import { ClipboardList, Download, Search } from "lucide-react"
import { useMemo, useState } from "react"

import { useDashboard } from "@/app/dashboard-context"
import { EmptyState } from "@/components/shared/empty-state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"

export function AuditPage() {
  const { auditEntries, authRequired, isAdmin } = useDashboard()
  const [query, setQuery] = useState("")

  const hasAdminAccess = !authRequired || isAdmin

  const filtered = useMemo(() => {
    if (!hasAdminAccess) return []
    const needle = query.toLowerCase().trim()
    if (!needle) return auditEntries
    return auditEntries.filter((item) =>
      [item.actor, item.action, item.object_type, item.object_id, item.source].join(" ").toLowerCase().includes(needle),
    )
  }, [auditEntries, hasAdminAccess, query])

  const exportSuffix = useMemo(() => {
    const params = new URLSearchParams()
    if (query.trim()) {
      params.set("q", query.trim())
    }
    return params.toString()
  }, [query])

  const csvHref = `/api/v1/audit?${exportSuffix ? `${exportSuffix}&` : ""}format=csv`
  const jsonHref = `/api/v1/audit?${exportSuffix ? `${exportSuffix}&` : ""}format=json`

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Audit Trail</h2>
        <p className="text-sm text-muted-foreground">Track every operational change in a searchable timeline.</p>
      </div>

      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <CardTitle className="flex items-center gap-2">
              <ClipboardList className="size-4 text-cyan-400" />
              Audit log
            </CardTitle>
            {hasAdminAccess ? (
              <div className="flex flex-wrap gap-2">
                <Button variant="outline" size="sm" asChild>
                  <a href={csvHref} target="_blank" rel="noreferrer">
                    <Download className="mr-2 size-4" />
                    Export CSV
                  </a>
                </Button>
                <Button variant="outline" size="sm" asChild>
                  <a href={jsonHref} target="_blank" rel="noreferrer">
                    <Download className="mr-2 size-4" />
                    Export JSON
                  </a>
                </Button>
              </div>
            ) : null}
          </div>
          <CardDescription>{hasAdminAccess ? `${filtered.length} records` : "Admin-only operational history"}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {hasAdminAccess ? (
            <>
              <div className="flex items-center gap-2 rounded-xl border border-border/70 bg-muted/30 px-3 py-2">
                <Search className="size-4 text-muted-foreground" />
                <Input
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  className="h-8 border-0 bg-transparent px-0 shadow-none focus-visible:ring-0 focus-visible:ring-offset-0"
                  placeholder="Filter by actor, action, object..."
                  aria-label="Filter audit log"
                />
              </div>

              <div className="space-y-2">
                {filtered.length === 0 ? (
                  <EmptyState
                    icon={ClipboardList}
                    title="No audit entries found"
                    description="No audit records match the current filter."
                  />
                ) : null}
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
              </div>
            </>
          ) : (
            <EmptyState
              icon={ClipboardList}
              title="Admin access required"
              description="Audit history and exports are available only to admin sessions."
            />
          )}
        </CardContent>
      </Card>
    </div>
  )
}
