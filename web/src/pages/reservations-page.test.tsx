import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { ReservationsPage } from "@/pages/reservations-page"

const mockUseDashboard = vi.fn()

vi.mock("@/app/dashboard-context", () => ({
  useDashboard: () => mockUseDashboard(),
}))

function makeDashboard(overrides: Record<string, unknown> = {}) {
  return {
    reservations: [],
    saveReservation: vi.fn().mockResolvedValue(undefined),
    removeReservation: vi.fn().mockResolvedValue(undefined),
    canMutate: true,
    ...overrides,
  }
}

describe("ReservationsPage", () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it("normalizes MAC format before saving a reservation and resets the form", async () => {
    const saveReservation = vi.fn().mockResolvedValue(undefined)
    mockUseDashboard.mockReturnValue(
      makeDashboard({
        saveReservation,
      }),
    )

    render(<ReservationsPage />)

    fireEvent.change(screen.getByLabelText("MAC address"), { target: { value: "aa-bb-cc-dd-ee-ff" } })
    fireEvent.change(screen.getByLabelText("IPv4 address"), { target: { value: "10.0.1.25" } })
    fireEvent.change(screen.getByLabelText("Hostname"), { target: { value: "camera-01" } })
    fireEvent.change(screen.getByLabelText("Subnet CIDR"), { target: { value: "10.0.1.0/24" } })

    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Save reservation" })).toBeEnabled()
    })

    fireEvent.click(screen.getByRole("button", { name: "Save reservation" }))

    await waitFor(() => {
      expect(saveReservation).toHaveBeenCalledWith({
        mac: "AA:BB:CC:DD:EE:FF",
        ip: "10.0.1.25",
        hostname: "camera-01",
        subnet_cidr: "10.0.1.0/24",
      })
    })

    await waitFor(() => {
      expect(screen.getByLabelText("MAC address")).toHaveValue("")
      expect(screen.getByLabelText("IPv4 address")).toHaveValue("")
    })
  })

  it("filters the visible reservation list by the active search query", async () => {
    mockUseDashboard.mockReturnValue(
      makeDashboard({
        reservations: [
          { mac: "AA:BB:CC:DD:EE:01", ip: "10.0.1.10", hostname: "camera-01", subnet_cidr: "10.0.1.0/24" },
          { mac: "AA:BB:CC:DD:EE:02", ip: "10.0.2.10", hostname: "switch-01", subnet_cidr: "10.0.2.0/24" },
        ],
      }),
    )

    render(<ReservationsPage />)

    fireEvent.change(screen.getByPlaceholderText("Search reservation list..."), { target: { value: "camera" } })

    await waitFor(() => {
      expect(screen.getByText("camera-01")).toBeInTheDocument()
      expect(screen.queryByText("switch-01")).not.toBeInTheDocument()
      expect(screen.getByText("1 records")).toBeInTheDocument()
    })
  })
})
