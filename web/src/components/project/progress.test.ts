import { describe, expect, it } from 'vitest'
import type { LessonProgress } from '../../protocol.gen'
import type { StepView } from '../../store/selectors'
import { projectProgress } from './progress'

const view = (id: string, status: StepView['status']) =>
  ({ step: { id }, status }) as unknown as StepView

const saved = (statuses: Record<string, string>): LessonProgress =>
  ({
    status: 'in-progress',
    counts: { done: 0, demonstrated: 0, skipped: 0, total: 3 },
    steps: Object.fromEntries(
      Object.entries(statuses).map(([k, status]) => [k, { status, hintsUsed: 0, resets: 0 }]),
    ),
  }) as LessonProgress

describe('project progress', () => {
  it('counts done, skipped and watched tasks', () => {
    const views = [view('a', 'done'), view('b', 'skipped'), view('c', 'active')]
    expect(projectProgress(views, 2, false, saved({ a: 'done', b: 'skipped' }), false)).toEqual({
      total: 3,
      done: 1,
      skipped: 1,
      watched: 0,
      finished: false,
    })
  })

  it('a finished project whose last task is still active (after a reload) is finished', () => {
    const views = [view('a', 'done'), view('b', 'done'), view('c', 'active')]
    const p = projectProgress(views, 2, false, saved({ a: 'done', b: 'done', c: 'done' }), false)
    expect(p).toMatchObject({ done: 3, finished: true })
    // Going back to an earlier task is not finished.
    const back = [view('a', 'active'), view('b', 'todo'), view('c', 'todo')]
    expect(
      projectProgress(back, 0, false, saved({ a: 'done', b: 'done', c: 'done' }), false).finished,
    ).toBe(false)
  })

  it('during the lap, the tasks before the active one are done', () => {
    const views = [view('a', 'done'), view('b', 'active'), view('c', 'todo')]
    const all = saved({ a: 'done', b: 'done', c: 'done' })
    expect(projectProgress(views, 1, false, all, true)).toMatchObject({ done: 1, finished: false })
    expect(projectProgress(views, -1, true, all, true)).toMatchObject({ done: 3 })
  })
})
