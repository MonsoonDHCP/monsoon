import { Activity, CircleGauge, Network, Workflow } from "lucide-react"

import { useDashboard } from "@/app/dashboard-context"
import { ErrorState } from "@/components/shared/error-state"
import { ProgressBar } from "@/components/shared/progress-bar"
import { RecordListSkeleton } from "@/components/shared/record-list-skeleton"
import { StatsGridSkeleton } from "@/components/shared/stats-grid-skeleton"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"

function metricColor(value: number) {
  if (value >= 80) return "danger"
  if (value >= 60) return "warning"
  return "success"
}

function formatDuration(ns?: number) {
  if (!ns || ns <= 0) return "-"
  const ms = ns / 1_000_000
  if (ms < 1000) return `${Math.round(ms)} ms`
  const sec = ms / 1000
  if (sec < 60) return `${sec.toFixed(sec < 10 ? 1 : 0)} s`
  const min = sec / 60
  return `${min.toFixed(1)} min`
}

function haVariant(peer?: string, role?: string) {
  if (peer !== "connected") return "warning"
  if (role === "primary") return "success"
  return "default"
}

export function OverviewPage() {
  const { leases, health, systemInfo, loading, error } = useDashboard()

  const activeLeases = leases.filter((lease) => ["bound", "renewing"].includes(lease.state)).length
  const offeredLeases = leases.filter((lease) => lease.state === "offered").length
  const subnetUtilization = Math.min(100, Math.round((activeLeases / Math.max(1, activeLeases + 80)) * 100))
  const ha = systemInfo?.ha ?? health?.components?.ha

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Live Overview</h2>
          <p className="text-sm text-muted-foreground">Real-time DHCP and IPAM posture for your network.</p>
        </div>
        <Badge variant={health?.status === "healthy" ? "success" : "warning"}>{health?.status ?? "unknown"}</Badge>
      </div>

      {loading ? <StatsGridSkeleton /> : null}

      {!loading ? (
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
            <CardDescription>HA role</CardDescription>
            <CardTitle className="text-3xl capitalize">{ha?.status === "disabled" ? "Off" : ha?.role ?? "n/a"}</CardTitle>
          </CardHeader>
          <CardContent className="text-xs text-muted-foreground">
            <Workflow className="mr-1 inline size-3" />
            Peer {ha?.peer ?? "unknown"}
          </CardContent>
        </Card>
      </div>
      ) : null}

      {loading ? (
        <div className="grid gap-4 xl:grid-cols-2">
          <div className="rounded-2xl border border-border/70 bg-card p-6 shadow-sm">
            <Skeleton className="h-5 w-32" />
            <Skeleton className="mt-2 h-4 w-56" />
            <Skeleton className="mt-6 h-3 w-full" />
            <div className="mt-3 flex justify-between">
              <Skeleton className="h-3 w-8" />
              <Skeleton className="h-5 w-14 rounded-full" />
              <Skeleton className="h-3 w-8" />
            </div>
          </div>
          <RecordListSkeleton rows={6} />
        </div>
      ) : null}

      {!loading ? (
      <div className="grid gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Capacity pulse</CardTitle>
            <CardDescription>Visual pressure indicator for current subnet pool.</CardDescription>
          </CardHeader>
          <CardContent>
            <ProgressBar
              value={subnetUtilization}
              label="Subnet capacity pulse"
              variant={metricColor(subnetUtilization) as "success" | "warning" | "danger"}
            />
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
            <div className="flex items-center justify-between rounded-lg bg-muted/50 px-3 py-2">
              <span>Uptime</span>
              <span className="text-muted-foreground">{health?.uptime ?? "-"}</span>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>HA posture</CardTitle>
            <CardDescription>Peer health, replication lag, and current failover stance.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <div className="flex items-center justify-between rounded-lg bg-muted/50 px-3 py-2">
              <span>Role</span>
              <Badge variant={haVariant(ha?.peer, ha?.role) as "default" | "success" | "warning"}>
                {ha?.status === "disabled" ? "Disabled" : ha?.role ?? "Unknown"}
              </Badge>
            </div>
            <div className="flex items-center justify-between rounded-lg bg-muted/50 px-3 py-2">
              <span>Peer</span>
              <span className="text-muted-foreground">{ha?.peer_node ? `${ha.peer_node} (${ha.peer ?? "unknown"})` : ha?.peer ?? "unknown"}</span>
            </div>
            <div className="flex items-center justify-between rounded-lg bg-muted/50 px-3 py-2">
              <span>Heartbeat latency</span>
              <span className="text-muted-foreground">{formatDuration(ha?.heartbeat_latency)}</span>
            </div>
            <div className="flex items-center justify-between rounded-lg bg-muted/50 px-3 py-2">
              <span>Sync lag</span>
              <span className="text-muted-foreground">{formatDuration(ha?.sync_lag)}</span>
            </div>
            <div className="flex items-center justify-between rounded-lg bg-muted/50 px-3 py-2">
              <span>Failover count</span>
              <span className="text-muted-foreground">{ha?.failover_count ?? 0}</span>
            </div>
            <div className="flex items-center justify-between rounded-lg bg-muted/50 px-3 py-2">
              <span>Election priority</span>
              <span className="text-muted-foreground">{ha?.priority ?? "-"}</span>
            </div>
            <div className="flex items-center justify-between rounded-lg bg-muted/50 px-3 py-2">
              <span>Fencing</span>
              <Badge variant={ha?.fenced ? "danger" : "success"}>{ha?.fenced ? ha?.fencing_reason ?? "Fenced" : "Clear"}</Badge>
            </div>
            {ha?.manual_step_down_until ? (
              <div className="rounded-lg border border-amber-400/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-200">
                Manual handoff pending until {new Date(ha.manual_step_down_until).toLocaleString()}.
              </div>
            ) : null}
            {ha?.witness_owner ? (
              <div className="rounded-lg border border-border/60 bg-background/40 px-3 py-2 text-xs text-muted-foreground">
                Witness owner: <span className="font-mono text-foreground">{ha.witness_owner}</span>
              </div>
            ) : null}
          </CardContent>
        </Card>
      </div>
      ) : null}

      {error ? <ErrorState title="API unreachable" description={error} /> : null}
    </div>
  )
}
