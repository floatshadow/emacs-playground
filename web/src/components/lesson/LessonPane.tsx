import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import type { LessonBundle, LessonRef } from '../../protocol.gen'
import { resetProgress, useMe, useProgress } from '../../api/queries'
import { useSessionStore } from '../../store/session'
import { toast, useUiStore } from '../../store/ui'
import { useStepTransition } from './stepTransition'
import { buttonClass } from '../common/buttonClass'
import { Button } from '../common/Button'
import { SegmentedControl } from '../common/SegmentedControl'
import { LESSON_PANE_ID } from '../screen/paneIds'
import { LessonNotices } from './LessonNotices'
import { Markdown } from './Markdown'
import { StepsSlotContext } from './stepsSlot'
import { Steps } from './Steps'
import { hasStepsMarker } from './directives'
import type { LessonSteps, ScenarioLoad } from './useLesson'
import styles from './Lesson.module.css'
import { errorMessage } from '../../lib/errors'

/** Drops a leading "# Title" when it repeats the front-matter title. */
function stripTitle(markdown: string, title: string): string {
  const m = /^\s*#\s+(.+)\n/.exec(markdown)
  if (m && m[1]?.trim().toLowerCase() === title.trim().toLowerCase())
    return markdown.slice(m[0].length)
  return markdown
}

export type LessonTab = 'read' | 'do'

/**
 * The lesson pane (frontend.md §7): title, lede and two views behind a
 * switch. Read is the Markdown with directives (::steps is a link to Do);
 * Do is the step list, the active step expanded, and the lesson-complete
 * box with the next lesson. The view is the learner's choice per lesson
 * (Read until the lesson is started); no event switches it: a step that
 * ends while they read pulses the Do label and is announced.
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
  const { flash, announce } = useStepTransition(bundle.id, bundle.scenario, pane)
  // Started: past the first step, or an outcome live (the snapshot's) or saved.
  const progress = useProgress()
  const live = useSessionStore(
    (s) => s.lesson?.lessonId === bundle.id && Object.keys(s.lesson.outcomes).length > 0,
  )
  const saved = Object.values(progress.data?.lessons?.[bundle.id]?.steps ?? {}).map((x) => x.status)
  const started =
    steps.activeIndex > 0 ||
    live ||
    saved.some((x) => x === 'done' || x === 'skipped' || x === 'demonstrated')
  const chosen = useUiStore((s) => s.lessonTab[bundle.id])
  const tab = chosen ?? (started ? 'do' : 'read')
  const setTab = (v: LessonTab) => useUiStore.getState().setLessonTab(bundle.id, v)
  // The default is decided once the snapshot and progress are in, then kept.
  const ready = useSessionStore((s) => !!s.hello) && !progress.isLoading
  useEffect(() => {
    if (!chosen && ready && steps.views.length > 0) setTab(tab)
  })
  const n = steps.views.length
  const at =
    steps.activeIndex >= 0
      ? steps.activeIndex + 1
      : steps.views.filter((v) => v.status === 'done' || v.status === 'skipped').length
  const qc = useQueryClient()
  const [restarting, setRestarting] = useState(false)
  const startLessonOver = async () => {
    setRestarting(true)
    try {
      await resetProgress(qc, bundle.id)
      load.loadHere(true)
      toast(t('lesson.startLessonOverDone'))
    } catch (e) {
      const msg = errorMessage(e)
      toast(t('startOver.failed', { error: msg }), 'error')
    } finally {
      setRestarting(false)
    }
  }
  const toDo = (
    <p>
      <Button size="small" onClick={() => setTab('do')}>
        {t('lesson.toDo', { n: at, count: n })}
      </Button>
    </p>
  )
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
      <Steps
        views={steps.views}
        onHint={steps.onHint}
        onGoTo={steps.onGoTo}
        showDetails={author}
        flash={flash}
        collapse={!author}
      />
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
          )}{' '}
          <Button
            variant="quiet"
            size="small"
            disabled={restarting}
            onClick={() => void startLessonOver()}
          >
            {t('lesson.startLessonOver')}
          </Button>
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
      <div className={`${styles.tabs} ${flash && tab === 'read' ? styles.pulse : ''}`}>
        <SegmentedControl
          label={t('lesson.view')}
          value={tab}
          onChange={setTab}
          options={[
            { value: 'read', label: t('lesson.read') },
            { value: 'do', label: t('lesson.do', { n: at, count: n }) },
          ]}
        />
      </div>
      <div className="sr-only" role="status" aria-live="polite">
        {announce}
      </div>
      <LessonNotices scenario={bundle.scenario} />
      {load.displaced && !load.loading && (
        <div className={styles.notice} role="status">
          <p>{t('lesson.displaced')}</p>
          <Button size="small" variant="primary" onClick={() => load.loadHere()}>
            {t('lesson.loadHere')}
          </Button>
        </div>
      )}
      {tab === 'do' ? (
        list
      ) : (
        <StepsSlotContext.Provider value={inline ? toDo : null}>
          <Markdown markdown={markdown} lessonId={bundle.id} className={styles.prose} />
          {!inline && toDo}
        </StepsSlotContext.Provider>
      )}
    </aside>
  )
}
