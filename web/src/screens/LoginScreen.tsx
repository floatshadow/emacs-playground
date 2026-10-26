import { useEffect, useState, type FormEvent } from 'react'
import { Navigate, useNavigate } from 'react-router'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { APIPrefix } from '../protocol.gen'
import { ApiError, api } from '../api/http'
import { qk, useAuthConfig } from '../api/queries'
import { Button } from '../components/common/Button'
import { buttonClass } from '../components/common/buttonClass'
import { AppShell } from '../components/layout/AppShell'
import { TopBar } from '../components/layout/TopBar'
import styles from './Screens.module.css'

/** /login (frontend.md §2): an invite code form, or the OIDC button (501 in slice 1). */
export default function LoginScreen() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const cfg = useAuthConfig()
  const [code, setCode] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    document.title = `${t('login.title')} — Emacs Playground`
  }, [t])

  if (cfg.data?.authMode === 'none') return <Navigate to="/" replace />

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await api.post(`${APIPrefix}/auth/invite`, { code: code.trim() })
      await qc.invalidateQueries({ queryKey: qk.me })
      void navigate('/', { replace: true })
    } catch (err) {
      setError(
        err instanceof ApiError && (err.status === 403 || err.status === 400)
          ? t('login.badCode')
          : err instanceof Error
            ? err.message
            : String(err),
      )
    } finally {
      setBusy(false)
    }
  }

  return (
    <AppShell scroll topbar={<TopBar crumbs={[{ label: t('login.title') }]} />}>
      <main className={styles.login}>
        <h1>{t('login.heading')}</h1>
        <p className={styles.dim}>{t('login.lede')}</p>
        {cfg.data?.authMode === 'oidc' ? (
          <>
            {/* TODO(gateway, backend.md §2.6): OIDC answers 501 in slice 1. */}
            <a
              className={buttonClass('primary')}
              href={cfg.data.oidcStartUrl ?? `${APIPrefix}/auth/oidc/start`}
            >
              {t('login.oidc')}
            </a>
          </>
        ) : (
          <form onSubmit={(e) => void submit(e)}>
            <label htmlFor="invite">{t('login.codeLabel')}</label>
            <input
              id="invite"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              autoComplete="one-time-code"
              autoCapitalize="none"
              spellCheck={false}
              required
            />
            {error && (
              <p className={styles.error} role="alert">
                {error}
              </p>
            )}
            <Button type="submit" variant="primary" disabled={busy || !code.trim()}>
              {t('login.submit')}
            </Button>
          </form>
        )}
      </main>
    </AppShell>
  )
}
