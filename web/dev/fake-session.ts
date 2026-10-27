/**
 * The fake session: a small Node server that stands in for the gateway and
 * one session (api.md §2 and §3) so the web app can be developed and tested
 * without Docker, Emacs or tmux. A port of the design demo's mock engine
 * drives it: typing in the terminal moves a fake Emacs, which emits key,
 * prefix, state, echo and minibuffer events; scenarios load, steps are
 * checked, checkpoints restore, previews rebuild.
 *
 *   node dev/fake-session.ts [--port 8090] [--host 127.0.0.1] [--static dist]
 *        [--auth none|invite] [--author] [--curriculum DIR] [--vite] [--sessiond]
 *   node dev/fake-session.ts --export-bundle DIR
 *
 * --static serves a built bundle (with the SPA fallback) for browser tests;
 * --vite also starts Vite on :5173 proxying to this server (pnpm dev:fake);
 * --curriculum serves a bundle built by `playctl curriculum build` instead
 * of the built-in fixtures; with --auth invite the code is "emacs".
 * --sessiond also answers sessiond's unprefixed paths and --export-bundle
 * writes the built-in lessons in the bundle layout, so a real gateway can run
 * in front of the fake (contract tests; see dev/README.md).
 */
import { spawn } from 'node:child_process'
import { existsSync, mkdirSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { createServer, type IncomingMessage, type ServerResponse } from 'node:http'
import { dirname, extname, join, normalize, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { CurriculumFormat } from '../src/protocol.gen.ts'
import type {
  CheckEvent,
  CheckpointInfo,
  CurriculumIndex,
  Event,
  LessonBundle,
  LessonProgress,
  LessonRef,
  Progress,
  Scenario,
  SessionInfo,
  State,
  StatusResult,
} from '../src/protocol.gen.ts'
import { DEFAULT_TABLES } from '../src/advice/defaults.ts'
import { normalizeKeysLoose } from '../src/advice/keys.ts'
import { decodeInput, textToKeys } from './input.ts'
import { LESSONS, genericLesson } from './lessons.ts'
import { MockEmacs } from './mock-emacs.ts'
import { makePdf } from './pdf.ts'
import { renderAnsi } from './render.ts'
import { serveRfb } from './rfb.ts'
import { evaluateStep } from './steps.ts'
import { HELLO, POLY, typstLines } from './workspace.ts'
import { acceptUpgrade, type WsConn } from './ws.ts'

const here = dirname(fileURLToPath(import.meta.url))
const SESSION = 'local'
const RING = 500

export interface FakeOptions {
  port: number
  host: string
  staticDir?: string
  auth: 'none' | 'invite'
  author: boolean
  curriculumDir?: string
  quiet?: boolean
  /** Speeds up artificial delays (tests). */
  fast?: boolean
  /**
   * Also answer sessiond's own paths (/events, /pty, /vnc, /rpc/{op},
   * /preview/…, /status) without the /s/{id} prefix, so a real gateway
   * (static backend, sessions.url pointing here) can proxy to this fake.
   */
  sessiond?: boolean
}

const SESSIOND_PATH = /^\/(events|pty|vnc|status|rpc\/|preview\/)/

/** Maps a sessiond-style path to the gateway-style one the fake routes on. */
function sessiondPath(p: string, on: boolean | undefined): string {
  return on && SESSIOND_PATH.test(p) ? `/s/${SESSION}${p}` : p
}

interface Checkpoint {
  label: string
  ref: string
  createdAt: number
  text: Map<string, string>
  blobs: Map<string, Buffer>
  step?: { lessonId: string; stepId: string }
}

const json = (
  res: ServerResponse,
  status: number,
  body: unknown,
  headers: Record<string, string> = {},
) => {
  res.writeHead(status, {
    'Content-Type': 'application/json',
    'Cache-Control': 'no-store',
    ...headers,
  })
  res.end(JSON.stringify(body))
}
const problem = (res: ServerResponse, status: number, code: string, message: string) => {
  res.writeHead(status, { 'Content-Type': 'application/problem+json' })
  res.end(JSON.stringify({ status, code, message }))
}
const noContent = (res: ServerResponse, headers: Record<string, string> = {}) => {
  res.writeHead(204, headers)
  res.end()
}

async function body(req: IncomingMessage): Promise<Record<string, unknown>> {
  const chunks: Buffer[] = []
  for await (const c of req) chunks.push(c as Buffer)
  const s = Buffer.concat(chunks).toString('utf8')
  if (!s) return {}
  try {
    return JSON.parse(s) as Record<string, unknown>
  } catch {
    return {}
  }
}

const MIME: Record<string, string> = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript',
  '.mjs': 'text/javascript',
  '.css': 'text/css',
  '.json': 'application/json',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.woff2': 'font/woff2',
  '.pdf': 'application/pdf',
  '.md': 'text/markdown; charset=utf-8',
  '.map': 'application/json',
}

/** The built-in course index (dev/fixtures/index.json) with the default tables. */
export function fixtureIndex(): CurriculumIndex {
  const index = JSON.parse(
    readFileSync(join(here, 'fixtures', 'index.json'), 'utf8'),
  ) as CurriculumIndex
  index.tables = DEFAULT_TABLES
  for (const p of index.parts)
    for (const l of p.lessons) l.steps = (LESSONS[l.id]?.scenario.steps.length ?? 1) as number
  return index
}

/**
 * Writes the built-in lessons as a bundle in the layout of
 * `playctl curriculum build -o DIR` (protocol/curriculum.go), so a real
 * gateway can serve them in front of this fake (--sessiond). Lessons have no
 * fixture tarballs: the fake keeps its workspace in memory.
 */
export function exportBundle(dir: string): number {
  const index = fixtureIndex()
  index.format = CurriculumFormat
  index.version = 'fake+' + new Date().toISOString().slice(0, 10)
  index.builtAt = Date.now()
  mkdirSync(dir, { recursive: true })
  writeFileSync(join(dir, 'curriculum.json'), JSON.stringify(index, null, 1))
  let n = 0
  for (const part of index.parts) {
    for (const ref of part.lessons) {
      if (ref.status !== 'ready') continue
      const b = (LESSONS[ref.id] as LessonBundle | undefined) ?? genericLesson(ref)
      const ld = join(dir, 'lessons', ...ref.id.split('/'))
      mkdirSync(ld, { recursive: true })
      writeFileSync(join(ld, 'bundle.json'), JSON.stringify(b, null, 1))
      writeFileSync(join(ld, 'scenario.json'), JSON.stringify(b.scenario, null, 1))
      n++
    }
  }
  for (const pr of index.projects) {
    const b = LESSONS[pr.id] as LessonBundle | undefined
    if (!b) continue
    const ld = join(dir, 'lessons', ...pr.id.split('/'))
    mkdirSync(ld, { recursive: true })
    writeFileSync(join(ld, 'bundle.json'), JSON.stringify(b, null, 1))
    writeFileSync(join(ld, 'scenario.json'), JSON.stringify(b.scenario, null, 1))
    n++
  }
  return n
}

export async function startFakeSession(opts: FakeOptions) {
  const log = (...a: unknown[]) => {
    if (!opts.quiet) console.log('[fake]', ...a)
  }
  const delay = (ms: number) => (opts.fast ? Math.min(ms, 20) : ms)
  const started = Date.now()

  /* ---------------- curriculum ---------------- */
  let index: CurriculumIndex
  if (opts.curriculumDir) {
    index = JSON.parse(
      readFileSync(join(opts.curriculumDir, 'curriculum.json'), 'utf8'),
    ) as CurriculumIndex
  } else {
    index = fixtureIndex()
  }
  const lessonRefs = new Map<string, LessonRef>()
  for (const p of index.parts) for (const l of p.lessons) lessonRefs.set(l.id, l)
  function bundleFor(id: string): LessonBundle | null {
    if (opts.curriculumDir) {
      const f = join(opts.curriculumDir, 'lessons', id, 'bundle.json')
      return existsSync(f) ? (JSON.parse(readFileSync(f, 'utf8')) as LessonBundle) : null
    }
    if (LESSONS[id]) return LESSONS[id] as LessonBundle
    const ref = lessonRefs.get(id)
    return ref && ref.status === 'ready' ? genericLesson(ref) : null
  }

  /* ---------------- workspace and Emacs ---------------- */
  const disk = new Map<string, string>(Object.entries({ ...HELLO, ...POLY }))
  const blobs = new Map<string, Buffer>()
  const buildPdf = () => {
    const src = disk.get('polyglot/docs/report.typ') ?? ''
    blobs.set(
      'polyglot/docs/report.pdf',
      makePdf(typstLines(src), `built ${new Date().toLocaleTimeString()} by the fake typst watch`),
    )
  }
  buildPdf()

  let seq = 0
  const ring: Event[] = []
  const subscribers = new Set<WsConn>()
  let mode: 'terminal' | 'gui' = 'terminal'
  let modules = ['base', 'agent']
  let lastKeyAt = Date.now()

  function publish(ev: Event) {
    ev.seq = ++seq
    ev.ts = Date.now()
    ring.push(ev)
    if (ring.length > RING) ring.shift()
    const s = JSON.stringify(ev)
    for (const c of subscribers) c.sendText(s)
  }

  const eng: MockEmacs = new MockEmacs(disk, {
    emit: (ev) => {
      if (ev.t === 'key') {
        lastKeyAt = Date.now()
        quits.push(ev.cmd === 'keyboard-quit' || ev.cmd === 'abort-minibuffers' ? Date.now() : -1)
        unbound.push(ev.cmd === 'undefined' ? ev.keys : '')
      }
      if (ev.t === 'minibuffer') miniSince = ev.open ? Date.now() : null
      publish(ev)
    },
    saved: (path) => {
      publish({ t: 'file', path, event: 'modified' })
      if (path === 'polyglot/docs/report.typ') {
        setTimeout(() => {
          buildPdf()
          publish({ t: 'file', path: 'polyglot/docs/report.pdf', event: 'modified' })
        }, delay(900))
      }
    },
  })
  eng.reset(['hello/README.md'])
  let miniSince: number | null = null
  const quits: number[] = []
  const unbound: string[] = []

  /* ---------------- state, screen ---------------- */
  let lastStateJSON = ''
  function publishState(force = false) {
    const st = eng.state(mode)
    const s = JSON.stringify(st)
    if (!force && s === lastStateJSON) return
    lastStateJSON = s
    publish(st)
  }

  let pty: WsConn | null = null
  let size = { cols: 120, rows: 36 }
  let renderQueued = false
  function render() {
    if (renderQueued) return
    renderQueued = true
    setImmediate(() => {
      renderQueued = false
      if (pty && !pty.closed) pty.sendBinary(renderAnsi(eng, size.cols, size.rows))
      if (mode === 'gui') refreshRfb()
    })
  }
  eng.changed = () => {
    render()
    publishState()
    schedule()
  }

  function afterInput() {
    render()
    publishState()
    schedule()
  }
  function feed(keys: string[]) {
    for (const k of keys) eng.handleKey(k)
    afterInput()
  }

  /* ---------------- scenario and steps ---------------- */
  interface Active {
    scenario: Scenario
    lessonId: string
    index: number
    base: Record<string, number>
    startedAt: number
    fired: Set<string>
    lastCheck: string
  }
  let active: Active | null = null
  const progress: Record<string, LessonProgress> = {}
  let progressAt = Date.now()
  const checkpoints: Checkpoint[] = []

  function setProgress(
    lessonId: string,
    stepId: string,
    patch: Partial<LessonProgress['steps'][string]>,
  ) {
    const lp = (progress[lessonId] ??= { status: 'in-progress', steps: {} })
    const cur = lp.steps[stepId] ?? { status: 'in-progress', hintsUsed: 0, resets: 0 }
    lp.steps[stepId] = { ...cur, ...patch }
    const scenario = bundleFor(lessonId)?.scenario
    lp.status =
      scenario && scenario.steps.every((s) => lp.steps[s.id]?.status === 'done')
        ? 'done'
        : 'in-progress'
    progressAt = Date.now()
  }

  function snapshot(label: string, step?: { lessonId: string; stepId: string }): Checkpoint {
    const cp: Checkpoint = {
      label,
      ref: 'refs/playground/' + label.replace(/[^A-Za-z0-9/_.-]+/g, '-'),
      createdAt: Date.now(),
      text: new Map(disk),
      blobs: new Map(blobs),
      ...(step ? { step } : {}),
    }
    const i = checkpoints.findIndex((c) => c.ref === cp.ref)
    if (i >= 0) checkpoints.splice(i, 1)
    checkpoints.unshift(cp)
    publish({ t: 'checkpoint', label: cp.label, ref: cp.ref, createdAt: cp.createdAt })
    return cp
  }

  function changedFiles(cp: Checkpoint): string[] {
    const out = new Set<string>()
    for (const [p, t] of disk) if (cp.text.get(p) !== t) out.add(p)
    for (const p of cp.text.keys()) if (!disk.has(p)) out.add(p)
    return [...out].sort()
  }

  function startStep(index: number) {
    if (!active) return
    const step = active.scenario.steps[index]
    if (!step) return
    active.index = index
    active.base = { ...eng.counts }
    active.startedAt = Date.now()
    active.fired = new Set()
    active.lastCheck = ''
    snapshot(`${active.lessonId}/${step.id}`, { lessonId: active.lessonId, stepId: step.id })
    setProgress(active.lessonId, step.id, {
      status: progress[active.lessonId]?.steps[step.id]?.status === 'done' ? 'done' : 'in-progress',
    })
    publish({
      t: 'step',
      lessonId: active.lessonId,
      stepId: step.id,
      status: 'loaded',
      index,
      count: active.scenario.steps.length,
    })
    schedule()
  }

  let evalTimer: ReturnType<typeof setTimeout> | null = null
  function schedule() {
    if (evalTimer) return
    evalTimer = setTimeout(() => {
      evalTimer = null
      evaluate()
    }, delay(150))
  }

  function evaluate(): CheckEvent | null {
    if (!active) return null
    const step = active.scenario.steps[active.index]
    if (!step) return null
    const st: State = eng.state(mode)
    const r = evaluateStep(active.scenario, step.id, { eng, state: st, disk, base: active.base })
    const ev: CheckEvent = {
      t: 'check',
      lessonId: active.lessonId,
      stepId: step.id,
      conditions: r.conditions,
      complete: r.complete,
    }
    const key = JSON.stringify([step.id, r])
    if (key !== active.lastCheck) {
      active.lastCheck = key
      publish(ev)
    }
    if (r.complete) {
      const last = active.index === active.scenario.steps.length - 1
      setProgress(active.lessonId, step.id, { status: 'done', completedAt: Date.now() })
      const done: Event = {
        t: 'step',
        lessonId: active.lessonId,
        stepId: step.id,
        status: 'complete',
        index: active.index,
        count: active.scenario.steps.length,
      }
      if (last) done.lessonComplete = true
      publish(done)
      if (!last) startStep(active.index + 1)
      else active.lastCheck = 'complete'
    }
    return ev
  }

  async function loadScenario(lessonId: string, stepId?: string, reset = false) {
    const bundle = bundleFor(lessonId)
    if (!bundle)
      throw Object.assign(new Error('no such lesson: ' + lessonId), {
        status: 404,
        code: 'not_found',
      })
    const scenario = bundle.scenario
    const same = active?.lessonId === lessonId
    const want = scenario.modules ?? []
    if (want.join(' ') !== modules.join(' ')) {
      modules = want
      publish({ t: 'modules', enabled: modules, restarting: true })
      await new Promise((r) => setTimeout(r, delay(700)))
      publish({ t: 'emacs', event: 'daemon-restarted', reason: 'module set changed' })
      publish({ t: 'modules', enabled: modules, restarting: false })
    }
    if (!same) {
      const root = scenario.setup?.root
      const fixtures = root === 'polyglot' ? POLY : HELLO
      for (const [p, t] of Object.entries(fixtures))
        if (reset || scenario.setup?.reset || !disk.has(p)) disk.set(p, t)
      eng.reset([])
      eng.open(scenario.setup?.open ?? [], scenario.setup?.layout)
    }
    const index = Math.max(0, stepId ? scenario.steps.findIndex((s) => s.id === stepId) : 0)
    active = {
      scenario,
      lessonId,
      index,
      base: {},
      startedAt: Date.now(),
      fired: new Set(),
      lastCheck: '',
    }
    startStep(index)
    afterInput()
    const step = scenario.steps[index]
    return {
      step: { lessonId, stepId: step?.id ?? '' },
      index,
      count: scenario.steps.length,
      modules,
      restarting: false,
    }
  }

  // Stuck rules and time-based checks every 2 s.
  const stuckTimer = setInterval(() => {
    if (!active) return
    const step = active.scenario.steps[active.index]
    if (!step?.stuck) return
    const now = Date.now()
    for (const [i, rule] of step.stuck.entries()) {
      const id = rule.id ?? `${step.id}/stuck/${i + 1}`
      if (active.fired.has(id)) continue
      const w = rule.when
      let hit = false
      if (w.minibuffer_seconds && miniSince && now - miniSince > w.minibuffer_seconds * 1000)
        hit = true
      if (w.step_seconds && now - active.startedAt > w.step_seconds * 1000) hit = true
      if (w.idle_seconds && now - lastKeyAt > w.idle_seconds * 1000) hit = true
      if (w.quits_in_window) {
        const since = now - w.quits_in_window.seconds * 1000
        if (quits.filter((t) => t > since).length >= w.quits_in_window.count) hit = true
      }
      if (w.unbound_repeats) {
        const tail = unbound.slice(-w.unbound_repeats)
        if (tail.length === w.unbound_repeats && tail[0] && tail.every((k) => k === tail[0]))
          hit = true
      }
      if (!hit) continue
      active.fired.add(id)
      const say = typeof rule.say === 'string' ? rule.say : (rule.say['en'] ?? '')
      const title = rule.title
        ? typeof rule.title === 'string'
          ? rule.title
          : (rule.title['en'] ?? '')
        : (say.split(/(?<=[.?!])\s/)[0] ?? say)
      publish({ t: 'nudge', id, kind: rule.kind ?? 'calm', title, body: say })
    }
  }, 2000)

  /* ---------------- RPC ---------------- */
  let restarting = false
  async function rpc(op: string, b: Record<string, unknown>): Promise<unknown> {
    switch (op) {
      case 'keys': {
        if (typeof b['keys'] === 'string')
          feed((b['keys'] as string).trim().split(/\s+/).filter(Boolean))
        if (typeof b['text'] === 'string') feed(textToKeys(b['text'] as string))
        return {}
      }
      case 'open': {
        const path = String(b['path'] ?? '').replace(/^\/+/, '')
        const buf = eng.visit(path)
        if (!buf)
          throw Object.assign(new Error('no such file in the workspace: ' + path), {
            status: 404,
            code: 'not_found',
          })
        eng.show(buf.name)
        afterInput()
        return { buffer: buf.name }
      }
      case 'eval': {
        const form = String(b['form'] ?? '')
        const m = /\(find-file "((?:[^"\\]|\\.)*)"\)/.exec(form)
        if (m) {
          const path = (m[1] as string).replace(/\\(.)/g, '$1').replace(/^\/work\//, '')
          const buf = eng.visit(path)
          if (buf) {
            eng.show(buf.name)
            afterInput()
            return { value: null }
          }
          return { value: 'no such file: ' + path }
        }
        return { value: 'the fake session evaluates only (find-file …)' }
      }
      case 'rescue': {
        const did = eng.rescue()
        afterInput()
        return { did }
      }
      case 'scenario':
        return loadScenario(
          String(b['lessonId'] ?? ''),
          b['stepId'] ? String(b['stepId']) : undefined,
          b['reset'] === true,
        )
      case 'check': {
        if (!active)
          throw Object.assign(new Error('no active step'), { status: 409, code: 'conflict' })
        active.lastCheck = ''
        const ev = evaluate()
        publishState(true)
        return ev
          ? {
              lessonId: ev.lessonId,
              stepId: ev.stepId,
              conditions: ev.conditions,
              complete: ev.complete,
            }
          : {}
      }
      case 'hint': {
        if (active) {
          const stepId = String(b['stepId'] ?? '')
          const cur = progress[active.lessonId]?.steps[stepId]?.hintsUsed ?? 0
          setProgress(active.lessonId, stepId, {
            hintsUsed: Math.max(cur, Number(b['level'] ?? 1)),
          })
        }
        return {}
      }
      case 'checkpoint': {
        const cp = snapshot(String(b['label'] ?? 'manual'))
        return { ref: cp.ref, createdAt: cp.createdAt }
      }
      case 'checkpoints': {
        const items: CheckpointInfo[] = checkpoints.map((c) => {
          const it: CheckpointInfo = {
            label: c.label,
            ref: c.ref,
            createdAt: c.createdAt,
            files: changedFiles(c),
          }
          if (c.step) it.step = c.step
          return it
        })
        return { items }
      }
      case 'restore': {
        const cp = checkpoints.find(
          (c) => (b['ref'] && c.ref === b['ref']) || (b['label'] && c.label === b['label']),
        )
        if (!cp)
          throw Object.assign(new Error('no such checkpoint'), { status: 404, code: 'not_found' })
        const changed = changedFiles(cp)
        disk.clear()
        for (const [p, t] of cp.text) disk.set(p, t)
        blobs.clear()
        for (const [p, t] of cp.blobs) blobs.set(p, t)
        eng.revertAll()
        for (const p of changed)
          publish({ t: 'file', path: p, event: cp.text.has(p) ? 'modified' : 'deleted' })
        if (active && cp.step && cp.step.lessonId === active.lessonId) {
          const idx = active.scenario.steps.findIndex((s) => s.id === cp.step?.stepId)
          if (idx >= 0) {
            active.index = idx
            active.base = { ...eng.counts }
            active.startedAt = Date.now()
            active.lastCheck = ''
            const resets = (progress[active.lessonId]?.steps[cp.step.stepId]?.resets ?? 0) + 1
            setProgress(active.lessonId, cp.step.stepId, { status: 'in-progress', resets })
            publish({
              t: 'step',
              lessonId: active.lessonId,
              stepId: cp.step.stepId,
              status: 'reset',
              index: idx,
              count: active.scenario.steps.length,
            })
          }
        }
        afterInput()
        return { restoredFiles: changed.length }
      }
      case 'mode': {
        const want = b['mode'] === 'gui' ? 'gui' : 'terminal'
        if (want !== mode) {
          if (want === 'gui') {
            await new Promise((r) => setTimeout(r, delay(900)))
            publish({ t: 'emacs', event: 'frame-opened', frame: 'gui' })
          }
          mode = want
          publish({ t: 'mode', mode })
          publishState(true)
        }
        return { mode }
      }
      case 'modules': {
        const enabled = (b['enabled'] as string[] | undefined) ?? modules
        const change = enabled.join(' ') !== modules.join(' ')
        modules = enabled
        publish({ t: 'modules', enabled, restarting: change })
        if (change) {
          setTimeout(() => {
            publish({ t: 'emacs', event: 'daemon-restarted', reason: 'module set changed' })
            publish({ t: 'modules', enabled, restarting: false })
          }, delay(1200))
        }
        return { restarting: change }
      }
      case 'restart-emacs': {
        if (!restarting) {
          restarting = true
          publish({ t: 'emacs', event: 'restarting', reason: 'restart requested' })
          setTimeout(() => {
            restarting = false
            publish({ t: 'emacs', event: 'daemon-restarted' })
            publishState(true)
            render()
          }, delay(1500))
        }
        return {}
      }
      case 'stuck':
        return { events: ring.slice(-200) }
      case 'status':
        return status()
      case 'describe':
        return eng.describeKeys(String(b['keys'] ?? ''))
      case 'drop-step':
        if (active?.lessonId === b['lessonId']) active = null
        return {}
    }
    throw Object.assign(new Error('unknown op ' + op), { status: 404, code: 'not_found' })
  }

  function status(): StatusResult {
    return {
      sessionId: SESSION,
      emacs: {
        version: '31.1',
        pid: 4242,
        uptimeMs: Date.now() - started,
        agent: true,
        responsive: true,
      },
      frames: { terminal: true, gui: mode === 'gui' },
      mode,
      step: active
        ? { lessonId: active.lessonId, stepId: active.scenario.steps[active.index]?.id ?? '' }
        : null,
      modules,
      versions: {
        emacs: '31.1 (fake)',
        rustc: '1.91.0',
        'rust-analyzer': '2026-10-20',
        python: '3.13.5',
        uv: '0.9.4',
        tmux: '3.5a',
      },
      activeWebSockets: subscribers.size + (pty ? 1 : 0) + rfbs.size,
    }
  }

  function sessionInfo(): SessionInfo {
    const info: SessionInfo = {
      id: SESSION,
      state: 'ready',
      createdAt: started,
      lastActiveAt: lastKeyAt,
      image: 'ghcr.io/zsy/emacs-playground/session:fake',
      gui: true,
      emacs: { version: '31.1', uptimeMs: Date.now() - started },
      mode,
    }
    if (active)
      info.step = {
        lessonId: active.lessonId,
        stepId: active.scenario.steps[active.index]?.id ?? '',
      }
    return info
  }

  /* ---------------- HTTP ---------------- */
  const authed = (req: IncomingMessage) =>
    opts.auth === 'none' || /(?:^|;\s*)pg_session=fake/.test(req.headers.cookie ?? '')

  function servePreview(res: ServerResponse, rel: string) {
    const path = decodeURIComponent(rel).replace(/^\/+/, '')
    if (path.includes('..')) return problem(res, 400, 'invalid_request', 'bad path')
    if (path === '' || path.endsWith('/')) {
      const dir = path
      const entries = new Map<string, { name: string; dir: boolean; size: number; mtime: number }>()
      for (const [p, v] of [...disk.entries(), ...blobs.entries()]) {
        if (!p.startsWith(dir)) continue
        const rest = p.slice(dir.length)
        const seg = rest.split('/')[0] as string
        const isDir = rest.includes('/')
        if (!entries.has(seg))
          entries.set(seg, { name: seg, dir: isDir, size: isDir ? 0 : v.length, mtime: Date.now() })
      }
      if (entries.size === 0 && dir !== '')
        return problem(res, 404, 'not_found', 'no such directory')
      return json(res, 200, { path: dir.replace(/\/$/, ''), entries: [...entries.values()] })
    }
    const blob = blobs.get(path)
    const text = disk.get(path)
    if (blob === undefined && text === undefined)
      return problem(res, 404, 'not_found', 'no such file')
    const headers: Record<string, string> = {
      'Content-Type': MIME[extname(path)] ?? 'text/plain; charset=utf-8',
      'Cache-Control': 'no-store',
      'X-Content-Type-Options': 'nosniff',
    }
    if (extname(path) === '.html')
      headers['Content-Security-Policy'] = 'sandbox allow-scripts allow-same-origin'
    res.writeHead(200, headers)
    res.end(blob ?? text)
  }

  function serveStatic(req: IncomingMessage, res: ServerResponse): boolean {
    if (!opts.staticDir) return false
    const url = new URL(req.url ?? '/', 'http://x')
    const root = resolve(opts.staticDir)
    let file = normalize(join(root, decodeURIComponent(url.pathname)))
    if (!file.startsWith(root)) return false
    if (!existsSync(file) || statSync(file).isDirectory()) file = join(root, 'index.html')
    if (!existsSync(file)) return false
    const ext = extname(file)
    res.writeHead(200, {
      'Content-Type': MIME[ext] ?? 'application/octet-stream',
      'Cache-Control': ext === '.html' ? 'no-cache' : 'public, max-age=3600',
    })
    res.end(readFileSync(file))
    return true
  }

  const server = createServer((req, res) => {
    void (async () => {
      const url = new URL(req.url ?? '/', 'http://x')
      const p = sessiondPath(url.pathname, opts.sessiond)
      const method = req.method ?? 'GET'
      try {
        if (p === '/healthz') return json(res, 200, { ok: true })
        if (p.startsWith('/api/v1/')) {
          const r = p.slice('/api/v1'.length)
          if (r === '/auth/config') return json(res, 200, { mode: 'local', authMode: opts.auth })
          if (r === '/auth/invite' && method === 'POST') {
            const b = await body(req)
            if (b['code'] === 'emacs')
              return noContent(res, {
                'Set-Cookie': 'pg_session=fake; Path=/; HttpOnly; SameSite=Lax',
              })
            return problem(res, 403, 'invalid_request', 'unknown invite code')
          }
          if (r === '/auth/logout' && method === 'POST')
            return noContent(res, { 'Set-Cookie': 'pg_session=; Path=/; Max-Age=0' })
          if (r.startsWith('/auth/oidc/'))
            return problem(res, 501, 'not_implemented', 'OIDC is not implemented in slice 1')
          if (!authed(req)) return problem(res, 401, 'unauthenticated', 'sign in first')
          if (r === '/me')
            return json(res, 200, {
              user: {
                id: 'local',
                displayName: opts.auth === 'invite' ? 'Invited learner' : 'Local learner',
                kind: opts.auth === 'invite' ? 'invite' : 'local',
              },
              mode: 'local',
              authMode: opts.auth,
              features: {
                gui: true,
                remoteBox: false,
                stuckReports: true,
                authorMode: opts.author,
              },
              version: 'fake',
            })
          if (r === '/sessions' && method === 'POST') return json(res, 201, sessionInfo())
          if (r === `/sessions/${SESSION}` && method === 'GET') return json(res, 200, sessionInfo())
          if (r === `/sessions/${SESSION}` && method === 'DELETE') return noContent(res)
          if (r === `/sessions/${SESSION}/restart-emacs` && method === 'POST')
            return json(res, 200, { ok: true, result: await rpc('restart-emacs', {}) })
          if (r.startsWith('/sessions/')) return problem(res, 404, 'not_found', 'no such session')
          if (r === '/curriculum') return json(res, 200, index)
          if (r.startsWith('/curriculum/lessons/')) {
            const id = decodeURIComponent(r.slice('/curriculum/lessons/'.length))
            const b = bundleFor(id)
            return b ? json(res, 200, b) : problem(res, 404, 'not_found', 'no such lesson: ' + id)
          }
          if (r.startsWith('/curriculum/assets/')) {
            if (opts.curriculumDir) {
              const rest = decodeURIComponent(r.slice('/curriculum/assets/'.length)).split('/')
              const f = join(
                opts.curriculumDir,
                'lessons',
                rest.slice(0, 2).join('/'),
                'assets',
                ...rest.slice(2),
              )
              if (existsSync(f) && !f.includes('..')) {
                res.writeHead(200, {
                  'Content-Type': MIME[extname(f)] ?? 'application/octet-stream',
                })
                return res.end(readFileSync(f))
              }
            }
            return problem(res, 404, 'not_found', 'no such asset')
          }
          if (r === '/progress')
            return json(res, 200, { lessons: progress, updatedAt: progressAt } satisfies Progress)
          if (r === '/progress/reset' && method === 'POST') {
            const b = await body(req)
            const id = String(b['lessonId'] ?? '')
            delete progress[id]
            if (active?.lessonId === id) active = null
            progressAt = Date.now()
            return noContent(res)
          }
          if (r === '/stuck' && method === 'POST') {
            const b = await body(req)
            log('stuck report', JSON.stringify(b))
            return json(res, 201, { id: String(Date.now()) })
          }
          return problem(res, 404, 'not_found', 'no such endpoint')
        }
        if (p.startsWith(`/s/${SESSION}/`)) {
          if (!authed(req)) return problem(res, 401, 'unauthenticated', 'sign in first')
          const r = p.slice(`/s/${SESSION}`.length)
          if (r.startsWith('/rpc/') && method === 'POST') {
            const op = r.slice('/rpc/'.length)
            try {
              const result = await rpc(op, await body(req))
              return json(res, 200, { ok: true, result })
            } catch (e) {
              const err = e as Error & { status?: number; code?: string }
              return problem(res, err.status ?? 500, err.code ?? 'internal', err.message)
            }
          }
          if (r.startsWith('/preview/')) return servePreview(res, r.slice('/preview/'.length))
          if (r === '/status') return json(res, 200, status())
          if (r === '/healthz') return json(res, 200, { ok: true })
          return problem(res, 404, 'not_found', 'no such session endpoint')
        }
        if (p.startsWith('/s/')) return problem(res, 404, 'not_found', 'no such session')
        if (p.startsWith('/fonts/'))
          return problem(res, 404, 'not_found', 'fonts are served by the gateway image')
        if (serveStatic(req, res)) return
        return problem(res, 404, 'not_found', 'not found')
      } catch (e) {
        log('error', e)
        if (!res.headersSent) problem(res, 500, 'internal', String(e))
      }
    })()
  })

  /* ---------------- WebSockets ---------------- */
  const rfbs = new Set<{ refresh(): void }>()
  // Raw frames are large: send at most four per second, and only in GUI mode.
  let rfbTimer: ReturnType<typeof setTimeout> | null = null
  function refreshRfb() {
    if (rfbTimer) return
    rfbTimer = setTimeout(() => {
      rfbTimer = null
      for (const r of rfbs) r.refresh()
    }, 250)
  }

  function hello(replayed: boolean): Event {
    const step = active
      ? { lessonId: active.lessonId, stepId: active.scenario.steps[active.index]?.id ?? '' }
      : null
    return {
      t: 'hello',
      seq,
      protocol: 1,
      sessionId: SESSION,
      mode,
      emacs: { version: '31.1', uptimeMs: Date.now() - started },
      frames: { terminal: true, gui: mode === 'gui' },
      step,
      modules,
      replayed,
    }
  }

  server.on('upgrade', (req, socket) => {
    const url = new URL(req.url ?? '/', 'http://x')
    url.pathname = sessiondPath(url.pathname, opts.sessiond)
    if (!url.pathname.startsWith(`/s/${SESSION}/`) || !authed(req)) {
      socket.end('HTTP/1.1 404 Not Found\r\n\r\n')
      return
    }
    const ws = acceptUpgrade(req, socket)
    if (!ws) return
    const r = url.pathname.slice(`/s/${SESSION}`.length)
    if (r === '/events') {
      const after = Number(url.searchParams.get('after') ?? '0')
      const first = ring[0]?.seq ?? seq + 1
      const canReplay = after > 0 && after >= first - 1 && after <= seq
      ws.sendJSON(hello(canReplay))
      if (canReplay) {
        for (const ev of ring) if ((ev.seq ?? 0) > after) ws.sendJSON(ev)
      } else {
        ws.sendJSON({ ...eng.state(mode), seq, ts: Date.now() })
      }
      subscribers.add(ws)
      ws.onMessage = (m) => {
        if ('text' in m && m.text.includes('"ping"')) ws.sendJSON({ t: 'pong' })
      }
      ws.onClose = () => subscribers.delete(ws)
      return
    }
    if (r === '/pty') {
      if (pty && !pty.closed) {
        pty.sendJSON({ t: 'replaced' })
        pty.close(4000, 'replaced')
      }
      pty = ws
      size = {
        cols: Number(url.searchParams.get('cols') ?? 120) || 120,
        rows: Number(url.searchParams.get('rows') ?? 36) || 36,
      }
      ws.sendJSON({ t: 'attached' })
      ws.sendBinary('\x1b[2J')
      render()
      ws.onMessage = (m) => {
        if ('binary' in m) {
          if (restarting) return
          feed(decodeInput(m.binary))
          return
        }
        try {
          const c = JSON.parse(m.text) as { t: string; cols?: number; rows?: number }
          if (c.t === 'resize' && c.cols && c.rows) {
            size = { cols: c.cols, rows: c.rows }
            ws.sendBinary('\x1b[2J')
            render()
          } else if (c.t === 'ping') ws.sendJSON({ t: 'pong' })
        } catch {
          // ignore
        }
      }
      ws.onClose = () => {
        if (pty === ws) pty = null
      }
      return
    }
    if (r === '/vnc') {
      if (mode !== 'gui') {
        ws.close(4002, 'gui_not_active')
        return
      }
      const handle = serveRfb(
        ws,
        {
          windows: () => eng.windows.map((_w, i) => ({ selected: i === eng.sel })),
          side: () => eng.windows.some((w) => w.side),
          title: () => 'Emacs (fake GUI frame)',
        },
        (key) => feed([normalizeKeysLoose(key)]),
      )
      rfbs.add(handle)
      ws.onClose = () => rfbs.delete(handle)
      return
    }
    ws.close(4404, 'unknown endpoint')
  })

  await new Promise<void>((r) => server.listen(opts.port, opts.host, r))
  const addr = server.address()
  const port = typeof addr === 'object' && addr ? addr.port : opts.port
  const url = `http://${opts.host}:${port}`
  log(
    `listening on ${url}${opts.staticDir ? ` (serving ${opts.staticDir})` : ''}; auth ${opts.auth}${opts.auth === 'invite' ? ' (code: emacs)' : ''}`,
  )
  return {
    url,
    port,
    close: () =>
      new Promise<void>((r) => {
        clearInterval(stuckTimer)
        eng.dispose()
        for (const c of subscribers) c.close(1001)
        pty?.close(1001)
        server.close(() => r())
        server.closeAllConnections()
      }),
  }
}

/* ---------------- CLI ---------------- */
function arg(name: string): string | undefined {
  const i = process.argv.indexOf(name)
  return i >= 0 ? process.argv[i + 1] : undefined
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const opts: FakeOptions = {
    port: Number(arg('--port') ?? process.env['PG_FAKE_PORT'] ?? 8090),
    host: arg('--host') ?? '127.0.0.1',
    auth: arg('--auth') === 'invite' ? 'invite' : 'none',
    author: process.argv.includes('--author'),
  }
  const exp = arg('--export-bundle')
  if (exp) {
    const n = exportBundle(exp)
    console.log(`[fake] wrote ${n} lessons to ${exp}`)
    process.exit(0)
  }
  if (process.argv.includes('--sessiond')) opts.sessiond = true
  const st = arg('--static')
  if (st) opts.staticDir = st
  const cur = arg('--curriculum')
  if (cur) opts.curriculumDir = cur
  const fake = await startFakeSession(opts)
  if (process.argv.includes('--vite')) {
    const vite = spawn(join(here, '..', 'node_modules', '.bin', 'vite'), [], {
      stdio: 'inherit',
      env: { ...process.env, PG_GATEWAY: fake.url },
      cwd: join(here, '..'),
    })
    vite.on('exit', (code) => process.exit(code ?? 0))
    const stop = () => {
      vite.kill('SIGTERM')
      void fake.close().then(() => process.exit(0))
    }
    process.on('SIGINT', stop)
    process.on('SIGTERM', stop)
  } else {
    const stop = () => void fake.close().then(() => process.exit(0))
    process.on('SIGINT', stop)
    process.on('SIGTERM', stop)
  }
}
