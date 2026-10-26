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
        new RegExp(arg)
      } catch (e) {
        return 'regex: ' + String(e)
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
        try {
          return new RegExp(String(arg)).test(s)
        } catch {
          return false
        }
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
