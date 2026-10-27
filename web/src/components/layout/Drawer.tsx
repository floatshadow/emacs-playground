import { useEffect, useRef, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import styles from './Layout.module.css'

/**
 * The rail container. On wide screens it is a plain column; at 1440 px and below it
 * is hidden unless open, then slides in from the right (the demo's
 * .rail.open) with a close button; Escape closes it, unless a dialog is
 * open (Escape is the dialog's). Closing it returns focus to the element
 * that had it when it opened (the "Where am I" button).
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
  const close = useRef(onClose)
  useEffect(() => {
    close.current = onClose
  }, [onClose])
  useEffect(() => {
    if (!open) return
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || e.defaultPrevented) return
      if (document.querySelector('dialog[open]')) return
      close.current()
    }
    const el = ref.current
    window.addEventListener('keydown', onKey)
    el?.focus()
    return () => {
      window.removeEventListener('keydown', onKey)
      // Focus inside the closing drawer (or already lost) goes back to the opener.
      const a = document.activeElement
      const lost = !a || a === document.body || !!el?.contains(a)
      if (lost && opener?.isConnected) opener.focus()
    }
  }, [open])
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
