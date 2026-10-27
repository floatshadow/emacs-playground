/**
 * VncClient (frontend.md §4, §8): a thin wrapper around noVNC's RFB over
 * /s/{id}/vnc. noVNC is imported lazily so it stays out of the main chunk.
 * The X screen follows the pane (resizeSession: noVNC asks Xvnc for the
 * pane's size, B13), so the GUI frame is drawn at scale 1 instead of a
 * fixed 1600×900 screen scaled into a letterbox; pane resizes are debounced
 * 200 ms and a hidden (zero-size) pane asks for nothing. scaleViewport stays
 * on as the fallback when the server refuses a size. clipViewport off, dot
 * cursor on; the remote clipboard goes to the browser clipboard, and paste
 * into the focused pane goes to the remote clipboard.
 */
import type RFB from '@novnc/novnc'
import type { ConnState } from '../../store/session'
import { backoffDelay } from './backoff'

/** Quiet time after the pane's last size change before the X screen follows it. */
export const VNC_RESIZE_DEBOUNCE_MS = 200

export interface VncClientOptions {
  url(): string
  onState(s: ConnState): void
  /** Text the remote side put on its clipboard. */
  onClipboard?(text: string): void
  /** True while the session is in GUI mode; reconnects only then. */
  wanted(): boolean
}

export class VncClient {
  private rfb: RFB | null = null
  private target: HTMLElement | null = null
  private attempt = 0
  private retryTimer: ReturnType<typeof setTimeout> | null = null
  private resizeTimer: ReturnType<typeof setTimeout> | null = null
  private sizeObserver: ResizeObserver | null = null
  private stopped = true
  private readonly opts: VncClientOptions

  constructor(opts: VncClientOptions) {
    this.opts = opts
  }

  get connected(): boolean {
    return this.rfb !== null
  }

  async connect(target: HTMLElement): Promise<void> {
    this.target = target
    this.stopped = false
    if (this.rfb) return
    this.opts.onState(this.attempt ? 'reconnecting' : 'connecting')
    let RFBCtor: typeof RFB
    try {
      RFBCtor = (await import('@novnc/novnc')).default
    } catch {
      this.opts.onState('closed')
      return
    }
    if (this.stopped || this.rfb) return
    // Our observer is created before noVNC's, so its callback runs first and
    // can hold noVNC's own resize request while the pane is still changing.
    this.watchSize(target)
    const rfb = new RFBCtor(target, this.opts.url(), { shared: true })
    rfb.scaleViewport = true
    rfb.clipViewport = false
    rfb.resizeSession = target.clientWidth > 0 && target.clientHeight > 0
    rfb.showDotCursor = true
    rfb.background = '#0d0e1c'
    rfb.addEventListener('connect', () => {
      this.attempt = 0
      this.opts.onState('open')
    })
    rfb.addEventListener('disconnect', () => {
      if (this.rfb === rfb) this.rfb = null
      if (this.stopped || !this.opts.wanted()) {
        this.opts.onState('closed')
        return
      }
      this.opts.onState('reconnecting')
      const delay = backoffDelay(this.attempt++)
      this.retryTimer = setTimeout(() => {
        this.retryTimer = null
        if (this.target && !this.stopped && this.opts.wanted()) void this.connect(this.target)
      }, delay)
    })
    rfb.addEventListener('clipboard', (e: Event) => {
      const text = (e as CustomEvent<{ text: string }>).detail?.text
      if (typeof text === 'string') this.opts.onClipboard?.(text)
    })
    this.rfb = rfb
  }

  /**
   * Debounces the X screen's resize: while the pane changes size noVNC's own
   * request is held (resizeSession off); 200 ms after the last change it is
   * turned back on, which asks Xvnc for the pane's size once. A hidden pane
   * (terminal mode) has no size and asks for nothing.
   */
  private watchSize(target: HTMLElement) {
    if (this.sizeObserver || typeof ResizeObserver === 'undefined') return
    this.sizeObserver = new ResizeObserver(() => {
      const rfb = this.rfb
      if (!rfb) return
      rfb.resizeSession = false
      if (this.resizeTimer) clearTimeout(this.resizeTimer)
      this.resizeTimer = null
      if (target.clientWidth === 0 || target.clientHeight === 0) return
      this.resizeTimer = setTimeout(() => {
        this.resizeTimer = null
        if (this.rfb === rfb) rfb.resizeSession = true
      }, VNC_RESIZE_DEBOUNCE_MS)
    })
    this.sizeObserver.observe(target)
  }

  /** Sends browser clipboard text to the remote clipboard (on paste in the focused pane). */
  paste(text: string): void {
    this.rfb?.clipboardPasteFrom(text)
  }

  focus(): void {
    this.rfb?.focus()
  }

  disconnect(): void {
    this.stopped = true
    if (this.retryTimer) clearTimeout(this.retryTimer)
    this.retryTimer = null
    if (this.resizeTimer) clearTimeout(this.resizeTimer)
    this.resizeTimer = null
    this.sizeObserver?.disconnect()
    this.sizeObserver = null
    const rfb = this.rfb
    this.rfb = null
    try {
      rfb?.disconnect()
    } catch {
      // ignore
    }
    this.opts.onState('closed')
  }
}
