import { useTranslation } from 'react-i18next'
import { useSessionStore } from '../../store/session'
import { selectWhereAmI } from '../../store/selectors'
import { KeyText } from '../common/Kbd'
import styles from './WhereAmI.module.css'

/**
 * "Where am I" (frontend.md §6): the current buffer, mode, windows and
 * prompt, flags, and one exit per matching row of tables.exits.
 */
export function WhereAmI() {
  const { t } = useTranslation()
  const view = useSessionStore(selectWhereAmI)
  if (!view) return <p className={styles.waiting}>{t('where.waiting')}</p>
  return (
    <div>
      <dl className={styles.where}>
        <dt>{t('where.buffer')}</dt>
        <dd>
          {view.buffer}
          {view.modified && <em> {t('where.unsaved')}</em>}
        </dd>
        <dt>{t('where.mode')}</dt>
        <dd>
          {view.modeName || view.mode}
          {view.minors.length > 0 && <span className={styles.dim}> + {view.minors.join(' ')}</span>}
        </dd>
        <dt>{t('where.windows')}</dt>
        <dd>
          {view.windows === 1 ? t('where.oneWindow') : t('where.manyWindows', { n: view.windows })}
        </dd>
        <dt>{t('where.prompt')}</dt>
        <dd>
          {view.prompt ? (
            <>
              {view.prompt} <em>{t('where.open')}</em>
            </>
          ) : (
            t('where.none')
          )}
        </dd>
      </dl>
      {view.flags.length > 0 && (
        <div className={styles.flags}>
          {view.flags.map((f) => (
            <span key={f} className={styles.flag}>
              {f}
            </span>
          ))}
        </div>
      )}
      {view.exits.length > 0 ? (
        <div>
          {view.exits.map((e) => (
            <div key={e.id} className={styles.exit}>
              <div className={styles.why}>{e.why}</div>
              <div className={styles.how}>
                <KeyText text={e.how} size="sm" />
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div className={styles.calm}>{t('where.calm')}</div>
      )}
    </div>
  )
}
