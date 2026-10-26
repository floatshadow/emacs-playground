import { useTranslation } from 'react-i18next'
import type { StepView } from '../../store/selectors'
import { Step } from './Step'
import styles from './Lesson.module.css'

/** The step list (frontend.md §7), rendered from the scenario and the latest check. */
export function Steps({
  views,
  onHint,
  onGoTo,
  showDetails,
  label,
}: {
  views: StepView[]
  onHint(view: StepView): void
  onGoTo?(view: StepView): void
  showDetails?: boolean
  label?: string
}) {
  const { t } = useTranslation()
  return (
    <ol className={styles.steps} aria-label={label ?? t('steps.label')}>
      {views.map((v) => (
        <Step
          key={v.step.id}
          view={v}
          onHint={onHint}
          {...(onGoTo ? { onGoTo } : {})}
          {...(showDetails ? { showDetails } : {})}
        />
      ))}
    </ol>
  )
}
