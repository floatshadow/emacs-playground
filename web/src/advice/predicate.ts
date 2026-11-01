/**
 * Predicates over the State document: a port of internal/protocol/predicate.go
 * (Predicate.Match, Resolve, StateDoc, ExpandTemplate). The Go functions are
 * the reference; keep the semantics identical (AGENTS.md §5).
 *
 * No value imports, so dev/fake-session.ts can load it with Node's type
 * stripping.
 */
import type { Predicate, State } from '../protocol.gen'
import { errorMessage } from '../lib/errors.ts'

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
        compileDialect(arg)
      } catch (e) {
        return 'regex: ' + errorMessage(e)
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
        const re = cachedDialect(String(arg))
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
  if (Array.isArray(a) || Array.isArray(b))
    return (
      Array.isArray(a) &&
      Array.isArray(b) &&
      a.length === b.length &&
      a.every((v, i) => equal(v, b[i]))
    )
  const left = a as Record<string, unknown>
  const right = b as Record<string, unknown>
  const keys = Object.keys(left)
  return (
    keys.length === Object.keys(right).length &&
    keys.every((k) => Object.hasOwn(right, k) && equal(left[k], right[k]))
  )
}

/* ------------------------------------------------------------------ */
/* Regex dialect 1 in JavaScript                                       */
/* ------------------------------------------------------------------ */

const PUNCT = '\\^$.|?*+()[]{}-/'
const SPACE = '\\t\\n\\f\\r '
const CONTROL: Record<string, number> = { t: 9, n: 10, r: 13 }

/**
 * Compiles a predicate regex of regex dialect 1 (CURRICULUM.md B.4; Go's
 * protocol.ValidateRegexDialect is the reference) to a RegExp with RE2's
 * matches: the same allow-list tokenizer, then flag u (and i) with the
 * constructs whose JavaScript meaning differs spelled out: `.` excludes
 * only \n (all under s), ^ and $ under m stop only at \n, \s and \S are
 * ASCII, and \- outside a class is a literal -. Counts have no leading
 * zeros and never follow a group. Anything else outside the dialect throws,
 * so validatePredicate rejects it.
 */
export function compileDialect(src: string): RegExp {
  const rs = Array.from(src)
  const lead = /^\(\?([ims]+)\)/.exec(src)?.[1] ?? ''
  const fail = (m: string): never => {
    throw new Error(m)
  }
  let i = lead ? lead.length + 3 : 0
  let out = ''
  let canRepeat = false
  while (i < rs.length) {
    const c = rs[i] as string
    const rest = rs.slice(i).join('')
    if (c === '\\') {
      const e = rs[i + 1] ?? fail('trailing backslash')
      if (!'dDwWsSbBtnr'.includes(e) && !PUNCT.includes(e)) fail(`\\${e} is not in regex dialect 1`)
      if (lead.includes('i') && 'bB'.includes(e)) fail(`\\${e} does not combine with (?i)`)
      out += e === 's' ? `[${SPACE}]` : e === 'S' ? `[^${SPACE}]` : e === '-' ? '-' : c + e
      canRepeat = !'bB'.includes(e)
      i += 2
    } else if (c === '[') {
      let j = rest.startsWith('[^') ? i + 2 : i + 1
      const first = j
      let prev = -1
      let body = ''
      for (; j < rs.length && rs[j] !== ']'; j++) {
        let lit = rs[j] as string
        const range = lit === '-' && j > first && j + 1 < rs.length && rs[j + 1] !== ']'
        if (range) lit = rs[++j] as string
        let point = lit.codePointAt(0) ?? -1
        let item = lit
        if (lit === '[' || lit === '^') fail(`a literal ${lit} in a class must be escaped`)
        if (lit === '\\') {
          const e = rs[++j] ?? fail('trailing backslash')
          if (!'dwstnr'.includes(e) && !PUNCT.includes(e)) fail(`\\${e} is not in a dialect class`)
          point = 'dws'.includes(e) ? -1 : (CONTROL[e] ?? e.codePointAt(0) ?? -1)
          item = e === 's' ? SPACE : lit + e
        }
        if (range && (prev < 0 || point < prev)) fail('a class range needs literal ends in order')
        prev = range ? -1 : point
        body += range ? '-' + item : item
      }
      if (j === first || j === rs.length) fail('an empty or unterminated class')
      out += (first === i + 2 ? '[^' : '[') + body + ']'
      canRepeat = true
      i = j + 1
    } else if ('*+?{'.includes(c)) {
      const count = c === '{' ? /^\{(0|[1-9][0-9]*)(?:,(0|[1-9][0-9]*)?)?\}/.exec(rest) : null
      if (c === '{' && !count) fail('a literal { must be escaped; counts have no leading zeros')
      if (count && (Number(count[1]) > 1000 || Number(count[2] || 0) > 1000))
        fail('repeat counts are at most 1000')
      if (!canRepeat) fail('nothing to repeat (a count never follows a group)')
      const n = (count?.[0].length ?? 1) + (rs[i + (count?.[0].length ?? 1)] === '?' ? 1 : 0)
      out += rs.slice(i, i + n).join('')
      canRepeat = false
      i += n
    } else if (c === '(' && rest.startsWith('(?') && !rest.startsWith('(?:')) {
      fail('only (?:…) groups and one leading flag group are in regex dialect 1')
    } else if (c === ']' || c === '}') {
      fail(`a literal ${c} must be escaped`)
    } else {
      const n = rest.startsWith('(?:') ? 3 : 1
      if (c === '.') out += lead.includes('s') ? '[\\s\\S]' : '[^\\n]'
      else if (lead.includes('m') && c === '^') out += '(?<![^\\n])'
      else if (lead.includes('m') && c === '$') out += '(?![^\\n])'
      else out += rs.slice(i, i + n).join('')
      canRepeat = !'^$|('.includes(c) && !rest.startsWith('){')
      i += n
    }
  }
  return new RegExp(out, lead.includes('i') ? 'ui' : 'u')
}

const dialectCache = new Map<string, RegExp | null>()

/** compileDialect with a cache; null for a pattern outside the dialect. */
function cachedDialect(src: string): RegExp | null {
  let re = dialectCache.get(src)
  if (re === undefined) {
    try {
      re = compileDialect(src)
    } catch {
      re = null
    }
    if (dialectCache.size > 500) dialectCache.clear()
    dialectCache.set(src, re)
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
