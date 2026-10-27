import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useEnsureSession } from '../../api/queries'
import { useSessionStore } from '../../store/session'
import { useUiStore } from '../../store/ui'
import { Button } from '../common/Button'
import { doRescue } from '../live/actions'
import styles from './Screen.module.css'

function Card({ title, body, children }: { title: string; body: string; children?: ReactNode }) {
  return (
    <div className={styles.overlay} role="alert" aria-label={title}>
      <div className={styles.overlayCard}>
        <strong>{title}</strong>
        <span>{body}</span>
        {children && <div className={styles.overlayActions}>{children}</div>}
      </div>
    </div>
  )
}

/**
 * Overlays over the pane (frontend.md §5): no session yet, attached
 * elsewhere, Emacs restarting, frame closed, suspended, stopping, Emacs
 * unavailable, modules reloading; plus the non-blocking busy banner and the
 * reconnecting chip. Driven by emacs events and connection state.
 */
export function ScreenOverlay({ onReattach }: { onReattach(): void }) {
  const { t } = useTranslation()
  const session = useEnsureSession()
  const sessionId = useSessionStore((s) => s.sessionId)
  const pty = useSessionStore((s) => s.connection.pty)
  const events = useSessionStore((s) => s.connection.events)
  const emacs = useSessionStore((s) => s.emacs)
  const modulesRestarting = useSessionStore((s) => s.modulesRestarting)
  const mode = useSessionStore((s) => s.mode)
  const skipping = useUiStore((s) => s.skipping)

  let card: ReactNode = null
  if (skipping) {
    // "Skip this step": Emacs plays the step; the learner watches
    // through a light veil and their keys wait (the terminal swallows them).
    card = (
      <div className={styles.skipOverlay} role="status" aria-label={t('stuck.skipping')}>
        <div className={styles.overlayCard}>
          <strong>{t('stuck.skipping')}</strong>
          <span>{t('stuck.skippingBody')}</span>
        </div>
      </div>
    )
  } else if (!sessionId) {
    card = session.isError ? (
      <Card
        title={t('overlay.sessionFailed')}
        body={session.error instanceof Error ? session.error.message : ''}
      >
        <Button variant="primary" onClick={() => void session.refetch()}>
          {t('common.retry')}
        </Button>
      </Card>
    ) : (
      <Card title={t('overlay.starting')} body={t('overlay.startingBody')} />
    )
  } else if (pty === 'replaced' && mode === 'terminal') {
    card = (
      <Card title={t('overlay.replaced')} body={t('overlay.replacedBody')}>
        <Button variant="primary" onClick={onReattach}>
          {t('overlay.reattach')}
        </Button>
      </Card>
    )
  } else if (emacs.overlay === 'restarting') {
    card = <Card title={t('overlay.restarting')} body={t('overlay.restartingBody')} />
  } else if (emacs.overlay === 'frame-closed') {
    card = <Card title={t('overlay.frameClosed')} body={t('overlay.frameClosedBody')} />
  } else if (emacs.overlay === 'suspended') {
    card = <Card title={t('overlay.suspended')} body={t('overlay.suspendedBody')} />
  } else if (emacs.overlay === 'stopping') {
    card = <Card title={t('overlay.stopping')} body={t('overlay.stoppingBody')} />
  } else if (modulesRestarting) {
    card = <Card title={t('overlay.modules')} body={t('overlay.modulesBody')} />
  } else if (pty === 'unavailable' && mode === 'terminal') {
    card = <Card title={t('overlay.unavailable')} body={t('overlay.unavailableBody')} />
  }

  const reconnecting =
    sessionId && (events === 'reconnecting' || (pty === 'reconnecting' && mode === 'terminal'))
  return (
    <>
      {emacs.busy && !card && (
        <div className={styles.banner} role="status">
          <span>
            <strong>{t('overlay.busy')}</strong> {t('overlay.busyBody')}
          </span>
          <Button size="small" variant="primary" onClick={() => void doRescue()}>
            {t('rescue.button')}
          </Button>
        </div>
      )}
      {reconnecting && !card && (
        <div className={styles.reconnecting} role="status">
          {t('overlay.reconnecting')}
        </div>
      )}
      {card}
    </>
  )
}
