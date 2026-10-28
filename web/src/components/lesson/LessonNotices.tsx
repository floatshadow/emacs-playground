import { useTranslation } from 'react-i18next'
import type { Scenario } from '../../protocol.gen'
import { useMe, useStatus } from '../../api/queries'
import { useSessionStore } from '../../store/session'
import { useUiStore } from '../../store/ui'
import { Button } from '../common/Button'
import { switchFrame } from '../screen/frame'
import { usesBox, wantsGui } from './lessonTags'
import styles from './Lesson.module.css'

/**
 * What the GUI frame is (frontend.md §8): GUI lessons open it after
 * loading; this says what it is, offers to open it when the learner went
 * back to the terminal, and says so when the server has it turned off.
 */
function GuiNotice() {
  const { t } = useTranslation()
  const me = useMe()
  const mode = useSessionStore((s) => s.mode)
  const starting = useUiStore((s) => s.guiStarting)
  if (me.data && !me.data.features.gui) {
    return (
      <div className={`${styles.notice} ${styles.noticeWarn}`}>
        <p>{t('notices.guiOff')}</p>
      </div>
    )
  }
  if (mode === 'gui') {
    return (
      <div className={styles.notice}>
        <p>{t('notices.guiOn')}</p>
      </div>
    )
  }
  return (
    <div className={styles.notice}>
      <p>{t('notices.guiNeeded')}</p>
      <Button
        size="small"
        variant="primary"
        disabled={starting}
        onClick={() => void switchFrame('gui')}
      >
        {starting ? t('notices.guiStarting') : t('notices.guiOpen')}
      </Button>
    </div>
  )
}

/**
 * The SSH box's status for remote lessons (deployment.md §6): available on
 * this server, and whether the `box` alias is set up yet (status
 * remoteBox), polled until it is.
 */
function BoxNotice() {
  const { t } = useTranslation()
  const me = useMe()
  const sessionId = useSessionStore((s) => s.sessionId)
  const off = !!me.data && !me.data.features.remoteBox
  // Polled while the box is being set up; a sessiond that does not report it is not polled.
  const status = useStatus(off ? null : sessionId, (s) => s?.remoteBox === false)
  const ready = status.data?.remoteBox
  let text: string
  let warn = false
  if (off) {
    text = t('notices.boxOff')
    warn = true
  } else if (ready === true) text = t('notices.boxReady')
  else if (ready === false) {
    text = t('notices.boxNotReady')
    warn = true
  } else text = status.data ? t('notices.boxUnknown') : t('notices.boxChecking')
  return (
    <div className={`${styles.notice} ${warn ? styles.noticeWarn : ''}`} role="status">
      <p>
        <span
          className={`${styles.boxDot} ${ready === true ? styles.boxDotOn : ''}`}
          aria-hidden="true"
        />
        {text}
      </p>
    </div>
  )
}

/** Notices for GUI and remote lessons and projects, above the lesson text. */
export function LessonNotices({ scenario }: { scenario: Scenario | undefined }) {
  const gui = wantsGui(scenario)
  const box = usesBox(scenario)
  if (!gui && !box) return null
  return (
    <>
      {gui && <GuiNotice />}
      {box && <BoxNotice />}
    </>
  )
}
