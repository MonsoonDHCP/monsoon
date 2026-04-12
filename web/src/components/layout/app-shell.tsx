import type { ReactNode } from "react"
import { Outlet } from "react-router-dom"

import { ConnectionBanner } from "@/components/layout/connection-banner"
import { Sidebar } from "@/components/layout/sidebar"
import { Topbar } from "@/components/layout/topbar"

type AppShellProps = {
  children?: ReactNode
  shellOnly?: boolean
}

export function AppShell({ children, shellOnly = false }: AppShellProps) {
  return (
    <div className="relative min-h-screen bg-background text-foreground transition-colors duration-150">
      <a href="#main-content" className="skip-link">
        Skip to main content
      </a>
      <div className="pointer-events-none absolute inset-0 overflow-hidden">
        <div className="absolute -left-20 top-0 h-80 w-80 rounded-full bg-primary/12 blur-3xl motion-reduce:hidden" />
        <div className="absolute right-0 top-1/3 h-96 w-96 rounded-full bg-info/10 blur-3xl motion-reduce:hidden" />
        <div className="absolute bottom-0 left-1/3 h-72 w-72 rounded-full bg-warning/10 blur-3xl motion-reduce:hidden" />
      </div>

      <div className="relative min-h-screen">
        <aside className="fixed inset-y-0 left-0 z-40 hidden w-64 border-r border-border/70 bg-background-subtle/85 p-4 backdrop-blur lg:block">
          <Sidebar />
        </aside>
        <main className="min-h-screen lg:pl-64">
          <ConnectionBanner />
          <Topbar />
          <div className="px-4 py-4 sm:px-6 sm:py-6 lg:px-8 lg:py-8">
            <section id="main-content" tabIndex={-1}>
              {shellOnly ? children : <Outlet />}
            </section>
          </div>
        </main>
      </div>
    </div>
  )
}
