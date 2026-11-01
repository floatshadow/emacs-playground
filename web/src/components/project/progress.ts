/**
 * A project's progress for the task panel (frontend.md §10), counted from
 * the step views; complete is the lesson snapshot's (or saved progress's)
 * verdict. During the lap, tasks before the active one are this lap's.
 */
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
  lap: boolean,
): ProjectProgress {
  const total = views.length
  if (lap) {
    const done = complete ? total : Math.max(0, activeIndex)
    return { total, done, skipped: 0, watched: 0, finished: false }
  }
  const count = (s: string) => views.filter((v) => v.status === s).length
  return {
    total,
    done: count('done'),
    skipped: count('skipped'),
    watched: count('demonstrated'),
    finished: complete,
  }
}
