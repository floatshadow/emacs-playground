import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { disconnectSession } from '../../api/session/connection'
import { useSessionStore } from '../../store/session'
import { ScreenOverlay } from './ScreenOverlay'

let posts = 0
type Problem = { status: number; code: string; message: string; details: unknown }
const QUOTA: Problem = {
  status: 429,
  code: 'group_quota_exceeded',
  message: 'all 2 active sessions allowed for group "cs101" are in use',
  details: { group: 'cs101', maxActiveSessions: 2 },
}
let problem = QUOTA

const server = setupServer(
  http.post('/api/v1/sessions', () => {
    posts++
    return HttpResponse.json(problem, { status: problem.status })
  }),
)

beforeAll(() => server.listen())
afterAll(() => server.close())
beforeEach(() => {
  posts = 0
  problem = QUOTA
  useSessionStore.getState().reset()
})
afterEach(() => disconnectSession())

function renderOverlay() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={qc}>
      <ScreenOverlay onReattach={() => undefined} />
    </QueryClientProvider>,
  )
}

describe('session start overlay', () => {
  it('explains a full group quota and retries on request', async () => {
    renderOverlay()
    const card = await screen.findByRole('alert', {
      name: 'Your group has no free Emacs right now.',
    })
    expect(card).toHaveTextContent('All 2 sessions allowed for the group “cs101” are in use.')
    expect(posts).toBe(1)
    await userEvent.click(within(card).getByRole('button', { name: 'Try again' }))
    await waitFor(() => expect(posts).toBe(2))
  })

  it("shows the server's message for other failures", async () => {
    problem = { status: 429, code: 'session_limit', message: 'the server is full', details: {} }
    renderOverlay()
    const card = await screen.findByRole('alert', { name: 'Could not start a session.' })
    expect(card).toHaveTextContent('the server is full')
  })
})
