/**
 * Derived data for the panels (frontend.md §3), memoised by input identity
 * so the ticker, Where am I and the step list re-render only when their
 * inputs change.
 */
import type {
  CheckEvent,
  LessonProgress,
  LessonSnapshot,
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
import { activeStep, type SessionData } from './session'

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
    | 'lesson'
  >,
  now: number,
): Nudge[] {
  const patterns = patternNudges({
    ticker: s.ticker,
    state: s.state,
    minibufferSince: s.minibufferSince,
    now,
    mentioned: mentionedNow(s.mentions, activeStep(s)),
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
  /**
   * The hint for the first unmet condition when hintLevel ≥ 1; text is
   * empty when the condition has none.
   */
  hint: { forText: string; text: string } | null
}

export interface StepsInput {
  scenario: Scenario
  lessonId: string
  /** sessiond's lesson snapshot (session store `lesson`). */
  lesson: LessonSnapshot | null
  /** The lesson's saved progress, if any. */
  progress: LessonProgress | undefined
  checks: Record<string, CheckEvent>
  hintLevels: Record<string, number>
  locale: string
}

export const hintKey = (lessonId: string, stepId: string) => `${lessonId}#${stepId}`

export function condId(stepId: string, index: number, explicit?: string): string {
  return explicit ?? `${stepId}/${index + 1}`
}

/**
 * The status of each step. While the snapshot shows this lesson it says
 * everything: its step is active, steps after it show no tick (they are
 * evaluated again when reached), the others show their saved outcome.
 * Otherwise the saved progress does.
 */
export function stepStatuses(
  scenario: Scenario,
  lesson: LessonSnapshot | null,
  progress: LessonProgress | undefined,
): StepStatus[] {
  const live = lesson?.lessonId === scenario.id ? lesson : null
  const activeIndex = live?.step ? scenario.steps.findIndex((s) => s.id === live.step?.id) : -1
  return scenario.steps.map((step, index) => {
    if (index === activeIndex) return 'active'
    if (activeIndex >= 0 && index > activeIndex) return 'todo'
    const o = live ? live.outcomes[step.id] : progress?.steps?.[step.id]?.status
    return o === 'done' || o === 'demonstrated' || o === 'skipped' ? o : 'todo'
  })
}

/**
 * The lesson is complete: the snapshot says so while it shows the lesson,
 * else every step is done or skipped in the saved progress.
 */
export function lessonComplete(
  scenario: Scenario,
  lesson: LessonSnapshot | null,
  progress: LessonProgress | undefined,
): boolean {
  if (lesson?.lessonId === scenario.id) return lesson.phase === 'complete'
  return (
    scenario.steps.length > 0 &&
    scenario.steps.every((s) => {
      const st = progress?.steps?.[s.id]?.status
      return st === 'done' || st === 'skipped'
    })
  )
}

export function selectStepView(inp: StepsInput): StepView[] {
  const { scenario, lesson, progress, checks, hintLevels, locale, lessonId } = inp
  const statuses = stepStatuses(scenario, lesson, progress)
  const watching = lesson?.lessonId === scenario.id && lesson.phase === 'watching'
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
        ? { forText: unmet.text, text: unmet.hint }
        : null
    return {
      step,
      index,
      status,
      watched: status === 'active' && watching,
      goal: lt(step.goal, locale),
      detail: lt(step.detail, locale),
      conds,
      firstUnmet,
      hintLevel,
      hint,
    }
  })
}
