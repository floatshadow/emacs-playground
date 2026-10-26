/**
 * Key names in Emacs notation: parsing (a port of internal/protocol/keys.go)
 * and keyify (a port of design/demo/mock-emacs.js), which finds key
 * sequences in prose so hints, exits and nudges render them as keycaps.
 *
 * This module has no imports so dev/fake-session.ts can load it directly
 * with Node's type stripping.
 */

/** Emacs modifier prefixes in key-description order. */
const MOD_ORDER = ['A', 'C', 'H', 'M', 'S', 's'] as const
type Mod = (typeof MOD_ORDER)[number]

/** Base key names Emacs prints without angle brackets. */
export const NAMED_KEYS: ReadonlySet<string> = new Set([
  'RET',
  'TAB',
  'SPC',
  'ESC',
  'DEL',
  'LFD',
  'NUL',
])

/** One key event, e.g. C-x, M-%, <f3>, C-<left>, RET, a. */
export interface Key {
  mods: ReadonlySet<Mod>
  /** A single character, a named key, or a function key name without brackets. */
  base: string
  /** True for function keys written <name>. */
  func: boolean
}

/** Renders a key canonically (modifiers in A- C- H- M- S- s- order). */
export function formatKey(k: Key): string {
  let out = ''
  for (const m of MOD_ORDER) if (k.mods.has(m)) out += m + '-'
  return out + (k.func ? `<${k.base}>` : k.base)
}

function isFunctionAlias(s: string): boolean {
  const m = /^F([0-9]{1,2})$/.exec(s)
  if (!m) return false
  const n = Number(m[1])
  return n >= 1 && n <= 35
}

/**
 * Parses one key token: optional modifier prefixes followed by a single
 * character, a named key, or a function key in angle brackets. Bare F1 to F35
 * are aliases for <f1> to <f35>. Throws on malformed input.
 */
export function parseKey(tok: string): Key {
  if (tok === '') throw new Error('empty key')
  const mods = new Set<Mod>()
  let rest = tok
  while (rest.length >= 3 && rest[1] === '-') {
    const m = rest[0] as Mod
    if (!(MOD_ORDER as readonly string[]).includes(m)) {
      throw new Error(
        `key ${JSON.stringify(tok)}: unknown modifier ${JSON.stringify(rest.slice(0, 2))}`,
      )
    }
    if (mods.has(m)) throw new Error(`key ${JSON.stringify(tok)}: repeated modifier`)
    mods.add(m)
    rest = rest.slice(2)
  }
  if (rest.length > 2 && rest.startsWith('<') && rest.endsWith('>')) {
    const name = rest.slice(1, -1)
    if (!/^[\p{L}\p{N}_-]+$/u.test(name)) {
      throw new Error(`key ${JSON.stringify(tok)}: bad function key name`)
    }
    return { mods, base: name, func: true }
  }
  if (NAMED_KEYS.has(rest)) return { mods, base: rest, func: false }
  if (isFunctionAlias(rest)) return { mods, base: 'f' + rest.slice(1), func: true }
  const chars = [...rest]
  if (chars.length !== 1 || /\s/u.test(rest)) {
    throw new Error(
      `key ${JSON.stringify(tok)}: not a single character, a named key or <function-key>`,
    )
  }
  return { mods, base: rest, func: false }
}

/** Parses a whitespace-separated key sequence ("C-x C-f", "C-x 4 f"). */
export function parseKeys(seq: string): Key[] {
  const fields = seq.trim().split(/\s+/).filter(Boolean)
  if (fields.length === 0) throw new Error('empty key sequence')
  return fields.map(parseKey)
}

/** Parses and re-renders a key sequence canonically ("M-C-x" → "C-M-x", "F3" → "<f3>"). */
export function normalizeKeys(seq: string): string {
  return parseKeys(seq).map(formatKey).join(' ')
}

/** Like normalizeKeys but returns the input unchanged when it does not parse. */
export function normalizeKeysLoose(seq: string): string {
  try {
    return normalizeKeys(seq)
  } catch {
    return seq.trim()
  }
}

/** True when the key inserts one printable character (no modifiers). */
export function isPrintableKey(k: Key): boolean {
  if (k.mods.size > 0 || k.func) return false
  if (k.base === 'SPC') return true
  return [...k.base].length === 1 && !/\s/u.test(k.base)
}

/* ------------------------------------------------------------------ */
/* keyify: keycaps in prose                                            */
/* ------------------------------------------------------------------ */

// A sequence may only continue with key-like tokens (single characters,
// modifiers, named keys), never ordinary words. From the demo, extended with
// bare function keys written <f3> or <insert>.
const KEY_TOKEN =
  '(?:(?:C-|M-|S-)+(?:<[a-z0-9-]+>|RET|TAB|DEL|ESC|SPC|[^\\s.,;])|<[a-z0-9-]+>|RET|TAB|DEL|ESC|SPC|F[0-9]{1,2}|Insert)'
const KEY_CONT =
  '(?:(?:C-|M-|S-)+(?:<[a-z0-9-]+>|RET|TAB|DEL|ESC|SPC|[^\\s.,;])|<[a-z0-9-]+>|RET|TAB|DEL|ESC|SPC|[^\\s.,;])'
const KEY_SOURCE = '(?<![\\w-])(' + KEY_TOKEN + '(?: ' + KEY_CONT + ')*|q)(?=[\\s.,;:)]|$)'

/** The demo's key regex (global). Use keyify or keyMatches rather than its lastIndex. */
export const KEY_RE = new RegExp(KEY_SOURCE, 'g')

/** A run of prose or a key sequence (one entry per key). */
export type KeySegment = { text: string } | { keys: string[] }

/** Splits prose into text and key sequences: "C-x 1 keeps" → [{keys:[C-x,1]}, {text:" keeps"}]. */
export function keyify(s: string): KeySegment[] {
  const out: KeySegment[] = []
  const re = new RegExp(KEY_SOURCE, 'g')
  let last = 0
  for (let m = re.exec(s); m; m = re.exec(s)) {
    if (m[0].length === 0) {
      re.lastIndex++
      continue
    }
    if (m.index > last) out.push({ text: s.slice(last, m.index) })
    out.push({ keys: m[0].split(' ') })
    last = m.index + m[0].length
  }
  if (last < s.length) out.push({ text: s.slice(last) })
  return out
}

/**
 * True when backticked text in a lesson should render as keycaps: the whole
 * string is a key sequence that starts with a modified, named or function
 * key (`C-x C-f`, `TAB`, `M-g M-n`, `<f3>`), or is `q`. Plain words and
 * single letters (`ma`, `x`) and paths stay code.
 */
export function isKeySequence(s: string): boolean {
  if (s === '' || s !== s.trim()) return false
  if (s === 'q') return true
  let keys: Key[]
  try {
    keys = s.split(/ +/).map(parseKey)
  } catch {
    return false
  }
  const first = keys[0]
  if (!first) return false
  return first.mods.size > 0 || first.func || NAMED_KEYS.has(first.base)
}
