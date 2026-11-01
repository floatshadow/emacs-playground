import { useEffect, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useEnsureSession, useLesson, useStatus } from '../../api/queries'
import { serverStepJump, useSessionStore } from '../../store/session'
import { useUiStore } from '../../store/ui'
import { sessionStartError } from '../../lib/sessionError'
import { Button } from '../common/Button'
import { doRescue } from '../live/actions'
import { lt } from '../../lib/localtext'
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
 * The "Going to step N" veil of a load sessiond runs that this page did not
 * start (it was reloaded meanwhile): hello's busy names it, status polls
 * until it ends, and the step event that ends it clears it sooner.
 */
function useServerStepJump(): { n: number; goal: string } | null {
  const sessionId = useSessionStore((s) => s.sessionId)
  const locale = useUiStore((s) => s.locale)
  const load = useSessionStore(serverStepJump)
  const lesson = useLesson(load?.lessonId)
  const status = useStatus(load ? sessionId : null, () => true)
  // Only a status fetched after hello named the op tells it ended.
  const since = useSessionStore((s) => s.serverBusySince)
  useEffect(() => {
    if (load && status.data && status.dataUpdatedAt > since)
      useSessionStore.getState().setServerBusy(status.data.busy ?? null)
  }, [load, since, status.data, status.dataUpdatedAt])
  const steps = lesson.data?.scenario.steps ?? []
  const i = load ? steps.findIndex((s) => s.id === load.stepId) : -1
  const step = steps[i]
  return step ? { n: i + 1, goal: lt(step.goal, locale) } : null
}

/**
 * Overlays over the pane (frontend.md §5): no session yet or it could not
 * start (a full group quota says so), attached elsewhere, Emacs restarting,
 * frame closed, suspended, stopping, Emacs unavailable, modules reloading;
 * plus the non-blocking busy banner and the reconnecting chip. Driven by
 * emacs events and connection state.
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
  const serverJump = useServerStepJump()
  const stepJump = useUiStore((s) => s.stepJump) ?? serverJump

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
  } else if (stepJump) {
    card = (
      <div className={styles.skipOverlay} role="status" aria-label={t('steps.jumping', stepJump)}>
        <div className={styles.overlayCard}>
          <strong>{t('steps.jumping', stepJump)}</strong>
          <span>{t('steps.jumpingBody')}</span>
        </div>
      </div>
    )
  } else if (!sessionId) {
    card = session.isError ? (
      <Card {...sessionStartError(session.error, t)}>
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
