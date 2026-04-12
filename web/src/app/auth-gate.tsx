import { zodResolver } from "@hookform/resolvers/zod"
import { KeyRound, Loader2, LockKeyhole, LogIn, ShieldCheck, UserRound } from "lucide-react"
import { useState } from "react"
import { useForm } from "react-hook-form"
import { z } from "zod"

import { ErrorState } from "@/components/shared/error-state"
import { ThemeToggle } from "@/components/layout/theme-toggle"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

type AuthGateProps = {
  busy: boolean
  error: string | null
  onLogin: (username: string, password: string) => Promise<void>
  onBootstrap: (username: string, password: string) => Promise<void>
}

const authSchema = z.object({
  username: z.string().trim().min(1, "Username is required"),
  password: z.string().min(1, "Password is required"),
})

type AuthFormValues = z.infer<typeof authSchema>

export function AuthGate({ busy, error, onLogin, onBootstrap }: AuthGateProps) {
  const [working, setWorking] = useState<"login" | "bootstrap" | null>(null)
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting, isValid },
  } = useForm<AuthFormValues>({
    resolver: zodResolver(authSchema),
    mode: "onChange",
    defaultValues: {
      username: "admin",
      password: "",
    },
  })

  const submitLogin = handleSubmit(async (values) => {
    setWorking("login")
    try {
      await onLogin(values.username, values.password)
      reset({ ...values, password: "" })
    } catch {
      // Error is surfaced by the shared dashboard error state.
    } finally {
      setWorking(null)
    }
  })

  const submitBootstrap = handleSubmit(async (values) => {
    setWorking("bootstrap")
    try {
      await onBootstrap(values.username, values.password)
      reset({ ...values, password: "" })
    } catch {
      // Error is surfaced by the shared dashboard error state.
    } finally {
      setWorking(null)
    }
  })

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
            <CardDescription>Authenticate with local credentials or bootstrap the first local admin account.</CardDescription>
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
            <CardDescription>Use your local operator account to continue.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <form className="space-y-3" onSubmit={(event) => void submitLogin(event)}>
              <div className="space-y-2">
                <Label htmlFor="auth-gate-username">Username</Label>
                <Input
                  id="auth-gate-username"
                  placeholder="Username"
                  aria-invalid={errors.username ? "true" : "false"}
                  {...register("username")}
                />
                {errors.username ? <p className="text-xs text-destructive">{errors.username.message}</p> : null}
              </div>

              <div className="space-y-2">
                <Label htmlFor="auth-gate-password">Password</Label>
                <Input
                  id="auth-gate-password"
                  placeholder="Password"
                  type="password"
                  aria-invalid={errors.password ? "true" : "false"}
                  {...register("password")}
                />
                {errors.password ? <p className="text-xs text-destructive">{errors.password.message}</p> : null}
              </div>

              <div className="grid gap-2 sm:grid-cols-2">
                <Button type="submit" disabled={busy || !isValid || isSubmitting || working !== null}>
                  {working === "login" ? <Loader2 className="mr-2 size-4 animate-spin" /> : <LogIn className="mr-2 size-4" />}
                  {working === "login" ? "Signing in..." : "Login"}
                </Button>
                <Button type="button" variant="outline" onClick={() => void submitBootstrap()} disabled={busy || !isValid || isSubmitting || working !== null}>
                  {working === "bootstrap" ? <Loader2 className="mr-2 size-4 animate-spin" /> : <UserRound className="mr-2 size-4" />}
                  {working === "bootstrap" ? "Bootstrapping..." : "Bootstrap Admin"}
                </Button>
              </div>
            </form>
            {error ? <ErrorState title="Authentication failed" description={error} className="p-4" /> : null}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
