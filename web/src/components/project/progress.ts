/**
 * A project's progress for the task panel (frontend.md §10). The engine's
 * last task stays active after the project completes, so after a reload a
 * finished project still has an active task; saved progress tells it is
 * finished. During the lap, tasks before the active one are this lap's.
 */
import type { LessonProgress } from '../../protocol.gen'
import type { StepView } from '../../store/selectors'

export interface ProjectProgress {
  total: number
  done: number
  skipped: number
  watched: number
  /** Every task is done or skipped: the lap is offered. */
  finished: boolean
}

export function projectProgress(
  views: readonly StepView[],
  activeIndex: number,
  complete: boolean,
  saved: LessonProgress | undefined,
  lap: boolean,
): ProjectProgress {
  const total = views.length
  if (lap) {
    const done = complete ? total : Math.max(0, activeIndex)
    return { total, done, skipped: 0, watched: 0, finished: false }
  }
  const savedStatus = (v: StepView) => saved?.steps?.[v.step.id]?.status
  const savedFinished =
    total > 0 && views.every((v) => savedStatus(v) === 'done' || savedStatus(v) === 'skipped')
  const finished = complete || (savedFinished && (activeIndex < 0 || activeIndex === total - 1))
  const status = (v: StepView) => (v.status === 'active' && finished ? savedStatus(v) : v.status)
  const count = (s: string) => views.filter((v) => status(v) === s).length
  return {
    total,
    done: count('done'),
    skipped: count('skipped'),
    watched: count('demonstrated'),
    finished,
  }
}
