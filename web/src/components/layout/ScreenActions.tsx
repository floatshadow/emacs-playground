import { useTranslation } from 'react-i18next'
import { useMe } from '../../api/queries'
import { useUiStore } from '../../store/ui'
import { Button } from '../common/Button'
import { doRescue } from '../live/actions'
import { FrameSwitch } from '../screen/FrameSwitch'
import { toggleFocusMode } from '../screen/focusMode'
import { SessionChip } from './TopBar'
import styles from './Layout.module.css'

/** The top-bar actions of the Emacs screens: session chip, frame switch, focus, rail, rescue. */
export function ScreenActions({ preview }: { preview?: boolean }) {
  const { t } = useTranslation()
  const me = useMe()
  const focusMode = useUiStore((s) => s.focusMode)
  const railOpen = useUiStore((s) => s.railOpen)
  const setRailOpen = useUiStore((s) => s.setRailOpen)
  const previewHidden = useUiStore((s) => s.previewHidden)
  const togglePreview = useUiStore((s) => s.togglePreview)
  return (
    <>
      <span className={styles.wideOnly}>
        <SessionChip />
      </span>
      {me.data?.features?.gui !== false && (
        <span className={styles.wideOnly}>
          <FrameSwitch />
        </span>
      )}
      {preview && (
        <Button className={styles.wideOnly} onClick={togglePreview} aria-pressed={!previewHidden}>
          {previewHidden ? t('topbar.showPreview') : t('topbar.hidePreview')}
        </Button>
      )}
      <Button
        className={styles.wideOnly}
        onClick={() => void toggleFocusMode()}
        title={t('topbar.focusTitle')}
        aria-pressed={focusMode}
        aria-describedby="focus-mode-desc"
      >
        {t('topbar.focus')}
      </Button>
      <span id="focus-mode-desc" className="sr-only">
        {t('topbar.focusTitle')}
      </span>
      <Button
        className={styles.railToggle}
        onClick={() => setRailOpen(!railOpen)}
        aria-expanded={railOpen}
      >
        {t('topbar.whereAmI')}
      </Button>
      <Button variant="primary" onClick={() => void doRescue()} title={t('rescue.explain')}>
        {t('rescue.button')}
      </Button>
    </>
  )
}
