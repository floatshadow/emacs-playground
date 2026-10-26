import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ago } from '../../lib/time'
import { useSessionStore } from '../../store/session'
import { baseName } from './kinds'
import styles from './Preview.module.css'

/**
 * The last file event for the artefact and the time since (frontend.md §9).
 * When the artefact follows a source that changed more recently, the
 * artefact is being rebuilt: the dot pulses until the artefact changes.
 */
export function PreviewStatus({ path, source }: { path: string; source?: string | undefined }) {
  const { t } = useTranslation()
  const mark = useSessionStore((s) => s.files[path])
  const src = useSessionStore((s) => (source ? s.files[source] : undefined))
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 5000)
    return () => clearInterval(timer)
  }, [])
  const building = !!src && (!mark || src.ts > mark.ts) && now - src.ts < 60_000
  let text: string
  if (building && source)
    text = t('preview.building', { source: baseName(source), name: baseName(path) })
  else if (mark?.event === 'deleted') text = t('preview.deleted', { name: baseName(path) })
  else if (mark) text = t('preview.updated', { name: baseName(path), when: ago(mark.ts, now) })
  else text = t('preview.watching', { name: baseName(path) })
  return (
    <div
      className={`${styles.status} ${building ? styles.building : ''}`}
      role="status"
      aria-live="polite"
    >
      <span className={styles.dot} aria-hidden="true" />
      <span>{text}</span>
    </div>
  )
}
