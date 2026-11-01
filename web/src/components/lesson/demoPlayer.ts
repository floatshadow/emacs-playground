/**
 * The demo player engine (frontend.md §7). "Watch it" is a demonstration,
 * not a completion: the player calls rpc('demo', start) first, so sessiond
 * restores the step's checkpoint when the learner already ran commands in
 * it and latches the step (a completion while the latch is set is recorded
 * "demonstrated", not done). Then it sends the demo through rpc('keys') one
 * chord or one typed character at a time, at human pace (600 ms per chord,
 * 120 ms per character), while the caption strip under the screen names
 * every key; rpc('demo', end) follows in any case. Any learner keypress in
 * the terminal stops it, and so does a snapshot of another attempt (Go to
 * this step, another lesson, a reset, a drop) or one whose phase left
 * "watching": the demo's keys belong to the attempt demo start returned and
 * are never sent into another.
 */
import { parseDemo, planDemo, WATCH_PACING, type DemoAction } from '../../lib/demo'
import { sleep } from '../../lib/time'
import { ApiError } from '../../api/http'
import { rpc } from '../../api/session/rpc'
import { useSessionStore } from '../../store/session'
import { toast, useUiStore } from '../../store/ui'
import i18n from '../../i18n'
import { focusTerminal, onTerminalInput } from '../screen/focus'
import { errorMessage } from '../../lib/errors'

let current: AbortController | null = null

export type SendFn = (a: DemoAction, signal: AbortSignal) => Promise<void>

const sendRpc: SendFn = async (a, signal) => {
  if (a.kind === 'keys') await rpc('keys', { keys: a.keys }, { signal, silent: true })
  else if (a.kind === 'text') await rpc('keys', { text: a.text }, { signal, silent: true })
}

/** Pause after "Starting from the beginning of this step" before the first key. */
export const RESTORED_PAUSE_MS = 900

/**
 * Stops the demo. byLearnerInput: the learner typed in the terminal, so the
 * key events that follow are theirs and may raise habit cards at once.
 */
export function cancelDemo(byLearnerInput = false): void {
  if (current) useSessionStore.getState().setDemoPlaying(false, byLearnerInput ? 0 : undefined)
  current?.abort()
  current = null
  useUiStore.getState().setDemo(null)
}

/**
 * Runs actions with pacing; resolves true when finished, false when
 * cancelled. onSent(n) is called after the n-th action was sent. Nothing is
 * sent once stillOn() returns false (the step the actions belong to is no
 * longer the active one).
 */
export async function runActions(
  actions: DemoAction[],
  signal: AbortSignal,
  send: SendFn = sendRpc,
  onSent?: (n: number) => void,
  stillOn: () => boolean = () => true,
): Promise<boolean> {
  try {
    for (const [i, a] of actions.entries()) {
      if (signal.aborted || !stillOn()) return false
      await send(a, signal)
      onSent?.(i + 1)
      await sleep(a.delayMs, signal)
    }
    return true
  } catch (e) {
    if (signal.aborted) return false
    throw e
  }
}

export async function playDemo(stepId: string, goal: string, demo: string): Promise<void> {
  cancelDemo()
  if (useSessionStore.getState().incompatible) return
  let actions: DemoAction[]
  try {
    actions = planDemo(parseDemo(demo), WATCH_PACING)
  } catch (e) {
    toast(i18n.t('demo.bad', { error: errorMessage(e) }), 'error')
    return
  }
  const ctl = new AbortController()
  current = ctl
  const ui = useUiStore.getState()
  const session = useSessionStore.getState()
  ui.setDemo({ stepId, goal, actions, sent: 0, restored: false })
  session.setDemoPlaying(true)
  focusTerminal()
  const offInput = onTerminalInput(() => {
    if (current === ctl) {
      cancelDemo(true)
      toast(i18n.t('demo.cancelled'))
    }
  })
  // The attempt demo start returned. The keys wait for a snapshot that shows
  // it watching (older attempts' snapshots are ignored) and stop once a later
  // snapshot leaves it: they are never sent into another attempt.
  let attempt = -1
  let watched = false
  let release = () => {}
  const onAttempt = () => {
    const l = useSessionStore.getState().lesson
    return l?.attempt === attempt && l.phase === 'watching'
  }
  const offStep = useSessionStore.subscribe((s, prev) => {
    if (s.lesson === prev.lesson || attempt < 0 || current !== ctl) return
    if (onAttempt()) release()
    else if (watched || !s.lesson || s.lesson.attempt > attempt) cancelDemo()
  })
  const off = () => {
    offInput()
    offStep()
  }
  let latched = false
  try {
    try {
      const r = await rpc('demo', { stepId, phase: 'start' }, { signal: ctl.signal, silent: true })
      latched = true
      attempt = r.attempt
      await new Promise<void>((resolve, reject) => {
        release = () => {
          watched = true
          resolve()
        }
        ctl.signal.addEventListener('abort', () => reject(ctl.signal.reason), { once: true })
        if (ctl.signal.aborted) reject(ctl.signal.reason)
        else if (onAttempt()) release()
      })
      if (r.restored) {
        useUiStore.getState().setDemoRestored(stepId)
        await sleep(RESTORED_PAUSE_MS, ctl.signal)
      }
    } catch (e) {
      if (ctl.signal.aborted) return
      if (e instanceof ApiError && e.status === 409) {
        toast(i18n.t('demo.notActive'), 'error')
        return
      }
      throw e
    }
    await runActions(
      actions,
      ctl.signal,
      sendRpc,
      (n) => useUiStore.getState().setDemoSent(stepId, n),
      onAttempt,
    )
  } catch (e) {
    if (!ctl.signal.aborted) toast(i18n.t('demo.failed', { error: errorMessage(e) }), 'error')
  } finally {
    off()
    if (latched) void rpc('demo', { stepId, phase: 'end' }, { silent: true }).catch(() => undefined)
    if (current === ctl) {
      current = null
      useUiStore.getState().setDemo(null)
      useSessionStore.getState().setDemoPlaying(false)
    }
  }
}

/**
 * "Do it yourself" after a demonstration: restore {} puts the step back to
 * its checkpoint and clears the demo latch, so the learner's own completion
 * counts as done.
 */
export async function doItYourself(): Promise<void> {
  cancelDemo()
  try {
    await rpc('restore', {})
  } catch {
    // rpc toasted
  } finally {
    focusTerminal()
  }
}
