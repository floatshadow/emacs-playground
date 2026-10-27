import { useEffect, useRef, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import styles from './Layout.module.css'

/**
 * The rail container. On wide screens it is a plain column; at 1440 px and below it
 * is hidden unless open, then slides in from the right (the demo's
 * .rail.open) with a close button; Escape closes it.
 */
export function Drawer({
  open,
  onClose,
  label,
  children,
}: {
  open: boolean
  onClose(): void
  label: string
  children: ReactNode
}) {
  const { t } = useTranslation()
  const ref = useRef<HTMLElement>(null)
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    ref.current?.focus()
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])
  return (
    <aside
      ref={ref}
      className={`${styles.drawer} ${open ? styles.open : ''}`}
      aria-label={label}
      tabIndex={-1}
    >
      <button
        type="button"
        className={styles.drawerClose}
        onClick={onClose}
        aria-label={t('rail.close')}
      >
        ×
      </button>
      {children}
    </aside>
  )
}
