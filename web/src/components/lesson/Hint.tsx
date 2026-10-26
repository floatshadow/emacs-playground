import { useTranslation } from 'react-i18next'
import { KeyText } from '../common/Kbd'
import styles from './Lesson.module.css'

/**
 * The hint box. Level 1 is the hint for the first unmet condition; level 2
 * adds the exact keys from the step's demo.
 */
export function Hint({
  forText,
  text,
  level,
  keys,
}: {
  forText: string
  text: string
  level: number
  keys?: string | null
}) {
  const { t } = useTranslation()
  return (
    <div className={styles.hint} role="note">
      <div className={styles.lvl}>{t('steps.hintFor', { text: forText })}</div>
      <KeyText text={text} size="sm" />
      {level >= 2 && keys && (
        <div className={styles.keysHint}>
          <div className={styles.lvl}>{t('steps.exactKeys')}</div>
          <code className={styles.demoKeys}>{keys}</code>
        </div>
      )}
    </div>
  )
}
