import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSessionStore } from '../../store/session'
import { selectNudges } from '../../store/selectors'
import { KeyText } from '../common/Kbd'
import styles from './Nudges.module.css'

/** Re-evaluates time-based patterns (prompt open for 20 s, C-g bursts) every few seconds. */
function useNow(ms: number): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), ms)
    return () => clearInterval(t)
  }, [ms])
  return now
}

/**
 * "Did you mean" (frontend.md §6): habit cards, stuck patterns and server
 * nudges in one dismissible list. Dismissals last for the session.
 */
export function Nudges() {
  const { t } = useTranslation()
  const cards = useSessionStore((s) => s.cards)
  const serverNudges = useSessionStore((s) => s.serverNudges)
  const dismissed = useSessionStore((s) => s.dismissed)
  const ticker = useSessionStore((s) => s.ticker)
  const state = useSessionStore((s) => s.state)
  const minibufferSince = useSessionStore((s) => s.minibufferSince)
  const dismiss = useSessionStore((s) => s.dismiss)
  const tick = useNow(5000)
  // "Now" is the clock tick or the newest event, whichever is later (pure during render).
  const now = Math.max(tick, ticker[ticker.length - 1]?.at ?? 0, state?.ts ?? 0)
  const nudges = useMemo(
    () => selectNudges({ cards, serverNudges, dismissed, ticker, state, minibufferSince }, now),
    [cards, serverNudges, dismissed, ticker, state, minibufferSince, now],
  )
  if (nudges.length === 0) return <div className={styles.empty}>{t('nudges.empty')}</div>
  return (
    <div>
      {nudges.map((n) => (
        <div key={n.id} className={`${styles.nudge} ${styles[n.kind] ?? ''}`}>
          <button
            type="button"
            className={styles.x}
            aria-label={t('nudges.dismiss')}
            onClick={() => dismiss(n.id)}
          >
            ×
          </button>
          <h3>
            <KeyText text={n.title} size="sm" />
          </h3>
          <p>
            <KeyText text={n.body} size="sm" />
          </p>
        </div>
      ))}
    </div>
  )
}
