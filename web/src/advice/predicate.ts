/**
 * Predicates over the State document: a port of internal/protocol/predicate.go
 * (Predicate.Match, Resolve, StateDoc, ExpandTemplate). The Go functions are
 * the reference; keep the semantics identical (AGENTS.md §5).
 *
 * No value imports, so dev/fake-session.ts can load it with Node's type
 * stripping.
 */
import type { Predicate, State } from '../protocol.gen'

type Doc = unknown
type Obj = Record<string, unknown>

const isObj = (v: unknown): v is Obj => typeof v === 'object' && v !== null && !Array.isArray(v)

const OPS = new Set(['in', 'not', 'gt', 'gte', 'lt', 'lte', 'exists', 'contains', 'regex'])

/** Validates operator objects and regular expressions; returns an error message or null. */
export function validatePredicate(p: Predicate): string | null {
  for (const [path, want] of Object.entries(p)) {
    if (path === '') return 'predicate: empty path'
    const err = validateExpectation(want)
    if (err) return `predicate ${JSON.stringify(path)}: ${err}`
  }
  return null
}

function validateExpectation(want: unknown): string | null {
  if (!isObj(want)) return null
  const keys = Object.keys(want)
  if (keys.length !== 1) return `operator object must have exactly one key, has ${keys.length}`
  const op = keys[0] as string
  const arg = want[op]
  if (!OPS.has(op)) return `unknown operator ${JSON.stringify(op)}`
  switch (op) {
    case 'in':
      return Array.isArray(arg) ? null : 'in: want a list'
    case 'gt':
    case 'gte':
    case 'lt':
    case 'lte':
      return typeof arg === 'number' ? null : `${op}: want a number`
    case 'exists':
      return typeof arg === 'boolean' ? null : 'exists: want true or false'
    case 'regex':
      if (typeof arg !== 'string') return 'regex: want a string'
      try {
        compileRe2(arg)
      } catch (e) {
        return 'regex: ' + (e instanceof Error ? e.message : String(e))
      }
      return null
    case 'not':
      return validateExpectation(arg)
  }
  return null
}

/** Evaluates p against doc (a decoded JSON value, normally stateDoc(state)). */
export function matchPredicate(p: Predicate, doc: Doc): boolean {
  for (const [path, want] of Object.entries(p)) {
    const [vals, found] = resolve(doc, path)
    if (!matchAny(vals, found, want)) return false
  }
  return true
}

/**
 * Converts a State to the document predicates and templates evaluate
 * against, adding the virtual roots otherWindows and selectedWindow.
 */
export function stateDoc(s: State | null | undefined): Obj {
  const doc: Obj = s ? { ...(s as unknown as Obj) } : {}
  const others: unknown[] = []
  const ws = doc['windows']
  if (Array.isArray(ws)) {
    for (const w of ws) {
      if (isObj(w) && w['selected'] === true) {
        doc['selectedWindow'] = w
        continue
      }
      others.push(w)
    }
  }
  doc['otherWindows'] = others
  return doc
}

/**
 * Returns the values at path in doc and whether the path exists. Array
 * mapping can yield several values.
 */
export function resolve(doc: Doc, path: string): [unknown[], boolean] {
  let cur: unknown[] = [doc]
  for (const seg of path.split('.')) {
    const next: unknown[] = []
    for (const v of cur) next.push(...step(v, seg))
    if (next.length === 0) return [[], false]
    cur = next
  }
  return [cur, true]
}

function step(v: unknown, seg: string): unknown[] {
  if (Array.isArray(v)) {
    if (seg === 'length') return [v.length]
    if (/^-?\d+$/.test(seg)) {
      const i = Number(seg)
      return i >= 0 && i < v.length ? [v[i]] : []
    }
    const out: unknown[] = []
    for (const e of v) out.push(...step(e, seg))
    return out
  }
  if (isObj(v)) return Object.prototype.hasOwnProperty.call(v, seg) ? [v[seg]] : []
  if (typeof v === 'string' && seg === 'length') return [[...v].length]
  return []
}

function matchAny(vals: unknown[], found: boolean, want: unknown): boolean {
  if (isObj(want)) {
    const keys = Object.keys(want)
    if (keys.length === 1) {
      const op = keys[0] as string
      if (op === 'exists') return found === (want[op] === true)
      if (op === 'not') return !matchAny(vals, found, want[op])
    }
  }
  if (!found) return want === null || want === undefined
  return vals.some((v) => matchOne(v, want))
}

function matchOne(v: unknown, want: unknown): boolean {
  if (Array.isArray(want)) return inList(v, want)
  if (isObj(want)) {
    const keys = Object.keys(want)
    if (keys.length !== 1) return false
    const op = keys[0] as string
    const arg = want[op]
    switch (op) {
      case 'in':
        return Array.isArray(arg) && inList(v, arg)
      case 'not':
        return !matchOne(v, arg)
      case 'gt':
      case 'gte':
      case 'lt':
      case 'lte': {
        if (typeof v !== 'number' || typeof arg !== 'number') return false
        if (op === 'gt') return v > arg
        if (op === 'gte') return v >= arg
        if (op === 'lt') return v < arg
        return v <= arg
      }
      case 'contains':
        if (typeof v === 'string') return typeof arg === 'string' && v.includes(arg)
        if (Array.isArray(v)) return inList(arg, v)
        return false
      case 'regex': {
        const s = typeof v === 'string' ? v : ''
        const re = cachedRe2(String(arg))
        return re ? re.test(s) : false
      }
      case 'exists':
        return arg === true
    }
    return false
  }
  return equal(v, want)
}

function inList(v: unknown, l: unknown[]): boolean {
  return l.some((e) => equal(v, e))
}

function equal(a: unknown, b: unknown): boolean {
  if (typeof a === 'number' || typeof b === 'number') return a === b
  if (a === b) return true
  if (a === null || b === null || typeof a !== 'object' || typeof b !== 'object') {
    // Go's reflect.DeepEqual(nil, missing) is handled by matchAny; here
    // undefined and null compare equal like JSON nulls.
    return (a === null || a === undefined) && (b === null || b === undefined)
  }
  return JSON.stringify(a) === JSON.stringify(b)
}

/* ------------------------------------------------------------------ */
/* RE2 patterns in JavaScript                                          */
/* ------------------------------------------------------------------ */

/**
 * The tables are written for Go's RE2 (internal/protocol/predicate.go), and
 * JavaScript's RegExp rejects RE2 syntax that authors use: the inline flag
 * group `(?i)` ("Invalid group"), scoped modifiers `(?i:…)` (only engines
 * from 2025 on have them) and named groups `(?P<name>…)`. A rejected pattern
 * used to count as "no match", so `{not: {regex: "(?i)…"}}` held for every
 * prompt (B11). translateRe2 rewrites those constructs:
 *
 *   - a leading `(?flags)` with i, m, s becomes RegExp flags;
 *   - `(?i:…)` and `(?s:…)` (also `(?is:…)`) become `(?:…)` with letters
 *     expanded to both cases (`a` → `[aA]`, `[a-z]` → `[a-zA-Z]`) and `.`
 *     to `[\s\S]`, which every engine understands;
 *   - `(?P<name>…)` becomes `(?<name>…)`;
 *   - `\p{…}` adds the u flag.
 *
 * Anything else RE2-specific that JavaScript cannot express (a flag group
 * that does not start the pattern, `(?m:…)`, `(?-i:…)`, the U flag) is an
 * error, so validatePredicate rejects it instead of the row never matching.
 */
export function translateRe2(src: string): { source: string; flags: string } {
  let flags = ''
  let s = src
  const lead = /^\(\?([a-zA-Z]+)\)/.exec(s)
  if (lead) {
    for (const f of lead[1] ?? '') {
      if (f !== 'i' && f !== 'm' && f !== 's') throw new Error(`flag ${f} is not supported`)
      if (!flags.includes(f)) flags += f
    }
    s = s.slice(lead[0].length)
  }
  // Per open group: the case folding and dot-all of the enclosing scope, to
  // restore at its ")".
  const scopes: { fold: boolean; dotAll: boolean }[] = []
  let fold = false
  let dotAll = false
  let inClass = false
  let out = ''
  const isLetter = (c: string | undefined) => c !== undefined && /^[a-zA-Z]$/.test(c)
  const other = (c: string) => (c === c.toLowerCase() ? c.toUpperCase() : c.toLowerCase())
  for (let i = 0; i < s.length; i++) {
    const c = s.charAt(i)
    if (c === '\\') {
      const next = s.charAt(i + 1)
      if (next === '') throw new Error('trailing backslash')
      if ((next === 'p' || next === 'P') && s.charAt(i + 2) === '{') {
        const end = s.indexOf('}', i + 3)
        if (end < 0) throw new Error('unterminated \\p{…}')
        out += s.slice(i, end + 1)
        if (!flags.includes('u')) flags += 'u'
        i = end
        continue
      }
      out += c + next
      i++
      continue
    }
    if (inClass) {
      if (c === ']') {
        inClass = false
        out += c
      } else if (fold && isLetter(c)) {
        const hi = s.charAt(i + 2)
        if (s.charAt(i + 1) === '-' && isLetter(hi)) {
          // A range a-z gains its other-case twin A-Z.
          out += `${c}-${hi}${other(c)}-${other(hi)}`
          i += 2
        } else {
          out += c + other(c)
        }
      } else {
        out += c
      }
      continue
    }
    if (c === '[') {
      inClass = true
      out += c
      if (s.charAt(i + 1) === '^') {
        out += '^'
        i++
      }
      // A ] right after [ or [^ is a literal in RE2.
      if (s.charAt(i + 1) === ']') {
        out += '\\]'
        i++
      }
      continue
    }
    if (c === '(') {
      scopes.push({ fold, dotAll })
      if (s.startsWith('(?P<', i)) {
        out += '(?<'
        i += 3
        continue
      }
      const mod = /^\(\?([a-zA-Z]*)(-[a-zA-Z]*)?([:)])/.exec(s.slice(i))
      if (mod && (mod[1] || mod[2])) {
        if (mod[3] === ')') throw new Error(`flag group ${mod[0]} must start the pattern`)
        if (mod[2]) throw new Error(`clearing flags (${mod[0]}) is not supported`)
        for (const f of mod[1] ?? '') {
          if (f === 'i') fold = !flags.includes('i')
          else if (f === 's') dotAll = !flags.includes('s')
          else throw new Error(`flag ${f} in ${mod[0]} is not supported`)
        }
        out += '(?:'
        i += mod[0].length - 1
        continue
      }
      out += c
      continue
    }
    if (c === ')') {
      const top = scopes.pop()
      if (top) ({ fold, dotAll } = top)
      out += c
      continue
    }
    if (c === '.' && dotAll) out += '[\\s\\S]'
    else if (fold && isLetter(c)) out += `[${c}${other(c)}]`
    else out += c
  }
  return { source: out, flags }
}

/** Compiles an RE2 pattern for JavaScript (translateRe2); throws when it cannot. */
export function compileRe2(src: string): RegExp {
  const { source, flags } = translateRe2(src)
  return new RegExp(source, flags)
}

const re2Cache = new Map<string, RegExp | null>()

/** compileRe2 with a cache; null for a pattern that does not compile. */
function cachedRe2(src: string): RegExp | null {
  let re = re2Cache.get(src)
  if (re === undefined) {
    try {
      re = compileRe2(src)
    } catch {
      re = null
    }
    if (re2Cache.size > 500) re2Cache.clear()
    re2Cache.set(src, re)
  }
  return re
}

const TEMPLATE_RE = /\{([A-Za-z0-9_.]+)\}/g

/**
 * Replaces {path} placeholders in tmpl with values resolved against doc.
 * When the row's predicate constrains a path that maps over an array (for
 * example "otherWindows.mode"), placeholders under the same array root
 * ("otherWindows.buf") take the first element that satisfies the predicate;
 * otherwise the first value is used. Missing values become "".
 */
export function expandTemplate(tmpl: string, doc: Doc, p: Predicate = {}): string {
  return tmpl.replace(TEMPLATE_RE, (_m, path: string) => {
    const [v, ok] = resolveForTemplate(doc, path, p)
    return ok ? formatValue(v) : ''
  })
}

function resolveForTemplate(doc: Doc, path: string, p: Predicate): [unknown, boolean] {
  const segs = path.split('.')
  for (let i = 1; i < segs.length; i++) {
    const root = segs.slice(0, i).join('.')
    const [arr, ok] = resolve(doc, root)
    if (!ok || arr.length !== 1) continue
    const list = arr[0]
    if (!Array.isArray(list)) continue
    const rest = segs.slice(i).join('.')
    for (const el of list) {
      if (elementSatisfies(el, root, p)) {
        const [v, ok2] = resolve(el, rest)
        if (ok2 && v.length > 0) return [v[0], true]
      }
    }
  }
  const [v, ok] = resolve(doc, path)
  if (!ok || v.length === 0) return [undefined, false]
  return [v[0], true]
}

function elementSatisfies(el: unknown, root: string, p: Predicate): boolean {
  const prefix = root + '.'
  for (const [path, want] of Object.entries(p)) {
    if (!path.startsWith(prefix)) continue
    const [vals, found] = resolve(el, path.slice(prefix.length))
    if (!matchAny(vals, found, want)) return false
  }
  return true
}

function formatValue(v: unknown): string {
  if (v === null || v === undefined) return ''
  if (typeof v === 'string') return v.trim()
  if (typeof v === 'number' || typeof v === 'boolean') return String(v)
  return JSON.stringify(v)
}
