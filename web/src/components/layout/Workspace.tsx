import type { CSSProperties, ReactNode } from 'react'
import { useUiStore } from '../../store/ui'
import { Splitter } from './Splitter'
import styles from './Layout.module.css'

/**
 * The work area: lesson or side pane, a splitter, the stage and the rail.
 * Collapses to two columns at 1440 px (the rail becomes a drawer, so the
 * terminal keeps its width) and to one at 860 px (frontend.md §11). The
 * pane's width is the learner's lessonWidth, else a default the grid
 * computes. In focus mode only the stage (class "stage") is shown.
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
  const width = useUiStore((s) => s.lessonWidth)
  const style = width ? ({ '--lesson-width': `${width}px` } as CSSProperties) : undefined
  return (
    <div className={`${styles.work} ${focus ? styles.focus : ''}`} style={style}>
      {left}
      <Splitter />
      {center}
      {right}
    </div>
  )
}
