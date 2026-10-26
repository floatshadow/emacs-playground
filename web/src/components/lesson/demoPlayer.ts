/**
 * The demo player engine (frontend.md §7): sends a step's demo through
 * rpc('keys') one chord or one typed character at a time, paced at 170 ms
 * per chord and 70 ms per character so the learner can follow in the
 * ticker. Any learner keypress in the terminal cancels it.
 */
import { parseDemo, planDemo, type DemoAction } from '../../lib/demo'
import { sleep } from '../../lib/time'
import { rpc } from '../../api/session/rpc'
import { useSessionStore } from '../../store/session'
import { toast, useUiStore } from '../../store/ui'
import i18n from '../../i18n'
import { focusTerminal, onTerminalInput } from '../screen/focus'

let current: AbortController | null = null

export type SendFn = (a: DemoAction, signal: AbortSignal) => Promise<void>

const sendRpc: SendFn = async (a, signal) => {
  if (a.kind === 'keys') await rpc('keys', { keys: a.keys }, { signal, silent: true })
  else if (a.kind === 'text') await rpc('keys', { text: a.text }, { signal, silent: true })
}

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

/** Runs actions with pacing; resolves true when finished, false when cancelled. */
export async function runActions(
  actions: DemoAction[],
  signal: AbortSignal,
  send: SendFn = sendRpc,
): Promise<boolean> {
  try {
    for (const a of actions) {
      if (signal.aborted) return false
      await send(a, signal)
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
  let actions: DemoAction[]
  try {
    actions = planDemo(parseDemo(demo))
  } catch (e) {
    toast(i18n.t('demo.bad', { error: e instanceof Error ? e.message : String(e) }), 'error')
    return
  }
  const ctl = new AbortController()
  current = ctl
  useUiStore.getState().setDemo({ stepId, goal })
  useSessionStore.getState().setDemoPlaying(true)
  focusTerminal()
  const off = onTerminalInput(() => {
    if (current === ctl) {
      cancelDemo(true)
      toast(i18n.t('demo.cancelled'))
    }
  })
  try {
    await runActions(actions, ctl.signal)
  } catch (e) {
    toast(i18n.t('demo.failed', { error: e instanceof Error ? e.message : String(e) }), 'error')
  } finally {
    off()
    if (current === ctl) {
      current = null
      useUiStore.getState().setDemo(null)
      useSessionStore.getState().setDemoPlaying(false)
    }
  }
}
