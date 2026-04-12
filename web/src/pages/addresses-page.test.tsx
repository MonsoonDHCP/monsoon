import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { AddressesPage } from "@/pages/addresses-page"

const mockUseDashboard = vi.fn()
const mockUseAddressesQuery = vi.fn()

vi.mock("@/app/dashboard-context", () => ({
  useDashboard: () => mockUseDashboard(),
}))

vi.mock("@/hooks/use-dashboard-queries", () => ({
  useAddressesQuery: (...args: unknown[]) => mockUseAddressesQuery(...args),
}))

function makeDashboard(overrides: Record<string, unknown> = {}) {
  return {
    subnets: [
      { cidr: "10.0.1.0/24", name: "Edge VLAN" },
      { cidr: "unassigned", name: "Unassigned" },
    ],
    settings: {
      auto_refresh: true,
    },
    ...overrides,
  }
}

function makeAddressesQuery(overrides: Record<string, unknown> = {}) {
  return {
    data: [],
    isPending: false,
    error: null,
    refetch: vi.fn(),
    ...overrides,
  }
}

describe("AddressesPage", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockUseDashboard.mockReturnValue(makeDashboard())
  })

  it("renders loading placeholders while address telemetry is pending", () => {
    mockUseAddressesQuery.mockReturnValue(
      makeAddressesQuery({
        isPending: true,
      }),
    )

    render(<AddressesPage />)

    expect(screen.getByText("No address map data")).toBeInTheDocument()
    expect(screen.getByText("No address records")).toBeInTheDocument()
  })

  it("filters the address list and retries when query loading fails", async () => {
    const refetch = vi.fn()
    mockUseAddressesQuery.mockReturnValue(
      makeAddressesQuery({
        data: [
          { ip: "10.0.1.10", state: "dhcp", hostname: "camera-01", mac: "AA:BB:CC:DD:EE:01", subnet_cidr: "10.0.1.0/24", source: "lease" },
          { ip: "10.0.1.11", state: "reserved", hostname: "switch-01", mac: "AA:BB:CC:DD:EE:02", subnet_cidr: "10.0.1.0/24", source: "reservation" },
        ],
        error: new Error("boom"),
        refetch,
      }),
    )

    render(<AddressesPage />)

    fireEvent.change(screen.getByLabelText("Search address records"), { target: { value: "camera" } })

    await waitFor(() => {
      expect(screen.getByText("camera-01")).toBeInTheDocument()
      expect(screen.queryByText("switch-01")).not.toBeInTheDocument()
      expect(screen.getByText("Unable to load addresses")).toBeInTheDocument()
    })

    fireEvent.click(screen.getByRole("button", { name: "Retry" }))
    expect(refetch).toHaveBeenCalledTimes(1)
  })
})
