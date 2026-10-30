import { useEffect } from 'react'
import { Navigate, useLocation, useSearchParams } from 'react-router'
import { useTranslation } from 'react-i18next'
import { APIPrefix, type LoginError } from '../protocol.gen'
import { useAuthConfig, useMe } from '../api/queries'
import { buttonClass } from '../components/common/buttonClass'
import { AppShell } from '../components/layout/AppShell'
import { TopBar } from '../components/layout/TopBar'
import { safeNext } from '../lib/auth'
import { errorMessage } from '../lib/errors'
import styles from './Screens.module.css'

const LOGIN_ERRORS: readonly LoginError[] = ['state', 'provider', 'token', 'forbidden']

/**
 * /login (frontend.md §2): one "Sign in with …" button that starts the OIDC
 * flow, returning to the page the learner came from, and the error the
 * callback redirected here with (?error=state|provider|token|forbidden).
 */
export default function LoginScreen() {
  const { t } = useTranslation()
  const location = useLocation()
  const [params] = useSearchParams()
  const cfg = useAuthConfig()
  const me = useMe()

  useEffect(() => {
    document.title = `${t('login.title')} — Emacs Playground`
  }, [t])

  if (cfg.data?.authMode === 'none' || me.data) return <Navigate to="/" replace />

  const err = params.get('error')
  const loginError =
    err === null ? null : LOGIN_ERRORS.includes(err as LoginError) ? err : 'unknown'
  const next = safeNext((location.state as { from?: unknown } | null)?.from)
  const start = `${cfg.data?.oidcStartUrl ?? `${APIPrefix}/auth/oidc/start`}?next=${encodeURIComponent(next)}`

  return (
    <AppShell scroll topbar={<TopBar crumbs={[{ label: t('login.title') }]} />}>
      <main className={styles.login}>
        <h1>{t('login.heading')}</h1>
        <p className={styles.dim}>{t('login.lede')}</p>
        {loginError && (
          <p className={styles.error} role="alert">
            {t(`login.error.${loginError}`)}
          </p>
        )}
        {cfg.isError ? (
          <p className={styles.error} role="alert">
            {errorMessage(cfg.error)}
          </p>
        ) : cfg.data ? (
          <a className={`${buttonClass('primary')} ${styles.loginButton}`} href={start}>
            {t('login.oidc', { name: cfg.data.oidcDisplayName || 'SSO' })}
          </a>
        ) : (
          <p className={styles.dim} aria-busy="true">
            {t('common.loading')}
          </p>
        )}
      </main>
    </AppShell>
  )
}
