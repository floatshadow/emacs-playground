/**
 * Lesson wiring shared by the lesson and project screens: load the scenario
 * into Emacs once the session says hello (resuming at the first step not
 * done), derive the step views, and record hint usage.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { Scenario } from '../../protocol.gen'
import { lessonMentions } from '../../advice/mentions'
import { useProgress } from '../../api/queries'
import { rpc } from '../../api/session/rpc'
import { useSessionStore } from '../../store/session'
import { useUiStore } from '../../store/ui'
import { hintKey, resumeStep, selectStepView, type StepView } from '../../store/selectors'
import { switchFrame } from '../screen/frame'
import { maxHintLevel } from './hintLevel'
import { errorMessage } from '../../lib/errors'

export interface ScenarioLoad {
  loading: boolean
  error: string | null
  retry(): void
}

/**
 * Calls rpc('scenario') when the active step is not already in this lesson
 * (or not the requested step). Waits for hello so a page reload mid-lesson
 * does not restart the lesson.
 */
export function useScenarioLoad(
  lessonId: string,
  scenario: Scenario | undefined,
  stepId?: string,
): ScenarioLoad {
  const hello = useSessionStore((s) => s.hello)
  const sessionId = useSessionStore((s) => s.sessionId)
  const active = useSessionStore((s) => s.activeStep)
  const outcomes = useSessionStore((s) => s.outcomes)
  const progress = useProgress()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [attempt, setAttempt] = useState(0)
  const requested = useRef<string | null>(null)

  const ready = !!hello && !!sessionId && !!scenario && !progress.isLoading
  // The resume target is decided on fresh progress: after a progress reset
  // (the "dropped" event, maybe from another page) the cached progress
  // still has the old steps done, and loading at its resume target would
  // fight the reset.
  const needsLoad =
    ready &&
    !progress.isFetching &&
    (active?.lessonId !== lessonId || (stepId !== undefined && active?.stepId !== stepId))

  // A dropped step (a progress reset) asks for a new load, even of the
  // step this page asked for before.
  const hadActive = useRef(false)
  useEffect(() => {
    if (active) hadActive.current = true
    else if (hadActive.current) {
      hadActive.current = false
      requested.current = null
    }
  }, [active])

  useEffect(() => {
    if (!needsLoad || !scenario) return
    const target =
      stepId ??
      resumeStep(scenario, outcomes[lessonId] ?? {}, progress.data?.lessons?.[lessonId]) ??
      scenario.steps[0]?.id
    const key = `${lessonId}#${target ?? ''}#${attempt}`
    if (requested.current === key) return
    requested.current = key
    setLoading(true)
    setError(null)
    rpc('scenario', target ? { lessonId, stepId: target } : { lessonId }, { silent: true })
      .then(() => {
        // GUI lessons (mode gui, or tagged gui) open in the GUI frame (frontend.md §8).
        const wantsGui = scenario.mode === 'gui' || (scenario.tags ?? []).includes('gui')
        if (wantsGui && useSessionStore.getState().mode !== 'gui') void switchFrame('gui')
      })
      .catch((e: unknown) => setError(errorMessage(e)))
      .finally(() => setLoading(false))
  }, [needsLoad, scenario, lessonId, stepId, attempt, progress.data, outcomes])

  return {
    loading: loading || (!ready && !!scenario),
    error,
    retry: () => setAttempt((n) => n + 1),
  }
}

/**
 * Tells the session store which keys this lesson mentions, so habit cards
 * and the "three windows" pattern stay quiet for keys the lesson teaches.
 * Cleared when the screen unmounts.
 */
export function useLessonMentions(
  lessonId: string,
  markdown: string | undefined,
  scenario: Scenario | undefined,
): void {
  useEffect(() => {
    if (markdown === undefined && !scenario) return
    useSessionStore.getState().setMentions(lessonMentions(lessonId, markdown ?? '', scenario))
    return () => {
      if (useSessionStore.getState().mentions?.lessonId === lessonId)
        useSessionStore.getState().setMentions(null)
    }
  }, [lessonId, markdown, scenario])
}

export interface LessonSteps {
  views: StepView[]
  activeIndex: number
  complete: boolean
  onHint(view: StepView): void
  onGoTo(view: StepView): void
}

export function useLessonSteps(lessonId: string, scenario: Scenario | undefined): LessonSteps {
  const { i18n } = useTranslation()
  const active = useSessionStore((s) => s.activeStep)
  const checks = useSessionStore((s) => s.checks)
  const outcomes = useSessionStore((s) => s.outcomes)
  const watched = useSessionStore((s) => s.watched)
  const lessonsComplete = useSessionStore((s) => s.lessonsComplete)
  const hintLevels = useUiStore((s) => s.hintLevels)
  const progress = useProgress()

  const views = useMemo(() => {
    if (!scenario) return []
    const activeStepId = active?.lessonId === lessonId ? active.stepId : null
    const levels: Record<string, number> = { ...hintLevels }
    // Hints already revealed in an earlier visit (progress counts them) stay revealed.
    const lp = progress.data?.lessons?.[lessonId]
    for (const s of scenario.steps) {
      const k = hintKey(lessonId, s.id)
      const used = lp?.steps?.[s.id]?.hintsUsed ?? 0
      if (levels[k] === undefined && used > 0) levels[k] = Math.min(used, s.demo ? 2 : 1)
    }
    return selectStepView({
      scenario,
      lessonId,
      activeStepId,
      outcomes: outcomes[lessonId] ?? {},
      progress: lp,
      watched: !!activeStepId && watched?.lessonId === lessonId && watched.stepId === activeStepId,
      checks: activeStepId ? checks : {},
      hintLevels: levels,
      locale: i18n.language,
    })
  }, [
    scenario,
    progress.data,
    lessonId,
    outcomes,
    watched,
    active,
    checks,
    hintLevels,
    i18n.language,
  ])

  const activeIndex = views.findIndex((v) => v.status === 'active')
  // Complete: the last step ended this session, or no step is active and
  // every step is done (skipped and watched steps leave the lesson open).
  const complete =
    views.length > 0 &&
    (lessonsComplete.includes(lessonId) ||
      (activeIndex < 0 && views.every((v) => v.status === 'done')))

  const onHint = useCallback(
    (view: StepView) => {
      const level = Math.min(view.hintLevel + 1, maxHintLevel(view))
      useUiStore.getState().setHintLevel(hintKey(lessonId, view.step.id), level)
      void rpc('hint', { stepId: view.step.id, level }, { silent: true }).catch(() => undefined)
    },
    [lessonId],
  )
  const onGoTo = useCallback(
    (view: StepView) => {
      void rpc('scenario', { lessonId, stepId: view.step.id }).catch(() => undefined)
    },
    [lessonId],
  )
  return { views, activeIndex, complete, onHint, onGoTo }
}
