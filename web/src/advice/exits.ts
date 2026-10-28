/**
 * Layer 1 of the advice engine (architecture.md §4, frontend.md §6): the
 * exits table evaluated in order against the latest State. Every matching
 * row yields a reason (why) and one exit (how); templates substitute
 * {path} placeholders from the state document.
 *
 * Besides table rows, a few "live" rows come from things the State object
 * cannot know: a pending prefix (from the input tap) and supervisor events
 * (frame closed, Emacs suspended). They are listed first, as in the demo.
 */
import type { ExitRow, ExitSession, State } from '../protocol.gen'
import { expandTemplate, matchPredicate, stateDoc } from './predicate'

export interface ExitView {
  id: string
  why: string
  how: string
  flag?: string
}

export interface WhereAmIView {
  buffer: string
  file: string | null
  modified: boolean
  modeName: string
  mode: string
  minors: string[]
  windows: number
  prompt: string | null
  flags: string[]
  exits: ExitView[]
}

/**
 * The virtual root "session" (protocol.ExitSession, frontend.md §6): facts
 * from the event stream that the State object does not carry.
 */
export function exitSession(ctx: LiveContext): ExitSession {
  let emacs: string | null = null
  if (ctx.overlay === 'frame-closed' || ctx.overlay === 'suspended' || ctx.overlay === 'restarting')
    emacs = ctx.overlay
  else if (ctx.busy) emacs = 'unresponsive'
  return {
    prefix: ctx.prefix || null,
    emacs,
    mode: ctx.mode ?? 'terminal',
    echo: ctx.echo ?? null,
  }
}

/** Evaluates exit rows in order; rows with invalid predicates never match. */
export function evaluateExits(
  state: State,
  rows: readonly ExitRow[],
  session?: ExitSession,
): ExitView[] {
  const doc = stateDoc(state)
  doc['session'] = session ?? exitSession({ prefix: null, overlay: null })
  const out: ExitView[] = []
  for (const row of rows) {
    let ok = false
    try {
      ok = matchPredicate(row.when ?? {}, doc)
    } catch {
      ok = false
    }
    if (!ok) continue
    const v: ExitView = {
      id: row.id,
      why: expandTemplate(row.why, doc, row.when),
      how: expandTemplate(row.how, doc, row.when),
    }
    if (row.flag) v.flag = row.flag
    out.push(v)
  }
  return out
}

/** Lighters for common minor modes; others are derived from the symbol. */
const MINOR_LIGHTERS: Record<string, string> = {
  'eglot--managed-mode': 'Eglot',
  'flymake-mode': 'Flymake',
  'corfu-mode': 'Corfu',
  'overwrite-mode': 'Ovwrt',
  'auto-revert-mode': 'ARev',
  'visual-line-mode': 'Wrap',
  'iedit-mode': 'Iedit',
  'multiple-cursors-mode': 'mc',
  'olivetti-mode': 'Olv',
  'follow-mode': 'Follow',
  'diff-hl-mode': 'diff-hl',
  'apheleia-mode': 'Apheleia',
  'smerge-mode': 'SMerge',
  'wdired-mode': 'Editable',
  'magit-blame-mode': 'Blame',
  'git-timemachine-mode': 'Timemachine',
  'compilation-shell-minor-mode': 'Compilation',
  'compilation-minor-mode': 'Compilation',
}

/**
 * Minor modes that are housekeeping: they never ask anything of the
 * learner, so the Mode row does not list them (the configuration hides
 * their lighters in the mode line too).
 */
export const HOUSEKEEPING_MINORS: ReadonlySet<string> = new Set([
  'ws-butler-mode',
  'global-ws-butler-mode',
  'auto-save-visited-mode',
  'save-place-mode',
  'recentf-mode',
  'savehist-mode',
  'global-auto-revert-mode',
  'electric-indent-mode',
  'global-font-lock-mode',
  'font-lock-mode',
  'which-key-mode',
  'gcmh-mode',
])

/** The minor modes worth naming in the Mode row, as lighters. */
export function visibleMinors(minors: readonly string[]): string[] {
  return minors.filter((m) => !HOUSEKEEPING_MINORS.has(m)).map(minorLighter)
}

/** "eglot--managed-mode" → "Eglot", "flymake-mode" → "Flymake". */
export function minorLighter(sym: string): string {
  const known = MINOR_LIGHTERS[sym]
  if (known) return known
  const base = sym
    .replace(/--.*$/, '')
    .replace(/-mode$/, '')
    .replace(/^global-/, '')
  return base ? base.charAt(0).toUpperCase() + base.slice(1) : sym
}

export interface LiveContext {
  /** Pending prefix from the input tap, e.g. "C-x". */
  prefix: string | null
  /** Supervisor overlay, if any. */
  overlay: 'frame-closed' | 'suspended' | 'restarting' | 'stopping' | null
  busy?: boolean
  /** Frame the browser shows. */
  mode?: 'terminal' | 'gui'
  /** Latest echo text until the next key event. */
  echo?: string | null
}

/** session.* fields the exits table already covers (so live rows step aside). */
function coveredSessionFields(rows: readonly ExitRow[]): Set<string> {
  const out = new Set<string>()
  for (const r of rows) {
    for (const path of Object.keys(r.when ?? {})) {
      const [root, field] = path.split('.')
      if (root === 'session' && field) out.add(field)
    }
  }
  return out
}

/** Live rows the State object cannot express (ported from the demo's whereAmI). */
export function liveExits(ctx: LiveContext): ExitView[] {
  const out: ExitView[] = []
  if (ctx.overlay === 'frame-closed') {
    out.push({
      id: 'live:frame-closed',
      why: 'The Emacs frame was closed with C-x C-c.',
      how: 'The playground reopens it in a moment. On your own machine this quits Emacs for real.',
    })
  }
  if (ctx.overlay === 'suspended') {
    out.push({
      id: 'live:suspended',
      why: 'Emacs is suspended (C-z sends it to the background).',
      how: 'The playground resumes it. In a plain terminal you would type fg.',
    })
  }
  if (ctx.overlay === 'restarting') {
    out.push({
      id: 'live:restarting',
      why: 'Emacs is restarting.',
      how: 'Your files are safe; buffers come back when it is ready.',
    })
  }
  if (ctx.busy) {
    out.push({
      id: 'live:busy',
      why: 'Emacs is busy and not answering.',
      how: 'Wait a moment, or press Rescue: it sends C-g for you.',
    })
  }
  if (ctx.prefix) {
    out.push({
      id: 'live:prefix',
      why: `Prefix ${ctx.prefix} is waiting for the next key.`,
      how: 'C-g cancels it. The popup lists what you can press.',
      flag: 'prefix ' + ctx.prefix,
    })
  }
  return out
}

/** The full "Where am I" panel model. */
export function whereAmI(
  state: State,
  rows: readonly ExitRow[],
  ctx: LiveContext = { prefix: null, overlay: null },
): WhereAmIView {
  // The table's session.* rows (curriculum/tables/exits.yaml) replace the
  // built-in live rows for the same facts; the live rows remain a fallback
  // for tables without them (for example the demo defaults).
  const covered = coveredSessionFields(rows)
  const live = liveExits(ctx).filter((e) =>
    e.id === 'live:prefix' ? !covered.has('prefix') : !covered.has('emacs'),
  )
  const exits = [...live, ...evaluateExits(state, rows, exitSession(ctx))]
  const flags: string[] = []
  for (const e of exits) if (e.flag && !flags.includes(e.flag)) flags.push(e.flag)
  if (state.markActive && !flags.includes('region active')) flags.push('region active')
  const mini = state.minibuffer
  return {
    buffer: state.buf,
    file: state.file,
    modified: state.modified,
    modeName: state.modeName,
    mode: state.mode,
    minors: visibleMinors(state.minors ?? []),
    windows: state.windows?.length ?? 1,
    prompt: mini ? mini.prompt.trim() : null,
    flags,
    exits,
  }
}
