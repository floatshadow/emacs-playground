import { useTranslation } from 'react-i18next'
import { describeDemo, parseDemo } from '../../lib/demo'
import type { StepView } from '../../store/selectors'
import { Button } from '../common/Button'
import { DemoPlayer } from './DemoPlayer'
import { doItYourself } from './demoPlayer'
import { Hint } from './Hint'
import { maxHintLevel } from './hintLevel'
import { Markdown } from './Markdown'
import styles from './Lesson.module.css'

function demoKeys(demo: string | undefined): string | null {
  if (!demo) return null
  try {
    return describeDemo(parseDemo(demo))
  } catch {
    return null
  }
}

const STATUS_LABEL: Record<StepView['status'], string> = {
  done: 'steps.statusDone',
  demonstrated: 'steps.statusDemonstrated',
  skipped: 'steps.statusSkipped',
  active: 'steps.statusActive',
  todo: 'steps.statusTodo',
}

/**
 * One step: the goal, details, ordered conditions (met, next, unmet,
 * unknown), the hint for the first unmet condition, Show a hint and Watch
 * it. A watched step offers "Do it yourself"; skipped and watched steps
 * carry their own marks (contracts b and c).
 */
export function Step({
  view,
  onHint,
  onGoTo,
  showDetails,
  flash,
}: {
  view: StepView
  onHint(view: StepView): void
  onGoTo?(view: StepView): void
  /** Author mode: show evaluator details next to conditions. */
  showDetails?: boolean
  /** The step just ended: highlighted for a moment (L1). */
  flash?: boolean
}) {
  const { t } = useTranslation()
  const active = view.status === 'active'
  const done = view.status === 'done'
  const cls = [
    styles.step,
    styles[view.status] ?? '',
    view.watched ? styles.watched : '',
    flash ? styles.justDone : '',
  ].join(' ')
  const maxLevel = maxHintLevel(view)
  return (
    <li className={cls} aria-current={active ? 'step' : undefined}>
      <span className={styles.mark} aria-hidden="true" />
      <div>
        <div className={styles.goal}>
          <span className="sr-only">{t(STATUS_LABEL[view.status])}</span>
          {view.goal}
          {view.status === 'skipped' && (
            <span className={styles.stateTag}> {t('steps.skippedTag')}</span>
          )}
          {view.status === 'demonstrated' && (
            <span className={styles.stateTag}> {t('steps.watchedTag')}</span>
          )}
        </div>
        {view.detail && (
          <div className={styles.detail}>
            <Markdown markdown={view.detail} inline />
          </div>
        )}
        <ul className={styles.conds}>
          {view.conds.map((c) => (
            <li key={c.id} className={styles[c.status]}>
              <span className="sr-only">{t('steps.cond.' + c.status)} </span>
              {c.text}
              {c.status === 'unknown' && (
                <span className={styles.unknownTag}> {t('steps.unknown')}</span>
              )}
              {showDetails && c.detail && <pre className={styles.condDetail}>{c.detail}</pre>}
            </li>
          ))}
        </ul>
        {view.hint && (
          <Hint
            forText={view.hint.forText}
            text={view.hint.text}
            level={view.hintLevel}
            keys={view.hintLevel >= 2 ? demoKeys(view.step.demo) : null}
          />
        )}
        {active && view.watched && (
          <p className={styles.watchedNote} role="status">
            {t('demo.watched')}
          </p>
        )}
        {active && (
          <div className={styles.actions}>
            {view.watched && (
              <Button
                variant="primary"
                size="small"
                onClick={() => void doItYourself()}
                title={t('demo.doItTitle')}
              >
                {t('demo.doItYourself')}
              </Button>
            )}
            {view.hintLevel < maxLevel && view.firstUnmet >= 0 && (
              <Button size="small" onClick={() => onHint(view)}>
                {view.hintLevel === 0 ? t('steps.showHint') : t('steps.showKeys')}
              </Button>
            )}
            {view.step.demo && (
              <DemoPlayer stepId={view.step.id} goal={view.goal} demo={view.step.demo} />
            )}
          </div>
        )}
        {!active && onGoTo && (
          <Button variant="quiet" size="small" className={styles.goto} onClick={() => onGoTo(view)}>
            {done ? t('steps.redo') : t('steps.goTo')}
          </Button>
        )}
      </div>
    </li>
  )
}
