/**
 * Focus mode (frontend.md §5, B14). It is an in-page state first: the
 * stage takes the whole window (the lesson pane and the rail step aside)
 * and a one-line banner says what the mode does and how to leave it (the
 * button, or Shift+Esc). Then, where the browser allows, the page goes
 * fullscreen and the keyboard is locked (Keyboard Lock API, Chromium) so
 * Ctrl+W and friends reach Emacs; each request gives up after 1.5 s (a
 * headless or embedded browser may never answer) and the banner says
 * whether the lock is on. Leaving fullscreen ends focus mode.
 */
import { useUiStore } from '../../store/ui'
import { focusTerminal } from './focus'

export const SCREEN_WRAP_ID = 'screen-wrap'

/** How long to wait for fullscreen and the keyboard lock before carrying on without them. */
export const FOCUS_REQUEST_TIMEOUT_MS = 1500

interface KeyboardLock {
  lock?(keys?: string[]): Promise<void>
  unlock?(): void
}

const keyboard = (): KeyboardLock | undefined =>
  (navigator as Navigator & { keyboard?: KeyboardLock }).keyboard

let listening = false
/** Fullscreen was entered by focus mode (so leaving it ends focus mode). */
let ownFullscreen = false
/** Bumped on every enter and exit: late answers to an older request are ignored. */
let generation = 0

function onFullscreenChange() {
  if (document.fullscreenElement) return
  keyboard()?.unlock?.()
  if (ownFullscreen) {
    ownFullscreen = false
    generation++
    useUiStore.getState().setFocusMode(false)
    useUiStore.getState().setFocusLock('off')
  }
}

/** Resolves true when p settles successfully within ms, false when it fails or times out. */
function within(p: Promise<unknown>, ms: number): Promise<boolean> {
  return new Promise((resolve) => {
    const t = setTimeout(() => resolve(false), ms)
    p.then(
      () => {
        clearTimeout(t)
        resolve(true)
      },
      () => {
        clearTimeout(t)
        resolve(false)
      },
    )
  })
}

export async function enterFocusMode(): Promise<void> {
  const ui = useUiStore.getState()
  const gen = ++generation
  ui.setFocusMode(true)
  ui.setFocusLock('trying')
  focusTerminal()
  const el = document.documentElement
  if (!listening) {
    document.addEventListener('fullscreenchange', onFullscreenChange)
    listening = true
  }
  let full = !!document.fullscreenElement
  if (!full && typeof el.requestFullscreen === 'function') {
    let request: Promise<void>
    try {
      request = el.requestFullscreen()
    } catch (e) {
      request = Promise.reject(e instanceof Error ? e : new Error(String(e)))
    }
    // A request that settles after the timeout still counts, if focus mode is still on.
    request.then(
      () => {
        if (gen !== generation) {
          if (document.fullscreenElement) void document.exitFullscreen().catch(() => undefined)
          return
        }
        ownFullscreen = true
      },
      () => undefined,
    )
    full = await within(request, FOCUS_REQUEST_TIMEOUT_MS)
  }
  if (gen !== generation) return
  let locked = false
  const kb = keyboard()
  if (full && kb?.lock) locked = await within(kb.lock(), FOCUS_REQUEST_TIMEOUT_MS)
  if (gen !== generation) return
  useUiStore.getState().setFocusLock(locked ? 'on' : 'off')
  focusTerminal()
}

export async function exitFocusMode(): Promise<void> {
  generation++
  keyboard()?.unlock?.()
  const wasOwn = ownFullscreen
  ownFullscreen = false
  useUiStore.getState().setFocusMode(false)
  useUiStore.getState().setFocusLock('off')
  if (wasOwn && document.fullscreenElement) await document.exitFullscreen().catch(() => undefined)
}

export function toggleFocusMode(): Promise<void> {
  return useUiStore.getState().focusMode ? exitFocusMode() : enterFocusMode()
}
