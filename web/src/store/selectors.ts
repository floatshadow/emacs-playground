/**
 * Derived data for the panels (frontend.md §3), memoised by input identity
 * so the ticker, Where am I and the step list re-render only when their
 * inputs change.
 */
import type { CheckEvent, Progress, Scenario, State, Step, Tables } from '../protocol.gen'
import { whereAmI, type LiveContext, type WhereAmIView } from '../advice/exits'
import type { Nudge } from '../advice/habits'
import { mergeNudges, patternNudges } from '../advice/patterns'
import { lt } from '../lib/localtext'
import type { SessionData } from './session'

/** Memoises a function on the identity of its arguments (last call only). */
export function memoLast<A extends unknown[], R>(fn: (...args: A) => R): (...args: A) => R {
  let lastArgs: A | null = null
  let last: R
  return (...args: A) => {
    if (
      lastArgs &&
      lastArgs.length === args.length &&
      lastArgs.every((a, i) => Object.is(a, args[i]))
    ) {
      return last
    }
    lastArgs = args
    last = fn(...args)
    return last
  }
}

const whereMemo = memoLast(
  (
    state: State,
    exits: Tables['exits'],
    prefix: string | null,
    overlay: LiveContext['overlay'],
    busy: boolean,
    mode: 'terminal' | 'gui',
    echo: string | null,
  ) => whereAmI(state, exits, { prefix, overlay, busy, mode, echo }),
)

export function selectWhereAmI(
  s: Pick<SessionData, 'state' | 'tables' | 'prefix' | 'emacs' | 'mode' | 'echoLive'>,
): WhereAmIView | null {
  if (!s.state) return null
  return whereMemo(
    s.state,
    s.tables.exits,
    s.prefix,
    s.emacs.overlay,
    s.emacs.busy,
    s.mode,
    s.echoLive,
  )
}

/** Habit cards, pattern nudges and server nudges merged, minus dismissed ones. */
export function selectNudges(
  s: Pick<
    SessionData,
    'cards' | 'serverNudges' | 'dismissed' | 'ticker' | 'state' | 'minibufferSince'
  >,
  now: number,
): Nudge[] {
  const patterns = patternNudges({
    ticker: s.ticker,
    state: s.state,
    minibufferSince: s.minibufferSince,
    now,
  })
  return mergeNudges([s.cards, patterns, s.serverNudges], s.dismissed)
}

export type CondStatus = 'met' | 'next' | 'unmet' | 'unknown'

export interface CondView {
  id: string
  text: string
  hint: string
  status: CondStatus
  detail?: string
}

export type StepStatus = 'done' | 'active' | 'todo'

export interface StepView {
  step: Step
  index: number
  status: StepStatus
  goal: string
  detail: string
  conds: CondView[]
  /** Index of the first unmet condition of the active step, or -1. */
  firstUnmet: number
  hintLevel: number
  /** The hint for the first unmet condition when hintLevel ≥ 1. */
  hint: { forText: string; text: string } | null
}

export interface StepsInput {
  scenario: Scenario
  lessonId: string
  /** Active step id when the active step belongs to this lesson. */
  activeStepId: string | null
  /** Steps known done (progress plus this session's step events). */
  done: ReadonlySet<string>
  checks: Record<string, CheckEvent>
  hintLevels: Record<string, number>
  locale: string
}

export const hintKey = (lessonId: string, stepId: string) => `${lessonId}#${stepId}`

export function condId(stepId: string, index: number, explicit?: string): string {
  return explicit ?? `${stepId}/${index + 1}`
}

export function selectStepView(inp: StepsInput): StepView[] {
  const { scenario, activeStepId, done, checks, hintLevels, locale, lessonId } = inp
  const activeIndex = activeStepId ? scenario.steps.findIndex((s) => s.id === activeStepId) : -1
  return scenario.steps.map((step, index) => {
    const isDone = done.has(step.id) || (activeIndex >= 0 && index < activeIndex)
    const status: StepStatus =
      index === activeIndex && !done.has(step.id) ? 'active' : isDone ? 'done' : 'todo'
    const check = checks[step.id]
    const byId = new Map((check?.conditions ?? []).map((c) => [c.id, c]))
    const base = step.conditions.map((c, i) => {
      const id = condId(step.id, i, c.id)
      const r = byId.get(id)
      return { id, text: lt(c.text, locale), hint: lt(c.hint, locale), r }
    })
    const firstUnmet = status === 'active' ? base.findIndex((c) => !c.r?.ok) : -1
    const conds: CondView[] = base.map((c, i) => {
      let st: CondStatus = 'unmet'
      if (status === 'done' || c.r?.ok) st = 'met'
      else if (c.r?.unknown) st = 'unknown'
      if (st !== 'met' && i === firstUnmet) st = 'next'
      const v: CondView = { id: c.id, text: c.text || c.id, hint: c.hint, status: st }
      if (c.r?.detail) v.detail = c.r.detail
      return v
    })
    const hintLevel = hintLevels[hintKey(lessonId, step.id)] ?? 0
    const unmet = firstUnmet >= 0 ? conds[firstUnmet] : undefined
    const hint =
      status === 'active' && hintLevel > 0 && unmet
        ? { forText: unmet.text, text: unmet.hint || 'No hint for this one: try "Watch it".' }
        : null
    return {
      step,
      index,
      status,
      goal: lt(step.goal, locale),
      detail: lt(step.detail, locale),
      conds,
      firstUnmet,
      hintLevel,
      hint,
    }
  })
}

/**
 * Done step ids for a lesson: this session's completions, plus steps the
 * progress store records as done unless they were (re)loaded this session
 * (redoing a step makes it active again until it completes).
 */
export function doneSteps(
  progress: Progress | undefined,
  lessonId: string,
  completed: Record<string, string[]>,
  reopened: Record<string, string[]> = {},
): Set<string> {
  const out = new Set<string>(completed[lessonId] ?? [])
  const again = new Set(reopened[lessonId] ?? [])
  const lp = progress?.lessons?.[lessonId]
  if (lp) {
    for (const [id, sp] of Object.entries(lp.steps ?? {})) {
      if (sp.status === 'done' && !again.has(id)) out.add(id)
    }
  }
  return out
}

/** The first step not done, for resuming a lesson. */
export function resumeStep(scenario: Scenario, done: ReadonlySet<string>): string | undefined {
  return scenario.steps.find((s) => !done.has(s.id))?.id
}
