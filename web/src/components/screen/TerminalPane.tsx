import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { getTerminalHost } from './hosts'
import styles from './Screen.module.css'

/**
 * The terminal frame (frontend.md §5): xterm.js over /pty, hosted outside
 * React so the socket survives navigation. This component only lends its
 * box to the persistent host.
 */
export default function TerminalPane({ hidden }: { hidden?: boolean }) {
  const ref = useRef<HTMLDivElement>(null)
  const { t } = useTranslation()
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const host = getTerminalHost()
    host.attach(el)
    return () => host.detach(el)
  }, [])
  return (
    <div
      ref={ref}
      className={styles.pane}
      hidden={hidden}
      role="application"
      aria-label={t('screen.terminalLabel')}
      aria-roledescription={t('screen.terminalRole')}
    />
  )
}
