import type { KeyboardEvent, PointerEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useUiStore } from '../../store/ui'
import styles from './Layout.module.css'

/** One arrow key moves the separator this far (px). */
const STEP = 24

/** The width the pane before the separator has now (CSS clamps the preference). */
const paneWidth = (sep: HTMLElement) =>
  Math.round(sep.previousElementSibling?.getBoundingClientRect().width ?? 0)

/**
 * The separator between the side pane and the stage (frontend.md §11): drag
 * it, or focus it and press ←/→ (24 px) or Home (the default width). The
 * width is the lessonWidth preference, applied by Workspace as
 * --lesson-width; the grid clamps it, and the terminal refits itself.
 */
export function Splitter() {
  const { t } = useTranslation()
  const width = useUiStore((s) => s.lessonWidth)
  const setWidth = (lessonWidth: number | null) => useUiStore.getState().setPrefs({ lessonWidth })

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    const sep = e.currentTarget
    const from = paneWidth(sep) - e.clientX
    sep.setPointerCapture(e.pointerId)
    const move = (ev: globalThis.PointerEvent) => setWidth(from + ev.clientX)
    // The capture ends after pointerup, pointercancel or a lost capture.
    const end = () => {
      sep.removeEventListener('pointermove', move)
      sep.removeEventListener('lostpointercapture', end)
    }
    sep.addEventListener('pointermove', move)
    sep.addEventListener('lostpointercapture', end)
    e.preventDefault()
  }

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const step = { ArrowLeft: -STEP, ArrowRight: STEP }[e.key]
    if (e.key === 'Home') setWidth(null)
    else if (step) setWidth(paneWidth(e.currentTarget) + step)
    else return
    e.preventDefault()
  }

  return (
    // A focusable separator is interactive (the WAI-ARIA window splitter).
    // eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={t('layout.resize')}
      aria-valuenow={width ?? undefined}
      // eslint-disable-next-line jsx-a11y/no-noninteractive-tabindex
      tabIndex={0}
      className={styles.splitter}
      onPointerDown={onPointerDown}
      onKeyDown={onKeyDown}
    />
  )
}
