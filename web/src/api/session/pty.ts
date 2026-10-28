/**
 * PtyClient (frontend.md §4, api.md §3.1): a binary WebSocket carrying
 * terminal bytes both ways; text frames are control messages. Resize frames
 * are {"t":"resize","cols","rows"}. Close 4000 means another viewer took
 * over: no automatic reconnect, the pane offers Reattach. Close 4001 means
 * Emacs is unavailable: retry with backoff.
 */
import {
  CloseEmacsUnavailable,
  CloseReplaced,
  CloseSessionStopping,
  type PtyControl,
} from '../../protocol.gen'
import type { ConnState } from '../../store/session'
import { backoffDelay } from './backoff'

export interface PtyClientOptions {
  url(cols: number, rows: number): string
  onData(bytes: Uint8Array): void
  onState(s: ConnState): void
  /** Server control frames ({"t":"attached"}, {"t":"replaced"}). */
  onControl?(msg: PtyControl): void
  WebSocketImpl?: typeof WebSocket
  random?: () => number
}

const encoder = new TextEncoder()

export class PtyClient {
  private ws: WebSocket | null = null
  private attempt = 0
  private stopped = true
  private retryTimer: ReturnType<typeof setTimeout> | null = null
  private pingTimer: ReturnType<typeof setInterval> | null = null
  private size = { cols: 120, rows: 36 }
  private everOpened = false
  private readonly opts: PtyClientOptions
  state: ConnState = 'idle'

  constructor(opts: PtyClientOptions) {
    this.opts = opts
  }

  private setState(s: ConnState) {
    this.state = s
    this.opts.onState(s)
  }

  connect(cols: number, rows: number): void {
    this.size = { cols, rows }
    if (!this.stopped) return
    this.stopped = false
    this.open()
  }

  /** Takes the terminal back after "attached elsewhere". */
  reattach(): void {
    this.stopped = false
    this.attempt = 0
    if (this.retryTimer) clearTimeout(this.retryTimer)
    this.retryTimer = null
    if (this.ws) return
    this.open()
  }

  close(): void {
    this.stopped = true
    if (this.retryTimer) clearTimeout(this.retryTimer)
    if (this.pingTimer) clearInterval(this.pingTimer)
    this.retryTimer = null
    this.pingTimer = null
    const ws = this.ws
    this.ws = null
    if (ws) {
      ws.onopen = ws.onmessage = ws.onclose = ws.onerror = null
      try {
        ws.close(1000, 'client stop')
      } catch {
        // ignore
      }
    }
    this.setState('closed')
  }

  /** Sends terminal input: strings are UTF-8 encoded, bytes go as they are. */
  send(data: string | Uint8Array<ArrayBuffer>): void {
    const ws = this.ws
    if (!ws || ws.readyState !== 1) return
    ws.send(typeof data === 'string' ? encoder.encode(data) : data)
  }

  resize(cols: number, rows: number): void {
    if (cols === this.size.cols && rows === this.size.rows) return
    this.size = { cols, rows }
    this.sendControl({ t: 'resize', cols, rows })
  }

  private sendControl(msg: PtyControl) {
    const ws = this.ws
    if (ws && ws.readyState === 1) ws.send(JSON.stringify(msg))
  }

  private open() {
    const WS = this.opts.WebSocketImpl ?? WebSocket
    this.setState(this.everOpened ? 'reconnecting' : 'connecting')
    let ws: WebSocket
    try {
      ws = new WS(this.opts.url(this.size.cols, this.size.rows))
    } catch {
      this.retry()
      return
    }
    ws.binaryType = 'arraybuffer'
    this.ws = ws
    ws.onopen = () => {
      this.everOpened = true
      this.attempt = 0
      this.setState('open')
      // The server may have attached at a stale size; send ours.
      ws.send(JSON.stringify({ t: 'resize', cols: this.size.cols, rows: this.size.rows }))
      if (this.pingTimer) clearInterval(this.pingTimer)
      this.pingTimer = setInterval(() => this.sendControl({ t: 'ping' }), 20000)
    }
    ws.onmessage = (m: MessageEvent) => {
      if (typeof m.data === 'string') {
        try {
          const msg = JSON.parse(m.data) as PtyControl
          if (msg.t === 'replaced') this.setState('replaced')
          this.opts.onControl?.(msg)
        } catch {
          // not a control frame
        }
        return
      }
      if (m.data instanceof ArrayBuffer) this.opts.onData(new Uint8Array(m.data))
      else if (ArrayBuffer.isView(m.data)) this.opts.onData(new Uint8Array(m.data.buffer))
    }
    ws.onclose = (e: CloseEvent) => {
      if (this.ws === ws) this.ws = null
      if (this.pingTimer) clearInterval(this.pingTimer)
      this.pingTimer = null
      if (this.stopped) return
      if (e.code === CloseReplaced) {
        this.stopped = true
        this.setState('replaced')
        return
      }
      if (e.code === CloseEmacsUnavailable) this.setState('unavailable')
      this.retry(e.code === CloseSessionStopping ? 4 : 0)
    }
    ws.onerror = () => {}
  }

  private retry(minAttempt = 0) {
    if (this.stopped) return
    if (this.state !== 'unavailable') this.setState('reconnecting')
    const delay = backoffDelay(Math.max(minAttempt, this.attempt++), this.opts.random)
    this.retryTimer = setTimeout(() => {
      this.retryTimer = null
      if (!this.stopped) this.open()
    }, delay)
  }
}
