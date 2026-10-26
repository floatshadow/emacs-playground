import type { ReactNode } from 'react'
import styles from './Layout.module.css'

/**
 * The demo's three-column work area: lesson or side pane, the stage, and the
 * rail. Collapses to two columns at 1180 px (the rail becomes a drawer) and
 * to one at 860 px (frontend.md §11).
 */
export function Workspace({
  left,
  center,
  right,
}: {
  left: ReactNode
  center: ReactNode
  right: ReactNode
}) {
  return (
    <div className={styles.work}>
      {left}
      {center}
      {right}
    </div>
  )
}
