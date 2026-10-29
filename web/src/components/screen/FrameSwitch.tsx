import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import type { FrameKind } from '../../protocol.gen'
import { useMe } from '../../api/queries'
import { useSessionStore } from '../../store/session'
import { toast, useUiStore } from '../../store/ui'
import { SegmentedControl } from '../common/SegmentedControl'
import { switchFrame } from './frame'

/** Longest wait for the GUI frame before telling the learner (it normally takes under 3 s). */
const GUI_START_TIMEOUT_MS = 20000

/**
 * Terminal | GUI (frontend.md §8). While the GUI frame starts the terminal
 * stays visible with a "starting GUI frame" chip; the panes swap when the
 * mode event arrives.
 */
export function FrameSwitch() {
  const { t } = useTranslation()
  const mode = useSessionStore((s) => s.mode)
  const sessionId = useSessionStore((s) => s.sessionId)
  const guiStarting = useUiStore((s) => s.guiStarting)
  const me = useMe()
  const guiAvailable = me.data?.features?.gui !== false
  useEffect(() => {
    if (mode === 'gui' && guiStarting) useUiStore.getState().setGuiStarting(false)
  }, [mode, guiStarting])
  useEffect(() => {
    if (!guiStarting) return
    const timer = setTimeout(() => {
      if (useUiStore.getState().guiStarting) {
        useUiStore.getState().setGuiStarting(false)
        toast(t('frame.guiTimeout'), 'error')
      }
    }, GUI_START_TIMEOUT_MS)
    return () => clearTimeout(timer)
  }, [guiStarting, t])
  return (
    <SegmentedControl<FrameKind>
      label={t('frame.label')}
      value={mode}
      options={[
        {
          value: 'terminal',
          label: t('frame.terminal'),
          disabled: !sessionId,
          title: t('frame.terminalTitle'),
        },
        {
          value: 'gui',
          label: t('frame.gui'),
          disabled: !sessionId || !guiAvailable,
          title: guiAvailable ? t('frame.guiTitle') : t('frame.guiUnavailable'),
        },
      ]}
      onChange={(m) => void switchFrame(m, 'learner')}
    />
  )
}
