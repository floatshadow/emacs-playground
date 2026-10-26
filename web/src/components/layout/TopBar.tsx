import { Fragment, type ReactNode } from 'react'
import { Link } from 'react-router'
import { useTranslation } from 'react-i18next'
import { useMe } from '../../api/queries'
import { useSessionStore } from '../../store/session'
import { Chip } from '../common/Chip'
import styles from './Layout.module.css'

export interface Crumb {
  label: string
  to?: string
}

/** Session chip: "local session · terminal frame", or the connection trouble. */
export function SessionChip() {
  const { t } = useTranslation()
  const me = useMe()
  const sessionId = useSessionStore((s) => s.sessionId)
  const events = useSessionStore((s) => s.connection.events)
  const mode = useSessionStore((s) => s.mode)
  const busy = useSessionStore((s) => s.emacs.busy)
  const where = me.data?.mode === 'server' ? t('topbar.serverSession') : t('topbar.localSession')
  if (!sessionId) return <Chip>{t('topbar.noSession')}</Chip>
  if (events === 'reconnecting' || events === 'connecting')
    return <Chip tone="busy">{t('topbar.connecting', { where })}</Chip>
  if (busy) return <Chip tone="warn">{t('topbar.busy')}</Chip>
  return (
    <Link to="/session" className={styles.chipLink} title={t('topbar.sessionTitle')}>
      <Chip tone="live">
        {where} · {mode === 'gui' ? t('frame.guiFrame') : t('frame.terminalFrame')}
      </Chip>
    </Link>
  )
}

/** The demo's top bar: brand, breadcrumbs, then screen-specific actions on the right. */
export function TopBar({ crumbs, children }: { crumbs: Crumb[]; children?: ReactNode }) {
  const { t } = useTranslation()
  return (
    <header className={styles.topbar}>
      <Link className={styles.brand} to="/">
        <i aria-hidden="true">E</i>
        {t('app.title')}
      </Link>
      <nav className={styles.crumbs} aria-label={t('topbar.breadcrumbs')}>
        {crumbs.map((c, i) => (
          <Fragment key={i}>
            {i > 0 && <span aria-hidden="true">›</span>}
            {c.to ? <Link to={c.to}>{c.label}</Link> : <span aria-current="page">{c.label}</span>}
          </Fragment>
        ))}
      </nav>
      <div className={styles.spacer} />
      <div className={styles.actions}>{children}</div>
    </header>
  )
}
