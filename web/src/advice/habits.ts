/**
 * Layer 2 of the advice engine (frontend.md §6): VS Code muscle memory.
 *
 * When a single-key command matches a row of tables.habits, one card is
 * created per key, once per session (the keys seen persist in localStorage
 * across reloads), unless the lesson mentions that key: a card must not
 * scold the learner for a key the lesson asked for. Cards stay until
 * dismissed or until another lesson loads; the panel shows the newest three.
 */
import type { HabitRow, NudgeKind } from '../protocol.gen'
import { normalizeKeysLoose } from './keys'
import { isMentioned } from './mentions'
import type { TickerEntry } from './ticker'

export interface Nudge {
  id: string
  kind: NudgeKind
  title: string
  body: string
}

export interface HabitCard extends Nudge {
  kind: 'habit'
  key: string
  /** totalCommands when the card was created. */
  createdAtTotal: number
}

/** Cards the "Did you mean" panel shows before "N earlier tips". */
export const NUDGES_SHOWN = 3

export function habitTitle(key: string, vscode: string): string {
  return `Coming from VS Code? ${key} is not ${vscode}.`
}

/**
 * The card for a ticker entry, or null when the entry is not a single-key
 * command, matches no habit, the habit was already shown this session, or
 * the lesson mentions the key (mentioned: normalised sequences; a card for
 * K is suppressed when K starts a command of one of them, isMentioned).
 */
export function habitCardFor(
  entry: TickerEntry,
  habits: readonly HabitRow[],
  seen: ReadonlySet<string>,
  totalCommands: number,
  mentioned: readonly string[] = [],
): HabitCard | null {
  if (entry.kind === 'prefix' || entry.kind === 'gap' || entry.mx) return null
  if (entry.typed !== undefined) return null
  if (entry.keys.length !== 1) return null
  const key = normalizeKeysLoose(entry.keys[0] ?? '')
  if (seen.has(key)) return null
  const row = habits.find((h) => normalizeKeysLoose(h.key) === key)
  if (!row) return null
  if (isMentioned(key, mentioned)) return null
  return {
    id: 'habit:' + key,
    kind: 'habit',
    key,
    title: habitTitle(key, row.vscode),
    body: row.say,
    createdAtTotal: totalCommands,
  }
}

/** localStorage key of the habit keys already shown in a session. */
export const habitsSeenKey = (sessionId: string) => `pg.habitsSeen.${sessionId}`

/** The habit keys already shown in this session (an earlier page load), or none. */
export function loadHabitsSeen(sessionId: string): string[] {
  try {
    const raw = localStorage.getItem(habitsSeenKey(sessionId))
    const v: unknown = raw ? JSON.parse(raw) : []
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []
  } catch {
    return []
  }
}

export function saveHabitsSeen(sessionId: string, seen: ReadonlySet<string>): void {
  try {
    if (seen.size === 0) localStorage.removeItem(habitsSeenKey(sessionId))
    else localStorage.setItem(habitsSeenKey(sessionId), JSON.stringify([...seen]))
  } catch {
    // private mode: "once each" lasts for the tab only
  }
}
