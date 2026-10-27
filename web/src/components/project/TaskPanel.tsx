import { useTranslation } from 'react-i18next'
import type { LessonBundle } from '../../protocol.gen'
import { Markdown } from '../lesson/Markdown'
import { Steps } from '../lesson/Steps'
import type { LessonSteps, ScenarioLoad } from '../lesson/useLesson'
import { Button } from '../common/Button'
import styles from './Project.module.css'
import lessonStyles from '../lesson/Lesson.module.css'

/**
 * The project's tasks (frontend.md §10): project tasks are the steps of the
 * project's scenario, so this is the step list with "Task N of M" and the
 * project brief folded away.
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
  const n = steps.views.length
  const i = steps.activeIndex
  const active = i >= 0 ? steps.views[i] : undefined
  return (
    <div className={styles.task}>
      <h2>{active ? t('project.taskOf', { n: i + 1, total: n }) : t('project.tasks')}</h2>
      {active && <div className={styles.goal}>{active.goal}</div>}
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
        onGoTo={steps.onGoTo}
        label={t('project.tasks')}
      />
      {steps.complete && (
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
