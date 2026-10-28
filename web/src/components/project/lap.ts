/**
 * The "again, without hints" lap of a project (CURRICULUM.md Part 14,
 * api.md §3.4): `scenario {lap: true}` reloads the project from its first
 * task with the fixtures restored and starts sessiond's timer; until the
 * last task completes, hint, demo and skip answer 409, so the task list
 * hides them. Any other load ends the lap. The store follows the lap from
 * hello and step events (session.ts); finished laps are in progress
 * (`laps`).
 */
import { rpc } from '../../api/session/rpc'
import { useSessionStore } from '../../store/session'
import { useUiStore } from '../../store/ui'
import { cancelDemo } from '../lesson/demoPlayer'
import { focusTerminal } from '../screen/focus'

/** Starts a lap of the project: its first task, fixtures restored, timer running. */
export async function startLap(lessonId: string): Promise<void> {
  cancelDemo()
  useUiStore.getState().clearHintLevels(lessonId)
  try {
    const r = await rpc('scenario', { lessonId, lap: true })
    if (r.lap && r.lap.startedAt > 0) useSessionStore.getState().setLap(r.lap)
  } catch {
    // toasted
  } finally {
    focusTerminal()
  }
}

/**
 * Stops the running lap by loading its current task again without the lap
 * (the task starts over from its checkpoint; hints come back).
 */
export async function stopLap(lessonId: string, stepId: string | undefined): Promise<void> {
  try {
    await rpc('scenario', stepId ? { lessonId, stepId } : { lessonId })
    useSessionStore.getState().setLap(null)
  } catch {
    // toasted
  } finally {
    focusTerminal()
  }
}
