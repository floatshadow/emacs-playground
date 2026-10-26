import { useTranslation } from 'react-i18next'
import { CheatsCommon } from '../../protocol.gen'
import { useSessionStore } from '../../store/session'
import { Kbd } from '../common/Kbd'
import styles from './CheatSheet.module.css'

/** Renders "n / p" or "C-x C-f" as caps, keeping the slash between alternatives. */
function CheatKeys({ keys }: { keys: string }) {
  const alts = keys.split(' / ')
  return (
    <>
      {alts.map((a, i) => (
        <span key={i}>
          {i > 0 && ' / '}
          <Kbd keys={a} split size="sm" />
        </span>
      ))}
    </>
  )
}

/** Keys for this buffer (frontend.md §6): rows for the major mode, then the common rows. */
export function CheatSheet({ heading }: { heading?: boolean }) {
  const { t } = useTranslation()
  const mode = useSessionStore((s) => s.state?.mode ?? '')
  const modeName = useSessionStore((s) => s.state?.modeName ?? '')
  const cheats = useSessionStore((s) => s.tables.cheats)
  const rows = [...(cheats[mode] ?? []), ...(cheats[CheatsCommon] ?? [])]
  return (
    <section aria-labelledby="cheat-h">
      {heading !== false && (
        <h2 id="cheat-h">
          {t('cheat.title')} {modeName && <small>{modeName}</small>}
        </h2>
      )}
      <table className={styles.cheat}>
        <tbody>
          {rows.map(([k, d], i) => (
            <tr key={k + i}>
              <td>
                <CheatKeys keys={k} />
              </td>
              <td>{d}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}
