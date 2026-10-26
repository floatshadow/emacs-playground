import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { useSessionStore } from '../../store/session'
import { getVncHost } from './hosts'
import styles from './Screen.module.css'

/**
 * The GUI frame (frontend.md §8): noVNC over /vnc, loaded on first use. Stays
 * mounted once shown so switching frames back and forth is immediate.
 */
export default function VncPane({ hidden }: { hidden?: boolean }) {
  const ref = useRef<HTMLDivElement>(null)
  const { t } = useTranslation()
  const mode = useSessionStore((s) => s.mode)
  const sessionId = useSessionStore((s) => s.sessionId)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const host = getVncHost()
    host.attach(el)
    return () => host.detach(el)
  }, [])
  useEffect(() => {
    if (mode === 'gui' && sessionId) getVncHost().ensure()
  }, [mode, sessionId])
  return (
    <div
      ref={ref}
      className={`${styles.pane} ${styles.vnc}`}
      hidden={hidden}
      role="application"
      aria-label={t('screen.guiLabel')}
    />
  )
}
