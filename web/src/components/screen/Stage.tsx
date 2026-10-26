import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSessionStore } from '../../store/session'
import { Ticker } from '../live/Ticker'
import { getTerminalHost } from './hosts'
import { LiveAnnouncer } from './LiveAnnouncer'
import { ScreenHeader } from './ScreenHeader'
import { ScreenOverlay } from './ScreenOverlay'
import { SCREEN_WRAP_ID } from './focusMode'
import TerminalPane from './TerminalPane'
import VncPane from './VncPane'
import styles from './Screen.module.css'

/**
 * The middle column: screen header, the Emacs screen (terminal and GUI
 * panes, both mounted once used, one visible), overlays, and the ticker.
 */
export function Stage() {
  const { t } = useTranslation()
  const mode = useSessionStore((s) => s.mode)
  // The GUI pane mounts the first time GUI mode is used and then stays mounted.
  const [vncMounted, setVncMounted] = useState(mode === 'gui')
  if (mode === 'gui' && !vncMounted) setVncMounted(true)
  return (
    <main className={styles.stage} aria-label={t('screen.stageLabel')}>
      <ScreenHeader />
      <div id={SCREEN_WRAP_ID} className={styles.wrap}>
        <div className={styles.screen}>
          <TerminalPane hidden={mode === 'gui'} />
          {vncMounted && <VncPane hidden={mode !== 'gui'} />}
          <ScreenOverlay onReattach={() => getTerminalHost().reattach()} />
        </div>
      </div>
      <Ticker />
      <LiveAnnouncer />
    </main>
  )
}
