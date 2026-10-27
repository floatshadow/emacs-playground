import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router'
import { useTranslation } from 'react-i18next'
import type { LessonBundle, LessonRef } from '../../protocol.gen'
import { useMe } from '../../api/queries'
import { useUiStore } from '../../store/ui'
import { buttonClass } from '../common/buttonClass'
import { Button } from '../common/Button'
import { LESSON_PANE_ID } from '../screen/paneIds'
import { Markdown } from './Markdown'
import { StepsSlotContext } from './stepsSlot'
import { Steps } from './Steps'
import { hasStepsMarker } from './directives'
import type { LessonSteps, ScenarioLoad } from './useLesson'
import styles from './Lesson.module.css'

/** Drops a leading "# Title" when it repeats the front-matter title. */
function stripTitle(markdown: string, title: string): string {
  const m = /^\s*#\s+(.+)\n/.exec(markdown)
  if (m && m[1]?.trim().toLowerCase() === title.trim().toLowerCase())
    return markdown.slice(m[0].length)
  return markdown
}

/**
 * While the current step is scrolled out of the lesson pane (a long lesson
 * puts its steps below the fold), a chip at the pane's bottom edge names it
 * and scrolls to it, so "what do I do now" is always one click away.
 */
function CurrentStepJump({
  pane,
  goal,
  stepId,
}: {
  pane: React.RefObject<HTMLElement | null>
  goal: string
  stepId: string
}) {
  const { t } = useTranslation()
  const reducedMotion = useUiStore((s) => s.reducedMotion)
  // Where the step with this id is, as last reported by the observer; a new
  // current step starts as "visible" until the observer says otherwise.
  const [seen, setSeen] = useState<{ stepId: string; where: 'above' | 'below' | null }>({
    stepId,
    where: null,
  })
  useEffect(() => {
    const root = pane.current
    const li = root?.querySelector<HTMLElement>('li[aria-current="step"]')
    if (!root || !li || typeof IntersectionObserver === 'undefined') return
    const io = new IntersectionObserver(
      ([e]) => {
        if (!e) return
        const where = e.isIntersecting
          ? null
          : e.boundingClientRect.top < (e.rootBounds?.top ?? 0)
            ? 'above'
            : 'below'
        setSeen({ stepId, where })
      },
      { root, threshold: 0.25 },
    )
    io.observe(li)
    return () => io.disconnect()
  }, [pane, stepId])
  const where = seen.stepId === stepId ? seen.where : null
  if (!where) return null
  const jump = () =>
    pane.current
      ?.querySelector<HTMLElement>('li[aria-current="step"]')
      ?.scrollIntoView({ block: 'center', behavior: reducedMotion ? 'auto' : 'smooth' })
  return (
    <div className={styles.jumpWrap}>
      <button type="button" className={styles.jump} onClick={jump}>
        <span className={styles.jumpLabel}>{t('steps.statusActive')}</span> {goal}{' '}
        <span aria-hidden="true">{where === 'below' ? '↓' : '↑'}</span>
      </button>
    </div>
  )
}

/**
 * The lesson pane (frontend.md §7): title, lede, the Markdown with
 * directives, the step list (at ::steps or at the end), and the
 * lesson-complete box with the next lesson.
 */
export function LessonPane({
  bundle,
  steps,
  load,
  next,
}: {
  bundle: LessonBundle
  steps: LessonSteps
  load: ScenarioLoad
  next: LessonRef | null
}) {
  const { t } = useTranslation()
  const me = useMe()
  const author = !!me.data?.features?.authorMode
  const title = bundle.front?.title || bundle.title
  const markdown = useMemo(() => stripTitle(bundle.markdown, title), [bundle.markdown, title])
  const inline = hasStepsMarker(markdown)
  const pane = useRef<HTMLElement>(null)
  const current = steps.views.find((v) => v.status === 'active')
  const list = (
    <>
      {load.error && (
        <div className={styles.loadError} role="alert">
          <p>{t('lesson.loadFailed', { error: load.error })}</p>
          <Button size="small" onClick={load.retry}>
            {t('common.retry')}
          </Button>
        </div>
      )}
      {load.loading && !load.error && <p className={styles.loading}>{t('lesson.loading')}</p>}
      <Steps views={steps.views} onHint={steps.onHint} onGoTo={steps.onGoTo} showDetails={author} />
      {steps.complete && (
        <div className={styles.doneAll} role="status">
          {t('lesson.complete')}{' '}
          {next ? (
            <>
              {t('lesson.next')}{' '}
              <Link className={buttonClass('primary', 'small')} to={`/lesson/${next.id}`}>
                {next.n} {next.title}
              </Link>
            </>
          ) : (
            <Link to="/">{t('lesson.backToMap')}</Link>
          )}
        </div>
      )}
    </>
  )
  return (
    <aside
      ref={pane}
      className={styles.lesson}
      id={LESSON_PANE_ID}
      tabIndex={-1}
      aria-label={t('lesson.paneLabel')}
    >
      <h1>{title}</h1>
      {bundle.front?.summary && <p className={styles.lede}>{bundle.front.summary}</p>}
      <StepsSlotContext.Provider value={inline ? list : null}>
        <Markdown markdown={markdown} lessonId={bundle.id} className={styles.prose} />
      </StepsSlotContext.Provider>
      {!inline && list}
      {current && <CurrentStepJump pane={pane} goal={current.goal} stepId={current.step.id} />}
    </aside>
  )
}
