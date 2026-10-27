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
 * "Skip this step" (S2, contract c): sessiond plays the step's demo itself
 * and records the step skipped, then loads the next one. The screen is
 * covered while it runs (up to a minute) so the learner's keys cannot mix
 * with the demo's.
 */
export async function doSkipStep(stepId: string, goal: string): Promise<void> {
  cancelDemo()
  useUiStore.getState().setSkipping({ stepId, goal })
  try {
    await rpc('skip', { stepId }, { silent: true })
    toast(i18n.t('stuck.skipped', { goal }))
  } catch (e) {
    toast(
      i18n.t('stuck.skipFailed', { error: e instanceof Error ? e.message : String(e) }),
      'error',
    )
  } finally {
    useUiStore.getState().setSkipping(null)
    focusTerminal()
  }
}
