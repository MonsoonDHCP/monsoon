import { fireEvent, render, screen } from "@testing-library/react"
import { House, Settings2 } from "lucide-react"
import { MemoryRouter } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"

import { Sidebar } from "@/components/layout/sidebar"

const { preloadOverview, preloadSettings } = vi.hoisted(() => ({
  preloadOverview: vi.fn().mockResolvedValue(undefined),
  preloadSettings: vi.fn().mockResolvedValue(undefined),
}))

vi.mock("@/app/navigation", () => ({
  navItems: [
    { to: "/", label: "Overview", icon: House, preload: preloadOverview },
    { to: "/settings", label: "Settings", icon: Settings2, preload: preloadSettings },
  ],
}))

describe("Sidebar", () => {
  it("prefetches routes on hover and focus, and notifies on navigation", () => {
    const onNavigate = vi.fn()

    render(
      <MemoryRouter initialEntries={["/"]}>
        <Sidebar onNavigate={onNavigate} />
      </MemoryRouter>,
    )

    const settingsLink = screen.getByRole("link", { name: "Settings" })

    fireEvent.mouseEnter(settingsLink)
    fireEvent.focus(settingsLink)
    fireEvent.click(settingsLink)

    expect(preloadSettings).toHaveBeenCalledTimes(2)
    expect(onNavigate).toHaveBeenCalledTimes(1)
  })
})
