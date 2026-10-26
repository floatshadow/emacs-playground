import { useTranslation } from 'react-i18next'
import { useSessionStore } from '../../store/session'
import { selectWhereAmI } from '../../store/selectors'

/**
 * The terminal is a canvas, so Where am I and the echo line are mirrored
 * into a visually hidden live region (frontend.md §13).
 */
export function LiveAnnouncer() {
  const { t } = useTranslation()
  const where = useSessionStore(selectWhereAmI)
  const echo = useSessionStore((s) => s.echo)
  const exit = where?.exits[0]
  return (
    <div className="sr-only" aria-live="polite" aria-atomic="true">
      {echo?.text && <p>{t('a11y.echo', { text: echo.text })}</p>}
      {where && (
        <p>
          {t('a11y.where', { buffer: where.buffer, mode: where.modeName })}
          {exit ? ` ${exit.why} ${exit.how}` : ''}
        </p>
      )}
    </div>
  )
}
