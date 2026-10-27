/**
 * Rescue and reset (frontend.md §6, architecture.md §3.4), shared by the
 * top bar, the rescue card and the overlays. Toasts name what was done.
 */
import { rpc } from '../../api/session/rpc'
import { useSessionStore } from '../../store/session'
import { toast, useUiStore } from '../../store/ui'
import i18n from '../../i18n'
import { cancelDemo } from '../lesson/demoPlayer'
import { focusTerminal } from '../screen/focus'
import { errorMessage } from '../../lib/errors'

export async function doRescue(): Promise<void> {
  try {
    const r = await rpc('rescue', {})
    const did = r.did ?? []
    const text = did.length
      ? i18n.t('rescue.did', { what: did.join(', ') })
      : i18n.t('rescue.nothing')
    toast(text)
    useSessionStore.getState().note({
      keys: ['Rescue'],
      cmd: 'playground-rescue',
      gloss: did.length ? did.join(', ') : i18n.t('rescue.nothingShort'),
      kind: 'quit',
    })
  } catch {
    // rpc already toasted
  } finally {
    focusTerminal()
  }
}

/**
 * Restores the active step's checkpoint (files and layout). restore {} is
 * "Reset this step" in api.md §3.4: it also clears a demo latch.
 */
export async function doResetStep(): Promise<void> {
  const step = useSessionStore.getState().activeStep
  if (!step) {
    toast(i18n.t('rescue.noStep'))
    return
  }
  cancelDemo()
  try {
    const r = await rpc('restore', {})
    toast(i18n.t('rescue.restored', { count: r.restoredFiles ?? 0 }))
    useSessionStore.getState().note({
      keys: ['Reset'],
      cmd: 'playground-reset-step',
      gloss: i18n.t('rescue.restoredShort'),
      kind: 'quit',
    })
  } catch {
    // toasted
  } finally {
    focusTerminal()
  }
}

/**
 * "Skip this step" (api.md §3.4): sessiond plays the step's demo itself
 * and records the step skipped, then loads the next one. The screen is
 * covered while it runs (up to a minute) so the learner's keys cannot mix
 * with the demo's.
 */
export async function doSkipStep(stepId: string, goal: string): Promise<void> {
  cancelDemo()
  useUiStore.getState().setSkipping({ stepId, goal })
  // The keys Emacs plays are not the learner's: no habit cards for them.
  useSessionStore.getState().setDemoPlaying(true)
  try {
    await rpc('skip', { stepId }, { silent: true })
    toast(i18n.t('stuck.skipped', { goal }))
  } catch (e) {
    toast(i18n.t('stuck.skipFailed', { error: errorMessage(e) }), 'error')
  } finally {
    useSessionStore.getState().setDemoPlaying(false)
    useUiStore.getState().setSkipping(null)
    focusTerminal()
  }
}

/**
 * Loads a lesson at a step, as "Go to this step" and the resume target do:
 * for a step after the first, sessiond restores its checkpoint or replays
 * the earlier steps' demos, which takes up to a few minutes. Any demo stops
 * first; the screen is covered and the learner's keys wait while it runs,
 * and the replayed keys raise no habit cards. Keyboard focus that was lost
 * meanwhile (the clicked button went away) returns to the terminal.
 * Resolves when the step is active; rejects with the rpc error, which is
 * toasted unless silent.
 */
export async function loadAtStep(
  lessonId: string,
  step: { id: string; n: number; goal: string },
  silent = false,
): Promise<void> {
  cancelDemo()
  const veiled = step.n > 1
  if (veiled) {
    useUiStore.getState().setStepJump({ n: step.n, goal: step.goal })
    useSessionStore.getState().setDemoPlaying(true)
  }
  try {
    await rpc('scenario', { lessonId, stepId: step.id }, { silent })
  } finally {
    if (veiled) {
      useSessionStore.getState().setDemoPlaying(false)
      useUiStore.getState().setStepJump(null)
    }
    const el = document.activeElement
    if (!el || el === document.body || el === document.documentElement) focusTerminal()
  }
}
