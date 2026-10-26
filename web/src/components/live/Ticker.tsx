import { useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { missingGlosses, selectTicker } from '../../advice/ticker'
import { rpc } from '../../api/session/rpc'
import { useSessionStore } from '../../store/session'
import { Keycap } from './Keycap'
import styles from './Ticker.module.css'

const inflight = new Set<string>()

/** Fetches glosses for commands the gloss table lacks (rpc describe, cached per session). */
function useDescribe() {
  const ticker = useSessionStore((s) => s.ticker)
  const gloss = useSessionStore((s) => s.tables.gloss)
  const described = useSessionStore((s) => s.described)
  const sessionId = useSessionStore((s) => s.sessionId)
  useEffect(() => {
    if (!sessionId) return
    for (const e of missingGlosses(ticker, gloss, described)) {
      const cmd = e.cmd as string
      if (inflight.has(cmd)) continue
      inflight.add(cmd)
      rpc('describe', { keys: e.keys.join(' ') }, { silent: true })
        .then((r) => {
          const doc = r.cmd === cmd ? r.doc : ''
          useSessionStore.getState().setDescribed(cmd, doc)
        })
        .catch(() => useSessionStore.getState().setDescribed(cmd, ''))
        .finally(() => inflight.delete(cmd))
    }
  }, [ticker, gloss, described, sessionId])
}

/**
 * The key ticker (frontend.md §6): newest entry first, left-most and
 * emphasised; six visible. Announced politely to screen readers.
 */
export function Ticker() {
  const { t } = useTranslation()
  const ticker = useSessionStore((s) => s.ticker)
  const gloss = useSessionStore((s) => s.tables.gloss)
  const described = useSessionStore((s) => s.described)
  const views = useMemo(() => selectTicker(ticker, gloss, described), [ticker, gloss, described])
  useDescribe()
  return (
    <div
      className={styles.ticker}
      aria-live="polite"
      aria-relevant="additions"
      aria-label={t('ticker.label')}
    >
      {views.length === 0 && <div className={styles.empty}>{t('ticker.empty')}</div>}
      {views.map(({ entry, last, label, gloss: g }) =>
        entry.kind === 'gap' ? (
          <div key={entry.id} className={styles.gap} title={t('ticker.gap')}>
            <span className="sr-only">{t('ticker.gap')}</span>
          </div>
        ) : (
          <div
            key={entry.id}
            className={`${styles.tk} ${styles[entry.kind]} ${last ? styles.last : ''}`}
          >
            <div className={styles.keys}>
              <Keycap keys={entry.keys} last={last} />
              {entry.mx && <span className={styles.mx}>{t('ticker.typedName')}</span>}
            </div>
            <div className={styles.cmd}>
              {entry.kind === 'prefix' ? t('ticker.waiting') : label}
            </div>
            <div className={styles.gloss}>
              {entry.kind === 'prefix' ? t('ticker.prefixGloss') : g}
            </div>
          </div>
        ),
      )}
    </div>
  )
}
