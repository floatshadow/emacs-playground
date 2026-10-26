/**
 * Demo strings (CURRICULUM.md Part B.3): a port of protocol.ParseDemo.
 *
 *   C-x C-f "ma" TAB ~400 RET
 *
 * Whitespace-separated tokens. A token starting with a double quote is text
 * typed literally up to the closing quote; inside it \" is a quote, \\ a
 * backslash, \n a newline (typed as RET) and \t a tab. ~N pauses N ms.
 * Anything else is a key in Emacs notation.
 *
 * Imported by dev/fake-session.ts too, hence the explicit .ts extension.
 */
import type { DemoToken } from '../protocol.gen'
import { formatKey, parseKey } from '../advice/keys.ts'

/** Pacing from protocol.DemoChordDelayMs and DemoCharDelayMs (frontend.md §7). */
export const CHORD_DELAY_MS = 170
export const CHAR_DELAY_MS = 70

export class DemoParseError extends Error {}

const isSpace = (c: string | undefined) => c !== undefined && /\s/u.test(c)

export function parseDemo(s: string): DemoToken[] {
  const out: DemoToken[] = []
  let i = 0
  while (i < s.length) {
    const c = s[i] as string
    if (isSpace(c)) {
      i++
      continue
    }
    if (c === '"') {
      let text = ''
      let j = i + 1
      let closed = false
      while (j < s.length) {
        const d = s[j] as string
        if (d === '\\' && j + 1 < s.length) {
          const e = s[j + 1]
          if (e === '"') text += '"'
          else if (e === '\\') text += '\\'
          else if (e === 'n') text += '\n'
          else if (e === 't') text += '\t'
          else throw new DemoParseError(`demo: bad escape \\${e} at offset ${j}`)
          j += 2
          continue
        }
        if (d === '"') {
          closed = true
          j++
          break
        }
        text += d
        j++
      }
      if (!closed) throw new DemoParseError(`demo: unterminated string at offset ${i}`)
      if (j < s.length && !isSpace(s[j])) {
        throw new DemoParseError(`demo: missing space after string at offset ${j}`)
      }
      if (text === '') throw new DemoParseError(`demo: empty string at offset ${i}`)
      out.push({ kind: 'text', text })
      i = j
      continue
    }
    let j = i
    while (j < s.length && !isSpace(s[j])) j++
    const tok = s.slice(i, j)
    if (tok.length > 1 && tok.startsWith('~')) {
      const body = tok.slice(1)
      const ms = /^\d+$/.test(body) ? Number(body) : NaN
      if (!Number.isFinite(ms) || ms < 0 || ms > 60000) {
        throw new DemoParseError(`demo: bad pause ${JSON.stringify(tok)} (want ~0 to ~60000)`)
      }
      out.push({ kind: 'pause', pauseMs: ms })
    } else {
      try {
        out.push({ kind: 'key', key: formatKey(parseKey(tok)) })
      } catch (e) {
        throw new DemoParseError('demo: ' + (e instanceof Error ? e.message : String(e)))
      }
    }
    i = j
  }
  if (out.length === 0) throw new DemoParseError('demo: empty')
  return out
}

/** One paced action of the demo player. */
export type DemoAction =
  | { kind: 'keys'; keys: string; delayMs: number }
  | { kind: 'text'; text: string; delayMs: number }
  | { kind: 'pause'; delayMs: number }

/**
 * Expands tokens into the actions DemoPlayer sends through rpc('keys'): one
 * chord per action followed by 170 ms, one typed character per action
 * followed by 70 ms (newlines and tabs in typed text become RET and TAB), and
 * pauses as given.
 */
export function planDemo(tokens: DemoToken[]): DemoAction[] {
  const out: DemoAction[] = []
  for (const t of tokens) {
    if (t.kind === 'key' && t.key) out.push({ kind: 'keys', keys: t.key, delayMs: CHORD_DELAY_MS })
    else if (t.kind === 'pause') out.push({ kind: 'pause', delayMs: t.pauseMs ?? 0 })
    else if (t.kind === 'text' && t.text) {
      for (const ch of t.text) {
        if (ch === '\n') out.push({ kind: 'keys', keys: 'RET', delayMs: CHORD_DELAY_MS })
        else if (ch === '\t') out.push({ kind: 'keys', keys: 'TAB', delayMs: CHORD_DELAY_MS })
        else out.push({ kind: 'text', text: ch, delayMs: CHAR_DELAY_MS })
      }
    }
  }
  return out
}

/** Renders tokens back as a readable key list for the "show the keys" hint level. */
export function describeDemo(tokens: DemoToken[]): string {
  return tokens
    .filter((t) => t.kind !== 'pause')
    .map((t) => (t.kind === 'key' ? t.key : JSON.stringify(t.text)))
    .join(' ')
}
