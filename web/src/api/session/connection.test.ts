import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { Event } from '../../protocol.gen'
import { useSessionStore } from '../../store/session'
import { disconnectSession, startEvents } from './connection'
import { rpc } from './rpc'

vi.mock('./rpc', async (original) => ({
  ...(await original<typeof import('./rpc')>()),
  rpc: vi.fn(() => Promise.resolve({})),
  wsUrl: (path: string) => `ws://localhost${path}`,
}))

class FakeWS {
  static all: FakeWS[] = []
  readyState = 1
  onopen: (() => void) | null = null
  onmessage: ((m: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  url: string
  constructor(url: string) {
    this.url = url
    FakeWS.all.push(this)
  }
  close() {
    this.readyState = 3
    this.onclose?.()
  }
  send() {}
  event(ev: Partial<Event>) {
    this.onmessage?.({ data: JSON.stringify(ev) })
  }
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('WebSocket', FakeWS)
  vi.mocked(rpc).mockClear()
  useSessionStore.getState().reset()
  FakeWS.all = []
})
afterEach(() => {
  disconnectSession()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

it('reconnects after a gap from the last received sequence', () => {
  startEvents('local')
  const first = FakeWS.all[0]!
  first.event({ t: 'key', seq: 7, keys: 'C-f', cmd: 'forward-char' })
  first.event({ t: 'gap', seq: 0, from: 8, to: 264 })
  first.event({ t: 'key', seq: 265, keys: 'C-f', cmd: 'forward-char' })
  vi.advanceTimersByTime(1000)
  expect(FakeWS.all).toHaveLength(2)
  expect(FakeWS.all[1]!.url).toContain('/events?after=7')
})

it.each([
  { replayed: false, step: { lessonId: 'a/b', stepId: 'one' }, checks: 1 },
  { replayed: true, step: { lessonId: 'a/b', stepId: 'one' }, checks: 0 },
  { replayed: false, step: null, checks: 0 },
])('refreshes checks on fresh hello: %j', ({ replayed, step, checks }) => {
  startEvents('local')
  FakeWS.all[0]!.event({ t: 'hello', seq: 0, replayed, step })
  expect(rpc).toHaveBeenCalledTimes(checks)
  if (checks) expect(rpc).toHaveBeenCalledWith('check', {}, { silent: true })
})
