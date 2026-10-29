import type { FrameKind } from '../../protocol.gen'
import { rpc } from '../../api/session/rpc'
import { useSessionStore } from '../../store/session'
import { useUiStore } from '../../store/ui'

/** Whether the learner picked the GUI frame themselves (this page load). */
let learnerChoseGui = false

/** True while the GUI frame is up because the learner asked for it. */
export function guiChosenByLearner(): boolean {
  return learnerChoseGui
}

/**
 * Switches the session between the terminal and GUI frames (frontend.md §8,
 * api.md §3.4 mode). While the GUI frame starts, the terminal stays visible
 * with a "starting GUI frame" chip. `by` says who asked: a lesson switches
 * back to the terminal only from a GUI frame that a lesson opened.
 */
export async function switchFrame(
  mode: FrameKind,
  by: 'learner' | 'lesson' = 'lesson',
): Promise<void> {
  const ui = useUiStore.getState()
  if (by === 'learner') learnerChoseGui = mode === 'gui'
  if (useSessionStore.getState().mode === mode) return
  if (mode === 'gui') ui.setGuiStarting(true)
  try {
    const r = await rpc('mode', { mode })
    // The mode event normally arrives first; the result is authoritative too.
    if (r?.mode) useSessionStore.getState().applyEvent({ t: 'mode', mode: r.mode })
  } catch {
    ui.setGuiStarting(false)
  }
}
