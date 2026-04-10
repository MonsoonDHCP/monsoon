import { KeyRound, LogIn, LogOut, MoonStar, Palette, Save, Shield, SunMedium, Trash2, UserRound } from "lucide-react"
import { useEffect, useState } from "react"
import { useTheme } from "next-themes"

import { useDashboard } from "@/app/dashboard-context"
import { ThemeToggle } from "@/components/layout/theme-toggle"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import type { UISettings } from "@/types/api"

export function SettingsPage() {
  const { settings, saveSettings, currentUser, authTokens, tokenSecret, loginWithPassword, bootstrapAndLogin, logoutCurrentUser, createToken, revokeToken, canMutate, isAdmin } = useDashboard()
  const { setTheme } = useTheme()
  const [local, setLocal] = useState<UISettings>({
    theme: "system",
    density: "comfortable",
    auto_refresh: true,
  })
  const [authForm, setAuthForm] = useState({ username: "admin", password: "" })
  const [tokenForm, setTokenForm] = useState({ name: "", role: "operator", expires_in_hours: 24, description: "" })

  useEffect(() => {
    if (settings) {
      setLocal(settings)
    }
  }, [settings])

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
          <CardDescription>Session login and API token controls.</CardDescription>
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
          ) : (
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
    </div>
  )
}
