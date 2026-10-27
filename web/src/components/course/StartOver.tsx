import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { resetProgress } from '../../api/queries'
import { toast } from '../../store/ui'
import { Button } from '../common/Button'
import { ConfirmDialog } from '../common/Dialog'

/**
 * "Start over" (fix plan contract a, B5): clears the progress of every
 * lesson after a confirmation. Files, checkpoints and the modules stay.
 * On the course map next to Continue, and on the Session page.
 */
export function StartOver({ variant }: { variant?: 'primary' | 'quiet' }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const confirm = async () => {
    setBusy(true)
    try {
      await resetProgress(qc)
      toast(t('startOver.done'))
      setOpen(false)
    } catch (e) {
      toast(t('startOver.failed', { error: e instanceof Error ? e.message : String(e) }), 'error')
    } finally {
      setBusy(false)
    }
  }
  return (
    <>
      <Button {...(variant ? { variant } : {})} onClick={() => setOpen(true)}>
        {t('startOver.button')}
      </Button>
      <ConfirmDialog
        open={open}
        title={t('startOver.title')}
        confirmLabel={t('startOver.button')}
        busy={busy}
        onClose={() => setOpen(false)}
        onConfirm={() => void confirm()}
      >
        <p>{t('startOver.body')}</p>
      </ConfirmDialog>
    </>
  )
}
