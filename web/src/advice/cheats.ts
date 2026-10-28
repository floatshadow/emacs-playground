/**
 * The cheat sheet's rows (frontend.md §6): tables.cheats rows for the
 * enabled minor modes that have any (smerge, wdired), then the major
 * mode's, then the common rows. A mode the table does not know yet gets
 * one row pointing at C-h m, which lists the keys of every mode.
 */
import { CheatsCommon, type CheatRow } from '../protocol.gen'

/** The fallback row for a mode without rows; the description is localised by the caller. */
export const DESCRIBE_MODE_KEYS = 'C-h m'

export function cheatRows(
  cheats: Readonly<Record<string, readonly CheatRow[]>>,
  mode: string,
  minors: readonly string[],
  describeMode: string,
): CheatRow[] {
  const out: CheatRow[] = []
  for (const m of minors) if (m !== mode) out.push(...(cheats[m] ?? []))
  const own = cheats[mode] ?? []
  out.push(...own)
  const common = cheats[CheatsCommon] ?? []
  if (mode && own.length === 0 && !common.some(([k]) => k === DESCRIBE_MODE_KEYS))
    out.push([DESCRIBE_MODE_KEYS, describeMode])
  out.push(...common)
  return out
}
