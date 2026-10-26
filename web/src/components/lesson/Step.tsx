import { useTranslation } from 'react-i18next'
import { describeDemo, parseDemo } from '../../lib/demo'
import type { StepView } from '../../store/selectors'
import { Button } from '../common/Button'
import { DemoPlayer } from './DemoPlayer'
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

/**
 * One step: the goal, details, ordered conditions (met, next, unmet,
 * unknown), the hint for the first unmet condition, Show a hint and Watch it.
 */
export function Step({
  view,
  onHint,
  onGoTo,
  showDetails,
}: {
  view: StepView
  onHint(view: StepView): void
  onGoTo?(view: StepView): void
  /** Author mode: show evaluator details next to conditions. */
  showDetails?: boolean
}) {
  const { t } = useTranslation()
  const active = view.status === 'active'
  const done = view.status === 'done'
  const cls = [styles.step, done ? styles.done : '', active ? styles.active : ''].join(' ')
  const maxLevel = maxHintLevel(view)
  return (
    <li className={cls} aria-current={active ? 'step' : undefined}>
      <span className={styles.mark} aria-hidden="true" />
      <div>
        <div className={styles.goal}>
          <span className="sr-only">
            {done
              ? t('steps.statusDone')
              : active
                ? t('steps.statusActive')
                : t('steps.statusTodo')}
          </span>
          {view.goal}
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
        {active && (
          <div className={styles.actions}>
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
