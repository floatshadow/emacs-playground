/**
 * Layer 2 of the advice engine (frontend.md §6): VS Code muscle memory.
 *
 * When a single-key command matches a row of tables.habits, one card is
 * created per key per session; cards expire after 12 commands.
 */
import type { HabitRow, NudgeKind } from '../protocol.gen'
import { normalizeKeysLoose } from './keys'
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
  /** totalCommands when the card was created; it expires 12 commands later. */
  createdAtTotal: number
}

/** Commands after which a habit card disappears. */
export const HABIT_TTL_COMMANDS = 12

export function habitTitle(key: string, vscode: string): string {
  return `Coming from VS Code? ${key} is not ${vscode}.`
}

/**
 * The card for a ticker entry, or null when the entry is not a single-key
 * command, matches no habit, or the habit was already shown this session.
 */
export function habitCardFor(
  entry: TickerEntry,
  habits: readonly HabitRow[],
  seen: ReadonlySet<string>,
  totalCommands: number,
): HabitCard | null {
  if (entry.kind === 'prefix' || entry.kind === 'gap' || entry.mx) return null
  if (entry.keys.length !== 1) return null
  const key = normalizeKeysLoose(entry.keys[0] ?? '')
  if (seen.has(key)) return null
  const row = habits.find((h) => normalizeKeysLoose(h.key) === key)
  if (!row) return null
  return {
    id: 'habit:' + key,
    kind: 'habit',
    key,
    title: habitTitle(key, row.vscode),
    body: row.say,
    createdAtTotal: totalCommands,
  }
}

/** Drops cards older than HABIT_TTL_COMMANDS commands; returns the same array when nothing expired. */
export function expireCards(cards: HabitCard[], totalCommands: number): HabitCard[] {
  const kept = cards.filter((c) => totalCommands - c.createdAtTotal <= HABIT_TTL_COMMANDS)
  return kept.length === cards.length ? cards : kept
}
