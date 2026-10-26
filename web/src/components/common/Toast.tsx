import { useTranslation } from 'react-i18next'
import { useUiStore } from '../../store/ui'
import styles from './Toast.module.css'

/** Toasts at the bottom centre (the demo's .toast), announced politely. */
export function Toasts() {
  const toasts = useUiStore((s) => s.toasts)
  const dismiss = useUiStore((s) => s.dismissToast)
  const { t } = useTranslation()
  return (
    <div className={styles.region} role="status" aria-live="polite">
      {toasts.map((x) => (
        <div key={x.id} className={`${styles.toast} ${x.tone === 'error' ? styles.error : ''}`}>
          <span>{x.text}</span>
          <button
            type="button"
            className={styles.x}
            aria-label={t('common.dismiss')}
            onClick={() => dismiss(x.id)}
          >
            ×
          </button>
        </div>
      ))}
    </div>
  )
}
