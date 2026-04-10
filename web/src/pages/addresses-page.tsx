import { Filter, MapPinned, Search, Waypoints } from "lucide-react"
import { useEffect, useMemo, useState } from "react"

import { useDashboard } from "@/app/dashboard-context"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import type { AddressRecord, AddressState } from "@/types/api"

const stateClass: Record<AddressState, string> = {
  available: "bg-emerald-400/70",
  dhcp: "bg-cyan-400/80",
  reserved: "bg-violet-400/80",
  conflict: "bg-rose-400/80",
  quarantined: "bg-amber-300/80",
}

function stateBadge(state: AddressState): "success" | "warning" | "danger" | "default" {
  if (state === "dhcp" || state === "reserved") return "success"
  if (state === "conflict") return "danger"
  if (state === "quarantined") return "warning"
  return "default"
}

export function AddressesPage() {
  const { subnets, loadAddressesForSubnet } = useDashboard()
  const [rows, setRows] = useState<AddressRecord[]>([])
  const [selectedSubnet, setSelectedSubnet] = useState("all")
  const [query, setQuery] = useState("")

  useEffect(() => {
    let alive = true
    const subnet = selectedSubnet === "all" ? undefined : selectedSubnet

    const refresh = () => {
      void loadAddressesForSubnet(subnet)
        .then((items) => {
          if (alive) {
            setRows(items)
          }
        })
        .catch(() => {
          if (alive) {
            setRows([])
          }
        })
    }

    refresh()
    const timer = setInterval(refresh, 10000)
    return () => {
      alive = false
      clearInterval(timer)
    }
  }, [selectedSubnet, loadAddressesForSubnet])

  const filtered = useMemo(() => {
    const needle = query.toLowerCase().trim()
    if (!needle) return rows
    return rows.filter((record) =>
      [record.ip, record.mac ?? "", record.hostname ?? "", record.subnet_cidr ?? "", record.state].join(" ").toLowerCase().includes(needle),
    )
  }, [rows, query])

  const counts = useMemo(() => {
    const map: Record<AddressState, number> = {
      available: 0,
      dhcp: 0,
      reserved: 0,
      conflict: 0,
      quarantined: 0,
    }
    filtered.forEach((item) => {
      map[item.state] += 1
    })
    return map
  }, [filtered])

  const mapItems = useMemo(() => filtered.slice(0, 256), [filtered])

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Address Intelligence</h2>
        <p className="text-sm text-muted-foreground">Unified view across DHCP leases, reservations and pool availability.</p>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
        {(["dhcp", "reserved", "available", "quarantined", "conflict"] as AddressState[]).map((state) => (
          <Card key={state}>
            <CardHeader className="pb-2">
              <CardDescription className="capitalize">{state}</CardDescription>
              <CardTitle className="text-3xl">{counts[state]}</CardTitle>
            </CardHeader>
            <CardContent>
              <Badge variant={stateBadge(state)} className="capitalize">
                {state}
              </Badge>
            </CardContent>
          </Card>
        ))}
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Filter className="size-4 text-cyan-400" />
            Scope and filter
          </CardTitle>
          <CardDescription>Switch subnet and search by IP, MAC, hostname or state.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 md:grid-cols-[280px_1fr]">
          <select
            value={selectedSubnet}
            onChange={(event) => setSelectedSubnet(event.target.value)}
            className="h-10 rounded-xl border border-border/70 bg-background px-3 text-sm"
          >
            <option value="all">All subnets</option>
            {subnets
              .filter((subnet) => subnet.cidr !== "unassigned")
              .map((subnet) => (
                <option key={subnet.cidr} value={subnet.cidr}>
                  {subnet.cidr} {subnet.name ? `| ${subnet.name}` : ""}
                </option>
              ))}
          </select>

          <div className="flex items-center gap-2 rounded-xl border border-border/70 bg-muted/30 px-3">
            <Search className="size-4 text-muted-foreground" />
            <input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              className="h-10 w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
              placeholder="Search records..."
            />
          </div>
        </CardContent>
      </Card>

      <div className="grid gap-4 xl:grid-cols-[1.2fr_1.8fr]">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <MapPinned className="size-4 text-teal-400" />
              Visual map
            </CardTitle>
            <CardDescription>First 256 records rendered as a compact occupancy map.</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="grid gap-1" style={{ gridTemplateColumns: "repeat(16, minmax(0, 1fr))" }}>
              {mapItems.map((item) => (
                <div
                  key={`${item.ip}-${item.state}`}
                  className={`h-4 rounded-[4px] ${stateClass[item.state]}`}
                  title={`${item.ip} | ${item.state}${item.mac ? ` | ${item.mac}` : ""}${item.hostname ? ` | ${item.hostname}` : ""}`}
                />
              ))}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Waypoints className="size-4 text-violet-300" />
              Address records
            </CardTitle>
            <CardDescription>{filtered.length} records in current scope.</CardDescription>
          </CardHeader>
          <CardContent className="max-h-[620px] overflow-auto">
            <div className="space-y-2">
              {filtered.map((item: AddressRecord) => (
                <div key={`${item.ip}-${item.mac ?? "na"}`} className="rounded-xl border border-border/70 bg-background/60 p-3">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <p className="font-mono text-xs text-cyan-300">{item.ip}</p>
                    <Badge variant={stateBadge(item.state)} className="capitalize">
                      {item.state}
                    </Badge>
                  </div>
                  <p className="mt-2 text-sm">{item.hostname || "-"}</p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    MAC: {item.mac || "-"} | subnet: {item.subnet_cidr || "-"} | source: {item.source || "-"}
                  </p>
                </div>
              ))}
              {filtered.length === 0 && <p className="text-sm text-muted-foreground">No address data available for this scope.</p>}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
