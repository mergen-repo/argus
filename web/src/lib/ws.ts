import { useAuthStore } from '@/stores/auth'

type EventHandler = (data: unknown) => void
type EnvelopeHandler = (envelope: WSEnvelope) => void

export interface WSEnvelope {
  id: string
  type: string
  timestamp: string
  data: unknown
}

export type WSStatus = 'connected' | 'connecting' | 'disconnected'

type StatusHandler = (status: WSStatus) => void

class WebSocketClient {
  private ws: WebSocket | null = null
  private handlers = new Map<string, Set<EventHandler>>()
  private envelopeHandlers = new Map<string, Set<EnvelopeHandler>>()
  private statusHandlers = new Set<StatusHandler>()
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private reconnectDelay = 1000
  private maxReconnectDelay = 30000
  private url: string
  private _status: WSStatus = 'disconnected'

  constructor() {
    const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws'
    this.url = `${protocol}://${window.location.host}/ws/v1/events`
  }

  private setStatus(status: WSStatus) {
    this._status = status
    this.statusHandlers.forEach((handler) => handler(status))
  }

  getStatus(): WSStatus {
    return this._status
  }

  reconnectNow() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    this.connect()
  }

  connect() {
    const token = useAuthStore.getState().token
    if (!token) return

    this.setStatus('connecting')

    try {
      this.ws = new WebSocket(`${this.url}?token=${token}`)

      this.ws.onopen = () => {
        this.reconnectDelay = 1000
        this.setStatus('connected')
      }

      this.ws.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data) as WSEnvelope
          const handlers = this.handlers.get(msg.type)
          if (handlers) {
            handlers.forEach((handler) => handler(msg.data))
          }
          const allHandlers = this.handlers.get('*')
          if (allHandlers) {
            allHandlers.forEach((handler) => handler(msg))
          }
          const envHandlers = this.envelopeHandlers.get(msg.type)
          if (envHandlers) {
            envHandlers.forEach((handler) => handler(msg))
          }
        } catch {
          // ignore malformed messages
        }
      }

      this.ws.onclose = () => {
        this.setStatus('disconnected')
        this.scheduleReconnect()
      }

      this.ws.onerror = () => {
        this.ws?.close()
      }
    } catch {
      this.setStatus('disconnected')
      this.scheduleReconnect()
    }
  }

  private scheduleReconnect() {
    if (this.reconnectTimer) return
    const token = useAuthStore.getState().token
    if (!token) return

    this.setStatus('connecting')
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
    this.setStatus('disconnected')
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

  onEnvelope(event: string, handler: EnvelopeHandler) {
    if (!this.envelopeHandlers.has(event)) {
      this.envelopeHandlers.set(event, new Set())
    }
    this.envelopeHandlers.get(event)!.add(handler)
    return () => {
      this.envelopeHandlers.get(event)?.delete(handler)
    }
  }

  onStatus(handler: StatusHandler) {
    this.statusHandlers.add(handler)
    return () => {
      this.statusHandlers.delete(handler)
    }
  }

  send(type: string, data: unknown) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify({ type, data }))
    }
  }
}

export const wsClient = new WebSocketClient()
