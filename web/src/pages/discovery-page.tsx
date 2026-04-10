import { Radar, ShieldAlert, Telescope } from "lucide-react"

import { useDashboard } from "@/app/dashboard-context"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

export function DiscoveryPage() {
  const { discovery, triggerScan } = useDashboard()

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Discovery Operations</h2>
        <p className="text-sm text-muted-foreground">Active scan orchestration and rogue DHCP detection workflow.</p>
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
            <Button onClick={() => void triggerScan()}>Trigger scan</Button>
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
    </div>
  )
}
