import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { SubnetsPage } from "@/pages/subnets-page"

const mockUseDashboard = vi.fn()

vi.mock("@/app/dashboard-context", () => ({
  useDashboard: () => mockUseDashboard(),
}))

function makeDashboard(overrides: Record<string, unknown> = {}) {
  return {
    subnets: [],
    subnetRecords: [],
    saveSubnet: vi.fn().mockResolvedValue(undefined),
    removeSubnet: vi.fn().mockResolvedValue(undefined),
    canMutate: true,
    ...overrides,
  }
}

describe("SubnetsPage", () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it("submits a validated subnet payload and resets the form", async () => {
    const saveSubnet = vi.fn().mockResolvedValue(undefined)
    mockUseDashboard.mockReturnValue(
      makeDashboard({
        saveSubnet,
      }),
    )

    render(<SubnetsPage />)

    fireEvent.change(screen.getByLabelText("Subnet CIDR"), { target: { value: "10.0.1.0/24" } })
    fireEvent.change(screen.getByLabelText("Display name"), { target: { value: "Edge VLAN" } })
    fireEvent.change(screen.getByLabelText("Gateway"), { target: { value: "10.0.1.1" } })
    fireEvent.change(screen.getByLabelText("VLAN"), { target: { value: "120" } })
    fireEvent.change(screen.getByLabelText("DNS servers"), { target: { value: "10.0.1.53, 1.1.1.1" } })
    fireEvent.change(screen.getByLabelText("Pool start"), { target: { value: "10.0.1.10" } })
    fireEvent.change(screen.getByLabelText("Pool end"), { target: { value: "10.0.1.220" } })
    fireEvent.change(screen.getByLabelText("Lease time in seconds"), { target: { value: "7200" } })

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Save subnet" })).toBeEnabled()
    })

    fireEvent.click(screen.getByRole("button", { name: "Save subnet" }))

    await waitFor(() => {
      expect(saveSubnet).toHaveBeenCalledWith({
        cidr: "10.0.1.0/24",
        name: "Edge VLAN",
        vlan: 120,
        gateway: "10.0.1.1",
        dns: ["10.0.1.53", "1.1.1.1"],
        dhcp_enabled: true,
        pool_start: "10.0.1.10",
        pool_end: "10.0.1.220",
        lease_time_sec: 7200,
      })
    })

    await waitFor(() => {
      expect(screen.getByLabelText("Subnet CIDR")).toHaveValue("")
      expect(screen.getByLabelText("Display name")).toHaveValue("")
      expect(screen.getByLabelText("VLAN")).toHaveValue(0)
    })
  })

  it("shows empty states for utilization and raw subnet records when there is no data", () => {
    mockUseDashboard.mockReturnValue(makeDashboard())

    render(<SubnetsPage />)

    expect(screen.getByText("No subnets yet")).toBeInTheDocument()
    expect(screen.getByText("No subnet records")).toBeInTheDocument()
  })
})
