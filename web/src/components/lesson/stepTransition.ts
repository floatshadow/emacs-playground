/**
 * The completion transition (L1, frontend.md §7): when a step of this
 * lesson ends, its tick pops and the step is highlighted for 600 ms, then
 * the next step scrolls into view; the live region announces it. Under
 * reduced motion there is no highlight and the scroll is instant.
 */
import { useEffect, useRef, useState, type RefObject } from 'react'
import { useTranslation } from 'react-i18next'
import type { Scenario } from '../../protocol.gen'
import { onSessionEvent } from '../../api/session/connection'
import { lt } from '../../lib/localtext'
import { useUiStore } from '../../store/ui'

export const STEP_FLASH_MS = 600

function prefersReducedMotion(): boolean {
  if (useUiStore.getState().reducedMotion) return true
  return (
    typeof window !== 'undefined' &&
    !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
  )
}

export function useStepTransition(
  lessonId: string,
  scenario: Scenario | undefined,
  pane: RefObject<HTMLElement | null>,
): { flash: string | null; announce: string } {
  const { t, i18n } = useTranslation()
  // The step that just ended, with a counter so the same step ending twice
  // (redo) flashes again.
  const [ended, setEnded] = useState<{ stepId: string; n: number; reduced: boolean } | null>(null)
  const [announce, setAnnounce] = useState('')
  const latest = useRef({ scenario, t, language: i18n.language })
  useEffect(() => {
    latest.current = { scenario, t, language: i18n.language }
  })

  useEffect(
    () =>
      onSessionEvent((ev) => {
        if (ev.t !== 'step' || ev.lessonId !== lessonId) return
        if (ev.status !== 'complete' && ev.status !== 'skipped') return
        const { scenario: sc, t: tr, language } = latest.current
        const step = sc?.steps.find((s) => s.id === ev.stepId)
        const goal = step ? lt(step.goal, language) : ev.stepId
        setAnnounce(
          tr(ev.status === 'complete' ? 'steps.completedAnnounce' : 'steps.skippedAnnounce', {
            goal,
          }),
        )
        setEnded((cur) => ({
          stepId: ev.stepId,
          n: (cur?.n ?? 0) + 1,
          reduced: prefersReducedMotion(),
        }))
      }),
    [lessonId],
  )

  // Highlight, then bring the next step into view.
  useEffect(() => {
    if (!ended) return
    const timer = setTimeout(
      () => {
        setEnded((cur) => (cur && cur.n === ended.n ? null : cur))
        pane.current
          ?.querySelector<HTMLElement>('li[aria-current="step"]')
          ?.scrollIntoView?.({ block: 'nearest', behavior: ended.reduced ? 'auto' : 'smooth' })
      },
      ended.reduced ? 0 : STEP_FLASH_MS,
    )
    return () => clearTimeout(timer)
  }, [ended, pane])

  return { flash: ended && !ended.reduced ? ended.stepId : null, announce }
}
