import { Suspense, useEffect, type ReactNode } from 'react'
import { Navigate, Outlet, useLocation } from 'react-router'
import { useTranslation } from 'react-i18next'
import { withDefaultTables } from '../../advice/defaults'
import { ApiError } from '../../api/http'
import { useCurriculum, useMe, useProgressInvalidation } from '../../api/queries'
import { useSessionStore } from '../../store/session'
import { useUiStore } from '../../store/ui'
import { Toasts } from '../common/Toast'

function Fallback() {
  const { t } = useTranslation()
  return (
    <p style={{ padding: 28, color: 'var(--ink-3)', fontStyle: 'italic' }} aria-busy="true">
      {t('common.loading')}
    </p>
  )
}

export function Lazy({ children }: { children: ReactNode }) {
  return <Suspense fallback={<Fallback />}>{children}</Suspense>
}

/**
 * The root layout: sends unauthenticated visitors to /login, feeds the
 * advice tables from the curriculum index into the session store, keeps
 * progress fresh on step events, and applies the motion preference.
 */
export function RootLayout() {
  const location = useLocation()
  const me = useMe()
  const curriculum = useCurriculum()
  const reducedMotion = useUiStore((s) => s.reducedMotion)
  useProgressInvalidation()

  useEffect(() => {
    if (curriculum.data)
      useSessionStore.getState().setTables(withDefaultTables(curriculum.data.tables))
  }, [curriculum.data])

  useEffect(() => {
    document.documentElement.classList.toggle('reduce-motion', reducedMotion)
  }, [reducedMotion])

  const unauthenticated = me.error instanceof ApiError && me.error.status === 401
  if (unauthenticated && location.pathname !== '/login') {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
  }
  return <Outlet />
}

export function NotFound() {
  const { t } = useTranslation()
  return (
    <main style={{ maxWidth: 640, margin: '0 auto', padding: '48px 28px' }}>
      <h1>{t('notFound.title')}</h1>
      <p>
        <a href="/">{t('notFound.back')}</a>
      </p>
      <Toasts />
    </main>
  )
}
