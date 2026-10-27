import type { ReactNode } from 'react'
import { useUiStore } from '../../store/ui'
import styles from './Layout.module.css'

/**
 * The demo's three-column work area: lesson or side pane, the stage, and the
 * rail. Collapses to two columns at 1440 px (the rail becomes a drawer, so
 * the terminal keeps its width; S5) and to one at 860 px (frontend.md §11).
 * In focus mode only the stage is shown (B14).
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
  const focus = useUiStore((s) => s.focusMode)
  return (
    <div className={`${styles.work} ${focus ? styles.focus : ''}`}>
      {left}
      {center}
      {right}
    </div>
  )
}
