import { render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { OverviewPage } from "@/pages/overview-page"

const mockUseDashboard = vi.fn()

vi.mock("@/app/dashboard-context", () => ({
  useDashboard: () => mockUseDashboard(),
}))

vi.mock("@/components/shared/stats-grid-skeleton", () => ({
  StatsGridSkeleton: () => <div data-testid="stats-grid-skeleton" />,
}))

vi.mock("@/components/shared/record-list-skeleton", () => ({
  RecordListSkeleton: () => <div data-testid="record-list-skeleton" />,
}))

vi.mock("@/components/ui/skeleton", () => ({
  Skeleton: () => <div data-testid="inline-skeleton" />,
}))

function makeDashboard(overrides: Record<string, unknown> = {}) {
  return {
    leases: [],
    health: {
      status: "healthy",
      uptime: "15m",
      components: {
        dhcpv4: {
          running: true,
          listen: ":67",
        },
      },
    },
    systemInfo: {
      ha: {
        role: "primary",
        peer: "connected",
        peer_node: "secondary-1",
        heartbeat_latency: 25_000_000,
        sync_lag: 10_000_000,
        failover_count: 2,
        priority: 120,
        fenced: false,
      },
    },
    loading: false,
    error: null,
    ...overrides,
  }
}

describe("OverviewPage", () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it("shows skeleton states while overview telemetry is loading", () => {
    mockUseDashboard.mockReturnValue(
      makeDashboard({
        loading: true,
      }),
    )

    render(<OverviewPage />)

    expect(screen.getByTestId("stats-grid-skeleton")).toBeInTheDocument()
    expect(screen.getByTestId("record-list-skeleton")).toBeInTheDocument()
    expect(screen.getAllByTestId("inline-skeleton")).not.toHaveLength(0)
    expect(screen.queryByText("Capacity pulse")).not.toBeInTheDocument()
  })

  it("renders active metrics, service posture, and HA posture from live data", () => {
    mockUseDashboard.mockReturnValue(
      makeDashboard({
        leases: [
          { ip: "10.0.0.10", mac: "aa:bb:cc:dd:ee:01", state: "bound" },
          { ip: "10.0.0.11", mac: "aa:bb:cc:dd:ee:02", state: "renewing" },
          { ip: "10.0.0.12", mac: "aa:bb:cc:dd:ee:03", state: "offered" },
          { ip: "10.0.0.13", mac: "aa:bb:cc:dd:ee:04", state: "expired" },
        ],
      }),
    )

    render(<OverviewPage />)

    expect(screen.getByText("Live Overview")).toBeInTheDocument()
    expect(screen.getByText("Capacity pulse")).toBeInTheDocument()
    expect(screen.getByText("Service status")).toBeInTheDocument()
    expect(screen.getByText("HA posture")).toBeInTheDocument()
    expect(screen.getByText("Bound + renewing clients").parentElement).toHaveTextContent("2")
    expect(screen.getByText("Clients in discovery phase").parentElement).toHaveTextContent("1")
    expect(screen.getByText("Peer connected")).toBeInTheDocument()
    expect(screen.getByText("secondary-1 (connected)")).toBeInTheDocument()
    expect(screen.getByText("15s polling")).toBeInTheDocument()
  })

  it("surfaces the shared page error state when the API is unreachable", () => {
    mockUseDashboard.mockReturnValue(
      makeDashboard({
        error: "Dial tcp 127.0.0.1:8443: connect: connection refused",
      }),
    )

    render(<OverviewPage />)

    expect(screen.getByText("API unreachable")).toBeInTheDocument()
    expect(screen.getByText(/connection refused/i)).toBeInTheDocument()
  })
})
