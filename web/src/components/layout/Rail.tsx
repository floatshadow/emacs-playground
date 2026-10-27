import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useUiStore } from '../../store/ui'
import { Drawer } from './Drawer'
import styles from './Layout.module.css'

/** A titled rail section (italic serif heading, as in the demo). */
export function RailSection({
  title,
  small,
  children,
}: {
  title: string
  small?: ReactNode
  children: ReactNode
}) {
  return (
    <section className={styles.section}>
      <h2>
        {title}
        {small ? <small>{small}</small> : null}
      </h2>
      {children}
    </section>
  )
}

/**
 * The right-hand rail. At 1440 px and below it is hidden and opens as a drawer from
 * the "Where am I" button. With a preview it splits: preview on top, live
 * panels below.
 */
export function Rail({ children, top }: { children: ReactNode; top?: ReactNode }) {
  const { t } = useTranslation()
  const open = useUiStore((s) => s.railOpen)
  const setOpen = useUiStore((s) => s.setRailOpen)
  return (
    <Drawer open={open} onClose={() => setOpen(false)} label={t('rail.label')}>
      {top ? (
        <div className={`${styles.rail} ${styles.split}`}>
          {top}
          <div className={styles.railBottom}>{children}</div>
        </div>
      ) : (
        <div className={styles.rail}>{children}</div>
      )}
    </Drawer>
  )
}
