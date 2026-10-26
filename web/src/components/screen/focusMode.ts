/**
 * Focus mode (frontend.md §5): the screen goes fullscreen and, where the
 * Keyboard Lock API exists (Chromium), the keyboard is locked so Ctrl+W and
 * friends reach Emacs. Leaving fullscreen ends focus mode. Firefox gets
 * fullscreen without the lock and a note about the keys it keeps.
 */
import { toast, useUiStore } from '../../store/ui'
import i18n from '../../i18n'
import { focusTerminal } from './focus'

export const SCREEN_WRAP_ID = 'screen-wrap'

interface KeyboardLock {
  lock?(keys?: string[]): Promise<void>
  unlock?(): void
}

const keyboard = (): KeyboardLock | undefined =>
  (navigator as Navigator & { keyboard?: KeyboardLock }).keyboard

let listening = false

function onFullscreenChange() {
  if (!document.fullscreenElement) {
    keyboard()?.unlock?.()
    useUiStore.getState().setFocusMode(false)
  }
}

export async function enterFocusMode(): Promise<void> {
  const el = document.getElementById(SCREEN_WRAP_ID)
  if (!el || !el.requestFullscreen) {
    toast(i18n.t('focus.unsupported'), 'error')
    return
  }
  if (!listening) {
    document.addEventListener('fullscreenchange', onFullscreenChange)
    listening = true
  }
  try {
    await el.requestFullscreen()
  } catch (e) {
    toast(i18n.t('focus.refused', { error: e instanceof Error ? e.message : String(e) }), 'error')
    return
  }
  const kb = keyboard()
  if (kb?.lock) {
    try {
      await kb.lock()
      toast(i18n.t('focus.locked'))
    } catch {
      toast(i18n.t('focus.noLock'))
    }
  } else {
    toast(i18n.t('focus.noLock'))
  }
  useUiStore.getState().setFocusMode(true)
  focusTerminal()
}

export async function exitFocusMode(): Promise<void> {
  keyboard()?.unlock?.()
  if (document.fullscreenElement) await document.exitFullscreen().catch(() => undefined)
  useUiStore.getState().setFocusMode(false)
}
