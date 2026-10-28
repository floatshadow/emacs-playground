/**
 * The fake session's step engine: evaluates a scenario's ordered conditions
 * against the mock Emacs the way sessiond does (backend.md §3.8), with a
 * tiny Elisp subset for the forms the fake lessons use. Anything it cannot
 * evaluate is reported as unknown.
 */
import type { CondResult, Condition, Scenario, State, Truth } from '../src/protocol.gen.ts'
import { matchPredicate, stateDoc } from '../src/advice/predicate.ts'
import type { MockEmacs } from './mock-emacs.ts'

export interface EvalContext {
  eng: MockEmacs
  state: State
  disk: Map<string, string>
  /** Command counts at step start. */
  base: Record<string, number>
}

/* ---- layout patterns (port of protocol.ParseLayoutPattern / MatchLayout) ---- */

type Pat = { single: true } | { name: string } | { split: 'vsplit' | 'hsplit'; children: Pat[] }
type Lay = number | ['vsplit' | 'hsplit', ...Lay[]] | null

function lex(s: string): string[] {
  const toks: string[] = []
  let i = 0
  while (i < s.length) {
    const c = s[i] as string
    if (/\s/.test(c)) i++
    else if (c === '(' || c === ')') {
      toks.push(c)
      i++
    } else if (c === '"') {
      let j = i + 1
      let b = ''
      while (j < s.length && s[j] !== '"') {
        if (s[j] === '\\') j++
        b += s[j] ?? ''
        j++
      }
      toks.push('\x00' + b)
      i = j + 1
    } else {
      let j = i
      while (j < s.length && !/[\s()"]/.test(s[j] as string)) j++
      toks.push(s.slice(i, j))
      i = j
    }
  }
  return toks
}

export function parseLayout(s: string): Pat {
  const toks = lex(s)
  if (toks.length === 1 && toks[0] === 'single') return { single: true }
  const [p, rest] = parseToks(toks)
  if (rest.length) throw new Error('layout: trailing tokens')
  return p
}

function parseToks(toks: string[]): [Pat, string[]] {
  const t = toks[0]
  if (t === undefined) throw new Error('layout: unexpected end')
  if (t === '(') {
    const kind = toks[1]
    if (kind !== 'vsplit' && kind !== 'hsplit') throw new Error('layout: want vsplit or hsplit')
    let rest = toks.slice(2)
    const children: Pat[] = []
    while (rest[0] !== ')') {
      if (!rest.length) throw new Error('layout: missing )')
      const [c, r] = parseToks(rest)
      children.push(c)
      rest = r
    }
    if (children.length < 2) throw new Error('layout: needs two children')
    return [{ split: kind, children }, rest.slice(1)]
  }
  return [{ name: t.startsWith('\x00') ? t.slice(1) : t }, toks.slice(1)]
}

const baseName = (n: string) =>
  n.endsWith('>') && n.lastIndexOf('<') > 0 ? n.slice(0, n.lastIndexOf('<')) : n

function flat(l: Lay): Lay {
  if (!Array.isArray(l)) return l
  const [kind, ...kids] = l
  const out: Lay[] = []
  for (const k of kids) {
    const f = flat(k)
    if (Array.isArray(f) && f[0] === kind) out.push(...(f.slice(1) as Lay[]))
    else out.push(f)
  }
  return out.length === 1 ? (out[0] as Lay) : [kind, ...out]
}

function flatPat(p: Pat): Pat {
  if (!('split' in p)) return p
  const out: Pat[] = []
  for (const c of p.children) {
    const f = flatPat(c)
    if ('split' in f && f.split === p.split) out.push(...f.children)
    else out.push(f)
  }
  return { split: p.split, children: out }
}

export function matchLayout(p: Pat, l: Lay, bufOf: (id: number) => string): boolean {
  const fl = flat(l)
  const fp = flatPat(p)
  if ('single' in fp) return typeof fl === 'number'
  return matchRec(fp, fl, bufOf)
}

function matchRec(p: Pat, l: Lay, bufOf: (id: number) => string): boolean {
  if ('name' in p) {
    if (typeof l !== 'number') return false
    if (p.name === '*') return true
    const want = p.name.includes('/') ? (p.name.split('/').pop() ?? p.name) : p.name
    return baseName(bufOf(l)) === want
  }
  if ('single' in p || !Array.isArray(l) || l[0] !== p.split || l.length - 1 !== p.children.length)
    return false
  return p.children.every((c, i) => matchRec(c, l[i + 1] as Lay, bufOf))
}

/* ---- the Elisp subset ---- */

export function evalElisp(form: string, ctx: EvalContext): Truth {
  const f = form.trim()
  let m = /^\(not\s+([\s\S]+)\)$/.exec(f)
  if (m) {
    const r = evalElisp(m[1] as string, ctx)
    return r === 'unknown' ? r : !r
  }
  m = /^\(equal \(buffer-file-name\) "([^"]*)"\)$/.exec(f)
  if (m) return ctx.state.file === m[1]
  if (f === '(buffer-modified-p)') return ctx.state.modified
  if (f === '(one-window-p)') return ctx.state.windows.length === 1
  m = /^\(= \(count-windows\) (\d+)\)$/.exec(f)
  if (m) return ctx.state.windows.length === Number(m[1])
  m = /^\(string-match-p "((?:[^"\\]|\\.)*)" \(buffer-string\)\)$/.exec(f)
  if (m) {
    const lit = (m[1] as string).replace(/\\(.)/g, '$1')
    return ctx.eng.cur().text.includes(lit)
  }
  m = /^\(equal \(buffer-name\) "([^"]*)"\)$/.exec(f)
  if (m) return ctx.state.buf === m[1]
  return 'unknown'
}

/* ---- conditions ---- */

function truth(c: Condition, ctx: EvalContext): Truth {
  if (c.event) {
    const n =
      (ctx.eng.counts[c.event.command] ?? 0) -
      (c.event.since === 'lesson' ? 0 : (ctx.base[c.event.command] ?? 0))
    return n >= (c.event.min ?? 1)
  }
  if (c.elisp !== undefined) return evalElisp(c.elisp, ctx)
  if (c.file) {
    const t = ctx.disk.get(c.file.path)
    if (c.file.exists !== undefined) return (t !== undefined) === c.file.exists
    if (t === undefined) return false
    if (c.file.contains !== undefined) return t.includes(c.file.contains)
    if (c.file.not_contains !== undefined) return !t.includes(c.file.not_contains)
    if (c.file.regex !== undefined) return new RegExp(c.file.regex).test(t)
    return true
  }
  if (c.layout !== undefined) {
    try {
      const bufOf = (id: number) => ctx.state.windows.find((w) => w.id === id)?.buf ?? ''
      return matchLayout(parseLayout(c.layout), ctx.state.layout as Lay, bufOf)
    } catch {
      return 'unknown'
    }
  }
  if (c.state) return matchPredicate(c.state, stateDoc(ctx.state))
  if (c.shell) return 'unknown'
  if (c.buffer) {
    // A live buffer by name (a uniquify suffix also matches); no processes here.
    const want = c.buffer
    const b = Object.values(ctx.eng.buffers).find(
      (x) => x.name === want.name || x.name.startsWith(want.name + '<'),
    )
    if (!b) return false
    if (want.mode !== undefined && b.mode !== want.mode) return false
    if (want.contains !== undefined && !b.text.includes(want.contains)) return false
    if (want.modified !== undefined && b.modified !== want.modified) return false
    if (want.process === 'running') return 'unknown'
    return true
  }
  if (c.all) {
    const rs = c.all.map((x) => truth(x, ctx))
    if (rs.some((r) => r === false)) return false
    return rs.every((r) => r === true) ? true : 'unknown'
  }
  if (c.any) {
    const rs = c.any.map((x) => truth(x, ctx))
    if (rs.some((r) => r === true)) return true
    return rs.every((r) => r === false) ? false : 'unknown'
  }
  if (c.not) {
    const r = truth(c.not, ctx)
    return r === 'unknown' ? r : !r
  }
  return 'unknown'
}

export function evaluateStep(
  scenario: Scenario,
  stepId: string,
  ctx: EvalContext,
): { conditions: CondResult[]; complete: boolean } {
  const step = scenario.steps.find((s) => s.id === stepId)
  if (!step) return { conditions: [], complete: false }
  const conditions = step.conditions.map((c, i): CondResult => {
    const t = truth(c, ctx)
    const r: CondResult = { id: c.id ?? `${stepId}/${i + 1}`, ok: t === true }
    if (t === 'unknown') {
      r.unknown = true
      r.detail = 'the fake session cannot evaluate this condition'
    }
    return r
  })
  return { conditions, complete: conditions.length > 0 && conditions.every((c) => c.ok) }
}
