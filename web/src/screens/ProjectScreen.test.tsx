import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient } from '@tanstack/react-query'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import WS from 'vitest-websocket-mock'
import type { CurriculumIndex, Event, LessonBundle, LessonProgress } from '../protocol.gen'
import { DEFAULT_TABLES } from '../advice/defaults'
import { disconnectSession } from '../api/session/connection'
import { useSessionStore } from '../store/session'
import { useUiStore } from '../store/ui'
import indexJson from '../../dev/fixtures/index.json'
import { LESSONS } from '../../dev/lessons.ts'
import { App } from '../app'

vi.mock('../components/screen/TerminalPane', () => ({
  default: () => <div data-testid="terminal" />,
}))
vi.mock('../components/screen/VncPane', () => ({ default: () => <div data-testid="vnc" /> }))
vi.mock('../components/screen/hosts', () => ({
  getTerminalHost: () => ({ reattach: vi.fn(), focus: vi.fn() }),
  getVncHost: () => ({ ensure: vi.fn(), focus: vi.fn() }),
}))
// pdf.js needs a real canvas and worker; the gate in front of it is what is tested here.
vi.mock('../components/preview/PdfView', () => ({
  default: ({ name }: { name: string }) => <div data-testid="pdf">{name}</div>,
}))

const P6 = 'projects/p6-polyglot'
const EVENTS_URL = 'ws://localhost:3000/s/local/events'
const P6_BUNDLE = LESSONS[P6] as LessonBundle
let bundle = P6_BUNDLE
const stepIds = P6_BUNDLE.scenario.steps.map((s) => s.id)

const rpcCalls: { op: string; body: unknown }[] = []
const index = { ...(indexJson as unknown as CurriculumIndex), tables: DEFAULT_TABLES }
let progress: Record<string, LessonProgress> = {}
let files: Record<string, string[]> = {}
let existing = new Set<string>()
let features = { gui: true, remoteBox: true, stuckReports: true, authorMode: false }
let remoteBox: boolean | undefined = true

const server = setupServer(
  http.get('/api/v1/me', () =>
    HttpResponse.json({
      user: { id: 'local', displayName: 'Local learner', kind: 'local' },
      mode: 'local',
      authMode: 'none',
      features,
    }),
  ),
  http.post('/api/v1/sessions', () =>
    HttpResponse.json({ id: 'local', state: 'ready', createdAt: 1, gui: true }, { status: 201 }),
  ),
  http.get('/api/v1/curriculum', () => HttpResponse.json(index)),
  http.get('/api/v1/curriculum/lessons/*', () => HttpResponse.json(bundle)),
  http.get('/api/v1/progress', () => HttpResponse.json({ lessons: progress, updatedAt: 1 })),
  http.get('/s/local/preview/*', ({ request }) => {
    const path = decodeURIComponent(new URL(request.url).pathname.replace('/s/local/preview/', ''))
    if (path.endsWith('/')) {
      const dir = path.replace(/\/$/, '')
      return HttpResponse.json({
        path: dir,
        entries: (files[dir] ?? []).map((n) => ({
          name: n.replace(/\/$/, ''),
          dir: n.endsWith('/'),
          size: 1,
          mtime: 1,
        })),
      })
    }
    return existing.has(path)
      ? new HttpResponse('x')
      : HttpResponse.json({ status: 404, code: 'not_found', message: 'no' }, { status: 404 })
  }),
  http.head('/s/local/preview/*', ({ request }) => {
    const path = decodeURIComponent(new URL(request.url).pathname.replace('/s/local/preview/', ''))
    return new HttpResponse(null, { status: existing.has(path) ? 200 : 404 })
  }),
  http.post('/s/local/rpc/:op', async ({ params, request }) => {
    const body = await request.json()
    const op = String(params['op'])
    rpcCalls.push({ op, body })
    if (op === 'scenario')
      return HttpResponse.json({
        ok: true,
        result: {
          step: { lessonId: P6, stepId: stepIds[0] },
          index: 0,
          count: stepIds.length,
          modules: [],
          restarting: false,
        },
      })
    if (op === 'checkpoints')
      return HttpResponse.json({
        ok: true,
        result: {
          items: [
            { label: 'mine', ref: 'refs/playground/manual/mine', createdAt: 3, files: [] },
            {
              label: `${P6}/rust-build`,
              ref: 'refs/playground/steps/p6/rust-build',
              createdAt: 2,
              files: ['polyglot/src/main.rs'],
              step: { lessonId: P6, stepId: 'rust-build' },
            },
            {
              label: '02-buffers-and-files/03-opening-files/open',
              ref: 'refs/playground/steps/x/open',
              createdAt: 1,
              files: [],
              step: { lessonId: '02-buffers-and-files/03-opening-files', stepId: 'open' },
            },
          ],
        },
      })
    if (op === 'status')
      return HttpResponse.json({
        ok: true,
        result: {
          sessionId: 'local',
          emacs: { alive: true },
          frames: { terminal: true, gui: false },
          mode: 'terminal',
          step: null,
          modules: [],
          activeWebSockets: 2,
          ...(remoteBox === undefined ? {} : { remoteBox }),
        },
      })
    if (op === 'describe') return HttpResponse.json({ ok: true, result: { cmd: '', doc: '' } })
    return HttpResponse.json({ ok: true, result: {} })
  }),
)

const ws = new WS(EVENTS_URL, { jsonProtocol: true })

beforeAll(() => server.listen({ onUnhandledFrame: 'bypass' }))
afterAll(() => {
  server.close()
  WS.clean()
})

beforeEach(() => {
  rpcCalls.length = 0
  progress = {}
  files = { polyglot: ['docs/', 'src/', 'README.md'], 'polyglot/docs': ['report.typ'] }
  existing = new Set(['polyglot/README.md'])
  features = { gui: true, remoteBox: true, stuckReports: true, authorMode: false }
  remoteBox = true
  bundle = P6_BUNDLE
  useSessionStore.getState().reset()
  useUiStore.setState({ hintLevels: {}, toasts: [], demo: null, previewTab: {}, previewExtra: {} })
  window.history.pushState({}, '', '/project/p6-polyglot')
})
afterEach(() => {
  disconnectSession()
  server.resetHandlers()
})

function send(ev: Event) {
  act(() => ws.send(ev))
}

const hello: Event = {
  t: 'hello',
  seq: 1,
  protocol: 1,
  sessionId: 'local',
  mode: 'terminal',
  emacs: { version: '31.1', uptimeMs: 1000 },
  frames: { terminal: true, gui: false },
  step: null,
  modules: ['base', 'agent'],
  replayed: false,
}

const step = (over: Partial<Extract<Event, { t: 'step' }>>): Event => ({
  t: 'step',
  lessonId: P6,
  stepId: stepIds[0] ?? '',
  status: 'loaded',
  index: 0,
  count: stepIds.length,
  ...over,
})

async function openProject() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<App queryClient={qc} />)
  await waitFor(() => expect(ws.server.clients().length).toBe(1))
  send(hello)
  await screen.findByRole('list', { name: 'Tasks' })
}

const allDone = (): LessonProgress => ({
  status: 'done',
  counts: { done: stepIds.length, demonstrated: 0, skipped: 0, total: stepIds.length },
  steps: Object.fromEntries(stepIds.map((id) => [id, { status: 'done', hintsUsed: 0, resets: 0 }])),
})

describe('ProjectScreen', () => {
  it('shows the tasks with progress, the file tree and its own checkpoints by task', async () => {
    await openProject()
    await waitFor(() =>
      expect(rpcCalls.find((c) => c.op === 'scenario')?.body).toEqual({
        lessonId: P6,
        stepId: stepIds[0],
      }),
    )
    send(step({}))
    expect(await screen.findByText(`0 of ${stepIds.length} tasks done`)).toBeInTheDocument()
    send(step({ status: 'complete' }))
    send(step({ stepId: stepIds[1], index: 1 }))
    expect(await screen.findByText(`1 of ${stepIds.length} tasks done`)).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: `Task 2 of ${stepIds.length}` })).toBeInTheDocument()

    const tree = screen.getByRole('list', { name: 'Project files' })
    expect(await within(tree).findByRole('button', { name: 'README.md' })).toBeInTheDocument()
    expect(within(tree).getByRole('button', { name: 'report.typ' })).toBeInTheDocument()

    // Checkpoints: this project's task checkpoint by task name and the manual one; not 2.3's.
    expect(await screen.findByText('Start of task 2: Fix the build, then rebuild')).toBeVisible()
    expect(screen.getByText('mine')).toBeVisible()
    expect(screen.queryByText(/03-opening-files/)).toBeNull()
    const user = userEvent.setup()
    const row = screen.getByText('Start of task 2: Fix the build, then rebuild').closest('li')
    await user.click(within(row as HTMLElement).getByRole('button', { name: 'Restore' }))
    const dialog = await screen.findByRole('dialog')
    expect(dialog).toHaveTextContent('polyglot/src/main.rs')
    await user.click(within(dialog).getByRole('button', { name: 'Restore' }))
    await waitFor(() =>
      expect(rpcCalls).toContainEqual({
        op: 'restore',
        body: { ref: 'refs/playground/steps/p6/rust-build' },
      }),
    )
  })

  it('offers the lap once every task is done, hides hints during it and reports the time', async () => {
    progress = { [P6]: allDone() }
    const user = userEvent.setup()
    await openProject()
    const start = await screen.findByRole('button', { name: 'Start the lap' })
    expect(screen.getByText(/Aim for 15 minutes/)).toBeInTheDocument()
    // A finished project is not loaded on opening.
    expect(rpcCalls.find((c) => c.op === 'scenario')).toBeUndefined()
    await user.click(start)
    await waitFor(() =>
      expect(rpcCalls).toContainEqual({ op: 'scenario', body: { lessonId: P6, lap: true } }),
    )
    send(step({ lap: true, ts: Date.now() }))
    expect(await screen.findByText(/Lap without hints/)).toBeInTheDocument()
    expect(screen.getByText('target 15:00')).toBeInTheDocument()
    expect(screen.getByText(`0 of ${stepIds.length} tasks done`)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Show a hint' })).toBeNull()
    expect(screen.queryByRole('button', { name: /Watch it/ })).toBeNull()
    expect(screen.queryByRole('button', { name: /Go to this step|Do this step again/ })).toBeNull()

    const last = stepIds.length - 1
    send(
      step({
        stepId: stepIds[last],
        index: last,
        status: 'complete',
        lap: true,
        lessonComplete: true,
        lapMs: 754_000,
      }),
    )
    expect(await screen.findByText(/Lap finished in 12:34\./)).toBeInTheDocument()
    expect(screen.getByText(/Within the target/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Another lap' })).toBeInTheDocument()
  })

  it('stopping the lap reloads the current task without it', async () => {
    progress = { [P6]: allDone() }
    const user = userEvent.setup()
    await openProject()
    await user.click(await screen.findByRole('button', { name: 'Start the lap' }))
    send(step({ lap: true, ts: Date.now() }))
    send(step({ status: 'complete', lap: true }))
    send(step({ stepId: stepIds[1], index: 1, lap: true }))
    await user.click(await screen.findByRole('button', { name: 'Stop the lap' }))
    await waitFor(() =>
      expect(rpcCalls).toContainEqual({
        op: 'scenario',
        body: { lessonId: P6, stepId: stepIds[1] },
      }),
    )
    send(step({ stepId: stepIds[1], index: 1 }))
    await waitFor(() => expect(screen.queryByText(/Lap without hints/)).toBeNull())
  })

  it('after a reload, a finished project whose last task is active still offers the lap', async () => {
    progress = { [P6]: allDone() }
    await openProject()
    const last = stepIds.length - 1
    send(step({ stepId: stepIds[last], index: last }))
    expect(await screen.findByRole('button', { name: 'Start the lap' })).toBeInTheDocument()
    expect(
      screen.getByText(`${stepIds.length} of ${stepIds.length} tasks done`),
    ).toBeInTheDocument()
  })

  it('shows finished laps from progress', async () => {
    progress = {
      [P6]: { ...allDone(), laps: { count: 2, bestMs: 600_000, lastMs: 660_000, lastAt: 1 } },
    }
    await openProject()
    expect(await screen.findByText('2 laps · best 10:00 · last 11:00')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Another lap' })).toBeInTheDocument()
  })

  it('remote projects show the SSH box status', async () => {
    await openProject()
    expect(await screen.findByText(/SSH box ready/)).toBeInTheDocument()
  })

  it('says when the box is not set up yet, or not provided', async () => {
    remoteBox = false
    await openProject()
    expect(await screen.findByText(/still being set up/)).toBeInTheDocument()
  })

  it('says when the server provides no box', async () => {
    features = { ...features, remoteBox: false }
    await openProject()
    expect(await screen.findByText(/does not provide/)).toBeInTheDocument()
  })

  it('a preview whose artefact is not built yet says what builds it', async () => {
    await openProject()
    // report.pdf is the first tab and does not exist; it follows report.typ.
    expect(await screen.findByText('report.pdf does not exist yet.')).toBeInTheDocument()
    expect(screen.getByText(/built from report\.typ/)).toBeInTheDocument()
    existing.add('polyglot/docs/report.pdf')
    send({ t: 'file', path: 'polyglot/docs/report.pdf', event: 'created' })
    expect(await screen.findByTestId('pdf')).toHaveTextContent('report.pdf')
  })

  it('files previewed from the tree get a closable tab', async () => {
    const user = userEvent.setup()
    files['polyglot/docs'] = ['report.typ', 'figure.svg']
    existing.add('polyglot/docs/figure.svg')
    await openProject()
    await user.click(await screen.findByRole('button', { name: 'Preview figure.svg' }))
    const tab = await screen.findByRole('tab', { name: 'figure.svg' })
    expect(tab).toHaveAttribute('aria-selected', 'true')
    expect(await screen.findByRole('img', { name: 'figure.svg' })).toBeInTheDocument()
    await user.click(screen.getByTestId('close-figure.svg'))
    expect(screen.queryByRole('tab', { name: 'figure.svg' })).toBeNull()
    // Delete on the focused tab closes it too.
    await user.click(screen.getByRole('button', { name: 'Preview figure.svg' }))
    const again = await screen.findByRole('tab', { name: 'figure.svg' })
    again.focus()
    await user.keyboard('{Delete}')
    expect(screen.queryByRole('tab', { name: 'figure.svg' })).toBeNull()
    // The scenario's own tabs cannot be closed.
    expect(screen.getByRole('tab', { name: 'report.pdf' })).not.toHaveAttribute('aria-keyshortcuts')
  })

  it('GUI projects open the GUI frame after loading and say what it is', async () => {
    bundle = { ...P6_BUNDLE, scenario: { ...P6_BUNDLE.scenario, tags: ['project', 'gui'] } }
    await openProject()
    await waitFor(() => expect(rpcCalls.find((c) => c.op === 'scenario')).toBeDefined())
    send(step({}))
    await waitFor(() => expect(rpcCalls).toContainEqual({ op: 'mode', body: { mode: 'gui' } }))
    expect(screen.queryByText(/SSH box/)).toBeNull()
    send({ t: 'mode', mode: 'gui' })
    expect(await screen.findByText(/You are in the GUI frame/)).toBeInTheDocument()
    send({ t: 'mode', mode: 'terminal' })
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Open the GUI frame' }))
    await waitFor(() => expect(rpcCalls.filter((c) => c.op === 'mode')).toHaveLength(2))
  })

  it('a task with mode gui opens the GUI frame when it becomes active', async () => {
    const steps = P6_BUNDLE.scenario.steps.map((s, i) =>
      i === 1 ? { ...s, mode: 'gui' as const } : s,
    )
    bundle = { ...P6_BUNDLE, scenario: { ...P6_BUNDLE.scenario, steps } }
    await openProject()
    await waitFor(() => expect(rpcCalls.find((c) => c.op === 'scenario')).toBeDefined())
    send(step({}))
    await new Promise((r) => setTimeout(r, 50))
    expect(rpcCalls.find((c) => c.op === 'mode')).toBeUndefined()
    send(step({ stepId: stepIds[1] ?? '', index: 1 }))
    await waitFor(() => expect(rpcCalls).toContainEqual({ op: 'mode', body: { mode: 'gui' } }))
  })

  it('with the GUI frame turned off, a GUI project says so and stays in the terminal', async () => {
    features = { ...features, gui: false }
    bundle = { ...P6_BUNDLE, scenario: { ...P6_BUNDLE.scenario, mode: 'gui', tags: ['project'] } }
    await openProject()
    expect(await screen.findByText(/has turned off/)).toBeInTheDocument()
    await waitFor(() => expect(rpcCalls.find((c) => c.op === 'scenario')).toBeDefined())
    send(step({}))
    await new Promise((r) => setTimeout(r, 50))
    expect(rpcCalls.find((c) => c.op === 'mode')).toBeUndefined()
  })
})
