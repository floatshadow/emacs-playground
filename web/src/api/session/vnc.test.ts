import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { VNC_RESIZE_DEBOUNCE_MS, VncClient } from './vnc'

// A stand-in for noVNC's RFB: records the properties the client sets.
const made: FakeRfb[] = []
class FakeRfb extends EventTarget {
  scaleViewport = false
  clipViewport = true
  resizeSession = false
  showDotCursor = false
  background = ''
  constructor() {
    super()
    made.push(this)
  }
  disconnect() {}
  focus() {}
  clipboardPasteFrom() {}
}
vi.mock('@novnc/novnc', () => ({ default: FakeRfb }))

// A ResizeObserver the test triggers by hand.
let observed: (() => void) | null = null
class FakeResizeObserver {
  constructor(cb: () => void) {
    observed = cb
  }
  observe() {}
  disconnect() {
    observed = null
  }
}

function pane(w: number, h: number): HTMLElement {
  const el = document.createElement('div')
  Object.defineProperty(el, 'clientWidth', { configurable: true, get: () => w })
  Object.defineProperty(el, 'clientHeight', { configurable: true, get: () => h })
  return el
}

describe('VncClient (B13: the X screen follows the pane)', () => {
  beforeEach(() => {
    made.length = 0
    vi.useFakeTimers()
    vi.stubGlobal('ResizeObserver', FakeResizeObserver)
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('asks for the pane size, scales only as a fallback, and debounces resizes', async () => {
    let size = { w: 900, h: 700 }
    const el = pane(0, 0)
    Object.defineProperty(el, 'clientWidth', { get: () => size.w })
    Object.defineProperty(el, 'clientHeight', { get: () => size.h })
    const c = new VncClient({ url: () => 'ws://x/vnc', onState: () => {}, wanted: () => true })
    await c.connect(el)
    const rfb = made[0] as FakeRfb
    expect(rfb.resizeSession).toBe(true)
    expect(rfb.scaleViewport).toBe(true)
    expect(rfb.clipViewport).toBe(false)
    // A burst of size changes holds the request until 200 ms after the last.
    size = { w: 1000, h: 700 }
    observed?.()
    expect(rfb.resizeSession).toBe(false)
    vi.advanceTimersByTime(VNC_RESIZE_DEBOUNCE_MS - 50)
    observed?.()
    vi.advanceTimersByTime(VNC_RESIZE_DEBOUNCE_MS - 50)
    expect(rfb.resizeSession).toBe(false)
    vi.advanceTimersByTime(60)
    expect(rfb.resizeSession).toBe(true)
    // Hidden (terminal mode): no request for a zero-size screen.
    size = { w: 0, h: 0 }
    observed?.()
    vi.advanceTimersByTime(1000)
    expect(rfb.resizeSession).toBe(false)
    c.disconnect()
    expect(observed).toBeNull()
  })
})
