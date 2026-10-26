import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { CheckpointInfo } from '../../protocol.gen'
import { useCheckpoints } from '../../api/queries'
import { rpc } from '../../api/session/rpc'
import { clock } from '../../lib/time'
import { toast } from '../../store/ui'
import { Button } from '../common/Button'
import { ConfirmDialog, Dialog } from '../common/Dialog'
import styles from './Project.module.css'

/**
 * Checkpoints (frontend.md §10): rpc('checkpoints'), Restore after a
 * confirmation that names what will be overwritten, and manual checkpoints.
 */
export function Checkpoints() {
  const { t } = useTranslation()
  const q = useCheckpoints()
  const [confirm, setConfirm] = useState<CheckpointInfo | null>(null)
  const [busy, setBusy] = useState(false)
  const [saveOpen, setSaveOpen] = useState(false)
  const [label, setLabel] = useState('')

  const restore = async (c: CheckpointInfo) => {
    setBusy(true)
    try {
      const r = await rpc('restore', { label: c.label })
      toast(t('checkpoints.restored', { label: c.label, count: r.restoredFiles ?? 0 }))
      setConfirm(null)
    } catch {
      // toasted
    } finally {
      setBusy(false)
    }
  }
  const save = async () => {
    const l = label.trim()
    if (!l) return
    setBusy(true)
    try {
      await rpc('checkpoint', { label: l })
      toast(t('checkpoints.saved', { label: l }))
      setSaveOpen(false)
      setLabel('')
    } catch {
      // toasted
    } finally {
      setBusy(false)
    }
  }

  const items = q.data?.items ?? []
  return (
    <div>
      {q.isLoading && <p className={styles.treeNote}>{t('common.loading')}</p>}
      {!q.isLoading && items.length === 0 && (
        <p className={styles.treeNote}>{t('checkpoints.none')}</p>
      )}
      <ul className={styles.checkpoints}>
        {items.map((c) => (
          <li key={c.ref}>
            <span>
              {c.label}
              <span className={styles.when}>
                {clock(c.createdAt)} · {t('checkpoints.files', { count: c.files?.length ?? 0 })}
              </span>
            </span>
            <Button size="small" onClick={() => setConfirm(c)}>
              {t('checkpoints.restore')}
            </Button>
          </li>
        ))}
      </ul>
      <Button size="small" variant="quiet" onClick={() => setSaveOpen(true)}>
        {t('checkpoints.save')}
      </Button>
      <ConfirmDialog
        open={!!confirm}
        title={t('checkpoints.confirmTitle', { label: confirm?.label ?? '' })}
        confirmLabel={t('checkpoints.restore')}
        busy={busy}
        onClose={() => setConfirm(null)}
        onConfirm={() => confirm && void restore(confirm)}
      >
        {confirm && confirm.files.length > 0 ? (
          <>
            <p>{t('checkpoints.willOverwrite', { count: confirm.files.length })}</p>
            <ul>
              {confirm.files.map((f) => (
                <li key={f}>{f}</li>
              ))}
            </ul>
          </>
        ) : (
          <p>{t('checkpoints.nothingChanged')}</p>
        )}
        <p>{t('checkpoints.keeps')}</p>
      </ConfirmDialog>
      <Dialog
        open={saveOpen}
        title={t('checkpoints.saveTitle')}
        onClose={() => setSaveOpen(false)}
        actions={
          <>
            <Button onClick={() => setSaveOpen(false)}>{t('common.cancel')}</Button>
            <Button variant="primary" onClick={() => void save()} disabled={busy || !label.trim()}>
              {t('checkpoints.saveButton')}
            </Button>
          </>
        }
      >
        <label className={styles.label} htmlFor="cp-label">
          {t('checkpoints.labelLabel')}
        </label>
        <input
          id="cp-label"
          className={styles.input}
          value={label}
          onChange={(e) => setLabel(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') void save()
          }}
          data-autofocus
        />
      </Dialog>
    </div>
  )
}
