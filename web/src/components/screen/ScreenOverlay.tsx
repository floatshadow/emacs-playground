import { type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { APIPrefix } from '../../protocol.gen'
import { api } from '../../api/http'
import { useEnsureSession, useLesson, useMe } from '../../api/queries'
import { useSessionStore } from '../../store/session'
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
 * The "Going to step N" veil of a load of a later step sessiond runs (one
 * this page started shows the same veil from useUiStore.stepJump): the
 * snapshot's busy names it, and the snapshot that clears it ends it.
 */
function useServerStepJump(): { n: number; goal: string } | null {
  const locale = useUiStore((s) => s.locale)
  const busy = useSessionStore((s) => s.lesson?.busy)
  const load = busy?.op === 'load' ? busy : null
  const lesson = useLesson(load?.lessonId)
  const steps = lesson.data?.scenario.steps ?? []
  const i = load ? steps.findIndex((s) => s.id === load.stepId) : -1
  const step = steps[i]
  return step && i > 0 ? { n: i + 1, goal: lt(step.goal, locale) } : null
}

/** Stops the session (the version gate) and reloads, which starts a current one. */
async function stopAndReload(sessionId: string | null) {
  await api.del(`${APIPrefix}/sessions/${encodeURIComponent(sessionId ?? '')}`).catch(() => {})
  window.location.reload()
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
  const incompatible = useSessionStore((s) => s.incompatible)
  const local = useMe().data?.mode !== 'server'
  const mode = useSessionStore((s) => s.mode)
  const skipping = useUiStore((s) => s.skipping)
  const serverJump = useServerStepJump()
  const stepJump = useUiStore((s) => s.stepJump) ?? serverJump

  let card: ReactNode = null
  if (incompatible) {
    // The session speaks another protocol (an older image): nothing
    // automatic runs. A local stack is shared by every page: restart it.
    card = (
      <Card title={t('overlay.incompatible')} body={t('overlay.incompatibleBody')}>
        {local ? (
          <span>{t('overlay.restartStack')}</span>
        ) : (
          <Button variant="primary" onClick={() => void stopAndReload(sessionId)}>
            {t('overlay.stop')}
          </Button>
        )}
      </Card>
    )
  } else if (skipping) {
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
    card = /module/i.test(emacs.reason ?? '') ? (
      <Card title={t('overlay.modules')} body={t('overlay.modulesBody')} />
    ) : (
      <Card title={t('overlay.restarting')} body={t('overlay.restartingBody')} />
    )
  } else if (emacs.overlay === 'frame-closed') {
    card = <Card title={t('overlay.frameClosed')} body={t('overlay.frameClosedBody')} />
  } else if (emacs.overlay === 'suspended') {
    card = <Card title={t('overlay.suspended')} body={t('overlay.suspendedBody')} />
  } else if (emacs.overlay === 'stopping') {
    card = <Card title={t('overlay.stopping')} body={t('overlay.stoppingBody')} />
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
