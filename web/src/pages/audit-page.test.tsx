import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { AuditPage } from "@/pages/audit-page"

const mockUseDashboard = vi.fn()

vi.mock("@/app/dashboard-context", () => ({
  useDashboard: () => mockUseDashboard(),
}))

function makeDashboard(overrides: Record<string, unknown> = {}) {
  return {
    authRequired: false,
    isAdmin: true,
    auditEntries: [],
    ...overrides,
  }
}

describe("AuditPage", () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it("filters audit entries and updates export links with the active query", async () => {
    mockUseDashboard.mockReturnValue(
      makeDashboard({
        auditEntries: [
          { id: "1", timestamp: "2026-04-12T10:00:00Z", actor: "admin", action: "create", object_type: "subnet", object_id: "10.0.1.0/24", source: "ui" },
          { id: "2", timestamp: "2026-04-12T10:05:00Z", actor: "operator", action: "delete", object_type: "reservation", object_id: "10.0.1.50", source: "api" },
        ],
      }),
    )

    render(<AuditPage />)

    fireEvent.change(screen.getByLabelText("Filter audit log"), { target: { value: "admin" } })

    await waitFor(() => {
      expect(screen.getByText("create")).toBeInTheDocument()
      expect(screen.queryByText("delete")).not.toBeInTheDocument()
      expect(screen.getByText("1 records")).toBeInTheDocument()
    })

    expect(screen.getByRole("link", { name: "Export CSV" })).toHaveAttribute("href", "/api/v1/audit?q=admin&format=csv")
    expect(screen.getByRole("link", { name: "Export JSON" })).toHaveAttribute("href", "/api/v1/audit?q=admin&format=json")
  })

  it("shows an empty state when there are no matching audit entries", async () => {
    mockUseDashboard.mockReturnValue(
      makeDashboard({
        auditEntries: [{ id: "1", timestamp: "2026-04-12T10:00:00Z", actor: "admin", action: "create", object_type: "subnet", object_id: "10.0.1.0/24", source: "ui" }],
      }),
    )

    render(<AuditPage />)

    fireEvent.change(screen.getByLabelText("Filter audit log"), { target: { value: "missing" } })

    expect(await screen.findByText("No audit entries found")).toBeInTheDocument()
  })

  it("shows an access message for non-admin sessions", async () => {
    mockUseDashboard.mockReturnValue(
      makeDashboard({
        authRequired: true,
        isAdmin: false,
      }),
    )

    render(<AuditPage />)

    expect(await screen.findByText("Admin access required")).toBeInTheDocument()
    expect(screen.queryByRole("link", { name: "Export CSV" })).not.toBeInTheDocument()
  })
})
