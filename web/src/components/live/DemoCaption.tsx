import { useEffect, useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { captionItems } from '../../lib/demo'
import { useUiStore } from '../../store/ui'
import { Button } from '../common/Button'
import { Kbd } from '../common/Kbd'
import { cancelDemo } from '../lesson/demoPlayer'
import styles from './Ticker.module.css'

/**
 * The caption strip of a playing demo (frontend.md §7): in the
 * ticker's place and at its height, "Watching: <goal>", the keys sent so
 * far left to right with the current one highlighted, typed text growing
 * inside quotes, and Stop. The screen above does not change size.
 */
export function DemoCaption() {
  const { t } = useTranslation()
  const demo = useUiStore((s) => s.demo)
  const strip = useRef<HTMLDivElement>(null)
  const items = useMemo(() => (demo ? captionItems(demo.actions, demo.sent) : []), [demo])
  // Keep the newest key in view.
  useEffect(() => {
    const el = strip.current
    if (el) el.scrollLeft = el.scrollWidth
  }, [items])
  if (!demo) return null
  return (
    <div className={styles.caption} role="status" aria-label={t('demo.captionLabel')}>
      <div className={styles.captionHead}>
        <span className={styles.captionGoal}>{t('demo.watching', { goal: demo.goal })}</span>
        {demo.restored && <span className={styles.captionNote}>{t('demo.fromStart')}</span>}
        <Button size="small" onClick={() => cancelDemo()}>
          {t('demo.stop')}
        </Button>
      </div>
      <div ref={strip} className={styles.captionKeys} aria-label={t('demo.captionKeys')}>
        {items.length === 0 && <span className={styles.captionWait}>{t('demo.waitingFirst')}</span>}
        {items.map((it, i) => {
          const cur = i === items.length - 1
          return it.kind === 'keys' ? (
            <span key={i} className={cur ? styles.captionCurrent : styles.captionItem}>
              <Kbd keys={it.keys} size="sm" />
            </span>
          ) : (
            <span key={i} className={cur ? styles.captionCurrent : styles.captionItem}>
              <span className={styles.typed}>“{it.text}”</span>
            </span>
          )
        })}
      </div>
    </div>
  )
}
