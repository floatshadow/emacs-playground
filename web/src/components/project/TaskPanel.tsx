import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { LapStats, LessonBundle } from '../../protocol.gen'
import { useProgress } from '../../api/queries'
import { useSessionStore } from '../../store/session'
import { Markdown } from '../lesson/Markdown'
import { Steps } from '../lesson/Steps'
import type { LessonSteps, ScenarioLoad } from '../lesson/useLesson'
import { Button } from '../common/Button'
import { formatLap } from '../../lib/time'
import { startLap, stopLap } from './lap'
import { projectProgress, type ProjectProgress } from './progress'
import styles from './Project.module.css'
import lessonStyles from '../lesson/Lesson.module.css'

/** Ticks once a second while `on`: the lap timer. */
function useNow(on: boolean): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!on) return
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [on])
  return now
}

/** Tasks done, as a bar and a sentence; during a lap, the tasks done in this lap. */
function TaskProgress({ p }: { p: ProjectProgress }) {
  const { t } = useTranslation()
  const label = t('project.progress', { done: p.done, total: p.total })
  return (
    <div className={styles.progress}>
      <progress max={p.total} value={p.done} aria-label={label} />
      <span>
        {label}
        {p.skipped > 0 && ` · ${t('project.progressSkipped', { count: p.skipped })}`}
        {p.watched > 0 && ` · ${t('project.progressWatched', { count: p.watched })}`}
      </span>
    </div>
  )
}

/**
 * The "again, without hints" lap (lap.ts): offered once every task is
 * finished; while it runs, a timer against the project's target and Stop.
 */
function LapPanel({
  lessonId,
  finished: projectFinished,
  lapMinutes,
  stats,
}: {
  lessonId: string
  finished: boolean
  lapMinutes: number
  stats: LapStats | undefined
}) {
  const { t } = useTranslation()
  const lap = useSessionStore((s) => (s.lap?.lessonId === lessonId ? s.lap : null))
  const done = useSessionStore((s) => (s.lapDone?.lessonId === lessonId ? s.lapDone : null))
  const activeStep = useSessionStore((s) =>
    s.activeStep?.lessonId === lessonId ? s.activeStep.stepId : undefined,
  )
  const [busy, setBusy] = useState(false)
  const now = useNow(!!lap)
  const run = (f: () => Promise<void>) => {
    setBusy(true)
    void f().finally(() => setBusy(false))
  }
  const target = lapMinutes > 0 ? lapMinutes * 60_000 : 0

  if (lap) {
    const elapsed = Math.max(0, now - lap.startedAt)
    const over = target > 0 && elapsed > target
    return (
      <div className={`${styles.lap} ${styles.lapRunning}`} role="status">
        <p className={styles.lapClock}>
          <span className={over ? styles.lapOver : undefined} aria-live="off">
            {formatLap(elapsed)}
          </span>
          {target > 0 && <small> {t('project.lapTarget', { time: formatLap(target) })}</small>}
        </p>
        <p>{t('project.lapRunning')}</p>
        <Button
          size="small"
          variant="quiet"
          disabled={busy}
          title={t('project.lapStopTitle')}
          onClick={() => run(() => stopLap(lessonId, activeStep))}
        >
          {t('project.lapStop')}
        </Button>
      </div>
    )
  }
  if (!projectFinished) return null
  const finished = done?.ms ?? null
  return (
    <div className={styles.lap}>
      <h3>{t('project.lapTitle')}</h3>
      {finished !== null ? (
        <p role="status">
          {t('project.lapFinished', { time: formatLap(finished) })}
          {target > 0 &&
            ` ${finished <= target ? t('project.lapUnderTarget') : t('project.lapOverTarget', { time: formatLap(target) })}`}
        </p>
      ) : (
        <p>
          {t('project.lapIntro')}
          {target > 0 && ` ${t('project.lapTargetIntro', { minutes: lapMinutes })}`}
        </p>
      )}
      {stats && stats.count > 0 && (
        <p className={styles.lapStats}>
          {t('project.lapStats', {
            count: stats.count,
            best: formatLap(stats.bestMs),
            last: formatLap(stats.lastMs),
          })}
        </p>
      )}
      <Button
        size="small"
        variant="primary"
        disabled={busy}
        title={t('project.lapStartTitle')}
        onClick={() => run(() => startLap(lessonId))}
      >
        {finished !== null || (stats?.count ?? 0) > 0
          ? t('project.lapAgain')
          : t('project.lapStart')}
      </Button>
    </div>
  )
}

/**
 * The project's tasks (frontend.md §10): project tasks are the steps of the
 * project's scenario, so this is the step list with "Task N of M", the
 * progress, the "again, without hints" lap and the project brief folded
 * away.
 */
export function TaskPanel({
  bundle,
  steps,
  load,
}: {
  bundle: LessonBundle
  steps: LessonSteps
  load: ScenarioLoad
}) {
  const { t } = useTranslation()
  const lessonId = bundle.id
  const lapOn = useSessionStore((s) => s.lap?.lessonId === lessonId)
  const progress = useProgress()
  const saved = progress.data?.lessons?.[lessonId]
  const p = projectProgress(steps.views, steps.activeIndex, steps.complete, saved, lapOn)
  const n = steps.views.length
  const i = steps.activeIndex
  const active = i >= 0 ? steps.views[i] : undefined
  return (
    <div className={styles.task}>
      <h2>{active ? t('project.taskOf', { n: i + 1, total: n }) : t('project.tasks')}</h2>
      {active && <div className={styles.goal}>{active.goal}</div>}
      {n > 0 && <TaskProgress p={p} />}
      <LapPanel
        lessonId={lessonId}
        finished={p.finished}
        lapMinutes={bundle.scenario.project?.lapMinutes ?? 0}
        stats={saved?.laps}
      />
      {load.error && (
        <div className={lessonStyles.loadError} role="alert">
          <p>{t('lesson.loadFailed', { error: load.error })}</p>
          <Button size="small" onClick={load.retry}>
            {t('common.retry')}
          </Button>
        </div>
      )}
      {load.displaced && !load.loading && (
        <div className={lessonStyles.notice} role="status">
          <p>{t('lesson.displaced')}</p>
          <Button size="small" variant="primary" onClick={load.loadHere}>
            {t('lesson.loadHere')}
          </Button>
        </div>
      )}
      {load.loading && !load.error && <p className={lessonStyles.loading}>{t('lesson.loading')}</p>}
      <Steps
        views={steps.views}
        onHint={steps.onHint}
        // Another load ends the lap: no "Go to this task" while it runs.
        {...(lapOn ? { hintsOff: true } : { onGoTo: steps.onGoTo })}
        label={t('project.tasks')}
      />
      {p.finished && (
        <div className={lessonStyles.doneAll} role="status">
          {t('project.complete')}
        </div>
      )}
      {bundle.markdown.trim() && (
        <details className={styles.brief}>
          <summary>{t('project.brief')}</summary>
          <Markdown
            markdown={bundle.markdown}
            lessonId={bundle.id}
            className={lessonStyles.prose}
          />
        </details>
      )}
    </div>
  )
}
