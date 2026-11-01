/**
 * useScenarioLoad mounted over the session store with a mocked rpc: the
 * load is a one-shot navigation intent decided from the lesson snapshot.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import type { HelloEvent, LessonProgress, LessonSnapshot, Scenario } from '../../protocol.gen'
import { useSessionStore } from '../../store/session'
import { rpc } from '../../api/session/rpc'
import { loadAtStep } from '../live/actions'
import { useScenarioLoad } from './useLesson'

let progress: Record<string, LessonProgress> = {}
vi.mock('../../api/queries', () => ({
  useProgress: () => ({ isLoading: false, data: { lessons: progress, updatedAt: 1 } }),
  useMe: () => ({ data: undefined }),
}))
vi.mock('../../api/session/rpc', () => ({ rpc: vi.fn(() => Promise.resolve({})) }))
vi.mock('../live/actions', () => ({ loadAtStep: vi.fn(() => Promise.resolve()) }))

const A = 'p/a'
const B = 'p/b'
const scenario = (id: string): Scenario => ({
  id,
  title: id,
  minutes: 1,
  modules: [],
  setup: {},
  steps: [
    { id: 'one', goal: 'One', conditions: [] },
    { id: 'two', goal: 'Two', conditions: [] },
  ],
})

let rev = 0
const snapshot = (lessonId: string, step: string | null, over: Partial<LessonSnapshot> = {}) => ({
  rev: ++rev,
  lessonId,
  step: step ? { id: step, index: step === 'one' ? 0 : 1, count: 2 } : null,
  phase: step ? ('active' as const) : ('idle' as const),
  attempt: rev,
  outcomes: {},
  ...over,
})

/** The store as after a hello with this snapshot. */
function hello(lesson: LessonSnapshot, protocol = 2) {
  act(() =>
    useSessionStore.getState().applyEvent({
      t: 'hello',
      protocol,
      sessionId: 'local',
      mode: 'terminal',
      emacs: { version: '31.1', uptimeMs: 1 },
      frames: { terminal: true, gui: false },
      modules: [],
      replayed: false,
      lesson,
    } satisfies HelloEvent),
  )
}

function publish(lesson: LessonSnapshot) {
  act(() => useSessionStore.getState().applyEvent({ t: 'lesson', change: 'loaded', lesson }))
}

const flush = () => act(() => new Promise((r) => setTimeout(r, 0)))

beforeEach(() => {
  progress = {}
  rev = 0
  useSessionStore.getState().reset()
  vi.mocked(rpc).mockReset().mockResolvedValue({})
  vi.mocked(loadAtStep).mockReset().mockResolvedValue(undefined)
})
afterEach(() => useSessionStore.getState().reset())

describe('useScenarioLoad', () => {
  it('two tabs: the second load displaces the first, which does not take Emacs back', async () => {
    hello(snapshot('', null))
    const tabA = renderHook(() => useScenarioLoad(A, scenario(A)))
    const tabB = renderHook(() => useScenarioLoad(B, scenario(B)))
    await flush()
    expect(vi.mocked(rpc).mock.calls.map((c) => c[1])).toEqual([{ lessonId: A }, { lessonId: B }])
    publish(snapshot(A, 'one'))
    publish(snapshot(B, 'one'))
    await flush()
    expect(tabA.result.current.displaced).toBe(true)
    expect(tabB.result.current.displaced).toBe(false)
    expect(rpc).toHaveBeenCalledTimes(2)
  })

  it('a project page on its last task is not reloaded when the project completes', async () => {
    hello(snapshot('', null))
    const h = renderHook(({ step }) => useScenarioLoad(A, scenario(A), step), {
      initialProps: { step: 'one' },
    })
    await flush()
    expect(loadAtStep).toHaveBeenCalledTimes(1)
    publish(snapshot(A, 'one'))
    // The URL follows the active step (projects).
    publish(snapshot(A, 'two'))
    h.rerender({ step: 'two' })
    publish(snapshot(A, null, { phase: 'complete', outcomes: { one: 'done', two: 'done' } }))
    await flush()
    expect(loadAtStep).toHaveBeenCalledTimes(1)
  })

  it('a task URL in a complete project loads that task', async () => {
    hello(snapshot(A, null, { phase: 'complete', outcomes: { one: 'done', two: 'done' } }))
    const h = renderHook(({ step }) => useScenarioLoad(A, scenario(A), step), {
      initialProps: { step: 'one' },
    })
    await flush()
    expect(vi.mocked(loadAtStep).mock.calls.map((c) => c[1].id)).toEqual(['one'])
    publish(snapshot(A, 'one'))
    publish(snapshot(A, null, { phase: 'complete', outcomes: { one: 'done', two: 'done' } }))
    // After the completion, another task's URL loads that task.
    h.rerender({ step: 'two' })
    await flush()
    expect(vi.mocked(loadAtStep).mock.calls.map((c) => c[1].id)).toEqual(['one', 'two'])
  })

  it('a lesson shown when the page opened is not reloaded after a drop elsewhere', async () => {
    hello(snapshot(A, 'two'))
    const h = renderHook(() => useScenarioLoad(A, scenario(A)))
    publish(snapshot('', null))
    await flush()
    expect(rpc).not.toHaveBeenCalled()
    expect(h.result.current.displaced).toBe(true)
    act(() => h.result.current.loadHere())
    await flush()
    expect(vi.mocked(rpc).mock.calls.map((c) => c[1])).toEqual([{ lessonId: A }])
  })

  it('a failed load waits for Retry', async () => {
    vi.mocked(rpc).mockRejectedValueOnce(new Error('boom'))
    hello(snapshot('', null))
    const h = renderHook(() => useScenarioLoad(A, scenario(A)))
    await flush()
    expect(h.result.current.error).toBe('boom')
    publish(snapshot('', null, { busy: undefined }))
    await flush()
    expect(rpc).toHaveBeenCalledTimes(1)
    act(() => h.result.current.retry())
    await flush()
    expect(rpc).toHaveBeenCalledTimes(2)
  })

  it('a lesson finished in an earlier visit shows complete and is not loaded', async () => {
    const done = { status: 'done' as const, hintsUsed: 0, resets: 0 }
    progress = {
      [A]: {
        status: 'done',
        counts: { done: 2, demonstrated: 0, skipped: 0, total: 2 },
        steps: { one: done, two: done },
      },
    }
    hello(snapshot(B, 'one'))
    renderHook(() => useScenarioLoad(A, scenario(A)))
    await flush()
    expect(rpc).not.toHaveBeenCalled()
  })

  it('waits while a lesson op runs and for a compatible session', async () => {
    const busy = { op: 'load', lessonId: A, stepId: 'two', startedAt: 1 }
    hello(snapshot(B, 'one', { busy }))
    renderHook(() => useScenarioLoad(A, scenario(A)))
    await flush()
    expect(rpc).not.toHaveBeenCalled()
    // The op ends with the lesson shown: nothing to load.
    publish(snapshot(A, 'two'))
    await flush()
    expect(rpc).not.toHaveBeenCalled()

    useSessionStore.getState().reset()
    hello(snapshot('', null), 1)
    renderHook(() => useScenarioLoad(B, scenario(B)))
    await flush()
    expect(rpc).not.toHaveBeenCalled()
  })
})
