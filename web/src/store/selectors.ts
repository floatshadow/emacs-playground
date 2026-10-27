/**
 * Derived data for the panels (frontend.md §3), memoised by input identity
 * so the ticker, Where am I and the step list re-render only when their
 * inputs change.
 */
import type {
  CheckEvent,
  LessonProgress,
  ProgressStatus,
  Scenario,
  State,
  Step,
  Tables,
} from '../protocol.gen'
import { whereAmI, type LiveContext, type WhereAmIView } from '../advice/exits'
import type { Nudge } from '../advice/habits'
import { mentionedNow } from '../advice/mentions'
import { mergeNudges, patternNudges } from '../advice/patterns'
import { lt } from '../lib/localtext'
import type { SessionData, StepOutcome } from './session'

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

/**
 * Pattern nudges, server nudges and habit cards merged, minus dismissed
 * ones: the live patterns first, then the newest server nudges and cards
 * first (the panel shows NUDGES_SHOWN of them).
 */
export function selectNudges(
  s: Pick<
    SessionData,
    | 'cards'
    | 'serverNudges'
    | 'dismissed'
    | 'ticker'
    | 'state'
    | 'minibufferSince'
    | 'mentions'
    | 'activeStep'
  >,
  now: number,
): Nudge[] {
  const patterns = patternNudges({
    ticker: s.ticker,
    state: s.state,
    minibufferSince: s.minibufferSince,
    now,
    mentioned: mentionedNow(s.mentions, s.activeStep),
  })
  return mergeNudges([patterns, [...s.serverNudges].reverse(), [...s.cards].reverse()], s.dismissed)
}

export type CondStatus = 'met' | 'next' | 'unmet' | 'unknown'

export interface CondView {
  id: string
  text: string
  hint: string
  status: CondStatus
  detail?: string
}

/**
 * A step in the list: done (by the learner), demonstrated (watched, not yet
 * done), skipped, the active one, or still to do.
 */
export type StepStatus = 'done' | 'demonstrated' | 'skipped' | 'active' | 'todo'

export interface StepView {
  step: Step
  index: number
  status: StepStatus
  /** The active step was watched: it offers "Do it yourself". */
  watched: boolean
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
  /** This visit's outcomes for the lesson's steps (session store `outcomes`). */
  outcomes: Readonly<Record<string, StepOutcome>>
  /** The lesson's saved progress, if any. */
  progress: LessonProgress | undefined
  /** The active step was watched (session store `watched`). */
  watched?: boolean
  checks: Record<string, CheckEvent>
  hintLevels: Record<string, number>
  locale: string
}

export const hintKey = (lessonId: string, stepId: string) => `${lessonId}#${stepId}`

export function condId(stepId: string, index: number, explicit?: string): string {
  return explicit ?? `${stepId}/${index + 1}`
}

/** A saved progress status as a step outcome, if it is one. */
function savedOutcome(s: ProgressStatus | undefined): StepOutcome | undefined {
  return s === 'done' || s === 'demonstrated' || s === 'skipped' ? s : undefined
}

/**
 * The status of each step: the engine's active step is always active,
 * whatever progress says; steps after it show no tick (they are evaluated
 * again when reached); other steps show this visit's outcome, else their
 * saved status when it is done, demonstrated or skipped, else to do.
 */
export function stepStatuses(
  scenario: Scenario,
  activeStepId: string | null,
  outcomes: Readonly<Record<string, StepOutcome>>,
  progress: LessonProgress | undefined,
): StepStatus[] {
  const activeIndex = activeStepId ? scenario.steps.findIndex((s) => s.id === activeStepId) : -1
  return scenario.steps.map((step, index) => {
    if (index === activeIndex) {
      // The last step stays the active one after it completes (the lesson
      // is complete); only an ending seen in this visit shows over it.
      const o = outcomes[step.id]
      return o === 'done' || o === 'skipped' ? o : 'active'
    }
    if (activeIndex >= 0 && index > activeIndex) return 'todo'
    return outcomes[step.id] ?? savedOutcome(progress?.steps?.[step.id]?.status) ?? 'todo'
  })
}

export function selectStepView(inp: StepsInput): StepView[] {
  const { scenario, activeStepId, outcomes, progress, checks, hintLevels, locale, lessonId } = inp
  const statuses = stepStatuses(scenario, activeStepId, outcomes, progress)
  return scenario.steps.map((step, index) => {
    const status: StepStatus = statuses[index] ?? 'todo'
    // Only the active step's latest check is current; others may be stale.
    const check = status === 'active' ? checks[step.id] : undefined
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
      watched: status === 'active' && !!inp.watched,
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
 * Where to resume a lesson (sessiond's Engine.FirstIncomplete agrees): the
 * first step that is neither done nor skipped, by this visit's outcome or
 * else the saved status. A demonstrated step is resumed, so the learner
 * practises what they watched.
 */
export function resumeStep(
  scenario: Scenario,
  outcomes: Readonly<Record<string, StepOutcome>>,
  progress: LessonProgress | undefined,
): string | undefined {
  return scenario.steps.find((s) => {
    const o = outcomes[s.id] ?? savedOutcome(progress?.steps?.[s.id]?.status)
    return o !== 'done' && o !== 'skipped'
  })?.id
}
