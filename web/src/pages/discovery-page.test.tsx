import { fireEvent, render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { DiscoveryPage } from "@/pages/discovery-page"

const mockUseDashboard = vi.fn()

vi.mock("@/app/dashboard-context", () => ({
  useDashboard: () => mockUseDashboard(),
}))

function makeDashboard(overrides: Record<string, unknown> = {}) {
  return {
    discovery: {
      scanning: false,
      sensor_online: true,
      last_scan_at: "2026-04-12T10:00:00Z",
      latest_scan_id: "scan-1",
      active_conflicts: 0,
      next_scheduled_scan: "2026-04-12T11:00:00Z",
    },
    discoveryProgress: {
      percent: 0,
      phase: "idle",
      processed: 0,
      total: 0,
    },
    discoveryResults: [],
    discoveryConflicts: [],
    rogueServers: [],
    triggerScan: vi.fn().mockResolvedValue(undefined),
    canMutate: true,
    ...overrides,
  }
}

describe("DiscoveryPage", () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it("triggers a discovery scan and shows empty anomaly/history states when no results exist", () => {
    const triggerScan = vi.fn().mockResolvedValue(undefined)
    mockUseDashboard.mockReturnValue(
      makeDashboard({
        triggerScan,
      }),
    )

    render(<DiscoveryPage />)

    expect(screen.getByText("No discovery results yet")).toBeInTheDocument()
    expect(screen.getByText("No active anomalies")).toBeInTheDocument()

    fireEvent.click(screen.getByRole("button", { name: "Trigger scan" }))

    expect(triggerScan).toHaveBeenCalledTimes(1)
  })

  it("renders persisted results and anomaly findings from the latest discovery pass", () => {
    mockUseDashboard.mockReturnValue(
      makeDashboard({
        discovery: {
          scanning: true,
          sensor_online: false,
          last_scan_at: "2026-04-12T10:00:00Z",
          latest_scan_id: "scan-7",
          active_conflicts: 2,
          next_scheduled_scan: "2026-04-12T11:00:00Z",
        },
        discoveryProgress: {
          percent: 45,
          phase: "arp-sweep",
          processed: 45,
          total: 100,
        },
        discoveryResults: [
          { scan_id: "scan-7", status: "completed", total_hosts: 30, new_hosts: 3, changed_hosts: 2, missing_hosts: 1 },
        ],
        discoveryConflicts: [
          { ip: "10.0.1.15", macs: ["AA:BB:CC:DD:EE:01", "AA:BB:CC:DD:EE:FF"], severity: "high" },
        ],
        rogueServers: [
          { ip: "10.0.1.2", detected: "2026-04-12T10:01:00Z", source: "active-probe" },
        ],
      }),
    )

    render(<DiscoveryPage />)

    expect(screen.getByRole("button", { name: "Scanning..." })).toBeDisabled()
    expect(screen.getByText("2 conflict(s)")).toBeInTheDocument()
    expect(screen.getByText("scan-7")).toBeInTheDocument()
    expect(screen.getByText(/hosts: 30 | new: 3 | changed: 2 | missing: 1/i)).toBeInTheDocument()
    expect(screen.getByText("10.0.1.15")).toBeInTheDocument()
    expect(screen.getByText("Rogue DHCP: 10.0.1.2")).toBeInTheDocument()
  })
})
