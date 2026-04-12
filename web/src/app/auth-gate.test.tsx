import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { AuthGate } from "@/app/auth-gate"

vi.mock("@/components/layout/theme-toggle", () => ({
  ThemeToggle: () => <div data-testid="theme-toggle" />,
}))

describe("AuthGate", () => {
  it("submits login credentials from the form", async () => {
    const onLogin = vi.fn().mockResolvedValue(undefined)

    render(<AuthGate busy={false} error={null} onLogin={onLogin} onBootstrap={vi.fn()} />)

    fireEvent.change(screen.getByPlaceholderText("Username"), { target: { value: "operator" } })
    fireEvent.change(screen.getByPlaceholderText("Password"), { target: { value: "secret-pass" } })
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Login" })).toBeEnabled()
    })
    fireEvent.click(screen.getByRole("button", { name: "Login" }))

    await waitFor(() => {
      expect(onLogin).toHaveBeenCalledWith("operator", "secret-pass")
    })
  })

  it("shows the shared auth error and supports bootstrap", async () => {
    const onBootstrap = vi.fn().mockResolvedValue(undefined)

    render(
      <AuthGate
        busy={false}
        error="Authentication required. Please sign in."
        onLogin={vi.fn()}
        onBootstrap={onBootstrap}
      />,
    )

    expect(screen.getByText("Authentication required. Please sign in.")).toBeInTheDocument()

    fireEvent.change(screen.getByPlaceholderText("Password"), { target: { value: "bootstrap-pass" } })
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Bootstrap Admin" })).toBeEnabled()
    })
    fireEvent.click(screen.getByRole("button", { name: "Bootstrap Admin" }))

    await waitFor(() => {
      expect(onBootstrap).toHaveBeenCalledWith("admin", "bootstrap-pass")
    })
  })
})
