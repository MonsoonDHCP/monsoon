import { zodResolver } from "@hookform/resolvers/zod"
import { Loader2, Plus, Trash2 } from "lucide-react"
import { useMemo } from "react"
import { useForm } from "react-hook-form"
import { z } from "zod"

import { useDashboard } from "@/app/dashboard-context"
import { EmptyState } from "@/components/shared/empty-state"
import { ProgressBar } from "@/components/shared/progress-bar"
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
import { Switch } from "@/components/ui/switch"
import type { UpsertSubnetPayload } from "@/types/api"

const ipv4Pattern = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/
const cidrPattern = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}\/([0-9]|[12]\d|3[0-2])$/

const subnetSchema = z
  .object({
    cidr: z.string().trim().regex(cidrPattern, "Enter a valid IPv4 CIDR"),
    name: z.string().trim().max(255, "Name must be 255 characters or fewer").optional().or(z.literal("")),
    gateway: z.string().trim().refine((value) => value === "" || ipv4Pattern.test(value), "Enter a valid IPv4 gateway"),
    vlan: z
      .string()
      .trim()
      .refine((value) => /^\d+$/.test(value), "VLAN must be a whole number")
      .refine((value) => {
        const parsed = Number(value)
        return parsed >= 0 && parsed <= 4094
      }, "VLAN must be between 0 and 4094"),
    pool_start: z.string().trim().refine((value) => value === "" || ipv4Pattern.test(value), "Enter a valid pool start IPv4"),
    pool_end: z.string().trim().refine((value) => value === "" || ipv4Pattern.test(value), "Enter a valid pool end IPv4"),
    dnsInput: z.string().trim(),
    dhcp_enabled: z.boolean(),
    lease_time_sec: z
      .string()
      .trim()
      .refine((value) => /^\d+$/.test(value), "Lease time must be a whole number")
      .refine((value) => Number(value) >= 60, "Lease time must be at least 60 seconds"),
  })
  .superRefine((values, ctx) => {
    if (values.dhcp_enabled && values.pool_start === "") {
      ctx.addIssue({ code: "custom", path: ["pool_start"], message: "Pool start is required when DHCP is enabled" })
    }
    if (values.dhcp_enabled && values.pool_end === "") {
      ctx.addIssue({ code: "custom", path: ["pool_end"], message: "Pool end is required when DHCP is enabled" })
    }
    if (values.dnsInput !== "") {
      const invalidValue = values.dnsInput
        .split(",")
        .map((item) => item.trim())
        .filter(Boolean)
        .find((item) => !ipv4Pattern.test(item))
      if (invalidValue) {
        ctx.addIssue({ code: "custom", path: ["dnsInput"], message: `Invalid DNS IP: ${invalidValue}` })
      }
    }
  })

type SubnetFormValues = z.infer<typeof subnetSchema>

const defaultForm: SubnetFormValues = {
  cidr: "",
  name: "",
  vlan: "0",
  gateway: "",
  dnsInput: "",
  dhcp_enabled: true,
  pool_start: "",
  pool_end: "",
  lease_time_sec: "43200",
}

function toSubnetPayload(values: SubnetFormValues): UpsertSubnetPayload {
  return {
    cidr: values.cidr.trim(),
    name: values.name?.trim() ?? "",
    vlan: Number(values.vlan),
    gateway: values.gateway.trim(),
    dns: values.dnsInput
      .split(",")
      .map((item) => item.trim())
      .filter(Boolean),
    dhcp_enabled: values.dhcp_enabled,
    pool_start: values.pool_start.trim(),
    pool_end: values.pool_end.trim(),
    lease_time_sec: Number(values.lease_time_sec),
  }
}

export function SubnetsPage() {
  const { subnets, subnetRecords, saveSubnet, removeSubnet, canMutate } = useDashboard()
  const {
    register,
    handleSubmit,
    reset,
    setValue,
    watch,
    formState: { errors, isSubmitting, isValid },
  } = useForm<SubnetFormValues>({
    resolver: zodResolver(subnetSchema),
    mode: "onChange",
    defaultValues: defaultForm,
  })

  const dhcpEnabled = watch("dhcp_enabled")

  const onSubmit = handleSubmit(async (values) => {
    try {
      await saveSubnet(toSubnetPayload(values))
      reset(defaultForm)
    } catch {
      return
    }
  })

  const sortedSubnets = useMemo(() => [...subnets].sort((left, right) => right.utilization - left.utilization), [subnets])

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Subnet Topology</h2>
        <p className="text-sm text-muted-foreground">Manage subnet inventory and monitor utilization in one place.</p>
        {!canMutate ? <Badge className="mt-2" variant="warning">Read-only role</Badge> : null}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Create or update subnet</CardTitle>
          <CardDescription>Validated with React Hook Form and Zod before writing to `/api/v1/subnets`.</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="grid gap-4 md:grid-cols-2" onSubmit={(event) => void onSubmit(event)}>
            <div className="space-y-2">
              <Label htmlFor="subnet-cidr">Subnet CIDR</Label>
              <Input
                id="subnet-cidr"
                placeholder="10.0.1.0/24"
                disabled={!canMutate || isSubmitting}
                aria-invalid={errors.cidr ? "true" : "false"}
                {...register("cidr")}
              />
              {errors.cidr ? <p className="text-xs text-destructive">{errors.cidr.message}</p> : null}
            </div>

            <div className="space-y-2">
              <Label htmlFor="subnet-name">Display name</Label>
              <Input
                id="subnet-name"
                placeholder="Edge VLAN"
                disabled={!canMutate || isSubmitting}
                aria-invalid={errors.name ? "true" : "false"}
                {...register("name")}
              />
              {errors.name ? <p className="text-xs text-destructive">{errors.name.message}</p> : null}
            </div>

            <div className="space-y-2">
              <Label htmlFor="subnet-gateway">Gateway</Label>
              <Input
                id="subnet-gateway"
                placeholder="10.0.1.1"
                disabled={!canMutate || isSubmitting}
                aria-invalid={errors.gateway ? "true" : "false"}
                {...register("gateway")}
              />
              {errors.gateway ? <p className="text-xs text-destructive">{errors.gateway.message}</p> : null}
            </div>

            <div className="space-y-2">
              <Label htmlFor="subnet-vlan">VLAN</Label>
              <Input
                id="subnet-vlan"
                type="number"
                min={0}
                max={4094}
                disabled={!canMutate || isSubmitting}
                aria-invalid={errors.vlan ? "true" : "false"}
                {...register("vlan")}
              />
              {errors.vlan ? <p className="text-xs text-destructive">{errors.vlan.message}</p> : null}
            </div>

            <div className="space-y-2 md:col-span-2">
              <Label htmlFor="subnet-dns">DNS servers</Label>
              <Input
                id="subnet-dns"
                placeholder="10.0.1.53, 1.1.1.1"
                disabled={!canMutate || isSubmitting}
                aria-invalid={errors.dnsInput ? "true" : "false"}
                {...register("dnsInput")}
              />
              {errors.dnsInput ? <p className="text-xs text-destructive">{errors.dnsInput.message}</p> : null}
            </div>

            <div className="space-y-2">
              <Label htmlFor="subnet-pool-start">Pool start</Label>
              <Input
                id="subnet-pool-start"
                placeholder="10.0.1.10"
                disabled={!canMutate || isSubmitting || !dhcpEnabled}
                aria-invalid={errors.pool_start ? "true" : "false"}
                {...register("pool_start")}
              />
              {errors.pool_start ? <p className="text-xs text-destructive">{errors.pool_start.message}</p> : null}
            </div>

            <div className="space-y-2">
              <Label htmlFor="subnet-pool-end">Pool end</Label>
              <Input
                id="subnet-pool-end"
                placeholder="10.0.1.220"
                disabled={!canMutate || isSubmitting || !dhcpEnabled}
                aria-invalid={errors.pool_end ? "true" : "false"}
                {...register("pool_end")}
              />
              {errors.pool_end ? <p className="text-xs text-destructive">{errors.pool_end.message}</p> : null}
            </div>

            <div className="space-y-2">
              <Label htmlFor="subnet-lease-time">Lease time in seconds</Label>
              <Input
                id="subnet-lease-time"
                type="number"
                min={60}
                disabled={!canMutate || isSubmitting}
                aria-invalid={errors.lease_time_sec ? "true" : "false"}
                {...register("lease_time_sec")}
              />
              {errors.lease_time_sec ? <p className="text-xs text-destructive">{errors.lease_time_sec.message}</p> : null}
            </div>

            <div className="flex items-end">
              <div className="flex min-h-10 w-full items-center justify-between rounded-xl border border-border/70 bg-muted/30 px-3 py-2">
                <div className="space-y-1">
                  <Label htmlFor="subnet-dhcp-switch">DHCP enabled</Label>
                  <p className="text-xs text-muted-foreground">Allow dynamic lease allocation in this subnet.</p>
                </div>
                <Switch
                  id="subnet-dhcp-switch"
                  checked={dhcpEnabled}
                  onCheckedChange={(checked) => setValue("dhcp_enabled", checked, { shouldDirty: true, shouldValidate: true })}
                  disabled={!canMutate || isSubmitting}
                  aria-label="Toggle DHCP for this subnet"
                />
              </div>
            </div>

            <div className="flex justify-end md:col-span-2">
              <Button type="submit" disabled={!canMutate || !isValid || isSubmitting} className="min-w-40">
                {isSubmitting ? <Loader2 className="mr-2 size-4 animate-spin" /> : <Plus className="mr-2 size-4" />}
                Save subnet
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Utilization view</CardTitle>
          <CardDescription>Summary from /api/v1/subnets</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {sortedSubnets.length === 0 ? (
            <EmptyState
              icon={Plus}
              title="No subnets yet"
              description="Create your first subnet to start DHCP and IPAM management."
            />
          ) : null}
          {sortedSubnets.map((subnet) => {
            const util = Math.min(100, Math.max(0, subnet.utilization))
            return (
              <div key={subnet.cidr} className="rounded-xl border border-border/70 bg-muted/30 p-3">
                <div className="mb-2 flex items-center justify-between gap-2">
                  <p className="font-mono text-xs text-muted-foreground">{subnet.cidr}</p>
                  <Badge variant={util >= 75 ? "warning" : "success"}>{subnet.active_leases} active</Badge>
                </div>
                <ProgressBar value={util} label={`${subnet.cidr} utilization`} variant={util >= 75 ? "warning" : "success"} className="h-2" />
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
          {subnetRecords.length === 0 ? (
            <EmptyState
              icon={Trash2}
              title="No subnet records"
              description="The raw subnet store is empty right now."
            />
          ) : null}
          {subnetRecords.map((record) => (
            <div key={record.cidr} className="flex items-center justify-between rounded-lg border border-border/70 bg-background/70 px-3 py-2">
              <div>
                <p className="font-mono text-xs text-muted-foreground">{record.cidr}</p>
                <p className="text-sm">{record.name || "(unnamed)"}</p>
              </div>
              <AlertDialog>
                <AlertDialogTrigger asChild>
                  <Button variant="outline" size="sm" disabled={!canMutate}>
                    <Trash2 className="mr-2 size-4" />
                    Delete
                  </Button>
                </AlertDialogTrigger>
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>Delete subnet {record.cidr}?</AlertDialogTitle>
                    <AlertDialogDescription>
                      This removes the subnet record {record.name ? `for ${record.name}` : ""} from inventory and can disrupt future address management.
                    </AlertDialogDescription>
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <AlertDialogCancel>Cancel</AlertDialogCancel>
                    <AlertDialogAction className="bg-destructive text-destructive-foreground hover:bg-destructive/90" onClick={() => void removeSubnet(record.cidr)}>
                      Delete subnet
                    </AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>
            </div>
          ))}
        </CardContent>
      </Card>
    </div>
  )
}
