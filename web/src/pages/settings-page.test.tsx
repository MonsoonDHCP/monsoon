import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { SettingsPage } from "@/pages/settings-page"

const mockUseDashboard = vi.fn()
const mockSetTheme = vi.fn()

vi.mock("@/app/dashboard-context", () => ({
  useDashboard: () => mockUseDashboard(),
}))

vi.mock("@/hooks/use-theme", () => ({
  useTheme: () => ({ setTheme: mockSetTheme }),
}))

vi.mock("@/components/layout/theme-toggle", () => ({
  ThemeToggle: () => <div data-testid="theme-toggle" />,
}))

function makeDashboard(overrides: Record<string, unknown> = {}) {
  return {
    settings: { theme: "system", density: "comfortable", auto_refresh: true },
    saveSettings: vi.fn().mockResolvedValue(undefined),
    currentUser: null,
    authTokens: [],
    tokenSecret: null,
    loginWithPassword: vi.fn().mockResolvedValue(undefined),
    bootstrapAndLogin: vi.fn().mockResolvedValue(undefined),
    logoutCurrentUser: vi.fn().mockResolvedValue(undefined),
    createToken: vi.fn().mockResolvedValue(undefined),
    revokeToken: vi.fn().mockResolvedValue(undefined),
    canMutate: true,
    isAdmin: false,
    systemInfo: { version: "dev", uptime_sec: 12, runtime: { goos: "linux", goarch: "amd64", num_cpu: 4 } },
    health: { components: {} },
    systemConfig: {},
    backups: [],
    createBackup: vi.fn().mockResolvedValue(undefined),
    restoreBackup: vi.fn().mockResolvedValue(undefined),
    refreshBackups: vi.fn().mockResolvedValue(undefined),
    refreshSystemConfig: vi.fn().mockResolvedValue(undefined),
    saveSystemConfig: vi.fn().mockResolvedValue(undefined),
    requestManualFailover: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  }
}

describe("SettingsPage", () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it("saves updated UI preferences and applies selected theme", async () => {
    const saveSettings = vi.fn().mockResolvedValue(undefined)
    mockUseDashboard.mockReturnValue(
      makeDashboard({
        saveSettings,
      }),
    )

    render(<SettingsPage />)

    await waitFor(() => {
      expect(screen.getByRole("switch", { name: "Toggle compact density" })).toHaveAttribute("data-state", "unchecked")
      expect(screen.getByRole("switch", { name: "Toggle auto refresh" })).toHaveAttribute("data-state", "checked")
    })

    fireEvent.click(screen.getByRole("button", { name: "dark" }))
    fireEvent.click(screen.getByRole("switch", { name: "Toggle compact density" }))
    fireEvent.click(screen.getByRole("switch", { name: "Toggle auto refresh" }))

    await waitFor(() => {
      expect(screen.getByRole("switch", { name: "Toggle compact density" })).toHaveAttribute("data-state", "checked")
      expect(screen.getByRole("switch", { name: "Toggle auto refresh" })).toHaveAttribute("data-state", "unchecked")
      expect(screen.getByRole("button", { name: "Save preferences" })).toBeEnabled()
    })

    fireEvent.click(screen.getByRole("button", { name: "Save preferences" }))

    await waitFor(() => {
      expect(saveSettings).toHaveBeenCalledWith({
        theme: "dark",
        density: "compact",
        auto_refresh: false,
      })
    })
    expect(mockSetTheme).toHaveBeenCalledWith("dark")
  })

  it("shows backup empty state when no snapshots are available", async () => {
    mockUseDashboard.mockReturnValue(makeDashboard())

    render(<SettingsPage />)

    expect(await screen.findByText("No backups found")).toBeInTheDocument()
    expect(await screen.findByText(/Create the first runtime snapshot/i)).toBeInTheDocument()
  })

  it("shows unsupported auth messaging when config is not local", async () => {
    mockUseDashboard.mockReturnValue(
      makeDashboard({
        systemConfig: {
          auth: {
            type: "ldap",
          },
        },
      }),
    )

    render(<SettingsPage />)

    expect(await screen.findByText(/Configured auth mode: ldap/i)).toBeInTheDocument()
    expect(await screen.findByText(/Local bootstrap and password login are unavailable in this build/i)).toBeInTheDocument()
  })
})
