import { useEffect, useRef, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from './Button'
import styles from './Dialog.module.css'

/**
 * A modal dialog over the native <dialog> element: showModal() makes the
 * rest of the page inert, so focus stays inside (frontend.md §13); Escape
 * closes it and focus returns to the opener.
 */
export function Dialog({
  open,
  title,
  children,
  onClose,
  actions,
}: {
  open: boolean
  title: string
  children: ReactNode
  onClose(): void
  actions?: ReactNode
}) {
  const ref = useRef<HTMLDialogElement>(null)
  const opener = useRef<Element | null>(null)
  useEffect(() => {
    const d = ref.current
    if (!d) return
    if (open && !d.open) {
      opener.current = document.activeElement
      if (typeof d.showModal === 'function') d.showModal()
      else d.setAttribute('open', '')
      const first = d.querySelector<HTMLElement>('[data-autofocus], input, button')
      first?.focus()
    } else if (!open && d.open) {
      if (typeof d.close === 'function') d.close()
      else d.removeAttribute('open')
      if (opener.current instanceof HTMLElement) opener.current.focus()
    }
  }, [open])
  return (
    <dialog
      ref={ref}
      className={styles.dialog}
      aria-labelledby="dialog-title"
      onCancel={(e) => {
        e.preventDefault()
        onClose()
      }}
    >
      {open && (
        <div className={styles.card}>
          <h2 id="dialog-title" className={styles.title}>
            {title}
          </h2>
          <div className={styles.body}>{children}</div>
          <div className={styles.actions}>{actions}</div>
        </div>
      )}
    </dialog>
  )
}

/** A yes/no confirmation built on Dialog. */
export function ConfirmDialog({
  open,
  title,
  children,
  confirmLabel,
  onConfirm,
  onClose,
  busy,
}: {
  open: boolean
  title: string
  children: ReactNode
  confirmLabel: string
  onConfirm(): void
  onClose(): void
  busy?: boolean
}) {
  const { t } = useTranslation()
  return (
    <Dialog
      open={open}
      title={title}
      onClose={onClose}
      actions={
        <>
          <Button onClick={onClose}>{t('common.cancel')}</Button>
          <Button variant="primary" onClick={onConfirm} disabled={busy} data-autofocus>
            {confirmLabel}
          </Button>
        </>
      }
    >
      {children}
    </Dialog>
  )
}
