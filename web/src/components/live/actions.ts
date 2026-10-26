/**
 * Rescue and reset (frontend.md §6, architecture.md §3.4), shared by the
 * top bar, the rescue card and the overlays. Toasts name what was done.
 */
import { rpc } from '../../api/session/rpc'
import { useSessionStore } from '../../store/session'
import { toast } from '../../store/ui'
import i18n from '../../i18n'
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

/** Restores the active step's checkpoint (files and layout). */
export async function doResetStep(): Promise<void> {
  const step = useSessionStore.getState().activeStep
  if (!step) {
    toast(i18n.t('rescue.noStep'))
    return
  }
  try {
    const r = await rpc('restore', { ref: `refs/playground/${step.lessonId}/${step.stepId}` })
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
