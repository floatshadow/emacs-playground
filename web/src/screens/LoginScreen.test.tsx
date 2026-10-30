import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient } from '@tanstack/react-query'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import type { AuthConfigInfo, CurriculumIndex, Me } from '../protocol.gen'
import { DEFAULT_TABLES } from '../advice/defaults'
import { logout } from '../api/queries'
import { disconnectSession } from '../api/session/connection'
import { safeNext } from '../lib/auth'
import { useSessionStore } from '../store/session'
import indexJson from '../../dev/fixtures/index.json'
import { App } from '../app'

const index = { ...(indexJson as unknown as CurriculumIndex), tables: DEFAULT_TABLES }
const oidcConfig: AuthConfigInfo = {
  mode: 'server',
  authMode: 'oidc',
  oidcStartUrl: '/api/v1/auth/oidc/start',
  oidcDisplayName: 'Acme SSO',
}
const ada: Me = {
  user: {
    id: 'u-0123456789abcdef01234567',
    displayName: 'Ada Lovelace',
    kind: 'oidc',
    username: 'ada',
    email: 'ada@example.org',
    groups: ['cs101'],
  },
  mode: 'server',
  authMode: 'oidc',
  features: { gui: true, remoteBox: false, stuckReports: true, authorMode: false },
}

let authConfig: AuthConfigInfo = oidcConfig
let signedIn = false
let logoutCalls = 0
let logoutRedirect: string | null = null
let quotaGroup = 'cs101'

const server = setupServer(
  http.get('/api/v1/auth/config', () => HttpResponse.json(authConfig)),
  http.get('/api/v1/me', () =>
    signedIn
      ? HttpResponse.json(ada)
      : HttpResponse.json(
          { status: 401, code: 'unauthenticated', message: 'sign in first' },
          { status: 401 },
        ),
  ),
  http.post('/api/v1/auth/logout', () => {
    logoutCalls++
    signedIn = false
    return logoutRedirect
      ? HttpResponse.json({ redirectUrl: logoutRedirect })
      : new HttpResponse(null, { status: 204 })
  }),
  http.post('/api/v1/sessions', () =>
    HttpResponse.json(
      {
        status: 429,
        code: 'group_quota_exceeded',
        message: 'all 30 active sessions allowed for group "cs101" are in use',
        details: { group: quotaGroup, maxActiveSessions: 30 },
      },
      { status: 429 },
    ),
  ),
  http.get('/api/v1/curriculum', () => HttpResponse.json(index)),
  http.get('/api/v1/progress', () => HttpResponse.json({ lessons: {}, updatedAt: 1 })),
)

beforeAll(() => server.listen())
afterAll(() => server.close())
beforeEach(() => {
  authConfig = oidcConfig
  signedIn = false
  logoutCalls = 0
  logoutRedirect = null
  quotaGroup = 'cs101'
  useSessionStore.getState().reset()
})
afterEach(() => {
  disconnectSession()
  server.resetHandlers()
})

function open(path: string) {
  window.history.pushState({}, '', path)
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<App queryClient={qc} />)
  return qc
}

describe('login screen', () => {
  it('offers one sign-in button that returns to the page asked for', async () => {
    open('/session?x=1')
    const button = await screen.findByRole('link', { name: 'Sign in with Acme SSO' })
    expect(button).toHaveAttribute(
      'href',
      `/api/v1/auth/oidc/start?next=${encodeURIComponent('/session?x=1')}`,
    )
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('labels the button SSO without a display name', async () => {
    authConfig = { mode: 'server', authMode: 'oidc' }
    open('/login')
    const button = await screen.findByRole('link', { name: 'Sign in with SSO' })
    expect(button).toHaveAttribute('href', '/api/v1/auth/oidc/start?next=%2F')
  })

  it.each([
    ['forbidden', /not allowed to use this playground/],
    ['provider', /provider reported an error/],
    ['state', /expired or was started in another tab/],
    ['token', /could not be verified/],
    ['bogus', /Signing in did not work/],
  ])('explains the callback error %s', async (code, message) => {
    open(`/login?error=${code}`)
    expect(await screen.findByRole('alert')).toHaveTextContent(message)
    expect(await screen.findByRole('link', { name: 'Sign in with Acme SSO' })).toBeInTheDocument()
  })

  it('is skipped without authentication', async () => {
    authConfig = { mode: 'local', authMode: 'none' }
    signedIn = true
    open('/login')
    await waitFor(() => expect(window.location.pathname).toBe('/'))
  })

  it('accepts only same-origin paths to return to', () => {
    expect(safeNext('/lesson/a/b?x#y')).toBe('/lesson/a/b?x#y')
    for (const bad of ['//evil.example', '/\\evil', 'https://evil.example', '/login', 3])
      expect(safeNext(bad)).toBe('/')
  })
})

describe('session page', () => {
  it('shows the account and why the session could not start', async () => {
    signedIn = true
    open('/session')
    const account = await screen.findByRole('region', { name: 'Account' })
    expect(within(account).getByText('Ada Lovelace')).toBeInTheDocument()
    expect(within(account).getByText('ada@example.org')).toBeInTheDocument()
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Your group has no free Emacs right now.')
    expect(alert).toHaveTextContent('All 30 sessions allowed for the group “cs101” are in use.')
    expect(alert).toHaveTextContent(/ask your instructor/)
    expect(within(alert).getByRole('button', { name: 'Try again' })).toBeInTheDocument()
  })

  it('names the shared pool of learners outside the listed groups', async () => {
    signedIn = true
    quotaGroup = ''
    open('/session')
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'All 30 sessions allowed for learners outside a course group are in use.',
    )
  })

  it('signs out to the login screen', async () => {
    signedIn = true
    open('/session')
    const account = await screen.findByRole('region', { name: 'Account' })
    await userEvent.click(within(account).getByRole('button', { name: 'Sign out' }))
    expect(await screen.findByRole('link', { name: 'Sign in with Acme SSO' })).toBeInTheDocument()
    expect(logoutCalls).toBe(1)
  })
})

describe('logout', () => {
  it("returns the provider's end-session URL when there is one", async () => {
    const qc = new QueryClient()
    logoutRedirect = 'https://idp.example/logout?client_id=pg'
    await expect(logout(qc)).resolves.toBe('https://idp.example/logout?client_id=pg')
    logoutRedirect = null
    await expect(logout(qc)).resolves.toBeNull()
  })
})
