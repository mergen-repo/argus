import { useAuthStore } from '@/stores/auth'

type EventHandler = (data: unknown) => void

class WebSocketClient {
  private ws: WebSocket | null = null
  private handlers = new Map<string, Set<EventHandler>>()
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private reconnectDelay = 1000
  private maxReconnectDelay = 30000
  private url: string

  constructor() {
    const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws'
    this.url = `${protocol}://${window.location.host}/ws`
  }

  connect() {
    const token = useAuthStore.getState().token
    if (!token) return

    try {
      this.ws = new WebSocket(`${this.url}?token=${token}`)

      this.ws.onopen = () => {
        this.reconnectDelay = 1000
      }

      this.ws.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data)
          const handlers = this.handlers.get(msg.type)
          if (handlers) {
            handlers.forEach((handler) => handler(msg.data))
          }
          const allHandlers = this.handlers.get('*')
          if (allHandlers) {
            allHandlers.forEach((handler) => handler(msg))
          }
        } catch {
          // ignore malformed messages
        }
      }

      this.ws.onclose = () => {
        this.scheduleReconnect()
      }

      this.ws.onerror = () => {
        this.ws?.close()
      }
    } catch {
      this.scheduleReconnect()
    }
  }

  private scheduleReconnect() {
    if (this.reconnectTimer) return
    const token = useAuthStore.getState().token
    if (!token) return

    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      this.connect()
    }, this.reconnectDelay)

    this.reconnectDelay = Math.min(this.reconnectDelay * 2, this.maxReconnectDelay)
  }

  disconnect() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    if (this.ws) {
      this.ws.onclose = null
      this.ws.close()
      this.ws = null
    }
  }

  on(event: string, handler: EventHandler) {
    if (!this.handlers.has(event)) {
      this.handlers.set(event, new Set())
    }
    this.handlers.get(event)!.add(handler)
    return () => {
      this.handlers.get(event)?.delete(handler)
    }
  }

  send(type: string, data: unknown) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify({ type, data }))
    }
  }
}

export const wsClient = new WebSocketClient()
