import { Plus, Trash2 } from "lucide-react"
import { useEffect, useState } from "react"

import { useDashboard } from "@/app/dashboard-context"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import type { UpsertSubnetPayload } from "@/types/api"

const defaultForm: UpsertSubnetPayload = {
  cidr: "",
  name: "",
  vlan: 0,
  gateway: "",
  dns: [],
  dhcp_enabled: true,
  pool_start: "",
  pool_end: "",
  lease_time_sec: 43200,
}

export function SubnetsPage() {
  const { subnets, subnetRecords, saveSubnet, removeSubnet, canMutate } = useDashboard()
  const [form, setForm] = useState<UpsertSubnetPayload>(defaultForm)
  const [dnsInput, setDnsInput] = useState("")

  useEffect(() => {
    setDnsInput(form.dns.join(", "))
  }, [form.dns])

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Subnet Topology</h2>
        <p className="text-sm text-muted-foreground">Manage subnet inventory and monitor utilization in one place.</p>
        {!canMutate && <Badge className="mt-2" variant="warning">Read-only role</Badge>}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Create or update subnet</CardTitle>
          <CardDescription>Writes to /api/v1/subnets</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 md:grid-cols-2">
          <input className="rounded-lg border border-border/70 bg-background px-3 py-2 text-sm" placeholder="CIDR (10.0.1.0/24)" value={form.cidr} onChange={(e) => setForm((s) => ({ ...s, cidr: e.target.value }))} disabled={!canMutate} />
          <input className="rounded-lg border border-border/70 bg-background px-3 py-2 text-sm" placeholder="Name" value={form.name} onChange={(e) => setForm((s) => ({ ...s, name: e.target.value }))} disabled={!canMutate} />
          <input className="rounded-lg border border-border/70 bg-background px-3 py-2 text-sm" placeholder="Gateway" value={form.gateway} onChange={(e) => setForm((s) => ({ ...s, gateway: e.target.value }))} disabled={!canMutate} />
          <input className="rounded-lg border border-border/70 bg-background px-3 py-2 text-sm" placeholder="VLAN" type="number" value={form.vlan} onChange={(e) => setForm((s) => ({ ...s, vlan: Number(e.target.value) }))} disabled={!canMutate} />
          <input className="rounded-lg border border-border/70 bg-background px-3 py-2 text-sm" placeholder="Pool start" value={form.pool_start} onChange={(e) => setForm((s) => ({ ...s, pool_start: e.target.value }))} disabled={!canMutate} />
          <input className="rounded-lg border border-border/70 bg-background px-3 py-2 text-sm" placeholder="Pool end" value={form.pool_end} onChange={(e) => setForm((s) => ({ ...s, pool_end: e.target.value }))} disabled={!canMutate} />
          <input
            className="rounded-lg border border-border/70 bg-background px-3 py-2 text-sm md:col-span-2"
            placeholder="DNS list (comma separated)"
            value={dnsInput}
            onChange={(e) => {
              const value = e.target.value
              setDnsInput(value)
              setForm((s) => ({ ...s, dns: value.split(",").map((x) => x.trim()).filter(Boolean) }))
            }}
            disabled={!canMutate}
          />
          <label className="flex items-center gap-2 text-sm text-muted-foreground">
            <input type="checkbox" checked={form.dhcp_enabled} onChange={(e) => setForm((s) => ({ ...s, dhcp_enabled: e.target.checked }))} disabled={!canMutate} />
            DHCP enabled
          </label>
          <div className="flex justify-end md:col-span-2">
            <Button
              onClick={() =>
                void saveSubnet(form).then(() => {
                  setForm(defaultForm)
                  setDnsInput("")
                })
              }
              disabled={!canMutate}
            >
              <Plus className="mr-2 size-4" />
              Save subnet
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Utilization view</CardTitle>
          <CardDescription>Summary from /api/v1/subnets</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {subnets.length === 0 && <p className="text-sm text-muted-foreground">No subnet activity yet.</p>}
          {subnets.map((subnet) => {
            const util = Math.min(100, Math.max(0, subnet.utilization))
            return (
              <div key={subnet.cidr} className="rounded-xl border border-border/70 bg-muted/30 p-3">
                <div className="mb-2 flex items-center justify-between gap-2">
                  <p className="font-mono text-xs text-muted-foreground">{subnet.cidr}</p>
                  <Badge variant={util >= 75 ? "warning" : "success"}>{subnet.active_leases} active</Badge>
                </div>
                <div className="h-2 rounded-full bg-muted">
                  <div className="h-full rounded-full bg-gradient-to-r from-cyan-500 to-teal-400" style={{ width: `${util}%` }} />
                </div>
                <p className="mt-2 text-xs text-muted-foreground">
                  {subnet.name || "(unnamed)"} | VLAN {subnet.vlan} | total leases: {subnet.total_leases} | utilization: {util}%
                </p>
              </div>
            )
          })}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Subnet records</CardTitle>
          <CardDescription>Raw stored subnet objects from /api/v1/subnets/raw</CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">
          {subnetRecords.map((record) => (
            <div key={record.cidr} className="flex items-center justify-between rounded-lg border border-border/70 bg-background/70 px-3 py-2">
              <div>
                <p className="font-mono text-xs text-muted-foreground">{record.cidr}</p>
                <p className="text-sm">{record.name || "(unnamed)"}</p>
              </div>
              <Button variant="outline" size="sm" onClick={() => void removeSubnet(record.cidr)} disabled={!canMutate}>
                <Trash2 className="mr-2 size-4" />
                Delete
              </Button>
            </div>
          ))}
        </CardContent>
      </Card>
    </div>
  )
}
