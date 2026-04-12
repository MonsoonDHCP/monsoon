import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"

import { App } from "@/app/app"

const mockUseDashboardData = vi.fn()
const mockSetTheme = vi.fn()
const mockAuthGate = vi.fn(({ error }: { error: string | null }) => <div data-testid="auth-gate">{error}</div>)

vi.mock("@/hooks/use-dashboard-data", () => ({
  useDashboardData: () => mockUseDashboardData(),
}))

vi.mock("@/hooks/use-theme", () => ({
  useTheme: () => ({ setTheme: mockSetTheme }),
}))

vi.mock("@/app/auth-gate", () => ({
  AuthGate: (props: { error: string | null }) => mockAuthGate(props),
}))

vi.mock("@/components/layout/app-shell", async () => {
  const router = await import("react-router-dom")
  return {
    AppShell: ({ children, shellOnly = false }: { children?: React.ReactNode; shellOnly?: boolean }) => (
      <div data-testid="app-shell">{shellOnly ? children : <router.Outlet />}</div>
    ),
  }
})

vi.mock("@/components/shared/page-skeleton", () => ({
  PageSkeleton: () => <div data-testid="page-skeleton" />,
}))

vi.mock("@/pages/overview-page", () => ({
  OverviewPage: () => <div>Overview Page</div>,
}))

vi.mock("@/pages/subnets-page", () => ({
  SubnetsPage: () => <div>Subnets Page</div>,
}))

vi.mock("@/pages/addresses-page", () => ({
  AddressesPage: () => <div>Addresses Page</div>,
}))

vi.mock("@/pages/reservations-page", () => ({
  ReservationsPage: () => <div>Reservations Page</div>,
}))

vi.mock("@/pages/leases-page", () => ({
  LeasesPage: () => <div>Leases Page</div>,
}))

vi.mock("@/pages/discovery-page", () => ({
  DiscoveryPage: () => <div>Discovery Page</div>,
}))

vi.mock("@/pages/audit-page", () => ({
  AuditPage: () => <div>Audit Page</div>,
}))

vi.mock("@/pages/settings-page", () => ({
  SettingsPage: () => <div>Settings Page</div>,
}))

describe("App", () => {
  it("shows the auth gate when authentication is required", () => {
    mockUseDashboardData.mockReturnValue({
      authRequired: true,
      currentUser: null,
      loading: false,
      error: "Authentication required. Please sign in.",
      settings: null,
      loginWithPassword: vi.fn(),
      bootstrapAndLogin: vi.fn(),
    })

    render(<App />)

    expect(screen.getByTestId("auth-gate")).toHaveTextContent("Authentication required. Please sign in.")
    expect(mockAuthGate).toHaveBeenCalled()
  })

  it("applies theme settings and renders the active route inside the shell", async () => {
    mockUseDashboardData.mockReturnValue({
      authRequired: false,
      currentUser: { username: "admin", role: "admin" },
      loading: false,
      error: null,
      settings: { theme: "dark", density: "compact", auto_refresh: true },
      loginWithPassword: vi.fn(),
      bootstrapAndLogin: vi.fn(),
    })

    render(
      <MemoryRouter initialEntries={["/settings"]}>
        <App />
      </MemoryRouter>,
    )

    expect(await screen.findByText("Settings Page")).toBeInTheDocument()
    expect(screen.getByTestId("app-shell")).toBeInTheDocument()
    expect(mockSetTheme).toHaveBeenCalledWith("dark")
    expect(document.documentElement).toHaveAttribute("data-density", "compact")
  })
})
