import { Radar, ShieldAlert, Telescope } from "lucide-react"

import { useDashboard } from "@/app/dashboard-context"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

export function DiscoveryPage() {
  const { discovery, discoveryProgress, discoveryResults, discoveryConflicts, rogueServers, triggerScan, canMutate } = useDashboard()

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Discovery Operations</h2>
        <p className="text-sm text-muted-foreground">Active scan orchestration and rogue DHCP detection workflow.</p>
        {!canMutate && <Badge className="mt-2" variant="warning">Read-only role</Badge>}
      </div>

      <div className="grid gap-4 xl:grid-cols-3">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Telescope className="size-4 text-cyan-400" />
              Planned scan
            </CardTitle>
            <CardDescription>Run a coordinated sweep over configured subnets.</CardDescription>
          </CardHeader>
          <CardContent>
            <Button onClick={() => void triggerScan()} disabled={discovery?.scanning || !canMutate}>
              {discovery?.scanning ? "Scanning..." : "Trigger scan"}
            </Button>
            <div className="mt-3">
              <div className="h-2 rounded-full bg-muted">
                <div
                  className="h-full rounded-full bg-gradient-to-r from-cyan-400 to-teal-400 transition-all"
                  style={{ width: `${Math.max(0, Math.min(100, discoveryProgress?.percent ?? 0))}%` }}
                />
              </div>
              <p className="mt-2 text-xs text-muted-foreground">
                Phase: {discoveryProgress?.phase ?? "idle"} | {discoveryProgress?.processed ?? 0}/{discoveryProgress?.total ?? 0}
              </p>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Radar className="size-4 text-teal-400" />
              Rogue detector
            </CardTitle>
            <CardDescription>Passive monitoring for unauthorized DHCPOFFER signals.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-2 text-sm text-muted-foreground">
            <div className="rounded-lg bg-muted/40 px-3 py-2">Sensor status: {discovery?.sensor_online ? "online" : "offline"}</div>
            <div className="rounded-lg bg-muted/40 px-3 py-2">Last scan: {discovery?.last_scan_at ?? "n/a"}</div>
            <div className="rounded-lg bg-muted/40 px-3 py-2">Latest scan id: {discovery?.latest_scan_id ?? "n/a"}</div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <ShieldAlert className="size-4 text-amber-300" />
              Conflict posture
            </CardTitle>
            <CardDescription>Network consistency confidence for latest pass.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-2">
            <Badge variant={(discovery?.active_conflicts ?? 0) > 0 ? "danger" : "success"}>
              {(discovery?.active_conflicts ?? 0) > 0 ? `${discovery?.active_conflicts} conflict(s)` : "No active conflict"}
            </Badge>
            <p className="text-xs text-muted-foreground">Next scan: {discovery?.next_scheduled_scan ?? "n/a"}</p>
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Recent scans</CardTitle>
            <CardDescription>{discoveryResults.length} latest scan records</CardDescription>
          </CardHeader>
          <CardContent className="space-y-2">
            {discoveryResults.map((scan) => (
              <div key={scan.scan_id} className="rounded-lg border border-border/70 bg-background/70 p-3">
                <div className="flex items-center justify-between gap-2">
                  <p className="font-mono text-xs text-cyan-300">{scan.scan_id}</p>
                  <Badge variant={scan.status === "completed" ? "success" : "warning"}>{scan.status}</Badge>
                </div>
                <p className="mt-1 text-xs text-muted-foreground">
                  hosts: {scan.total_hosts} | new: {scan.new_hosts} | changed: {scan.changed_hosts} | missing: {scan.missing_hosts}
                </p>
              </div>
            ))}
            {discoveryResults.length === 0 && <p className="text-sm text-muted-foreground">No scan results yet.</p>}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Conflict & rogue feed</CardTitle>
            <CardDescription>Latest anomaly findings from discovery pass.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-2">
            {discoveryConflicts.map((conflict) => (
              <div key={conflict.ip} className="rounded-lg border border-rose-400/30 bg-rose-500/10 p-3">
                <p className="font-mono text-xs text-rose-200">{conflict.ip}</p>
                <p className="text-xs text-rose-100/90">MACs: {conflict.macs.join(", ")}</p>
              </div>
            ))}
            {rogueServers.map((rogue) => (
              <div key={`${rogue.ip}-${rogue.detected}`} className="rounded-lg border border-amber-400/30 bg-amber-500/10 p-3">
                <p className="font-mono text-xs text-amber-100">Rogue DHCP: {rogue.ip}</p>
                <p className="text-xs text-amber-50/90">{rogue.source || "unknown source"}</p>
              </div>
            ))}
            {discoveryConflicts.length === 0 && rogueServers.length === 0 && (
              <p className="text-sm text-muted-foreground">No active anomalies from latest scan.</p>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
