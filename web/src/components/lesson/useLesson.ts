/**
 * Lesson wiring shared by the lesson and project screens: load the scenario
 * into Emacs once the session says hello (sessiond resumes it), derive the
 * step views from the lesson snapshot, and record hint usage.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { Scenario } from '../../protocol.gen'
import { lessonMentions } from '../../advice/mentions'
import { useMe, useProgress } from '../../api/queries'
import { rpc } from '../../api/session/rpc'
import { activeStep, useSessionStore } from '../../store/session'
import { useUiStore } from '../../store/ui'
import { hintKey, lessonComplete, selectStepView, type StepView } from '../../store/selectors'
import { guiChosenByLearner, switchFrame } from '../screen/frame'
import { wantsGui } from './lessonTags'
import { focusTerminal } from '../screen/focus'
import { loadAtStep } from '../live/actions'
import { maxHintLevel } from './hintLevel'
import { cancelDemo } from './demoPlayer'
import { errorMessage } from '../../lib/errors'
import { lt } from '../../lib/localtext'

export interface ScenarioLoad {
  loading: boolean
  error: string | null
  retry(): void
  /**
   * This page loaded its lesson, and since then the snapshot shows another
   * lesson or none (another tab or window changed it): Emacs no longer
   * shows this lesson.
   */
  displaced: boolean
  /** Loads this page's lesson again (with reset: its files too). */
  loadHere(reset?: boolean): void
}

/**
 * Loads the lesson into Emacs: one request per navigation intent
 * (lessonId, stepId, retry), sent once the session said hello, its
 * protocol matches, no lesson op runs and the snapshot shows another
 * lesson or another step. Without a step sessiond resumes the lesson. A
 * lesson whose steps are all done or skipped (progress) is not loaded: it
 * shows its saved ticks until "Start this lesson over" or a step's "Go to".
 * A failed load waits for Retry, and a page whose lesson was replaced by
 * another page's does not take Emacs back on its own (`displaced`).
 */
export function useScenarioLoad(
  lessonId: string,
  scenario: Scenario | undefined,
  stepId?: string,
): ScenarioLoad {
  const hello = useSessionStore((s) => s.hello)
  const sessionId = useSessionStore((s) => s.sessionId)
  const lesson = useSessionStore((s) => s.lesson)
  const incompatible = useSessionStore((s) => s.incompatible)
  const active = useSessionStore(activeStep)
  const progress = useProgress()
  const guiOff = useMe().data?.features.gui === false
  const locale = useTranslation().i18n.language
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [retry, setRetry] = useState(0)
  const [reset, setReset] = useState(false)
  const requested = useRef<string | null>(null)

  const ready = !!hello && !!sessionId && !!scenario && !progress.isLoading
  const finished =
    !stepId && !!scenario && lessonComplete(scenario, null, progress.data?.lessons?.[lessonId])
  // A step URL is shown only while that step is active, so a task URL in a
  // complete project loads that task.
  const shows = lesson?.lessonId === lessonId && (!stepId || lesson.step?.id === stepId)
  const key = `${lessonId}#${stepId ?? ''}#${retry}`
  // The intent the snapshot fulfilled (by this page's load or before it):
  // when the snapshot moves on, the page says so instead of loading again,
  // and a project URL that followed its last task survives the completion.
  // A reset (Start this lesson over) loads whatever the snapshot shows.
  const [held, setHeld] = useState<string | null>(null)
  if (shows && held !== key && !reset) setHeld(key)
  const displaced = held === key && lesson?.lessonId !== lessonId
  const wants =
    ready && !incompatible && !lesson?.busy && held !== key && (reset || (!shows && !finished))

  useEffect(() => {
    if (!wants || !scenario || requested.current === key) return
    requested.current = key
    setLoading(true)
    setError(null)
    const index = scenario.steps.findIndex((s) => s.id === stepId)
    const step = scenario.steps[index]
    const load = step
      ? loadAtStep(lessonId, { id: step.id, n: index + 1, goal: lt(step.goal, locale) }, true)
      : rpc('scenario', { lessonId, ...(reset ? { reset: true } : {}) }, { silent: true })
    load
      .then(() => {
        setReset(false)
        // GUI lessons (mode gui, or tagged gui) open in the GUI frame
        // (frontend.md §8), unless the server has it turned off.
        if (wantsGui(scenario) && !guiOff && useSessionStore.getState().mode !== 'gui')
          void switchFrame('gui')
      })
      .catch((e: unknown) => setError(errorMessage(e)))
      .finally(() => setLoading(false))
  }, [wants, key, reset, scenario, lessonId, stepId, locale, guiOff])
  // A step that needs the GUI frame (a project task with images or PDF
  // pages: step mode gui) switches to it when it becomes active, once per
  // activation, so the learner can switch back. A step of a terminal
  // lesson, or a terminal task, returns to the terminal from a GUI frame
  // that a lesson opened (not one the learner picked).
  const guiStep = useRef<string | null>(null)
  useEffect(() => {
    if (!scenario || !active || active.lessonId !== lessonId || guiOff) return
    const key = `${active.lessonId}#${active.stepId}`
    if (guiStep.current === key) return
    guiStep.current = key
    const step = scenario.steps.find((s) => s.id === active.stepId)
    const mode = useSessionStore.getState().mode
    if (step?.mode === 'gui') {
      if (mode !== 'gui') void switchFrame('gui')
    } else if (!wantsGui(scenario) && mode === 'gui' && !guiChosenByLearner()) {
      void switchFrame('terminal')
    }
  }, [active, lessonId, scenario, guiOff])

  return {
    loading: loading || (!ready && !!scenario),
    error,
    retry: () => setRetry((n) => n + 1),
    displaced,
    loadHere: (withReset = false) => {
      setReset(withReset)
      setRetry((n) => n + 1)
    },
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
  const lesson = useSessionStore((s) => s.lesson)
  const checks = useSessionStore((s) => s.checks)
  const hintLevels = useUiStore((s) => s.hintLevels)
  const progress = useProgress()

  const views = useMemo(() => {
    if (!scenario) return []
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
      lesson,
      progress: lp,
      checks: lesson?.lessonId === lessonId ? checks : {},
      hintLevels: levels,
      locale: i18n.language,
    })
  }, [scenario, progress.data, lessonId, lesson, checks, hintLevels, i18n.language])

  const activeIndex = views.findIndex((v) => v.status === 'active')
  const complete =
    !!scenario && lessonComplete(scenario, lesson, progress.data?.lessons?.[lessonId])

  const onHint = useCallback(
    (view: StepView) => {
      const level = Math.min(view.hintLevel + 1, maxHintLevel(view))
      useUiStore.getState().setHintLevel(hintKey(lessonId, view.step.id), level)
      void rpc('hint', { stepId: view.step.id, level }, { silent: true }).catch(() => undefined)
      // The hint names keys to press: they go to Emacs, not to the button.
      focusTerminal()
    },
    [lessonId],
  )
  const onGoTo = useCallback(
    (view: StepView) => {
      const step = { id: view.step.id, n: view.index + 1, goal: view.goal }
      void loadAtStep(lessonId, step).catch(() => undefined)
    },
    [lessonId],
  )
  // A demo belongs to this lesson's step: leaving the lesson stops it.
  useEffect(() => () => cancelDemo(), [lessonId])
  return { views, activeIndex, complete, onHint, onGoTo }
}
