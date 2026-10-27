/**
 * Stuck patterns (frontend.md §6), the fixed rules from the demo's nudges():
 * three C-g within 8 s, the same unbound key twice in a row, typing into a
 * read-only buffer, a prompt open for 20 s, three or more windows after the
 * learner split one (unless the lesson teaches splitting: C-x 2 or C-x 3 is
 * mentioned; windows that pop up, such as a build's output, are no
 * accident). Pure functions of the ticker, the latest state, the clock and
 * the keys the lesson mentions.
 */
import type { State } from '../protocol.gen'
import type { Nudge } from './habits'
import { isMentioned } from './mentions'
import type { TickerEntry } from './ticker'

export const PANIC_WINDOW_MS = 8000
export const PANIC_COUNT = 3
export const PROMPT_STUCK_MS = 20000
export const MANY_WINDOWS = 3

export interface PatternInput {
  ticker: readonly TickerEntry[]
  state: State | null
  /** When the current minibuffer opened (ms), or null. */
  minibufferSince: number | null
  now: number
  /** Keys the lesson mentions (mentions.ts): splitting on purpose is no accident. */
  mentioned?: readonly string[]
}

/** Commands that split a window (C-x 2, C-x 3 and their relatives). */
export const SPLIT_COMMANDS: ReadonlySet<string> = new Set([
  'split-window-below',
  'split-window-right',
  'split-window-vertically',
  'split-window-horizontally',
  'split-root-window-below',
  'split-root-window-right',
])

const isQuit = (e: TickerEntry) =>
  e.kind === 'quit' &&
  (e.cmd === 'keyboard-quit' ||
    e.cmd === 'abort-minibuffers' ||
    e.cmd === 'minibuffer-keyboard-quit' ||
    e.cmd === 'abort-recursive-edit' ||
    e.cmd === 'isearch-abort')

export function patternNudges({
  ticker,
  state,
  minibufferSince,
  now,
  mentioned = [],
}: PatternInput): Nudge[] {
  const out: Nudge[] = []
  const done = ticker.filter((e) => e.kind !== 'prefix' && e.kind !== 'gap')
  const last = done[done.length - 1]

  const recentQuits = ticker.filter((e) => isQuit(e) && now - e.at < PANIC_WINDOW_MS).length
  if (recentQuits >= PANIC_COUNT) {
    out.push({
      id: 'panic',
      kind: 'calm',
      title: 'Three C-g in a row.',
      body: 'Nothing is broken; C-g is always safe. If the screen still looks wrong, press Rescue, or read "Where am I" on the right.',
    })
  }

  const lastTwo = done.slice(-2)
  if (
    lastTwo.length === 2 &&
    lastTwo.every((e) => e.kind === 'unbound') &&
    lastTwo[0]?.keys.join(' ') === lastTwo[1]?.keys.join(' ')
  ) {
    const k = lastTwo[1]?.keys.join(' ') ?? ''
    out.push({
      id: 'unbound:' + k,
      kind: 'ask',
      title: `${k} is not bound here.`,
      body: 'Ask Emacs what a key does: press C-h k, then the key. In this buffer the common keys are in the cheat sheet below.',
    })
  }

  if (state && last && last.cmd === 'self-insert-command' && state.readonly) {
    const special = state.file === null || state.buf.startsWith('*')
    out.push({
      id: 'ro:' + state.buf,
      kind: 'ask',
      title: `You typed into ${state.buf}, which is read-only.`,
      body: special
        ? 'Press q to leave it, then type in your file.'
        : 'C-x C-q makes this buffer writable.',
    })
  }

  if (state?.minibuffer && minibufferSince !== null && now - minibufferSince > PROMPT_STUCK_MS) {
    out.push({
      id: 'mini-stuck',
      kind: 'calm',
      title: 'Still in the prompt?',
      body: 'RET accepts what you typed. C-g cancels and puts you back where you were.',
    })
  }

  const splitsTaught = isMentioned('C-x 2', mentioned) || isMentioned('C-x 3', mentioned)
  const learnerSplit = done.some((e) => e.cmd !== undefined && SPLIT_COMMANDS.has(e.cmd))
  if (state && (state.windows?.length ?? 0) >= MANY_WINDOWS && learnerSplit && !splitsTaught) {
    out.push({
      id: 'many-windows',
      kind: 'ask',
      title: 'Three windows is usually an accident.',
      body: 'C-x 1 keeps only the current window. (C-x 2 and C-x 3 split; it is easy to press them twice.)',
    })
  }
  return out
}

/** Merges habit cards, pattern nudges and server nudges, minus dismissed ids, without duplicates. */
export function mergeNudges(
  groups: ReadonlyArray<readonly Nudge[]>,
  dismissed: ReadonlySet<string>,
): Nudge[] {
  const seen = new Set<string>()
  const out: Nudge[] = []
  for (const g of groups) {
    for (const n of g) {
      if (dismissed.has(n.id) || seen.has(n.id)) continue
      seen.add(n.id)
      out.push(n)
    }
  }
  return out
}
