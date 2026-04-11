import { ArrowRightLeft, Copy, HardDriveDownload, KeyRound, LogIn, LogOut, MoonStar, Palette, RefreshCw, Save, Server, Shield, SunMedium, Trash2, UserRound, Workflow } from "lucide-react"
import { useEffect, useMemo, useState } from "react"
import { useTheme } from "next-themes"

import { useDashboard } from "@/app/dashboard-context"
import { ThemeToggle } from "@/components/layout/theme-toggle"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
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

export function SettingsPage() {
  const { settings, saveSettings, currentUser, authTokens, tokenSecret, loginWithPassword, bootstrapAndLogin, logoutCurrentUser, createToken, revokeToken, canMutate, isAdmin, systemInfo, health, systemConfig, backups, createBackup, restoreBackup, refreshBackups, refreshSystemConfig, saveSystemConfig, requestManualFailover } = useDashboard()
  const { setTheme } = useTheme()
  const [local, setLocal] = useState<UISettings>({
    theme: "system",
    density: "comfortable",
    auto_refresh: true,
  })
  const [authForm, setAuthForm] = useState({ username: "admin", password: "" })
  const [tokenForm, setTokenForm] = useState({ name: "", role: "operator", expires_in_hours: 24, description: "" })
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">("idle")
  const [configDraft, setConfigDraft] = useState("")
  const [configEditing, setConfigEditing] = useState(false)
  const [configError, setConfigError] = useState<string | null>(null)
  const [haReason, setHAReason] = useState("Planned maintenance")
  const [haActionState, setHAActionState] = useState<"idle" | "submitting" | "done" | "failed">("idle")
  const [haActionError, setHAActionError] = useState<string | null>(null)

  const systemConfigJSON = useMemo(() => JSON.stringify(systemConfig ?? {}, null, 2), [systemConfig])
  const ha = systemInfo?.ha ?? health?.components?.ha
  const haConfig = (systemConfig?.ha as Record<string, unknown> | undefined) ?? undefined
  const authConfig = (systemConfig?.auth as Record<string, unknown> | undefined) ?? undefined
  const configuredAuthType = typeof authConfig?.type === "string" ? authConfig.type.trim().toLowerCase() : "local"
  const localAuthAvailable = configuredAuthType === "" || configuredAuthType === "local"
  const canTriggerFailover = Boolean(isAdmin && canMutate && ha?.role === "primary" && ha?.peer === "connected")

  useEffect(() => {
    if (settings) {
      setLocal(settings)
    }
  }, [settings])

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

  const restoreSnapshot = async (backupName: string) => {
    if (!window.confirm(`Restore snapshot ${backupName}? This will replace in-memory state with the selected backup.`)) {
      return
    }
    try {
      await restoreBackup({ name: backupName })
    } catch (error) {
      setConfigError(error instanceof Error ? error.message : "Restore failed")
    }
  }

  const triggerManualHandover = async () => {
    try {
      setHAActionState("submitting")
      setHAActionError(null)
      await requestManualFailover(haReason)
      setHAActionState("done")
      window.setTimeout(() => setHAActionState("idle"), 1800)
    } catch (err) {
      setHAActionState("failed")
      setHAActionError(err instanceof Error ? err.message : "Manual failover failed")
    }
  }

  return (
    <div className="space-y-6">
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
                variant={local.theme === theme ? "default" : "outline"}
                size="sm"
                onClick={() => {
                  setTheme(theme)
                  setLocal((prev) => ({ ...prev, theme }))
                }}
                className="capitalize"
                disabled={!canMutate}
              >
                {theme}
              </Button>
            ))}
            <ThemeToggle onThemeChange={(theme) => setLocal((prev) => ({ ...prev, theme }))} />
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
          <label className="flex items-center justify-between rounded-lg border border-border/70 p-3">
            <span className="text-sm">Compact density</span>
            <input
              type="checkbox"
              checked={local.density === "compact"}
              onChange={(event) => setLocal((prev) => ({ ...prev, density: event.target.checked ? "compact" : "comfortable" }))}
              disabled={!canMutate}
            />
          </label>

          <label className="flex items-center justify-between rounded-lg border border-border/70 p-3">
            <span className="text-sm">Auto refresh</span>
            <input
              type="checkbox"
              checked={local.auto_refresh}
              onChange={(event) => setLocal((prev) => ({ ...prev, auto_refresh: event.target.checked }))}
              disabled={!canMutate}
            />
          </label>

          <Button onClick={() => void saveSettings(local)} disabled={!canMutate}>
            <Save className="mr-2 size-4" />
            Save preferences
          </Button>
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
            <div className="grid gap-3 md:grid-cols-2">
              <input
                className="rounded-lg border border-border/70 bg-background px-3 py-2 text-sm"
                placeholder="Username"
                value={authForm.username}
                onChange={(event) => setAuthForm((prev) => ({ ...prev, username: event.target.value }))}
              />
              <input
                className="rounded-lg border border-border/70 bg-background px-3 py-2 text-sm"
                placeholder="Password"
                type="password"
                value={authForm.password}
                onChange={(event) => setAuthForm((prev) => ({ ...prev, password: event.target.value }))}
              />
              <div className="md:col-span-2 flex flex-wrap justify-end gap-2">
                <Button variant="outline" onClick={() => void bootstrapAndLogin(authForm.username, authForm.password)}>
                  <UserRound className="mr-2 size-4" />
                  Bootstrap Admin
                </Button>
                <Button onClick={() => void loginWithPassword(authForm.username, authForm.password)}>
                  <LogIn className="mr-2 size-4" />
                  Login
                </Button>
              </div>
            </div>
          ) : (
            <div className="rounded-xl border border-amber-400/40 bg-amber-500/10 p-3 text-sm text-amber-100">
              The configured auth mode is <span className="font-mono">{configuredAuthType}</span>. Local bootstrap and password login are unavailable in this build.
            </div>
          )}

          {isAdmin && (
            <div className="space-y-3 rounded-xl border border-border/70 bg-background/50 p-3">
              <p className="text-sm font-medium">API tokens</p>
              <div className="grid gap-3 md:grid-cols-2">
                <input
                  className="rounded-lg border border-border/70 bg-background px-3 py-2 text-sm"
                  placeholder="Token name"
                  value={tokenForm.name}
                  onChange={(event) => setTokenForm((prev) => ({ ...prev, name: event.target.value }))}
                  disabled={!canMutate}
                />
                <select
                  className="rounded-lg border border-border/70 bg-background px-3 py-2 text-sm"
                  value={tokenForm.role}
                  onChange={(event) => setTokenForm((prev) => ({ ...prev, role: event.target.value }))}
                  disabled={!canMutate}
                >
                  <option value="operator">operator</option>
                  <option value="viewer">viewer</option>
                  <option value="admin">admin</option>
                </select>
                <input
                  className="rounded-lg border border-border/70 bg-background px-3 py-2 text-sm"
                  placeholder="Expires in hours (optional)"
                  type="number"
                  value={tokenForm.expires_in_hours}
                  onChange={(event) => setTokenForm((prev) => ({ ...prev, expires_in_hours: Number(event.target.value) }))}
                  disabled={!canMutate}
                />
                <input
                  className="rounded-lg border border-border/70 bg-background px-3 py-2 text-sm"
                  placeholder="Description"
                  value={tokenForm.description}
                  onChange={(event) => setTokenForm((prev) => ({ ...prev, description: event.target.value }))}
                  disabled={!canMutate}
                />
                <div className="md:col-span-2 flex justify-end">
                  <Button onClick={() => void createToken(tokenForm)} disabled={!canMutate}>
                    <KeyRound className="mr-2 size-4" />
                    Create token
                  </Button>
                </div>
              </div>

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
                    <Button variant="outline" size="sm" onClick={() => void revokeToken(token.id)} disabled={!canMutate}>
                      <Trash2 className="mr-2 size-4" />
                      Revoke
                    </Button>
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
              <textarea
                value={configDraft}
                onChange={(event) => setConfigDraft(event.target.value)}
                className="h-72 w-full resize-y bg-transparent px-3 py-2 font-mono text-[11px] leading-relaxed text-foreground/90 outline-none"
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
                  <Button variant="outline" size="sm" onClick={() => void restoreSnapshot(backup.name)} disabled={!isAdmin || !canMutate}>
                    Restore
                  </Button>
                </div>
              </div>
            ))}
            {backups.length === 0 && <p className="text-sm text-muted-foreground">No backups found yet.</p>}
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
              <input
                className="mt-3 w-full rounded-lg border border-border/70 bg-background px-3 py-2 text-sm"
                value={haReason}
                onChange={(event) => setHAReason(event.target.value)}
                placeholder="Reason for failover"
                disabled={!isAdmin || !canMutate}
              />
              <div className="mt-3 flex flex-wrap items-center gap-2">
                <Button onClick={() => void triggerManualHandover()} disabled={!canTriggerFailover || haActionState === "submitting"}>
                  <ArrowRightLeft className="mr-2 size-4" />
                  {haActionState === "submitting" ? "Triggering..." : "Trigger manual failover"}
                </Button>
                {!canTriggerFailover ? <Badge variant="warning">Available only on connected primary node</Badge> : null}
                {haActionState === "done" ? <Badge variant="success">Failover requested</Badge> : null}
              </div>
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
