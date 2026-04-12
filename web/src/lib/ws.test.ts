import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { connectLiveSocket } from "@/lib/ws"

type Listener = (event?: Event | MessageEvent) => void

class MockWebSocket {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3
  static instances: MockWebSocket[] = []

  readonly url: string
  readyState = MockWebSocket.CONNECTING
  sent: string[] = []
  private listeners = new Map<string, Listener[]>()

  constructor(url: string) {
    this.url = url
    MockWebSocket.instances.push(this)
  }

  addEventListener(type: string, listener: Listener) {
    const current = this.listeners.get(type) ?? []
    current.push(listener)
    this.listeners.set(type, current)
  }

  send(payload: string) {
    this.sent.push(payload)
  }

  close() {
    this.readyState = MockWebSocket.CLOSED
    this.emit("close", new Event("close"))
  }

  open() {
    this.readyState = MockWebSocket.OPEN
    this.emit("open", new Event("open"))
  }

  message(payload: string) {
    this.emit("message", new MessageEvent("message", { data: payload }))
  }

  error() {
    this.emit("error", new Event("error"))
  }

  private emit(type: string, event: Event | MessageEvent) {
    for (const listener of this.listeners.get(type) ?? []) {
      listener(event)
    }
  }
}

describe("connectLiveSocket", () => {
  function firstSocket() {
    const socket = MockWebSocket.instances[0]
    expect(socket).toBeDefined()
    return socket as MockWebSocket
  }

  beforeEach(() => {
    MockWebSocket.instances = []
    vi.useFakeTimers()
    vi.stubGlobal("WebSocket", MockWebSocket)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it("subscribes on open and forwards decoded events", () => {
    const onOpen = vi.fn()
    const onEvent = vi.fn()
    const client = connectLiveSocket({
      events: ["lease.created"],
      onOpen,
      onEvent,
    })

    const socket = firstSocket()
    expect(socket.url).toBe(`${window.location.protocol === "https:" ? "wss:" : "ws:"}//${window.location.host}/ws`)

    socket.open()

    expect(onOpen).toHaveBeenCalledOnce()
    expect(socket.sent).toEqual([JSON.stringify({ action: "subscribe", events: ["lease.created"] })])

    socket.message(JSON.stringify({ type: "lease.created", data: { ip: "10.0.1.50" } }))

    expect(onEvent).toHaveBeenCalledWith({ type: "lease.created", data: { ip: "10.0.1.50" } })

    client.close()
  })

  it("reports parse errors and reconnects after close", () => {
    const onError = vi.fn()
    const onClose = vi.fn()
    const client = connectLiveSocket({
      onError,
      onClose,
      onEvent: vi.fn(),
    })

    const socket = firstSocket()
    socket.message("{not-json")
    expect(onError).toHaveBeenCalledOnce()

    socket.close()
    expect(onClose).toHaveBeenCalledOnce()

    vi.advanceTimersByTime(500)
    expect(MockWebSocket.instances).toHaveLength(2)

    client.close()
  })
})
