import { afterEach, describe, expect, it, vi } from "vitest"

import { ApiError, fetchHealth, login, restoreSystemBackup } from "@/lib/api"

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe("api client", () => {
  it("sends JSON requests with credentials and returns envelope data", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ data: { status: "ok" } }), {
        status: 200,
        headers: { "content-type": "application/json" },
      }),
    )
    vi.stubGlobal("fetch", fetchMock)

    await expect(fetchHealth()).resolves.toEqual({ status: "ok" })
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/system/health",
      expect.objectContaining({
        credentials: "include",
        headers: expect.objectContaining({
          "Content-Type": "application/json",
        }),
      }),
    )
  })

  it("surfaces API errors with status and code", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ error: { message: "invalid credentials", code: "invalid_credentials" } }), {
        status: 401,
        headers: { "content-type": "application/json" },
      }),
    )
    vi.stubGlobal("fetch", fetchMock)

    await expect(login("admin", "wrong-password")).rejects.toEqual(
      expect.objectContaining<ApiError>({
        name: "ApiError",
        message: "invalid credentials",
        status: 401,
        code: "invalid_credentials",
      }),
    )
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/auth/login",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ username: "admin", password: "wrong-password" }),
      }),
    )
  })

  it("posts restore requests to the system restore endpoint", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ data: { name: "monsoon-existing.snapshot" } }), {
        status: 200,
        headers: { "content-type": "application/json" },
      }),
    )
    vi.stubGlobal("fetch", fetchMock)

    await expect(restoreSystemBackup({ name: "monsoon-existing.snapshot" })).resolves.toEqual({ name: "monsoon-existing.snapshot" })
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/system/restore",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ name: "monsoon-existing.snapshot" }),
      }),
    )
  })
})
