import { describe, expect, it } from 'vitest'
import type { CurriculumIndex, LessonProgress, Progress } from '../../protocol.gen'
import { currentLesson, lessonFinished, lessonState } from './progress'

const lesson = (id: string, n: string) => ({
  id,
  n,
  title: id,
  minutes: 5,
  status: 'ready' as const,
  tags: [],
  vscode: '',
})
const INDEX = {
  parts: [
    {
      id: 'p0',
      n: 0,
      title: 'P0',
      minutes: 10,
      status: 'ready',
      outcome: '',
      modules: [],
      lessons: [lesson('a', '0.1'), lesson('b', '0.2'), lesson('c', '0.3')],
    },
  ],
  projects: [],
} as unknown as CurriculumIndex

const lp = (
  status: LessonProgress['status'],
  counts: Partial<LessonProgress['counts']>,
): LessonProgress => ({
  status,
  counts: { done: 0, demonstrated: 0, skipped: 0, total: 3, ...counts },
  steps: {},
})
const progress = (lessons: Record<string, LessonProgress>): Progress => ({ lessons, updatedAt: 1 })

describe('course map progress (contract c)', () => {
  it('a lesson whose steps are all done, watched or skipped is finished, not done', () => {
    const p = progress({
      a: lp('done', { done: 3 }),
      b: lp('in-progress', { done: 1, skipped: 1, demonstrated: 1 }),
      c: lp('in-progress', { skipped: 1 }),
    })
    expect(lessonFinished(p, 'a')).toBe(false)
    expect(lessonFinished(p, 'b')).toBe(true)
    expect(lessonFinished(p, 'c')).toBe(false)
    const part = INDEX.parts[0]!
    const states = part.lessons.map((l) => lessonState(part, l, INDEX, p, null))
    expect(states).toEqual(['done', 'finished', 'in-progress'])
  })

  it('Continue passes lessons whose steps are all done or skipped, not watched ones', () => {
    const allSkipped = progress({ a: lp('in-progress', { done: 2, skipped: 1 }) })
    expect(currentLesson(INDEX, allSkipped, null)?.id).toBe('b')
    const watched = progress({ a: lp('in-progress', { done: 2, demonstrated: 1 }) })
    expect(currentLesson(INDEX, watched, null)?.id).toBe('a')
    expect(currentLesson(INDEX, progress({}), 'c')?.id).toBe('c')
  })
})
