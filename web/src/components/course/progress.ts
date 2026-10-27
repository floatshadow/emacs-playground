/** Course-map progress helpers (frontend.md §10): suggested order, never locks. */
import type { CurriculumIndex, LessonRef, Part, Progress } from '../../protocol.gen'

export type LessonState =
  'done' | 'finished' | 'current' | 'in-progress' | 'not-started' | 'suggested' | 'planned'

export function lessonDone(progress: Progress | undefined, id: string): boolean {
  return progress?.lessons?.[id]?.status === 'done'
}

/**
 * Every step is done, demonstrated or skipped, but not all are done:
 * the row reads "finished · N skipped · M watched".
 */
export function lessonFinished(progress: Progress | undefined, id: string): boolean {
  const lp = progress?.lessons?.[id]
  const c = lp?.counts
  if (!lp || !c || c.total <= 0 || lp.status === 'done') return false
  return c.done + c.demonstrated + c.skipped >= c.total
}

/** Done, or every step done or skipped: Continue moves past it (a watched step is practised). */
function lessonPassed(progress: Progress | undefined, id: string): boolean {
  if (lessonDone(progress, id)) return true
  const c = progress?.lessons?.[id]?.counts
  return !!c && c.total > 0 && c.done + c.skipped >= c.total
}

export function lessonStarted(progress: Progress | undefined, id: string): boolean {
  const s = progress?.lessons?.[id]?.status
  return s !== undefined && s !== 'not-started'
}

/** Ready lessons in course order. */
export function allLessons(index: CurriculumIndex): LessonRef[] {
  return index.parts.flatMap((p) => p.lessons.filter((l) => !l.id.startsWith('projects/')))
}

/**
 * "You are here": the lesson of the active step if any, else the first
 * lesson in progress, else the first ready lesson not done.
 */
export function currentLesson(
  index: CurriculumIndex,
  progress: Progress | undefined,
  activeLessonId: string | null,
): LessonRef | null {
  const ready = allLessons(index).filter((l) => l.status === 'ready')
  if (activeLessonId) {
    const a = ready.find((l) => l.id === activeLessonId)
    if (a && !lessonPassed(progress, a.id)) return a
  }
  return (
    ready.find(
      (l) => progress?.lessons?.[l.id]?.status === 'in-progress' && !lessonPassed(progress, l.id),
    ) ??
    ready.find((l) => !lessonPassed(progress, l.id)) ??
    null
  )
}

/** The ready lesson after id, for "Next:" links. */
export function nextLesson(index: CurriculumIndex | undefined, id: string): LessonRef | null {
  if (!index) return null
  const ready = allLessons(index).filter((l) => l.status === 'ready')
  const i = ready.findIndex((l) => l.id === id)
  return i >= 0 ? (ready[i + 1] ?? null) : null
}

export function findLesson(
  index: CurriculumIndex | undefined,
  id: string,
): { part: Part; lesson: LessonRef } | null {
  if (!index) return null
  for (const part of index.parts) {
    const lesson = part.lessons.find((l) => l.id === id)
    if (lesson) return { part, lesson }
  }
  return null
}

export function partDoneFraction(part: Part, progress: Progress | undefined): number {
  const ready = part.lessons.filter((l) => l.status === 'ready')
  if (ready.length === 0) return 0
  return ready.filter((l) => lessonDone(progress, l.id)).length / ready.length
}

export function lessonState(
  part: Part,
  lesson: LessonRef,
  index: CurriculumIndex,
  progress: Progress | undefined,
  current: LessonRef | null,
): LessonState {
  if (lesson.status === 'planned') return 'planned'
  if (lessonDone(progress, lesson.id)) return 'done'
  if (current?.id === lesson.id) return 'current'
  if (lessonFinished(progress, lesson.id)) return 'finished'
  if (lessonStarted(progress, lesson.id)) return 'in-progress'
  if (part.suggestedAfter !== undefined) {
    const before = index.parts.find((p) => p.n === part.suggestedAfter)
    if (before && partDoneFraction(before, progress) < 1) return 'suggested'
  }
  return 'not-started'
}
