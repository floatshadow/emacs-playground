import { afterEach, describe, expect, it, vi } from 'vitest'
import type { DemoAction } from '../../lib/demo'
import type { LessonSnapshot } from '../../protocol.gen'
import { rpc } from '../../api/session/rpc'
import { useSessionStore } from '../../store/session'
import { useUiStore } from '../../store/ui'
import { cancelDemo, playDemo, runActions } from './demoPlayer'

vi.mock('../../api/session/rpc', () => ({ rpc: vi.fn() }))

const keys = (k: string): DemoAction => ({ kind: 'keys', keys: k, delayMs: 0 })

describe('runActions', () => {
  it('sends nothing more once its step is no longer the active one', async () => {
    const sent: string[] = []
    let onStep = true
    const done = await runActions(
      [keys('C-x'), keys('o'), keys('RET')],
      new AbortController().signal,
      (a) => {
        if (a.kind === 'keys') sent.push(a.keys)
        // The second key completes the step and the engine moves on.
        if (sent.length === 2) onStep = false
        return Promise.resolve()
      },
      undefined,
      () => onStep,
    )
    expect(done).toBe(false)
    expect(sent).toEqual(['C-x', 'o'])
  })
})

let rev = 0
const lesson = (attempt: number, phase: LessonSnapshot['phase']): LessonSnapshot => ({
  rev: ++rev,
  lessonId: 'p/a',
  step: { id: 'one', index: 0, count: 1 },
  phase,
  attempt,
  outcomes: {},
})
const sentKeys = () =>
  vi.mocked(rpc).mock.calls.flatMap(([op, req]) => (op === 'keys' ? [req] : []))
const flush = () => new Promise((r) => setTimeout(r, 0))

describe('playDemo', () => {
  afterEach(async () => {
    cancelDemo()
    await flush()
    useSessionStore.getState().reset()
    vi.mocked(rpc).mockReset()
  })

  it('sends keys only once the attempt demo start returned is watching', async () => {
    useSessionStore.setState({ lesson: lesson(1, 'active') })
    vi.mocked(rpc).mockImplementation((op) =>
      op === 'demo' ? Promise.resolve({ attempt: 2, restored: false }) : Promise.resolve({}),
    )
    void playDemo('one', 'One', 'C-f')
    await flush()
    // A queued snapshot of the older attempt neither cancels nor starts it.
    useSessionStore.setState({ lesson: lesson(1, 'active') })
    await flush()
    expect(sentKeys()).toEqual([])
    expect(useUiStore.getState().demo).not.toBeNull()
    useSessionStore.setState({ lesson: lesson(2, 'watching') })
    await vi.waitFor(() => expect(sentKeys()).toEqual([{ keys: 'C-f' }]))
  })

  it('does nothing in an incompatible session', async () => {
    useSessionStore.setState({ lesson: lesson(1, 'active'), incompatible: true })
    await playDemo('one', 'One', 'C-f')
    expect(rpc).not.toHaveBeenCalled()
  })
})
