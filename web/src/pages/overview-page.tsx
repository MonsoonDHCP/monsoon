import { Activity, CircleGauge, Network, ShieldAlert } from "lucide-react"

import { useDashboard } from "@/app/dashboard-context"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

function metricColor(value: number) {
  if (value >= 80) return "danger"
  if (value >= 60) return "warning"
  return "success"
}

export function OverviewPage() {
  const { leases, health, loading, error } = useDashboard()

  const activeLeases = leases.filter((lease) => ["bound", "renewing"].includes(lease.state)).length
  const offeredLeases = leases.filter((lease) => lease.state === "offered").length
  const declinedLeases = leases.filter((lease) => lease.state === "declined").length
  const subnetUtilization = Math.min(100, Math.round((activeLeases / Math.max(1, activeLeases + 80)) * 100))

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Live Overview</h2>
          <p className="text-sm text-muted-foreground">Real-time DHCP and IPAM posture for your network.</p>
        </div>
        <Badge variant={health?.status === "healthy" ? "success" : "warning"}>{health?.status ?? "unknown"}</Badge>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Card>
          <CardHeader className="pb-2">
            <CardDescription>Active leases</CardDescription>
            <CardTitle className="text-3xl">{activeLeases}</CardTitle>
          </CardHeader>
          <CardContent className="text-xs text-muted-foreground">
            <Activity className="mr-1 inline size-3" />
            Bound + renewing clients
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription>Offers pending</CardDescription>
            <CardTitle className="text-3xl">{offeredLeases}</CardTitle>
          </CardHeader>
          <CardContent className="text-xs text-muted-foreground">
            <Network className="mr-1 inline size-3" />
            Clients in discovery phase
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription>Subnet pressure</CardDescription>
            <CardTitle className="text-3xl">{subnetUtilization}%</CardTitle>
          </CardHeader>
          <CardContent className="text-xs text-muted-foreground">
            <CircleGauge className="mr-1 inline size-3" />
            Estimated address utilization
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription>Conflicts flagged</CardDescription>
            <CardTitle className="text-3xl">{declinedLeases}</CardTitle>
          </CardHeader>
          <CardContent className="text-xs text-muted-foreground">
            <ShieldAlert className="mr-1 inline size-3" />
            Declined leases under review
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Capacity pulse</CardTitle>
            <CardDescription>Visual pressure indicator for current subnet pool.</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="h-3 rounded-full bg-muted">
              <div
                className="h-full rounded-full bg-gradient-to-r from-cyan-500 via-teal-400 to-amber-300 transition-all"
                style={{ width: `${subnetUtilization}%` }}
              />
            </div>
            <div className="mt-3 flex justify-between text-xs text-muted-foreground">
              <span>0%</span>
              <Badge variant={metricColor(subnetUtilization) as "success" | "warning" | "danger"}>{subnetUtilization}%</Badge>
              <span>100%</span>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Service status</CardTitle>
            <CardDescription>Current runtime posture of DHCP service plane.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <div className="flex items-center justify-between rounded-lg bg-muted/50 px-3 py-2">
              <span>DHCPv4</span>
              <Badge variant={health?.components?.dhcpv4?.running ? "success" : "warning"}>
                {health?.components?.dhcpv4?.running ? "Running" : "Stopped"}
              </Badge>
            </div>
            <div className="flex items-center justify-between rounded-lg bg-muted/50 px-3 py-2">
              <span>Listener</span>
              <span className="font-mono text-xs text-muted-foreground">{health?.components?.dhcpv4?.listen ?? "n/a"}</span>
            </div>
            <div className="flex items-center justify-between rounded-lg bg-muted/50 px-3 py-2">
              <span>Data refresh</span>
              <span className="text-muted-foreground">15s polling</span>
            </div>
          </CardContent>
        </Card>
      </div>

      {error && (
        <Card className="border-rose-500/40 bg-rose-500/10">
          <CardHeader>
            <CardTitle className="text-rose-200">API unreachable</CardTitle>
            <CardDescription className="text-rose-100/80">{error}</CardDescription>
          </CardHeader>
        </Card>
      )}

      {loading && <p className="text-sm text-muted-foreground">Loading telemetry...</p>}
    </div>
  )
}
