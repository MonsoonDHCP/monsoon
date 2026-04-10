import { Plus, ShieldCheck, Trash2 } from "lucide-react"
import { useMemo, useState } from "react"

import { useDashboard } from "@/app/dashboard-context"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import type { UpsertReservationPayload } from "@/types/api"

const emptyForm: UpsertReservationPayload = {
  mac: "",
  ip: "",
  hostname: "",
  subnet_cidr: "",
}

export function ReservationsPage() {
  const { reservations, saveReservation, removeReservation, canMutate } = useDashboard()
  const [form, setForm] = useState<UpsertReservationPayload>(emptyForm)
  const [query, setQuery] = useState("")

  const filtered = useMemo(() => {
    const needle = query.toLowerCase().trim()
    if (!needle) return reservations
    return reservations.filter((item) => [item.ip, item.mac, item.hostname ?? "", item.subnet_cidr].join(" ").toLowerCase().includes(needle))
  }, [reservations, query])

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Reservation Manager</h2>
        <p className="text-sm text-muted-foreground">Maintain fixed MAC to IP bindings with persistent backend storage.</p>
        {!canMutate && <Badge className="mt-2" variant="warning">Read-only role</Badge>}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Create or update reservation</CardTitle>
          <CardDescription>Writes to /api/v1/reservations</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 md:grid-cols-2">
          <input
            className="rounded-lg border border-border/70 bg-background px-3 py-2 text-sm"
            placeholder="MAC (AA:BB:CC:DD:EE:FF)"
            value={form.mac}
            onChange={(event) => setForm((prev) => ({ ...prev, mac: event.target.value }))}
            disabled={!canMutate}
          />
          <input
            className="rounded-lg border border-border/70 bg-background px-3 py-2 text-sm"
            placeholder="IP (10.0.1.10)"
            value={form.ip}
            onChange={(event) => setForm((prev) => ({ ...prev, ip: event.target.value }))}
            disabled={!canMutate}
          />
          <input
            className="rounded-lg border border-border/70 bg-background px-3 py-2 text-sm"
            placeholder="Hostname (optional)"
            value={form.hostname}
            onChange={(event) => setForm((prev) => ({ ...prev, hostname: event.target.value }))}
            disabled={!canMutate}
          />
          <input
            className="rounded-lg border border-border/70 bg-background px-3 py-2 text-sm"
            placeholder="Subnet CIDR (optional)"
            value={form.subnet_cidr}
            onChange={(event) => setForm((prev) => ({ ...prev, subnet_cidr: event.target.value }))}
            disabled={!canMutate}
          />
          <div className="md:col-span-2 flex justify-end">
            <Button
              disabled={!canMutate}
              onClick={() =>
                void saveReservation(form).then(() => {
                  setForm(emptyForm)
                })
              }
            >
              <Plus className="mr-2 size-4" />
              Save reservation
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <ShieldCheck className="size-4 text-emerald-400" />
            Active reservations
          </CardTitle>
          <CardDescription>{filtered.length} records</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <input
            className="w-full rounded-lg border border-border/70 bg-background px-3 py-2 text-sm"
            placeholder="Search reservation list..."
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
          {filtered.map((item) => (
            <div key={`${item.mac}-${item.ip}`} className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border/70 bg-background/70 p-3">
              <div>
                <p className="font-mono text-xs text-cyan-300">{item.ip}</p>
                <p className="mt-1 text-sm">{item.hostname || "(no hostname)"}</p>
                <p className="mt-1 font-mono text-xs text-muted-foreground">
                  {item.mac} | {item.subnet_cidr}
                </p>
              </div>
              <div className="flex items-center gap-2">
                <Badge variant="success">Reserved</Badge>
                <Button variant="outline" size="sm" onClick={() => void removeReservation(item.mac)} disabled={!canMutate}>
                  <Trash2 className="mr-2 size-4" />
                  Delete
                </Button>
              </div>
            </div>
          ))}
          {filtered.length === 0 && <p className="text-sm text-muted-foreground">No reservations found.</p>}
        </CardContent>
      </Card>
    </div>
  )
}
