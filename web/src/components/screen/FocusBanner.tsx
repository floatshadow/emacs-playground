import { useTranslation } from 'react-i18next'
import { useUiStore } from '../../store/ui'
import { Button } from '../common/Button'
import { exitFocusMode } from './focusMode'
import styles from './Screen.module.css'

/**
 * The focus-mode banner (B14): one line over the screen saying what focus
 * mode does, whether the keyboard lock is on, and how to leave it.
 */
export function FocusBanner() {
  const { t } = useTranslation()
  const on = useUiStore((s) => s.focusMode)
  const lock = useUiStore((s) => s.focusLock)
  if (!on) return null
  const lockText =
    lock === 'trying' ? t('focus.trying') : lock === 'on' ? t('focus.lockOn') : t('focus.lockOff')
  return (
    <div className={styles.focusBanner} role="status">
      <span className={styles.focusText}>
        <strong>{t('focus.banner')}</strong> {lockText}
      </span>
      <Button size="small" onClick={() => void exitFocusMode()}>
        {t('focus.leave')}
      </Button>
      <span className={styles.focusHint}>{t('focus.leaveHint')}</span>
    </div>
  )
}
