/**
 * EventsClient (frontend.md §4): /s/{id}/events?after=<lastSeq>, one JSON
 * event per frame, dispatched to applyEvent. Reconnects with backoff
 * (250 ms to 8 s, jitter), pings every 20 s, and reports gap events so the
 * caller can request a fresh check.
 */
import type { Event, GapEvent } from '../../protocol.gen'
import type { ConnState } from '../../store/session'
import { backoffDelay } from './backoff'

export const EVENTS_PING_MS = 20000

export interface EventsClientOptions {
  /** URL for a connection resuming after seq `after`. */
  url(after: number): string
  getLastSeq(): number
  onEvent(ev: Event): void
  onState(s: ConnState): void
  onGap?(ev: GapEvent): void
  WebSocketImpl?: typeof WebSocket
  random?: () => number
}

export class EventsClient {
  private ws: WebSocket | null = null
  private attempt = 0
  private stopped = true
  private pingTimer: ReturnType<typeof setInterval> | null = null
  private retryTimer: ReturnType<typeof setTimeout> | null = null
  private everOpened = false
  private readonly opts: EventsClientOptions

  constructor(opts: EventsClientOptions) {
    this.opts = opts
  }

  get running(): boolean {
    return !this.stopped
  }

  start(): void {
    if (!this.stopped) return
    this.stopped = false
    this.connect()
  }

  stop(): void {
    this.stopped = true
    this.clearTimers()
    const ws = this.ws
    this.ws = null
    if (ws) {
      ws.onopen = ws.onmessage = ws.onclose = ws.onerror = null
      try {
        ws.close(1000, 'client stop')
      } catch {
        // already closed
      }
    }
    this.opts.onState('closed')
  }

  /** Drops the connection and reconnects now (used after a gap). */
  reconnect(): void {
    if (this.stopped) return
    this.ws?.close(4100, 'reconnect')
  }

  private clearTimers() {
    if (this.pingTimer) clearInterval(this.pingTimer)
    if (this.retryTimer) clearTimeout(this.retryTimer)
    this.pingTimer = null
    this.retryTimer = null
  }

  private connect() {
    if (this.stopped) return
    const WS = this.opts.WebSocketImpl ?? WebSocket
    this.opts.onState(this.everOpened ? 'reconnecting' : 'connecting')
    let ws: WebSocket
    try {
      ws = new WS(this.opts.url(this.opts.getLastSeq()))
    } catch {
      this.scheduleRetry()
      return
    }
    this.ws = ws
    ws.onopen = () => {
      this.everOpened = true
      this.opts.onState('open')
      if (this.pingTimer) clearInterval(this.pingTimer)
      this.pingTimer = setInterval(() => {
        if (ws.readyState === 1) ws.send(JSON.stringify({ t: 'ping' }))
      }, EVENTS_PING_MS)
    }
    ws.onmessage = (m: MessageEvent) => {
      if (typeof m.data !== 'string') return
      let ev: Event
      try {
        ev = JSON.parse(m.data) as Event
      } catch {
        return
      }
      if (!ev || typeof ev !== 'object' || typeof (ev as { t?: unknown }).t !== 'string') return
      if ((ev as { t: string }).t === 'pong') return
      if (ev.t === 'hello') this.attempt = 0
      this.opts.onEvent(ev)
      if (ev.t === 'gap') this.opts.onGap?.(ev)
    }
    ws.onerror = () => {
      // onclose follows and schedules the retry.
    }
    ws.onclose = () => {
      if (this.ws === ws) this.ws = null
      if (this.pingTimer) clearInterval(this.pingTimer)
      this.pingTimer = null
      if (this.stopped) return
      this.scheduleRetry()
    }
  }

  private scheduleRetry() {
    if (this.stopped) return
    this.opts.onState('reconnecting')
    const delay = backoffDelay(this.attempt++, this.opts.random)
    this.retryTimer = setTimeout(() => {
      this.retryTimer = null
      this.connect()
    }, delay)
  }
}
