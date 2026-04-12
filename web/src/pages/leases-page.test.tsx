import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { LeasesPage } from "@/pages/leases-page"

const mockUseDashboard = vi.fn()

vi.mock("@/app/dashboard-context", () => ({
  useDashboard: () => mockUseDashboard(),
}))

function makeDashboard(overrides: Record<string, unknown> = {}) {
  return {
    leases: [],
    release: vi.fn().mockResolvedValue(undefined),
    reserveLease: vi.fn().mockResolvedValue(undefined),
    canMutate: true,
    ...overrides,
  }
}

describe("LeasesPage", () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it("filters visible leases and reserves the selected address", async () => {
    const reserveLease = vi.fn().mockResolvedValue(undefined)
    mockUseDashboard.mockReturnValue(
      makeDashboard({
        reserveLease,
        leases: [
          { ip: "10.0.1.10", mac: "AA:BB:CC:DD:EE:01", hostname: "camera-01", state: "bound", subnet_id: "10.0.1.0/24" },
          { ip: "10.0.1.11", mac: "AA:BB:CC:DD:EE:02", hostname: "switch-01", state: "offered", subnet_id: "10.0.1.0/24" },
        ],
      }),
    )

    render(<LeasesPage />)

    fireEvent.change(screen.getByLabelText("Search leases"), { target: { value: "camera" } })

    await waitFor(() => {
      expect(screen.getByText("camera-01")).toBeInTheDocument()
      expect(screen.queryByText("switch-01")).not.toBeInTheDocument()
      expect(screen.getByText("1 visible records")).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole("button", { name: "Reserve" }))

    expect(reserveLease).toHaveBeenCalledWith("10.0.1.10")
  })

  it("shows an empty state when no leases match the search query", async () => {
    mockUseDashboard.mockReturnValue(
      makeDashboard({
        leases: [{ ip: "10.0.1.10", mac: "AA:BB:CC:DD:EE:01", hostname: "camera-01", state: "bound", subnet_id: "10.0.1.0/24" }],
      }),
    )

    render(<LeasesPage />)

    fireEvent.change(screen.getByLabelText("Search leases"), { target: { value: "printer" } })

    expect(await screen.findByText("No leases found")).toBeInTheDocument()
  })
})
