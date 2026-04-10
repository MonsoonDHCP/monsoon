import { KeyRound, LockKeyhole, LogIn, ShieldCheck, UserRound } from "lucide-react"
import { useState } from "react"

import { ThemeToggle } from "@/components/layout/theme-toggle"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

type AuthGateProps = {
  busy: boolean
  error: string | null
  onLogin: (username: string, password: string) => Promise<void>
  onBootstrap: (username: string, password: string) => Promise<void>
}

export function AuthGate({ busy, error, onLogin, onBootstrap }: AuthGateProps) {
  const [username, setUsername] = useState("admin")
  const [password, setPassword] = useState("")
  const [working, setWorking] = useState<"login" | "bootstrap" | null>(null)

  const submitLogin = async () => {
    setWorking("login")
    try {
      await onLogin(username, password)
    } catch {
      // Error is surfaced by the shared dashboard error state.
    } finally {
      setWorking(null)
    }
  }

  const submitBootstrap = async () => {
    setWorking("bootstrap")
    try {
      await onBootstrap(username, password)
    } catch {
      // Error is surfaced by the shared dashboard error state.
    } finally {
      setWorking(null)
    }
  }

  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden bg-background p-4">
      <div className="pointer-events-none absolute inset-0">
        <div className="absolute -left-20 -top-14 h-80 w-80 rounded-full bg-cyan-500/20 blur-3xl" />
        <div className="absolute right-0 top-1/4 h-96 w-96 rounded-full bg-teal-400/15 blur-3xl" />
        <div className="absolute bottom-0 left-1/3 h-72 w-72 rounded-full bg-amber-300/15 blur-3xl" />
      </div>

      <div className="absolute right-4 top-4">
        <ThemeToggle />
      </div>

      <div className="relative grid w-full max-w-5xl gap-4 lg:grid-cols-[1.2fr_1fr]">
        <Card className="border-border/70 bg-card/80 backdrop-blur">
          <CardHeader className="space-y-3">
            <Badge variant="success" className="w-fit">
              Monsoon Console
            </Badge>
            <CardTitle className="text-3xl leading-tight">Secure access to DHCP + IPAM control plane</CardTitle>
            <CardDescription>Authenticate with local credentials or bootstrap the first admin account.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3 text-sm text-muted-foreground">
            <div className="rounded-xl border border-border/70 bg-background/50 p-3">
              <ShieldCheck className="mb-2 size-4 text-cyan-300" />
              Session cookies are `HttpOnly` and protected by strict same-site policy.
            </div>
            <div className="rounded-xl border border-border/70 bg-background/50 p-3">
              <KeyRound className="mb-2 size-4 text-teal-300" />
              API tokens are hash-stored and shown only once on creation.
            </div>
            <div className="rounded-xl border border-border/70 bg-background/50 p-3">
              <LockKeyhole className="mb-2 size-4 text-amber-300" />
              Role-based access control gates mutation endpoints automatically.
            </div>
          </CardContent>
        </Card>

        <Card className="border-border/70 bg-card/90 backdrop-blur">
          <CardHeader>
            <CardTitle>Sign In</CardTitle>
            <CardDescription>Use your operator account to continue.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <input
              className="h-10 w-full rounded-xl border border-border/70 bg-background px-3 text-sm outline-none focus:ring-2 focus:ring-ring"
              placeholder="Username"
              value={username}
              onChange={(event) => setUsername(event.target.value)}
            />
            <input
              className="h-10 w-full rounded-xl border border-border/70 bg-background px-3 text-sm outline-none focus:ring-2 focus:ring-ring"
              placeholder="Password"
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
            />
            <div className="grid gap-2 sm:grid-cols-2">
              <Button onClick={() => void submitLogin()} disabled={busy || working !== null}>
                <LogIn className="mr-2 size-4" />
                {working === "login" ? "Signing in..." : "Login"}
              </Button>
              <Button variant="outline" onClick={() => void submitBootstrap()} disabled={busy || working !== null}>
                <UserRound className="mr-2 size-4" />
                {working === "bootstrap" ? "Bootstrapping..." : "Bootstrap Admin"}
              </Button>
            </div>
            {error && <p className="rounded-lg border border-rose-400/40 bg-rose-500/10 px-3 py-2 text-xs text-rose-200">{error}</p>}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
