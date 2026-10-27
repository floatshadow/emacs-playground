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
  const [flash, setFlash] = useState<string | null>(null)
  const [announce, setAnnounce] = useState('')
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)
  useEffect(() => {
    const off = onSessionEvent((ev) => {
      if (ev.t !== 'step' || ev.lessonId !== lessonId) return
      if (ev.status !== 'complete' && ev.status !== 'skipped') return
      const step = scenario?.steps.find((s) => s.id === ev.stepId)
      const goal = step ? lt(step.goal, i18n.language) : ev.stepId
      setAnnounce(
        t(ev.status === 'complete' ? 'steps.completedAnnounce' : 'steps.skippedAnnounce', {
          goal,
        }),
      )
      const reduced = prefersReducedMotion()
      if (timer.current) clearTimeout(timer.current)
      if (!reduced) setFlash(ev.stepId)
      timer.current = setTimeout(
        () => {
          timer.current = null
          setFlash(null)
          pane.current
            ?.querySelector<HTMLElement>('li[aria-current="step"]')
            ?.scrollIntoView?.({ block: 'nearest', behavior: reduced ? 'auto' : 'smooth' })
        },
        reduced ? 0 : STEP_FLASH_MS,
      )
    })
    return () => {
      off()
      if (timer.current) clearTimeout(timer.current)
    }
  }, [lessonId, scenario, pane, t, i18n.language])
  return { flash, announce }
}
