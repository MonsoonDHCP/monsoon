import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ConnectionBanner } from "@/components/layout/connection-banner"

const mockUseDashboard = vi.fn()

vi.mock("@/app/dashboard-context", () => ({
  useDashboard: () => mockUseDashboard(),
}))

describe("ConnectionBanner", () => {
  it("does not render when websocket connectivity is healthy", () => {
    mockUseDashboard.mockReturnValue({
      liveConnection: "websocket",
      reload: vi.fn(),
    })

    render(<ConnectionBanner />)

    expect(screen.queryByText(/Realtime connection is offline/i)).not.toBeInTheDocument()
  })

  it("renders offline state, retries sync, and can be dismissed", () => {
    const reload = vi.fn()
    mockUseDashboard.mockReturnValue({
      liveConnection: "offline",
      reload,
    })

    render(<ConnectionBanner />)

    expect(screen.getByText("Realtime connection is offline. Updates may lag until the stream reconnects.")).toBeInTheDocument()

    fireEvent.click(screen.getByRole("button", { name: "Retry sync" }))
    expect(reload).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole("button", { name: "Dismiss connection banner" }))
    expect(screen.queryByText("Realtime connection is offline. Updates may lag until the stream reconnects.")).not.toBeInTheDocument()
  })
})
