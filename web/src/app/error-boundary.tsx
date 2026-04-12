import { AlertOctagon } from "lucide-react"
import type { ReactNode } from "react"
import { ErrorBoundary, type FallbackProps } from "react-error-boundary"

import { AppShell } from "@/components/layout/app-shell"
import { ErrorState } from "@/components/shared/error-state"

interface AppErrorBoundaryProps {
  children: ReactNode
}

function ErrorFallback({ error, resetErrorBoundary }: FallbackProps) {
  const message = error instanceof Error ? error.message : "An unexpected error interrupted rendering."
  return (
    <AppShell shellOnly>
      <div className="space-y-6">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Something went wrong</h2>
          <p className="text-sm text-muted-foreground">The dashboard hit an unexpected runtime error.</p>
        </div>
        <ErrorState
          icon={AlertOctagon}
          title="Dashboard rendering failed"
          description={message}
          actionLabel="Try again"
          onAction={resetErrorBoundary}
        />
      </div>
    </AppShell>
  )
}

export function AppErrorBoundary({ children }: AppErrorBoundaryProps) {
  return (
    <ErrorBoundary
      FallbackComponent={ErrorFallback}
      onReset={() => {
        window.location.reload()
      }}
    >
      {children}
    </ErrorBoundary>
  )
}
