import { zodResolver } from "@hookform/resolvers/zod"
import { ArrowRightLeft, Copy, HardDriveDownload, KeyRound, Loader2, LogIn, LogOut, MoonStar, Palette, RefreshCw, Save, Server, Shield, SunMedium, Trash2, UserRound, Workflow } from "lucide-react"
import { useEffect, useMemo, useState } from "react"
import { useForm } from "react-hook-form"
import { z } from "zod"

import { useDashboard } from "@/app/dashboard-context"
import { ThemeToggle } from "@/components/layout/theme-toggle"
import { EmptyState } from "@/components/shared/empty-state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { useTheme } from "@/hooks/use-theme"
import type { UISettings } from "@/types/api"

function formatDuration(ns?: number) {
  if (!ns || ns <= 0) return "-"
  const ms = ns / 1_000_000
  if (ms < 1000) return `${Math.round(ms)} ms`
  const sec = ms / 1000
  if (sec < 60) return `${sec.toFixed(sec < 10 ? 1 : 0)} s`
  const min = sec / 60
  return `${min.toFixed(1)} min`
}

const settingsSchema = z.object({
  theme: z.enum(["light", "dark", "system"]),
  density: z.enum(["compact", "comfortable"]),
  auto_refresh: z.boolean(),
})

const authSchema = z.object({
  username: z.string().trim().min(1, "Username is required"),
  password: z.string().min(1, "Password is required"),
})

const tokenSchema = z.object({
  name: z.string().trim().min(1, "Token name is required").max(120, "Token name must be 120 characters or fewer"),
  role: z.enum(["admin", "operator", "viewer"]),
  expires_in_hours: z
    .string()
    .trim()
    .refine((value) => value === "" || /^\d+$/.test(value), "Expiration must be a whole number")
    .refine((value) => value === "" || Number(value) > 0, "Expiration must be greater than zero"),
  description: z.string().trim().max(255, "Description must be 255 characters or fewer"),
})

const haSchema = z.object({
  reason: z.string().trim().min(3, "Provide a short reason for the failover").max(200, "Reason must be 200 characters or fewer"),
})

type SettingsFormValues = z.infer<typeof settingsSchema>
type AuthFormValues = z.infer<typeof authSchema>
type TokenFormValues = z.infer<typeof tokenSchema>
type HAFormValues = z.infer<typeof haSchema>

export function SettingsPage() {
  const { settings, saveSettings, currentUser, authTokens, tokenSecret, loginWithPassword, bootstrapAndLogin, logoutCurrentUser, createToken, revokeToken, canMutate, isAdmin, systemInfo, health, systemConfig, backups, createBackup, restoreBackup, refreshBackups, refreshSystemConfig, saveSystemConfig, requestManualFailover } = useDashboard()
  const { setTheme } = useTheme()
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">("idle")
  const [configDraft, setConfigDraft] = useState("")
  const [configEditing, setConfigEditing] = useState(false)
  const [configError, setConfigError] = useState<string | null>(null)
  const [haActionState, setHAActionState] = useState<"idle" | "done" | "failed">("idle")
  const [haActionError, setHAActionError] = useState<string | null>(null)

  const settingsForm = useForm<SettingsFormValues>({
    resolver: zodResolver(settingsSchema),
    mode: "onChange",
    defaultValues: {
      theme: "system",
      density: "comfortable",
      auto_refresh: true,
    },
  })
  const authForm = useForm<AuthFormValues>({
    resolver: zodResolver(authSchema),
    mode: "onChange",
    defaultValues: {
      username: "admin",
      password: "",
    },
  })
  const tokenForm = useForm<TokenFormValues>({
    resolver: zodResolver(tokenSchema),
    mode: "onChange",
    defaultValues: {
      name: "",
      role: "operator",
      expires_in_hours: "24",
      description: "",
    },
  })
  const haForm = useForm<HAFormValues>({
    resolver: zodResolver(haSchema),
    mode: "onChange",
    defaultValues: {
      reason: "Planned maintenance",
    },
  })

  const systemConfigJSON = useMemo(() => JSON.stringify(systemConfig ?? {}, null, 2), [systemConfig])
  const ha = systemInfo?.ha ?? health?.components?.ha
  const haConfig = (systemConfig?.ha as Record<string, unknown> | undefined) ?? undefined
  const authConfig = (systemConfig?.auth as Record<string, unknown> | undefined) ?? undefined
  const configuredAuthType = typeof authConfig?.type === "string" ? authConfig.type.trim().toLowerCase() : "local"
  const localAuthAvailable = configuredAuthType === "" || configuredAuthType === "local"
  const canTriggerFailover = Boolean(isAdmin && canMutate && ha?.role === "primary" && ha?.peer === "connected")
  const liveStatus = copyState === "copied"
    ? "Configuration copied to clipboard."
    : copyState === "failed"
      ? "Configuration copy failed."
      : haActionState === "done"
        ? "Manual failover request sent."
        : haActionState === "failed"
          ? haActionError ?? "Manual failover request failed."
          : configError ?? ""

  useEffect(() => {
    if (settings) {
      settingsForm.reset(settings)
    }
  }, [settings, settingsForm])

  useEffect(() => {
    if (!configEditing) {
      setConfigDraft(systemConfigJSON)
      setConfigError(null)
    }
  }, [configEditing, systemConfigJSON])

  const copyConfig = async () => {
    try {
      await navigator.clipboard.writeText(configDraft || systemConfigJSON)
      setCopyState("copied")
    } catch {
      setCopyState("failed")
    }
    setTimeout(() => setCopyState("idle"), 1400)
  }

  const saveConfigDraft = async () => {
    try {
      setConfigError(null)
      const parsed = JSON.parse(configDraft) as Record<string, unknown>
      await saveSystemConfig(parsed)
      setConfigEditing(false)
    } catch (err) {
      setConfigError(err instanceof Error ? err.message : "Invalid configuration JSON")
    }
  }

  const submitSettings = settingsForm.handleSubmit(async (values) => {
    await saveSettings(values as UISettings)
  })

  const submitLogin = authForm.handleSubmit(async (values) => {
    await loginWithPassword(values.username, values.password)
    authForm.reset({ ...values, password: "" })
  })

  const submitBootstrap = authForm.handleSubmit(async (values) => {
    await bootstrapAndLogin(values.username, values.password)
    authForm.reset({ ...values, password: "" })
  })

  const submitToken = tokenForm.handleSubmit(async (values) => {
    await createToken({
      name: values.name.trim(),
      role: values.role,
      expires_in_hours: values.expires_in_hours === "" ? undefined : Number(values.expires_in_hours),
      description: values.description.trim() || undefined,
    })
    tokenForm.reset({
      name: "",
      role: values.role,
      expires_in_hours: "24",
      description: "",
    })
  })

  const restoreSnapshot = async (backupName: string) => {
    try {
      await restoreBackup({ name: backupName })
    } catch (error) {
      setConfigError(error instanceof Error ? error.message : "Restore failed")
    }
  }

  const triggerManualHandover = haForm.handleSubmit(async (values) => {
    try {
      setHAActionError(null)
      await requestManualFailover(values.reason)
      setHAActionState("done")
      window.setTimeout(() => setHAActionState("idle"), 1800)
    } catch (err) {
      setHAActionState("failed")
      setHAActionError(err instanceof Error ? err.message : "Manual failover failed")
    }
  })

  return (
    <div className="space-y-6">
      <p aria-live="polite" className="sr-only">
        {liveStatus}
      </p>

      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Settings</h2>
        <p className="text-sm text-muted-foreground">Interface preferences and operator-level controls.</p>
        {!canMutate && currentUser ? <Badge className="mt-2" variant="warning">Read-only role</Badge> : null}
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Palette className="size-4" />
            Theme preferences
          </CardTitle>
          <CardDescription>Choose dark, light, or system appearance.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex flex-wrap gap-2">
            {(["light", "dark", "system"] as const).map((theme) => (
              <Button
                key={theme}
                variant={settingsForm.watch("theme") === theme ? "default" : "outline"}
                size="sm"
                onClick={() => {
                  setTheme(theme)
                  settingsForm.setValue("theme", theme, { shouldDirty: true, shouldValidate: true })
                }}
                className="capitalize"
                disabled={!canMutate || settingsForm.formState.isSubmitting}
              >
                {theme}
              </Button>
            ))}
            <ThemeToggle onThemeChange={(theme) => settingsForm.setValue("theme", theme, { shouldDirty: true, shouldValidate: true })} />
          </div>
          <div className="flex items-center gap-3 text-sm text-muted-foreground">
            <SunMedium className="size-4 text-amber-300" />
            <MoonStar className="size-4 text-cyan-300" />
            Responsive adaptive gradients enabled
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>UI preferences</CardTitle>
          <CardDescription>Stored via /api/v1/settings/ui</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <form className="space-y-4" onSubmit={(event) => void submitSettings(event)}>
            <div className="flex items-center justify-between rounded-lg border border-border/70 p-3">
              <div className="space-y-1">
                <Label htmlFor="density-switch">Compact density</Label>
                <p className="text-xs text-muted-foreground">Reduce spacing across the dashboard.</p>
              </div>
              <Switch
                id="density-switch"
                checked={settingsForm.watch("density") === "compact"}
                onCheckedChange={(checked) => settingsForm.setValue("density", checked ? "compact" : "comfortable", { shouldDirty: true, shouldValidate: true })}
                disabled={!canMutate || settingsForm.formState.isSubmitting}
                aria-label="Toggle compact density"
              />
            </div>

            <div className="flex items-center justify-between rounded-lg border border-border/70 p-3">
              <div className="space-y-1">
                <Label htmlFor="refresh-switch">Auto refresh</Label>
                <p className="text-xs text-muted-foreground">Refresh live dashboard data automatically.</p>
              </div>
              <Switch
                id="refresh-switch"
                checked={settingsForm.watch("auto_refresh")}
                onCheckedChange={(checked) => settingsForm.setValue("auto_refresh", checked, { shouldDirty: true, shouldValidate: true })}
                disabled={!canMutate || settingsForm.formState.isSubmitting}
                aria-label="Toggle auto refresh"
              />
            </div>

            <Button type="submit" disabled={!canMutate || !settingsForm.formState.isValid || settingsForm.formState.isSubmitting}>
              {settingsForm.formState.isSubmitting ? <Loader2 className="mr-2 size-4 animate-spin" /> : <Save className="mr-2 size-4" />}
              Save preferences
            </Button>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Shield className="size-4 text-cyan-400" />
            Authentication
          </CardTitle>
          <CardDescription>{localAuthAvailable ? "Local session login and API token controls." : `Configured auth mode: ${configuredAuthType}. This build exposes local auth controls only.`}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {currentUser ? (
            <div className="rounded-xl border border-border/70 bg-muted/30 p-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <p className="text-sm font-medium">{currentUser.username}</p>
                  <p className="text-xs text-muted-foreground">role: {currentUser.role}</p>
                </div>
                <div className="flex items-center gap-2">
                  <Badge variant="success">Authenticated</Badge>
                  <Button variant="outline" size="sm" onClick={() => void logoutCurrentUser()}>
                    <LogOut className="mr-2 size-4" />
                    Logout
                  </Button>
                </div>
              </div>
            </div>
          ) : localAuthAvailable ? (
            <form className="grid gap-3 md:grid-cols-2" onSubmit={(event) => void submitLogin(event)}>
              <div className="space-y-2">
                <Label htmlFor="auth-username">Username</Label>
                <Input
                  id="auth-username"
                  placeholder="admin"
                  aria-invalid={authForm.formState.errors.username ? "true" : "false"}
                  {...authForm.register("username")}
                />
                {authForm.formState.errors.username ? <p className="text-xs text-destructive">{authForm.formState.errors.username.message}</p> : null}
              </div>
              <div className="space-y-2">
                <Label htmlFor="auth-password">Password</Label>
                <Input
                  id="auth-password"
                  type="password"
                  placeholder="Password"
                  aria-invalid={authForm.formState.errors.password ? "true" : "false"}
                  {...authForm.register("password")}
                />
                {authForm.formState.errors.password ? <p className="text-xs text-destructive">{authForm.formState.errors.password.message}</p> : null}
              </div>
              <div className="md:col-span-2 flex flex-wrap justify-end gap-2">
                <Button type="button" variant="outline" onClick={() => void submitBootstrap()} disabled={!authForm.formState.isValid || authForm.formState.isSubmitting}>
                  {authForm.formState.isSubmitting ? <Loader2 className="mr-2 size-4 animate-spin" /> : <UserRound className="mr-2 size-4" />}
                  Bootstrap Admin
                </Button>
                <Button type="submit" disabled={!authForm.formState.isValid || authForm.formState.isSubmitting}>
                  {authForm.formState.isSubmitting ? <Loader2 className="mr-2 size-4 animate-spin" /> : <LogIn className="mr-2 size-4" />}
                  Login
                </Button>
              </div>
            </form>
          ) : (
            <div className="rounded-xl border border-amber-400/40 bg-amber-500/10 p-3 text-sm text-amber-100">
              The configured auth mode is <span className="font-mono">{configuredAuthType}</span>. Local bootstrap and password login are unavailable in this build.
            </div>
          )}

          {isAdmin && (
            <div className="space-y-3 rounded-xl border border-border/70 bg-background/50 p-3">
              <p className="text-sm font-medium">API tokens</p>
              <form className="grid gap-3 md:grid-cols-2" onSubmit={(event) => void submitToken(event)}>
                <div className="space-y-2">
                  <Label htmlFor="token-name">Token name</Label>
                  <Input
                    id="token-name"
                    placeholder="Edge automation"
                    disabled={!canMutate || tokenForm.formState.isSubmitting}
                    aria-invalid={tokenForm.formState.errors.name ? "true" : "false"}
                    {...tokenForm.register("name")}
                  />
                  {tokenForm.formState.errors.name ? <p className="text-xs text-destructive">{tokenForm.formState.errors.name.message}</p> : null}
                </div>
                <div className="space-y-2">
                  <Label htmlFor="token-role">Role</Label>
                  <Select
                    value={tokenForm.watch("role")}
                    onValueChange={(value) => tokenForm.setValue("role", value as TokenFormValues["role"], { shouldDirty: true, shouldValidate: true })}
                    disabled={!canMutate || tokenForm.formState.isSubmitting}
                  >
                    <SelectTrigger id="token-role" aria-label="Select token role">
                      <SelectValue placeholder="Select role" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="operator">operator</SelectItem>
                      <SelectItem value="viewer">viewer</SelectItem>
                      <SelectItem value="admin">admin</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="token-expiry">Expires in hours</Label>
                  <Input
                    id="token-expiry"
                    placeholder="24"
                    type="number"
                    min={1}
                    disabled={!canMutate || tokenForm.formState.isSubmitting}
                    aria-invalid={tokenForm.formState.errors.expires_in_hours ? "true" : "false"}
                    {...tokenForm.register("expires_in_hours")}
                  />
                  {tokenForm.formState.errors.expires_in_hours ? <p className="text-xs text-destructive">{tokenForm.formState.errors.expires_in_hours.message}</p> : null}
                </div>
                <div className="space-y-2">
                  <Label htmlFor="token-description">Description</Label>
                  <Input
                    id="token-description"
                    placeholder="Optional purpose"
                    disabled={!canMutate || tokenForm.formState.isSubmitting}
                    aria-invalid={tokenForm.formState.errors.description ? "true" : "false"}
                    {...tokenForm.register("description")}
                  />
                  {tokenForm.formState.errors.description ? <p className="text-xs text-destructive">{tokenForm.formState.errors.description.message}</p> : null}
                </div>
                <div className="md:col-span-2 flex justify-end">
                  <Button type="submit" disabled={!canMutate || !tokenForm.formState.isValid || tokenForm.formState.isSubmitting}>
                    {tokenForm.formState.isSubmitting ? <Loader2 className="mr-2 size-4 animate-spin" /> : <KeyRound className="mr-2 size-4" />}
                    Create token
                  </Button>
                </div>
              </form>

              {tokenSecret && (
                <div className="rounded-lg border border-emerald-400/40 bg-emerald-500/10 px-3 py-2">
                  <p className="text-xs text-emerald-300">New token secret (shown once):</p>
                  <p className="mt-1 break-all font-mono text-xs text-emerald-100">{tokenSecret}</p>
                </div>
              )}

              <div className="space-y-2">
                {authTokens.map((token) => (
                  <div key={token.id} className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border/70 bg-muted/20 p-2">
                    <div>
                      <p className="text-sm">{token.name}</p>
                      <p className="font-mono text-xs text-muted-foreground">
                        {token.id} | prefix {token.prefix} | role {token.role}
                      </p>
                    </div>
                    <AlertDialog>
                      <AlertDialogTrigger asChild>
                        <Button variant="outline" size="sm" disabled={!canMutate}>
                          <Trash2 className="mr-2 size-4" />
                          Revoke
                        </Button>
                      </AlertDialogTrigger>
                      <AlertDialogContent>
                        <AlertDialogHeader>
                          <AlertDialogTitle>Revoke token {token.name}?</AlertDialogTitle>
                          <AlertDialogDescription>
                            This will invalidate the token with prefix {token.prefix}. Any automation using it will lose access immediately.
                          </AlertDialogDescription>
                        </AlertDialogHeader>
                        <AlertDialogFooter>
                          <AlertDialogCancel>Cancel</AlertDialogCancel>
                          <AlertDialogAction className="bg-destructive text-destructive-foreground hover:bg-destructive/90" onClick={() => void revokeToken(token.id)}>
                            Revoke token
                          </AlertDialogAction>
                        </AlertDialogFooter>
                      </AlertDialogContent>
                    </AlertDialog>
                  </div>
                ))}
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Server className="size-4" />
            System diagnostics
          </CardTitle>
          <CardDescription>Runtime and backup controls.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="grid gap-2 text-sm text-muted-foreground md:grid-cols-2">
            <div className="rounded-lg border border-border/70 bg-background/60 px-3 py-2">
              Version: <span className="font-medium text-foreground">{systemInfo?.version ?? "-"}</span>
            </div>
            <div className="rounded-lg border border-border/70 bg-background/60 px-3 py-2">
              Uptime: <span className="font-medium text-foreground">{typeof systemInfo?.uptime_sec === "number" ? `${systemInfo.uptime_sec}s` : "-"}</span>
            </div>
            <div className="rounded-lg border border-border/70 bg-background/60 px-3 py-2">
              Runtime: <span className="font-medium text-foreground">{systemInfo?.runtime?.goos ?? "-"} / {systemInfo?.runtime?.goarch ?? "-"}</span>
            </div>
            <div className="rounded-lg border border-border/70 bg-background/60 px-3 py-2">
              CPUs: <span className="font-medium text-foreground">{systemInfo?.runtime?.num_cpu ?? "-"}</span>
            </div>
          </div>

          <div className="flex flex-wrap gap-2">
            <Button onClick={() => void createBackup()} disabled={!isAdmin || !canMutate}>
              <HardDriveDownload className="mr-2 size-4" />
              Create backup
            </Button>
            <Button variant="outline" onClick={() => void refreshBackups()}>
              <RefreshCw className="mr-2 size-4" />
              Refresh backups
            </Button>
            <Button variant="outline" onClick={() => void refreshSystemConfig()}>
              <RefreshCw className="mr-2 size-4" />
              Refresh config
            </Button>
            <Button variant="outline" onClick={() => void copyConfig()}>
              <Copy className="mr-2 size-4" />
              {copyState === "copied" ? "Copied" : copyState === "failed" ? "Copy failed" : "Copy config"}
            </Button>
            <Button variant="outline" size="sm" asChild>
              <a href="/api/v1/system/config/export?format=yaml" target="_blank" rel="noreferrer">
                Export config YAML
              </a>
            </Button>
            <Button variant="outline" size="sm" asChild>
              <a href="/api/v1/system/config/export?format=json" target="_blank" rel="noreferrer">
                Export config JSON
              </a>
            </Button>
          </div>

          <div className="rounded-lg border border-border/70 bg-background/60">
            <div className="flex items-center justify-between border-b border-border/60 px-3 py-2">
              <span className="text-xs text-muted-foreground">Sanitized runtime config</span>
              <div className="flex gap-2">
                <Button variant="outline" size="sm" onClick={() => setConfigEditing((v) => !v)} disabled={!isAdmin || !canMutate}>
                  {configEditing ? "Cancel edit" : "Edit config"}
                </Button>
                {configEditing ? (
                  <Button size="sm" onClick={() => void saveConfigDraft()} disabled={!isAdmin || !canMutate}>
                    Save config
                  </Button>
                ) : null}
              </div>
            </div>
            {configEditing ? (
              <Textarea
                value={configDraft}
                onChange={(event) => setConfigDraft(event.target.value)}
                className="h-72 resize-y border-0 bg-transparent font-mono text-[11px] leading-relaxed text-foreground/90 shadow-none focus-visible:ring-0 focus-visible:ring-offset-0"
              />
            ) : (
              <pre className="max-h-72 overflow-auto px-3 py-2 text-[11px] leading-relaxed text-foreground/90">{systemConfigJSON}</pre>
            )}
            {configError ? <p className="border-t border-border/60 px-3 py-2 text-xs text-rose-300">{configError}</p> : null}
          </div>

          <div className="space-y-2">
            {backups.slice(0, 8).map((backup) => (
              <div key={backup.path} className="rounded-lg border border-border/70 bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
                <div className="flex flex-wrap items-start justify-between gap-2">
                  <div className="min-w-0 flex-1">
                    <p className="font-mono text-[11px] text-foreground">{backup.name}</p>
                    <p>{backup.created_at} | {(backup.size_bytes / 1024).toFixed(1)} KB</p>
                    <p className="truncate">{backup.path}</p>
                  </div>
                  <AlertDialog>
                    <AlertDialogTrigger asChild>
                      <Button variant="outline" size="sm" disabled={!isAdmin || !canMutate}>
                        Restore
                      </Button>
                    </AlertDialogTrigger>
                    <AlertDialogContent>
                      <AlertDialogHeader>
                        <AlertDialogTitle>Restore snapshot {backup.name}?</AlertDialogTitle>
                        <AlertDialogDescription>
                          This replaces current in-memory state with the selected backup. Use it only when you intend to roll the node back.
                        </AlertDialogDescription>
                      </AlertDialogHeader>
                      <AlertDialogFooter>
                        <AlertDialogCancel>Cancel</AlertDialogCancel>
                        <AlertDialogAction className="bg-destructive text-destructive-foreground hover:bg-destructive/90" onClick={() => void restoreSnapshot(backup.name)}>
                          Restore snapshot
                        </AlertDialogAction>
                      </AlertDialogFooter>
                    </AlertDialogContent>
                  </AlertDialog>
                </div>
              </div>
            ))}
            {backups.length === 0 ? (
              <EmptyState
                icon={HardDriveDownload}
                title="No backups found"
                description="Create the first runtime snapshot to make restore and rollback available."
              />
            ) : null}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Workflow className="size-4" />
            High availability
          </CardTitle>
          <CardDescription>Peer posture, sync health, and controlled handoff from the current primary node.</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-2 text-sm md:grid-cols-2 xl:grid-cols-4">
            <div className="rounded-lg border border-border/70 bg-background/60 px-3 py-2">
              <p className="text-xs text-muted-foreground">Role</p>
              <div className="mt-1 flex items-center justify-between gap-2">
                <span className="font-medium capitalize text-foreground">{ha?.status === "disabled" ? "disabled" : ha?.role ?? "unknown"}</span>
                <Badge variant={ha?.peer === "connected" ? (ha?.role === "primary" ? "success" : "default") : "warning"}>{ha?.peer ?? "unknown"}</Badge>
              </div>
            </div>
            <div className="rounded-lg border border-border/70 bg-background/60 px-3 py-2">
              <p className="text-xs text-muted-foreground">Peer node</p>
              <p className="mt-1 font-medium text-foreground">{ha?.peer_node ?? "-"}</p>
            </div>
            <div className="rounded-lg border border-border/70 bg-background/60 px-3 py-2">
              <p className="text-xs text-muted-foreground">Heartbeat latency</p>
              <p className="mt-1 font-medium text-foreground">{formatDuration(ha?.heartbeat_latency)}</p>
            </div>
            <div className="rounded-lg border border-border/70 bg-background/60 px-3 py-2">
              <p className="text-xs text-muted-foreground">Sync lag</p>
              <p className="mt-1 font-medium text-foreground">{formatDuration(ha?.sync_lag)}</p>
            </div>
          </div>

          <div className="grid gap-3 md:grid-cols-2">
            <div className="rounded-lg border border-border/70 bg-background/60 p-3 text-sm">
              <p className="font-medium text-foreground">HA config</p>
              <div className="mt-3 space-y-2 text-muted-foreground">
                <div className="flex items-center justify-between gap-3">
                  <span>Mode</span>
                  <span className="font-mono text-xs text-foreground">{String(haConfig?.mode ?? ha?.mode ?? "active-passive")}</span>
                </div>
                <div className="flex items-center justify-between gap-3">
                  <span>Priority</span>
                  <span className="font-mono text-xs text-foreground">{String(haConfig?.priority ?? ha?.priority ?? 100)}</span>
                </div>
                <div className="flex items-center justify-between gap-3">
                  <span>Peer address</span>
                  <span className="font-mono text-xs text-foreground">{String(haConfig?.peer_address ?? ha?.peer_addr ?? "-")}</span>
                </div>
                <div className="flex items-center justify-between gap-3">
                  <span>Listen address</span>
                  <span className="font-mono text-xs text-foreground">{ha?.listen_addr ?? "-"}</span>
                </div>
                <div className="flex items-center justify-between gap-3">
                  <span>Witness path</span>
                  <span className="font-mono text-xs text-foreground">{String(haConfig?.witness_path ?? ha?.witness_path ?? "-")}</span>
                </div>
                <div className="flex items-center justify-between gap-3">
                  <span>Lease sync</span>
                  <span className="font-mono text-xs text-foreground">{String(haConfig?.lease_sync ?? "false")}</span>
                </div>
                <div className="flex items-center justify-between gap-3">
                  <span>Failovers observed</span>
                  <span className="font-mono text-xs text-foreground">{ha?.failover_count ?? 0}</span>
                </div>
                <div className="flex items-center justify-between gap-3">
                  <span>Fencing status</span>
                  <Badge variant={ha?.fenced ? "danger" : "success"}>{ha?.fenced ? ha?.fencing_reason ?? "fenced" : "clear"}</Badge>
                </div>
              </div>
            </div>

            <div className="rounded-lg border border-border/70 bg-background/60 p-3 text-sm">
              <p className="font-medium text-foreground">Controlled handoff</p>
              <p className="mt-1 text-xs text-muted-foreground">
                Trigger this only on the active primary node. The node enters a draining window so its peer can take over cleanly.
              </p>
              <form className="mt-3 space-y-3" onSubmit={(event) => void triggerManualHandover(event)}>
                <div className="space-y-2">
                  <Label htmlFor="ha-reason">Reason for failover</Label>
                  <Input
                    id="ha-reason"
                    placeholder="Planned maintenance"
                    disabled={!isAdmin || !canMutate || haForm.formState.isSubmitting}
                    aria-invalid={haForm.formState.errors.reason ? "true" : "false"}
                    {...haForm.register("reason")}
                  />
                  {haForm.formState.errors.reason ? <p className="text-xs text-destructive">{haForm.formState.errors.reason.message}</p> : null}
                </div>
                <div className="flex flex-wrap items-center gap-2">
                <Button type="submit" disabled={!canTriggerFailover || !haForm.formState.isValid || haForm.formState.isSubmitting}>
                  {haForm.formState.isSubmitting ? <Loader2 className="mr-2 size-4 animate-spin" /> : <ArrowRightLeft className="mr-2 size-4" />}
                  Trigger manual failover
                </Button>
                {!canTriggerFailover ? <Badge variant="warning">Available only on connected primary node</Badge> : null}
                {haActionState === "done" ? <Badge variant="success">Failover requested</Badge> : null}
                </div>
              </form>
              {ha?.manual_step_down_until ? (
                <p className="mt-3 text-xs text-amber-200">Manual handoff window active until {new Date(ha.manual_step_down_until).toLocaleString()}.</p>
              ) : null}
              {haActionError ? <p className="mt-3 text-xs text-rose-300">{haActionError}</p> : null}
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
