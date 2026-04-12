import { zodResolver } from "@hookform/resolvers/zod"
import { Loader2, Plus, Search, ShieldCheck, Trash2 } from "lucide-react"
import { useMemo, useState } from "react"
import { useForm } from "react-hook-form"
import { z } from "zod"

import { useDashboard } from "@/app/dashboard-context"
import { EmptyState } from "@/components/shared/empty-state"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

const macPattern = /^(?:[0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}$/
const ipv4Pattern = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/
const cidrPattern = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}\/([0-9]|[12]\d|3[0-2])$/

const reservationSchema = z.object({
  mac: z.string().trim().regex(macPattern, "Enter a valid MAC address"),
  ip: z.string().trim().regex(ipv4Pattern, "Enter a valid IPv4 address"),
  hostname: z.string().trim().max(255, "Hostname must be 255 characters or fewer").optional().or(z.literal("")),
  subnet_cidr: z
    .string()
    .trim()
    .refine((value) => value === "" || cidrPattern.test(value), "Enter a valid subnet CIDR")
    .optional()
    .or(z.literal("")),
})

type ReservationFormValues = z.infer<typeof reservationSchema>

const emptyForm: ReservationFormValues = {
  mac: "",
  ip: "",
  hostname: "",
  subnet_cidr: "",
}

export function ReservationsPage() {
  const { reservations, saveReservation, removeReservation, canMutate } = useDashboard()
  const [query, setQuery] = useState("")

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting, isValid },
  } = useForm<ReservationFormValues>({
    resolver: zodResolver(reservationSchema),
    mode: "onChange",
    defaultValues: emptyForm,
  })

  const filtered = useMemo(() => {
    const needle = query.toLowerCase().trim()
    if (!needle) return reservations
    return reservations.filter((item) => [item.ip, item.mac, item.hostname ?? "", item.subnet_cidr].join(" ").toLowerCase().includes(needle))
  }, [reservations, query])

  const onSubmit = handleSubmit(async (values) => {
    try {
      await saveReservation({
        mac: values.mac.trim().toUpperCase().replaceAll("-", ":"),
        ip: values.ip.trim(),
        hostname: values.hostname?.trim() ?? "",
        subnet_cidr: values.subnet_cidr?.trim() || undefined,
      })
      reset(emptyForm)
    } catch {
      return
    }
  })

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Reservation Manager</h2>
        <p className="text-sm text-muted-foreground">Maintain fixed MAC to IP bindings with persistent backend storage.</p>
        {!canMutate ? <Badge className="mt-2" variant="warning">Read-only role</Badge> : null}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Create or update reservation</CardTitle>
          <CardDescription>Validated with React Hook Form and Zod before writing to `/api/v1/reservations`.</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="grid gap-4 md:grid-cols-2" onSubmit={(event) => void onSubmit(event)}>
            <div className="space-y-2">
              <Label htmlFor="reservation-mac">MAC address</Label>
              <Input
                id="reservation-mac"
                placeholder="AA:BB:CC:DD:EE:FF"
                disabled={!canMutate || isSubmitting}
                aria-invalid={errors.mac ? "true" : "false"}
                {...register("mac")}
              />
              {errors.mac ? <p className="text-xs text-destructive">{errors.mac.message}</p> : null}
            </div>

            <div className="space-y-2">
              <Label htmlFor="reservation-ip">IPv4 address</Label>
              <Input
                id="reservation-ip"
                placeholder="10.0.1.10"
                disabled={!canMutate || isSubmitting}
                aria-invalid={errors.ip ? "true" : "false"}
                {...register("ip")}
              />
              {errors.ip ? <p className="text-xs text-destructive">{errors.ip.message}</p> : null}
            </div>

            <div className="space-y-2">
              <Label htmlFor="reservation-hostname">Hostname</Label>
              <Input
                id="reservation-hostname"
                placeholder="optional-hostname"
                disabled={!canMutate || isSubmitting}
                aria-invalid={errors.hostname ? "true" : "false"}
                {...register("hostname")}
              />
              {errors.hostname ? <p className="text-xs text-destructive">{errors.hostname.message}</p> : null}
            </div>

            <div className="space-y-2">
              <Label htmlFor="reservation-subnet">Subnet CIDR</Label>
              <Input
                id="reservation-subnet"
                placeholder="10.0.1.0/24"
                disabled={!canMutate || isSubmitting}
                aria-invalid={errors.subnet_cidr ? "true" : "false"}
                {...register("subnet_cidr")}
              />
              {errors.subnet_cidr ? <p className="text-xs text-destructive">{errors.subnet_cidr.message}</p> : null}
            </div>

            <div className="flex justify-end md:col-span-2">
              <Button type="submit" disabled={!canMutate || !isValid || isSubmitting} className="min-w-40">
                {isSubmitting ? <Loader2 className="mr-2 size-4 animate-spin" /> : <Plus className="mr-2 size-4" />}
                Save reservation
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <ShieldCheck className="size-4 text-success" />
            Active reservations
          </CardTitle>
          <CardDescription>{filtered.length} records</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex items-center gap-2 rounded-xl border border-border/70 bg-muted/30 px-3">
            <Search className="size-4 text-muted-foreground" />
            <Input
              className="border-0 bg-transparent px-0 shadow-none focus-visible:ring-0 focus-visible:ring-offset-0"
              placeholder="Search reservation list..."
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
          </div>

          {filtered.length === 0 ? (
            <EmptyState
              icon={ShieldCheck}
              title="No reservations found"
              description="Create a reservation or widen your search query to see existing bindings."
            />
          ) : null}

          {filtered.map((item) => (
            <div key={`${item.mac}-${item.ip}`} className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border/70 bg-background/70 p-3">
              <div>
                <p className="font-mono text-xs text-info">{item.ip}</p>
                <p className="mt-1 text-sm">{item.hostname || "(no hostname)"}</p>
                <p className="mt-1 font-mono text-xs text-muted-foreground">
                  {item.mac} | {item.subnet_cidr}
                </p>
              </div>
              <div className="flex items-center gap-2">
                <Badge variant="success">Reserved</Badge>
                <AlertDialog>
                  <AlertDialogTrigger asChild>
                    <Button variant="outline" size="sm" disabled={!canMutate}>
                      <Trash2 className="mr-2 size-4" />
                      Delete
                    </Button>
                  </AlertDialogTrigger>
                  <AlertDialogContent>
                    <AlertDialogHeader>
                      <AlertDialogTitle>Delete reservation {item.ip}?</AlertDialogTitle>
                      <AlertDialogDescription>
                        This removes the MAC binding for {item.mac}. Future clients can receive a different address.
                      </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel>Cancel</AlertDialogCancel>
                      <AlertDialogAction className="bg-destructive text-destructive-foreground hover:bg-destructive/90" onClick={() => void removeReservation(item.mac)}>
                        Delete reservation
                      </AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
              </div>
            </div>
          ))}
        </CardContent>
      </Card>
    </div>
  )
}
