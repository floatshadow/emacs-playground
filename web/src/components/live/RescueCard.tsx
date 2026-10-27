import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { APIPrefix } from '../../protocol.gen'
import { api } from '../../api/http'
import { useLesson, useMe } from '../../api/queries'
import { lt } from '../../lib/localtext'
import { useSessionStore } from '../../store/session'
import { toast } from '../../store/ui'
import { Button } from '../common/Button'
import { ConfirmDialog, Dialog } from '../common/Dialog'
import { doRescue, doResetStep, doSkipStep } from './actions'
import styles from './RescueCard.module.css'
import { errorMessage } from '../../lib/errors'

/**
 * Rescue, Reset this step, and "I'm stuck" (frontend.md §6, api.md §2.5).
 * Rescue never touches files; Reset this step does, so it has its own line
 * and asks first. After a stuck report the dialog offers "Skip this
 * step": Emacs does the step for the learner.
 */
export function RescueCard() {
  const { t, i18n } = useTranslation()
  const me = useMe()
  const activeStep = useSessionStore((s) => s.activeStep)
  const sessionId = useSessionStore((s) => s.sessionId)
  const lesson = useLesson(activeStep?.lessonId)
  const [stuckOpen, setStuckOpen] = useState(false)
  const [sent, setSent] = useState(false)
  const [confirmReset, setConfirmReset] = useState(false)
  const [note, setNote] = useState('')
  const [sending, setSending] = useState(false)
  const stuckEnabled = me.data?.features?.stuckReports !== false
  const step = lesson.data?.scenario?.steps.find((s) => s.id === activeStep?.stepId)
  const goal = step ? lt(step.goal, i18n.language) : (activeStep?.stepId ?? '')

  const closeStuck = () => {
    setStuckOpen(false)
    setSent(false)
  }

  const sendStuck = async () => {
    if (!sessionId) return
    setSending(true)
    try {
      await api.post(`${APIPrefix}/stuck`, {
        sessionId,
        lessonId: activeStep?.lessonId,
        stepId: activeStep?.stepId,
        note,
      })
      setNote('')
      if (activeStep) setSent(true)
      else {
        toast(t('stuck.sent'))
        closeStuck()
      }
    } catch (e) {
      toast(t('stuck.failed', { error: errorMessage(e) }), 'error')
    } finally {
      setSending(false)
    }
  }

  const skip = () => {
    const st = activeStep
    closeStuck()
    if (st) void doSkipStep(st.stepId, goal)
  }

  return (
    <div className={styles.card}>
      <p>{t('rescue.explain')}</p>
      <div className={styles.actions}>
        <Button variant="primary" onClick={() => void doRescue()}>
          {t('rescue.button')}
        </Button>
      </div>
      <p className={styles.resetExplain}>{t('rescue.resetExplain')}</p>
      <div className={styles.actions}>
        <Button
          onClick={() => setConfirmReset(true)}
          disabled={!activeStep}
          title={t('rescue.resetTitle')}
        >
          {t('rescue.reset')}
        </Button>
      </div>
      {stuckEnabled && (
        <Button
          variant="quiet"
          size="small"
          className={styles.stuck}
          onClick={() => setStuckOpen(true)}
        >
          {t('stuck.button')}
        </Button>
      )}
      <ConfirmDialog
        open={confirmReset}
        title={t('rescue.resetConfirmTitle')}
        confirmLabel={t('rescue.resetConfirm')}
        onClose={() => setConfirmReset(false)}
        onConfirm={() => {
          setConfirmReset(false)
          void doResetStep()
        }}
      >
        <p>{t('rescue.resetConfirmBody')}</p>
      </ConfirmDialog>
      <Dialog
        open={stuckOpen}
        title={t('stuck.title')}
        onClose={closeStuck}
        actions={
          sent ? (
            <>
              <Button onClick={closeStuck}>{t('stuck.close')}</Button>
              <Button variant="primary" onClick={skip} disabled={!activeStep} data-autofocus>
                {t('stuck.skip')}
              </Button>
            </>
          ) : (
            <>
              <Button onClick={closeStuck}>{t('common.cancel')}</Button>
              <Button variant="primary" onClick={() => void sendStuck()} disabled={sending}>
                {t('stuck.send')}
              </Button>
            </>
          )
        }
      >
        {sent ? (
          <p role="status">{t('stuck.sentBody')}</p>
        ) : (
          <>
            <p>{t('stuck.explain')}</p>
            <label className={styles.label} htmlFor="stuck-note">
              {t('stuck.noteLabel')}
            </label>
            <textarea
              id="stuck-note"
              className={styles.note}
              rows={4}
              value={note}
              onChange={(e) => setNote(e.target.value)}
              data-autofocus
            />
          </>
        )}
      </Dialog>
    </div>
  )
}
