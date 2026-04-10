export type LiveEvent = {
  type: string
  timestamp?: string
  data?: Record<string, unknown>
}

type SocketOptions = {
  events?: string[]
  onEvent: (event: LiveEvent) => void
  onOpen?: () => void
  onClose?: () => void
  onError?: () => void
}

type SocketClient = {
  close: () => void
}

const DEFAULT_EVENTS = [
  "lease.*",
  "discovery.*",
  "address.*",
  "subnet.*",
  "reservation.*",
  "settings.*",
  "ha.*",
]

export function connectLiveSocket(options: SocketOptions): SocketClient {
  const protocol = window.location.protocol === "https:" ? "wss:" : "ws:"
  const socketURL = `${protocol}//${window.location.host}/ws`
  const events = options.events && options.events.length > 0 ? options.events : DEFAULT_EVENTS

  let socket: WebSocket | null = null
  let reconnectTimer: number | null = null
  let reconnectAttempt = 0
  let closed = false

  const clearReconnectTimer = () => {
    if (reconnectTimer !== null) {
      window.clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
  }

  const scheduleReconnect = () => {
    if (closed) {
      return
    }
    clearReconnectTimer()
    const delay = Math.min(15000, 500 * 2 ** reconnectAttempt)
    reconnectAttempt += 1
    reconnectTimer = window.setTimeout(connect, delay)
  }

  const subscribe = () => {
    if (!socket || socket.readyState !== WebSocket.OPEN) {
      return
    }
    socket.send(
      JSON.stringify({
        action: "subscribe",
        events,
      }),
    )
  }

  const connect = () => {
    clearReconnectTimer()
    socket = new WebSocket(socketURL)

    socket.addEventListener("open", () => {
      reconnectAttempt = 0
      subscribe()
      options.onOpen?.()
    })

    socket.addEventListener("message", (message) => {
      if (typeof message.data !== "string" || !message.data.trim()) {
        return
      }
      try {
        const event = JSON.parse(message.data) as LiveEvent
        if (event?.type) {
          options.onEvent(event)
        }
      } catch {
        options.onError?.()
      }
    })

    socket.addEventListener("close", () => {
      options.onClose?.()
      scheduleReconnect()
    })

    socket.addEventListener("error", () => {
      options.onError?.()
    })
  }

  connect()

  return {
    close: () => {
      closed = true
      clearReconnectTimer()
      if (socket && socket.readyState === WebSocket.OPEN) {
        socket.close()
      } else if (socket) {
        socket.close()
      }
      socket = null
    },
  }
}
