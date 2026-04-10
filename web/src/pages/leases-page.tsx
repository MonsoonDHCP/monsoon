import { Search, ShieldPlus } from "lucide-react"
import { useMemo, useState } from "react"

import { useDashboard } from "@/app/dashboard-context"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

export function LeasesPage() {
  const { leases, release, reserveLease, canMutate } = useDashboard()
  const [query, setQuery] = useState("")

  const filtered = useMemo(() => {
    const lower = query.toLowerCase().trim()
    if (!lower) return leases
    return leases.filter((lease) => [lease.ip, lease.mac, lease.hostname ?? "", lease.subnet_id ?? ""].join(" ").toLowerCase().includes(lower))
  }, [leases, query])

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Lease Browser</h2>
        <p className="text-sm text-muted-foreground">Filter, inspect and release active addresses.</p>
        {!canMutate && <Badge className="mt-2" variant="warning">Read-only role</Badge>}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Lease table</CardTitle>
          <CardDescription>{filtered.length} visible records</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="mb-4 flex items-center gap-2 rounded-xl border border-border/70 bg-muted/30 px-3 py-2">
            <Search className="size-4 text-muted-foreground" />
            <input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              className="h-8 w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
              placeholder="Search by IP, MAC, hostname, subnet..."
            />
          </div>

          <div className="overflow-x-auto">
            <table className="w-full min-w-[760px] text-sm">
              <thead>
                <tr className="border-b border-border/70 text-left text-xs uppercase tracking-wide text-muted-foreground">
                  <th className="pb-2">IP</th>
                  <th className="pb-2">MAC</th>
                  <th className="pb-2">Hostname</th>
                  <th className="pb-2">State</th>
                  <th className="pb-2">Subnet</th>
                  <th className="pb-2 text-right">Action</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((lease) => (
                  <tr key={lease.ip} className="border-b border-border/40">
                    <td className="py-3 font-mono text-xs text-cyan-300">{lease.ip}</td>
                    <td className="py-3 font-mono text-xs text-muted-foreground">{lease.mac}</td>
                    <td className="py-3">{lease.hostname || "-"}</td>
                    <td className="py-3">
                      <Badge variant={lease.state === "bound" ? "success" : lease.state === "declined" ? "danger" : "warning"}>{lease.state}</Badge>
                    </td>
                    <td className="py-3 text-muted-foreground">{lease.subnet_id || "-"}</td>
                    <td className="py-3 text-right">
                      <div className="flex justify-end gap-2">
                        <Button variant="outline" size="sm" onClick={() => void reserveLease(lease.ip)} disabled={!canMutate}>
                          <ShieldPlus className="mr-2 size-4" />
                          Reserve
                        </Button>
                        <Button variant="outline" size="sm" onClick={() => void release(lease.ip)} disabled={!canMutate}>
                          Release
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
