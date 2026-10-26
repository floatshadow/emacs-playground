import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Event } from '../../protocol.gen'
import type { ConnState } from '../../store/session'
import { BACKOFF_MAX_MS, backoffDelay } from './backoff'
import { EVENTS_PING_MS, EventsClient } from './events'
import { PtyClient } from './pty'

/** A scriptable WebSocket stand-in for the connection clients. */
class FakeWS {
  static all: FakeWS[] = []
  url: string
  readyState = 0
  binaryType = 'blob'
  sent: unknown[] = []
  closedWith: number | null = null
  onopen: (() => void) | null = null
  onmessage: ((m: { data: unknown }) => void) | null = null
  onclose: ((e: { code: number }) => void) | null = null
  onerror: (() => void) | null = null
  constructor(url: string) {
    this.url = url
    FakeWS.all.push(this)
  }
  send(d: unknown) {
    this.sent.push(d)
  }
  close(code = 1000) {
    this.closedWith = code
    this.readyState = 3
    this.onclose?.({ code })
  }
  open() {
    this.readyState = 1
    this.onopen?.()
  }
  json(o: unknown) {
    this.onmessage?.({ data: JSON.stringify(o) })
  }
  bytes(b: number[]) {
    this.onmessage?.({ data: new Uint8Array(b).buffer })
  }
  drop(code = 1006) {
    this.readyState = 3
    this.onclose?.({ code })
  }
}
const last = () => FakeWS.all[FakeWS.all.length - 1] as FakeWS

beforeEach(() => {
  FakeWS.all = []
  vi.useFakeTimers()
})
afterEach(() => vi.useRealTimers())

describe('backoff', () => {
  it('grows from 250 ms to 8 s with jitter', () => {
    expect(backoffDelay(0, () => 0)).toBe(125)
    expect(backoffDelay(0, () => 1)).toBe(250)
    expect(backoffDelay(3, () => 1)).toBe(2000)
    expect(backoffDelay(20, () => 1)).toBe(BACKOFF_MAX_MS)
    expect(backoffDelay(20, () => 0)).toBe(BACKOFF_MAX_MS / 2)
  })
})

describe('EventsClient', () => {
  function client() {
    const events: Event[] = []
    const states: ConnState[] = []
    let lastSeq = 0
    const gaps: Event[] = []
    const c = new EventsClient({
      url: (after) => `ws://x/s/local/events${after ? `?after=${after}` : ''}`,
      getLastSeq: () => lastSeq,
      onEvent: (ev) => {
        events.push(ev)
        if (typeof ev.seq === 'number') lastSeq = ev.seq
      },
      onState: (s) => states.push(s),
      onGap: (ev) => gaps.push(ev),
      WebSocketImpl: FakeWS as unknown as typeof WebSocket,
      random: () => 1,
    })
    return { c, events, states, gaps }
  }

  it('dispatches JSON frames and ignores junk and pongs', () => {
    const { c, events, states } = client()
    c.start()
    expect(last().url).toBe('ws://x/s/local/events')
    last().open()
    last().json({ t: 'hello', seq: 4, protocol: 1 })
    last().onmessage?.({ data: 'not json' })
    last().json({ t: 'pong' })
    last().json({ nope: 1 })
    expect(events.map((e) => e.t)).toEqual(['hello'])
    expect(states).toEqual(['connecting', 'open'])
    c.stop()
  })

  it('pings every 20 s while open', () => {
    const { c } = client()
    c.start()
    last().open()
    vi.advanceTimersByTime(EVENTS_PING_MS * 2 + 10)
    expect(last().sent).toEqual(['{"t":"ping"}', '{"t":"ping"}'])
    c.stop()
  })

  it('reconnects with backoff and resumes after the last seq', () => {
    const { c, states } = client()
    c.start()
    const first = last()
    first.open()
    first.json({ t: 'hello', seq: 1, protocol: 1 })
    first.json({ t: 'key', seq: 7, keys: 'C-f', cmd: 'forward-char' })
    first.drop()
    expect(states.at(-1)).toBe('reconnecting')
    vi.advanceTimersByTime(249)
    expect(FakeWS.all).toHaveLength(1)
    vi.advanceTimersByTime(1)
    expect(FakeWS.all).toHaveLength(2)
    expect(last().url).toBe('ws://x/s/local/events?after=7')
    // Failing again doubles the delay.
    last().drop()
    vi.advanceTimersByTime(499)
    expect(FakeWS.all).toHaveLength(2)
    vi.advanceTimersByTime(1)
    expect(FakeWS.all).toHaveLength(3)
    // A hello resets the backoff.
    last().open()
    last().json({ t: 'hello', seq: 8, protocol: 1, replayed: true })
    last().drop()
    vi.advanceTimersByTime(250)
    expect(FakeWS.all).toHaveLength(4)
    c.stop()
    expect(states.at(-1)).toBe('closed')
  })

  it('reports gaps', () => {
    const { c, gaps } = client()
    c.start()
    last().open()
    last().json({ t: 'gap', seq: 9, from: 2, to: 8 })
    expect(gaps).toHaveLength(1)
    c.stop()
  })

  it('stop() prevents reconnects', () => {
    const { c } = client()
    c.start()
    last().open()
    c.stop()
    vi.advanceTimersByTime(60_000)
    expect(FakeWS.all).toHaveLength(1)
  })
})

describe('PtyClient', () => {
  function client() {
    const data: number[][] = []
    const states: ConnState[] = []
    const control: unknown[] = []
    const c = new PtyClient({
      url: (cols, rows) => `ws://x/s/local/pty?cols=${cols}&rows=${rows}`,
      onData: (b) => data.push([...b]),
      onState: (s) => states.push(s),
      onControl: (m) => control.push(m),
      WebSocketImpl: FakeWS as unknown as typeof WebSocket,
      random: () => 1,
    })
    return { c, data, states, control }
  }

  it('opens with the size, sends our size on open, encodes input and writes output', () => {
    const { c, data, control } = client()
    c.connect(100, 30)
    expect(last().url).toBe('ws://x/s/local/pty?cols=100&rows=30')
    expect(last().binaryType).toBe('arraybuffer')
    last().open()
    expect(last().sent[0]).toBe('{"t":"resize","cols":100,"rows":30}')
    c.send('é')
    expect([...(last().sent[1] as Uint8Array)]).toEqual([0xc3, 0xa9])
    last().bytes([27, 91, 72])
    expect(data).toEqual([[27, 91, 72]])
    last().json({ t: 'attached' })
    expect(control).toEqual([{ t: 'attached' }])
    c.close()
  })

  it('sends resize control frames only when the size changes', () => {
    const { c } = client()
    c.connect(80, 24)
    last().open()
    c.resize(80, 24)
    c.resize(120, 40)
    expect(last().sent).toEqual([
      '{"t":"resize","cols":80,"rows":24}',
      '{"t":"resize","cols":120,"rows":40}',
    ])
    c.close()
  })

  it('"replaced" (close 4000) stops reconnecting until Reattach', () => {
    const { c, states } = client()
    c.connect(80, 24)
    last().open()
    last().json({ t: 'replaced' })
    last().drop(4000)
    expect(states.at(-1)).toBe('replaced')
    vi.advanceTimersByTime(60_000)
    expect(FakeWS.all).toHaveLength(1)
    c.reattach()
    expect(FakeWS.all).toHaveLength(2)
    c.close()
  })

  it('Emacs unavailable (close 4001) retries with backoff', () => {
    const { c, states } = client()
    c.connect(80, 24)
    last().open()
    last().drop(4001)
    expect(states.at(-1)).toBe('unavailable')
    vi.advanceTimersByTime(250)
    expect(FakeWS.all).toHaveLength(2)
    c.close()
  })
})
