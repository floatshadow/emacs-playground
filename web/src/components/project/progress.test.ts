import { describe, expect, it } from 'vitest'
import type { StepView } from '../../store/selectors'
import { projectProgress } from './progress'

const view = (id: string, status: StepView['status']) =>
  ({ step: { id }, status }) as unknown as StepView

describe('project progress', () => {
  it('counts done, skipped and watched tasks', () => {
    const views = [view('a', 'done'), view('b', 'skipped'), view('c', 'demonstrated')]
    expect(projectProgress(views, -1, false, false)).toEqual({
      total: 3,
      done: 1,
      skipped: 1,
      watched: 1,
      finished: false,
    })
  })

  it('is finished exactly when the lesson is complete', () => {
    const views = [view('a', 'done'), view('b', 'skipped'), view('c', 'done')]
    expect(projectProgress(views, -1, true, false)).toMatchObject({ done: 2, finished: true })
    const back = [view('a', 'active'), view('b', 'todo'), view('c', 'todo')]
    expect(projectProgress(back, 0, false, false).finished).toBe(false)
  })

  it('during the lap, the tasks before the active one are done', () => {
    const views = [view('a', 'done'), view('b', 'active'), view('c', 'todo')]
    expect(projectProgress(views, 1, false, true)).toMatchObject({ done: 1, finished: false })
    expect(projectProgress(views, -1, true, true)).toMatchObject({ done: 3 })
  })
})
