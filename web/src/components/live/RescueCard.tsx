import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { APIPrefix } from '../../protocol.gen'
import { api } from '../../api/http'
import { useMe } from '../../api/queries'
import { useSessionStore } from '../../store/session'
import { toast } from '../../store/ui'
import { Button } from '../common/Button'
import { Dialog } from '../common/Dialog'
import { doRescue, doResetStep } from './actions'
import styles from './RescueCard.module.css'

/** Rescue, Reset this step, and "I'm stuck" (frontend.md §6, api.md §2.5). */
export function RescueCard() {
  const { t } = useTranslation()
  const me = useMe()
  const activeStep = useSessionStore((s) => s.activeStep)
  const sessionId = useSessionStore((s) => s.sessionId)
  const [stuckOpen, setStuckOpen] = useState(false)
  const [note, setNote] = useState('')
  const [sending, setSending] = useState(false)
  const stuckEnabled = me.data?.features?.stuckReports !== false

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
      toast(t('stuck.sent'))
      setStuckOpen(false)
      setNote('')
    } catch (e) {
      toast(t('stuck.failed', { error: e instanceof Error ? e.message : String(e) }), 'error')
    } finally {
      setSending(false)
    }
  }

  return (
    <div className={styles.card}>
      <p>{t('rescue.explain')}</p>
      <div className={styles.actions}>
        <Button variant="primary" onClick={() => void doRescue()}>
          {t('rescue.button')}
        </Button>
        <Button
          onClick={() => void doResetStep()}
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
      <Dialog
        open={stuckOpen}
        title={t('stuck.title')}
        onClose={() => setStuckOpen(false)}
        actions={
          <>
            <Button onClick={() => setStuckOpen(false)}>{t('common.cancel')}</Button>
            <Button variant="primary" onClick={() => void sendStuck()} disabled={sending}>
              {t('stuck.send')}
            </Button>
          </>
        }
      >
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
      </Dialog>
    </div>
  )
}
