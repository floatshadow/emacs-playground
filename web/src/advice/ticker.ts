/**
 * The key ticker (frontend.md §6): a reducer over prefix and key events.
 *
 * A prefix event adds or replaces the pending entry; a key event removes
 * pending prefix entries and appends the completed command. The agent reports
 * an unbound sequence as cmd "undefined". Consecutive typing commands
 * (self-insert-command and friends) collapse into one entry, "typed N
 * characters" with the last twelve, so a word does not push the chords out
 * of view (B10, R6). Entries are kept oldest first (max 40); the view shows
 * the newest six, newest first.
 */
import type { KeyEvent, PrefixEvent } from '../protocol.gen'

export type TickerKind = 'ok' | 'prefix' | 'unbound' | 'quit' | 'gap'

export interface TickerEntry {
  id: number
  keys: string[]
  cmd?: string
  gloss?: string
  kind: TickerKind
  mx?: boolean
  at: number
  /** Typing runs: the last TYPED_SHOWN characters typed. */
  typed?: string
  /** Typing runs: how many characters were typed. */
  count?: number
}

export const TICKER_MAX = 40
export const TICKER_VISIBLE = 6

/** Commands that insert the key typed: consecutive ones collapse into one entry. */
export const TYPING_COMMANDS: ReadonlySet<string> = new Set([
  'self-insert-command',
  'isearch-printing-char',
  'org-self-insert-command',
  'term-send-raw',
])

/** Characters of a typing run the ticker shows. */
export const TYPED_SHOWN = 12

/** The character a typing command's key inserts ("SPC" is a space). */
export function typedChar(keys: string): string {
  const k = keys.trim()
  if (k === 'SPC') return ' '
  if (k === 'TAB') return '\t'
  return k.replace(/^S-/, '')
}

/** Command name the agent reports for an unbound key (protocol.UndefinedCommand). */
export const UNDEFINED_COMMAND = 'undefined'

/** Commands shown in the quit colour and counted by the "three C-g" pattern. */
export const QUIT_COMMANDS: ReadonlySet<string> = new Set([
  'keyboard-quit',
  'keyboard-escape-quit',
  'abort-minibuffers',
  'abort-recursive-edit',
  'minibuffer-keyboard-quit',
  'isearch-abort',
  'isearch-cancel',
  'playground-rescue',
])

let nextId = 1

function dropPending(entries: TickerEntry[]): TickerEntry[] {
  let n = entries.length
  while (n > 0 && entries[n - 1]?.kind === 'prefix') n--
  return n === entries.length ? entries : entries.slice(0, n)
}

function push(entries: TickerEntry[], e: TickerEntry): TickerEntry[] {
  const out =
    entries.length >= TICKER_MAX ? entries.slice(entries.length - TICKER_MAX + 1) : entries.slice()
  out.push(e)
  return out
}

export function kindForCommand(cmd: string): TickerKind {
  if (cmd === UNDEFINED_COMMAND) return 'unbound'
  if (QUIT_COMMANDS.has(cmd)) return 'quit'
  return 'ok'
}

// A mouse event as `key-description' prints it (<mouse-1>, <down-mouse-1>,
// <C-wheel-up>, <drag-mouse-1>) and the screen areas it can be prefixed with
// (<mode-line> <mouse-1>).
const MOUSE_TOKEN =
  /^<(?:[ACHMSs]-)*(?:(?:down|drag|double|triple|up)-)*(?:mouse-\d+|wheel-(?:up|down|left|right))>$/
const AREA_TOKEN =
  /^<(?:mode-line|header-line|tab-line|tab-bar|menu-bar|vertical-line|vertical-scroll-bar|horizontal-scroll-bar|left-margin|right-margin|left-fringe|right-fringe)>$/

/**
 * True when a key event is a mouse action only: clicking into the terminal
 * to focus it, scrolling, dragging. The ticker teaches keys, and a single
 * click reports two commands (mouse-drag-region on the press,
 * mouse-set-point on the release) that would push the learner's last keys
 * out of view, so such events are not shown.
 */
export function isMouseOnly(keys: string): boolean {
  const ks = keys.trim().split(/\s+/).filter(Boolean)
  return (
    ks.some((k) => MOUSE_TOKEN.test(k)) &&
    ks.every((k) => MOUSE_TOKEN.test(k) || AREA_TOKEN.test(k))
  )
}

/** Splits an event's key description into keys; an M-x invocation shows as M-x. */
export function splitKeys(keys: string, mx?: boolean): string[] {
  if (mx) return ['M-x']
  const ks = keys.trim().split(/\s+/).filter(Boolean)
  return ks.length ? ks : ['?']
}

export function tickerOnKey(entries: TickerEntry[], ev: KeyEvent, now: number): TickerEntry[] {
  const base = dropPending(entries)
  const at = ev.ts ?? now
  if (!ev.mx && TYPING_COMMANDS.has(ev.cmd)) {
    const ch = typedChar(ev.keys)
    const last = base[base.length - 1]
    if (last && last.typed !== undefined && last.cmd === ev.cmd) {
      // Same id: the entry grows in place (no new ticker card, no new
      // announcement for screen readers on every letter).
      const typed = [...(last.typed + ch)].slice(-TYPED_SHOWN).join('')
      const merged: TickerEntry = {
        ...last,
        keys: splitKeys(ev.keys),
        typed,
        count: (last.count ?? 1) + 1,
        at,
      }
      return [...base.slice(0, -1), merged]
    }
    return push(base, {
      id: nextId++,
      keys: splitKeys(ev.keys),
      cmd: ev.cmd,
      kind: 'ok',
      at,
      typed: ch,
      count: 1,
    })
  }
  const e: TickerEntry = {
    id: nextId++,
    keys: splitKeys(ev.keys, ev.mx),
    cmd: ev.cmd,
    kind: kindForCommand(ev.cmd),
    at,
  }
  if (ev.mx) e.mx = true
  return push(base, e)
}

export function tickerOnPrefix(
  entries: TickerEntry[],
  ev: PrefixEvent,
  now: number,
): TickerEntry[] {
  const base = dropPending(entries)
  if (!ev.keys.trim()) return base
  return push(base, { id: nextId++, keys: splitKeys(ev.keys), kind: 'prefix', at: ev.ts ?? now })
}

/** Marks a gap in the event stream (events were lost; a fresh check was requested). */
export function tickerGap(entries: TickerEntry[], now: number): TickerEntry[] {
  const base = dropPending(entries)
  if (base[base.length - 1]?.kind === 'gap') return base
  return push(base, { id: nextId++, keys: [], kind: 'gap', at: now })
}

/** Adds a synthetic entry, e.g. for the Rescue button. */
export function tickerNote(
  entries: TickerEntry[],
  note: { keys: string[]; cmd: string; gloss?: string; kind: TickerKind },
  now: number,
): TickerEntry[] {
  return push(dropPending(entries), { id: nextId++, ...note, at: now })
}

export interface TickerView {
  entry: TickerEntry
  last: boolean
  label: string
  gloss: string
}

/**
 * The visible entries, newest first, with labels and glosses resolved from
 * the gloss table and the describe cache (commands the table lacks).
 */
export function selectTicker(
  entries: TickerEntry[],
  gloss: Record<string, string>,
  described: Record<string, string>,
  max = TICKER_VISIBLE,
): TickerView[] {
  const items = entries.slice(-max).reverse()
  return items.map((entry, i) => {
    if (entry.kind === 'prefix') {
      return {
        entry,
        last: i === 0,
        label: 'waiting for the next key',
        gloss: 'This is a prefix: it does nothing alone.',
      }
    }
    if (entry.kind === 'unbound') {
      return {
        entry,
        last: i === 0,
        label: '(unbound)',
        gloss: entry.gloss ?? 'Nothing is bound to this key here',
      }
    }
    const cmd = entry.cmd ?? ''
    return {
      entry,
      last: i === 0,
      label: cmd,
      gloss: entry.gloss ?? gloss[cmd] ?? described[cmd] ?? '',
    }
  })
}

/** Commands in the visible window that need an rpc('describe') gloss. */
export function missingGlosses(
  entries: TickerEntry[],
  gloss: Record<string, string>,
  described: Record<string, string>,
  max = TICKER_VISIBLE,
): TickerEntry[] {
  return entries
    .slice(-max)
    .filter(
      (e) =>
        (e.kind === 'ok' || e.kind === 'quit') &&
        !e.mx &&
        !e.gloss &&
        e.cmd !== undefined &&
        e.cmd !== '' &&
        !(e.cmd in gloss) &&
        !(e.cmd in described),
    )
}
