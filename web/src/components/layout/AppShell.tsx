import type { ReactNode } from 'react'
import { Toasts } from '../common/Toast'
import styles from './Layout.module.css'

/** The page frame: top bar, then the screen's content filling the viewport. */
export function AppShell({
  topbar,
  children,
  scroll,
}: {
  topbar: ReactNode
  children: ReactNode
  scroll?: boolean
}) {
  return (
    <div className={`${styles.app} ${scroll ? styles.scroll : ''}`}>
      <a className={styles.skip} href="#main-content">
        Skip to content
      </a>
      {topbar}
      <div id="main-content" className={styles.content}>
        {children}
      </div>
      <Toasts />
    </div>
  )
}
