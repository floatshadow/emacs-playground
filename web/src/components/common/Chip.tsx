import type { ReactNode } from 'react'
import styles from './Chip.module.css'

export function Chip({
  children,
  tone,
  title,
}: {
  children: ReactNode
  tone?: 'live' | 'warn' | 'err' | 'busy'
  title?: string
}) {
  return (
    <span className={`${styles.chip} ${tone ? styles[tone] : ''}`} title={title}>
      {children}
    </span>
  )
}
