// Acceptance checks for docs/feedback/2026-10-27-fix-plan.md: one check per
// item of the newcomer review (B1..B18, L1, X3, X11, S1..S5, R1, R9, R10). Each
// check drives the running local stack (http://127.0.0.1:8080) through
// headless Chrome on CDP :9222 and asserts the behaviour the plan asks for,
// so before the fixes most checks fail; the plan records that baseline.
//
//   cd web && node e2e/review.mjs                  all checks
//   PG_REVIEW_ONLY=B1,B4 node e2e/review.mjs       a subset
//
// Writes artifacts/review/<id>.png and results.json. Every check starts a
// fresh browser context (empty localStorage) and loads its lesson with
// `reset`; all progress is reset when the run ends. Page loads are paced
// (pageLoad) because each one asks the gateway for the session, which it
// allows ten times a minute per user.
import { chromium } from 'playwright-core'
import { spawnSync } from 'node:child_process'
import { mkdirSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { inflateSync } from 'node:zlib'

const here = dirname(fileURLToPath(import.meta.url))
const repo = resolve(here, '../..')
const url = process.env.PG_URL ?? 'http://127.0.0.1:8080'
const cdp = process.env.PG_CDP ?? 'http://127.0.0.1:9222'
const out = process.env.OUT ?? join(repo, 'artifacts/review')
const only = (process.env.PG_REVIEW_ONLY ?? '').split(',').filter(Boolean)
const sessionToken = process.env.PG_SESSION_TOKEN ?? 'local'
mkdirSync(out, { recursive: true })

const L01 = '00-orientation/01-reading-the-screen'
const L02 = '00-orientation/02-getting-unstuck'
const L03 = '00-orientation/03-asking-emacs'
const L04 = '00-orientation/04-which-key-and-the-ticker'
const L11 = '01-moving-and-editing/01-motion'
const L12 = '01-moving-and-editing/02-kill-and-yank'
const L24 = '02-buffers-and-files/04-switch-save-kill'
const L31 = '03-windows-and-layouts/01-windows-and-frames'
const L35 = '03-windows-and-layouts/05-taming-pop-ups'
const L27 = '02-buffers-and-files/07-projects'

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

// Each page load posts /api/v1/sessions, which the gateway allows ten times a
// minute per user (internal/gateway/server.go createSession); a full run
// loads more pages than that, so loads wait for room under the limit.
const loads = []
async function pageLoad() {
  for (;;) {
    const now = Date.now()
    while (loads.length && now - loads[0] > 61000) loads.shift()
    if (loads.length < 8) break
    await sleep(61000 - (now - loads[0]) + 200)
  }
  loads.push(Date.now())
}
const browser = await chromium.connectOverCDP(cdp)

// --- HTTP ---------------------------------------------------------------

async function api(path, init = {}) {
  const res = await fetch(url + path, {
    ...init,
    headers: { 'content-type': 'application/json', origin: url, ...(init.headers ?? {}) },
  })
  const text = await res.text()
  let body = null
  try {
    body = text ? JSON.parse(text) : null
  } catch {
    body = text
  }
  return { status: res.status, body }
}
const post = (path, body) =>
  api(path, { method: 'POST', ...(body === undefined ? {} : { body: JSON.stringify(body) }) })
const rpc = (op, body = {}) => post('/s/local/rpc/' + op, body)
const resetLesson = (lessonId) => post('/api/v1/progress/reset', { lessonId })

/** Start over; falls back to per-lesson resets while the reset-all contract is missing. */
async function resetAll() {
  const r = await post('/api/v1/progress/reset')
  if (r.status === 204) return r.status
  const p = await api('/api/v1/progress')
  for (const id of Object.keys(p.body?.lessons ?? {})) await resetLesson(id)
  return r.status
}

const lessonCache = new Map()
async function lesson(id) {
  if (!lessonCache.has(id)) {
    const r = await api('/api/v1/curriculum/lessons/' + id)
    if (r.status !== 200) throw new Error(`lesson ${id}: ${r.status}`)
    lessonCache.set(id, r.body)
  }
  return lessonCache.get(id)
}
const steps = async (id) => (await lesson(id)).scenario.steps
const text = (v) => (typeof v === 'string' ? v : (v?.en ?? ''))

// --- Browser --------------------------------------------------------------

/** A fresh context and page with /events and /pty frames recorded. */
async function freshPage(viewport = { width: 1600, height: 1000 }) {
  const context = await browser.newContext({ viewport })
  const page = await context.newPage()
  const events = []
  const resizes = []
  page.on('websocket', (ws) => {
    if (/\/events/.test(ws.url())) {
      ws.on('framereceived', (f) => {
        try {
          const e = JSON.parse(String(f.payload))
          e._at = Date.now()
          events.push(e)
        } catch {
          /* not JSON */
        }
      })
    }
    if (/\/pty/.test(ws.url())) {
      ws.on('framesent', (f) => {
        if (typeof f.payload === 'string' && f.payload.includes('"resize"'))
          resizes.push({ at: Date.now(), payload: f.payload })
      })
    }
  })
  const h = {
    context,
    page,
    events,
    resizes,
    since: (t, pred = () => true) => events.filter((e) => e._at >= t && pred(e)),
    async waitEvent(t, pred, timeout, what) {
      const until = Date.now() + timeout
      for (;;) {
        const hit = events.find((e) => e._at >= t && pred(e))
        if (hit) return hit
        if (Date.now() > until) throw new Error(`no ${what} within ${timeout} ms`)
        await sleep(25)
      }
    },
    lastState: (t = 0) => events.filter((e) => e.t === 'state' && e._at >= t).at(-1) ?? null,
    async press(keys, gap = 250) {
      for (const k of keys) {
        await page.keyboard.press(k)
        await sleep(gap)
      }
    },
    async type(s, delay = 30) {
      await page.keyboard.type(s, { delay })
    },
    async focus() {
      await page
        .locator('.pg-terminal .xterm')
        .first()
        .click({ position: { x: 300, y: 120 } })
      await sleep(250)
    },
    screen: () =>
      page.evaluate(() =>
        typeof window.__pgScreenText === 'function' ? window.__pgScreenText() : [],
      ),
    rail: (heading) =>
      page.locator('section', { has: page.getByRole('heading', { name: heading }) }).first(),
    async where() {
      return (
        await h
          .rail('Where am I')
          .innerText({ timeout: 3000 })
          .catch(() => '')
      ).replace(/\s+/g, ' ')
    },
    async cards() {
      return (await h.rail('Did you mean').locator('h3').allInnerTexts()).map((s) =>
        s.replace(/\s+/g, ' '),
      )
    },
    async ticker() {
      return (
        await page
          .locator('[aria-label^="Key ticker"]')
          .first()
          .innerText({ timeout: 3000 })
          .catch(() => '')
      ).replace(/\s+/g, ' ')
    },
    active: () => page.locator('li[aria-current="step"]').first(),
    async headerSize() {
      const t = await page
        .locator('main')
        .getByText(/\d+×\d+/)
        .first()
        .innerText({ timeout: 3000 })
        .catch(() => '')
      const m = /(\d+)×(\d+)/.exec(t)
      return m ? { cols: Number(m[1]), rows: Number(m[2]), label: m[0] } : null
    },
    /** Opens a lesson page; the scenario must already be loaded. */
    async open(lessonId, goal) {
      await pageLoad()
      await page.goto(`${url}/lesson/${lessonId}`, { waitUntil: 'load' })
      await page.locator('.pg-terminal .xterm-screen').waitFor({ timeout: 30000 })
      await page
        .getByText('Loading the lesson into Emacs…')
        .waitFor({ state: 'detached', timeout: 60000 })
        .catch(() => undefined)
      if (goal)
        await h
          .active()
          .filter({ hasText: goal.slice(0, 24) })
          .waitFor({ timeout: 30000 })
          .catch(() => {
            throw new Error(`the step "${goal}" is not shown as the active step`)
          })
      await sleep(800)
    },
    /** Resets the lesson's progress, loads it at stepId with reset, opens the page. */
    async load(lessonId, stepId) {
      const all = await steps(lessonId)
      const st = stepId ? all.find((s) => s.id === stepId) : all[0]
      if (!st) throw new Error(`${lessonId} has no step ${stepId}`)
      // A lesson page still open makes its own lesson active again as soon
      // as another one loads (useScenarioLoad), and the page opened next
      // would then load its lesson at the resume target: leave it first.
      if (page.url().startsWith(url)) await page.goto('about:blank')
      await resetLesson(lessonId)
      const r = await rpc('scenario', { lessonId, stepId: st.id, reset: true })
      if (r.status !== 200) throw new Error('rpc scenario: ' + JSON.stringify(r.body))
      await h.open(lessonId, text(st.goal))
      return st
    },
    async close() {
      await context.close().catch(() => undefined)
    },
  }
  return h
}

/** Plays a demo string through rpc keys at playctl pacing (170 ms per chord, 70 per character). */
async function playDemo(demo) {
  const re = /"((?:[^"\\]|\\.)*)"|~(\d+)|(\S+)/g
  for (const m of demo.matchAll(re)) {
    if (m[1] !== undefined) {
      for (const ch of JSON.parse(`"${m[1]}"`)) {
        await rpc('keys', { text: ch })
        await sleep(70)
      }
    } else if (m[2] !== undefined) await sleep(Number(m[2]))
    else {
      await rpc('keys', { keys: m[3] })
      await sleep(170)
    }
  }
}

// --- PNG sampling (B16) -----------------------------------------------------

function decodePng(buf) {
  let pos = 8
  let width = 0
  let height = 0
  let colorType = 0
  const idat = []
  while (pos < buf.length) {
    const len = buf.readUInt32BE(pos)
    const type = buf.toString('ascii', pos + 4, pos + 8)
    const data = buf.subarray(pos + 8, pos + 8 + len)
    if (type === 'IHDR') {
      width = data.readUInt32BE(0)
      height = data.readUInt32BE(4)
      colorType = data[9]
    } else if (type === 'IDAT') idat.push(data)
    pos += 12 + len
  }
  const bpp = colorType === 6 ? 4 : 3
  const raw = inflateSync(Buffer.concat(idat))
  const stride = width * bpp
  const px = Buffer.alloc(height * stride)
  for (let y = 0; y < height; y++) {
    const f = raw[y * (stride + 1)]
    for (let x = 0; x < stride; x++) {
      const v = raw[y * (stride + 1) + 1 + x]
      const a = x >= bpp ? px[y * stride + x - bpp] : 0
      const b = y > 0 ? px[(y - 1) * stride + x] : 0
      const c = x >= bpp && y > 0 ? px[(y - 1) * stride + x - bpp] : 0
      let r = v
      if (f === 1) r = v + a
      else if (f === 2) r = v + b
      else if (f === 3) r = v + ((a + b) >> 1)
      else if (f === 4) {
        const p = a + b - c
        const pa = Math.abs(p - a)
        const pb = Math.abs(p - b)
        const pc = Math.abs(p - c)
        r = v + (pa <= pb && pa <= pc ? a : pb <= pc ? b : c)
      }
      px[y * stride + x] = r & 0xff
    }
  }
  return { width, height, bpp, px }
}

/** The most frequent colour of a screenshot region (the background behind text). */
async function dominantColor(page, clip) {
  const png = decodePng(await page.screenshot({ clip, type: 'png' }))
  const counts = new Map()
  for (let i = 0; i < png.px.length; i += png.bpp) {
    const k = `${png.px[i]},${png.px[i + 1]},${png.px[i + 2]}`
    counts.set(k, (counts.get(k) ?? 0) + 1)
  }
  return [...counts.entries()]
    .sort((a, b) => b[1] - a[1])[0][0]
    .split(',')
    .map(Number)
}
function luminance([r, g, b]) {
  const lin = (c) => {
    const s = c / 255
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  }
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)
}

// --- Runner ------------------------------------------------------------------

const results = []
async function check(id, name, fn, timeoutMs = 150000) {
  if (only.length && !only.includes(id)) return
  const t0 = Date.now()
  const evidence = []
  const note = (s) => evidence.push(s)
  const h = await freshPage(fn.viewport)
  let error = null
  try {
    await Promise.race([
      fn(h, note),
      sleep(timeoutMs).then(() => {
        throw new Error(`timed out after ${timeoutMs} ms`)
      }),
    ])
  } catch (e) {
    error = String(e?.message ?? e).slice(0, 1200)
  }
  await h.page.screenshot({ path: join(out, `${id}.png`) }).catch(() => undefined)
  await h.close()
  const r = { id, name, pass: !error, ms: Date.now() - t0, evidence, ...(error ? { error } : {}) }
  results.push(r)
  console.error(`${r.pass ? 'PASS' : 'FAIL'} ${id} ${name} (${r.ms} ms)`)
  for (const e of evidence) console.error('    ' + e)
  if (error) console.error('    ERROR ' + error)
}
const assert = (ok, msg) => {
  if (!ok) throw new Error(msg)
}
const keyAfter = (h, t) => h.waitEvent(t, (e) => e.t === 'key', 3000, 'key event')

// --- Checks -----------------------------------------------------------------

await check('B1', 'End, Home and Delete keys', async (h, note) => {
  await h.load(L11)
  await h.focus()
  await h.press(['Alt+Shift+Comma', 'Control+n', 'Control+a'])
  let t = Date.now()
  await h.page.keyboard.press('End')
  let k = await keyAfter(h, t)
  await sleep(400)
  note(`End -> ${k.keys} ${k.cmd}; modified=${h.lastState(t)?.modified}`)
  assert(k.keys === '<end>' && k.cmd === 'move-end-of-line', 'End is not <end> move-end-of-line')
  assert(h.lastState(t)?.modified === false, 'End modified the buffer')
  t = Date.now()
  await h.page.keyboard.press('Home')
  k = await keyAfter(h, t)
  note(`Home -> ${k.keys} ${k.cmd}`)
  assert(k.keys === '<home>' && /beginning-of-line/.test(k.cmd), 'Home is not <home>')
  t = Date.now()
  await h.page.keyboard.press('Delete')
  k = await keyAfter(h, t)
  note(`Delete -> ${k.keys} ${k.cmd}`)
  assert(
    k.keys === '<deletechar>' && /^delete-(forward-)?char$/.test(k.cmd),
    'Delete is not <deletechar> deleting forward',
  )
  await h.press(['Control+/'])
})

await check(
  'B2',
  '2.4: Watch it from a moved state, Reset this step, Go to step',
  async (h, note) => {
    const all = await steps(L24)
    const first = await h.load(L24)
    await h.focus()
    await h.press(['Control+x', 'Control+f', 'Control+a', 'Control+k'], 200)
    await h.type('/work/hello/src/main.rs')
    await h.press(['Enter'], 1000)
    let t = Date.now()
    await h
      .active()
      .getByRole('button', { name: /Watch it/ })
      .click()
    const dem = await h
      .waitEvent(
        t,
        (e) => e.t === 'step' && e.stepId === first.id && /demonstrated|complete/.test(e.status),
        45000,
        'demonstrated event for ' + first.id,
      )
      .catch((e) => {
        const c = h.since(t, (x) => x.t === 'check').at(-1)
        throw new Error(`${e.message}; last check ${JSON.stringify(c?.conditions)}`)
      })
    note(`(a) Watch it from /work/hello/src/main.rs -> step ${dem.status}`)
    assert(dem.status === 'demonstrated', 'the demo completed the step as done (contract b)')
    t = Date.now()
    await h
      .active()
      .getByRole('button', { name: /Do it yourself/ })
      .click()
    await h.waitEvent(t, (e) => e.t === 'step' && e.status === 'reset', 15000, 'reset event')
    await sleep(800)
    await h.focus()
    await h.press(['Control+x', 'Control+f', 'Control+a', 'Control+k'], 200)
    await h.type('/work/projects/polyglot/README.md')
    await h.press(['Enter'], 1200)
    t = Date.now()
    await h.page.getByRole('button', { name: 'Reset this step' }).first().click()
    const dlg = h.page.getByRole('dialog')
    if (await dlg.isVisible().catch(() => false))
      await dlg.getByRole('button', { name: /^Reset/ }).click()
    await h.waitEvent(t, (e) => e.t === 'step' && e.status === 'reset', 15000, 'reset event')
    await sleep(2500)
    const st = h.lastState(t)
    note(`(b) after Reset: file=${st?.file} windows=${st?.windows?.map((w) => w.buf).join(',')}`)
    assert(st?.file === '/work/projects/hello/README.md', 'Reset did not return to hello/README.md')
    assert(!st.windows.some((w) => /polyglot/.test(w.buf)), 'a polyglot buffer is still shown')
    const sw = all.find((s) => s.id === 'switch')
    t = Date.now()
    const r = await rpc('scenario', { lessonId: L24, stepId: 'switch' })
    assert(r.status === 200, 'rpc scenario switch: ' + JSON.stringify(r.body))
    await h.waitEvent(
      t,
      (e) => e.t === 'step' && e.stepId === 'switch' && e.status === 'loaded',
      90000,
      'loaded event for switch',
    )
    await sleep(1000)
    const t2 = Date.now()
    await playDemo(sw.demo)
    const done = await h
      .waitEvent(
        t2,
        (e) => e.t === 'step' && e.stepId === 'switch' && e.status === 'complete',
        15000,
        'complete event for switch',
      )
      .catch((e) => {
        const c = h.since(t2, (x) => x.t === 'check').at(-1)
        throw new Error(`${e.message}; last check ${JSON.stringify(c?.conditions)}`)
      })
    note(`(c) Go to step "switch", then its demo -> ${done.status}`)
  },
)

await check('B3', '3.1 step 2 with the learner’s own comment', async (h, note) => {
  await h.load(L31, 'split')
  await h.focus()
  let t = Date.now()
  await h.press(['Control+x', '2'], 300)
  await h.waitEvent(
    t,
    (e) => e.t === 'step' && e.stepId === 'split' && e.status === 'complete',
    5000,
    'split complete',
  )
  await sleep(800)
  t = Date.now()
  await h.press(['Control+x', 'o', 'Alt+Shift+Period'], 300)
  await h.type('// my own words')
  const ev = await h
    .waitEvent(
      t,
      (e) => e.t === 'step' && e.stepId === 'edit' && e.status === 'complete',
      6000,
      'edit complete',
    )
    .catch((e) => {
      const c = h.since(t, (x) => x.t === 'check' && x.stepId === 'edit').at(-1)
      throw new Error(`${e.message}; last check ${JSON.stringify(c?.conditions)}`)
    })
  note(`edit -> ${ev.status}`)
})

await check('B4', 'no stale ESC prefix after ESC ESC ESC', async (h, note) => {
  await h.load(L02, 'escape')
  await h.focus()
  await h.press(['Control+x', '2', 'Control+x', '3', 'Alt+x'], 300)
  await sleep(600)
  for (const round of [1, 2]) {
    const t = Date.now()
    await h.press(['Escape', 'Escape', 'Escape'], 150)
    const k = await h.waitEvent(
      t,
      (e) => e.t === 'key' && e.cmd === 'keyboard-escape-quit',
      3000,
      'keyboard-escape-quit',
    )
    await sleep(1200)
    const late = h.since(k._at, (e) => e.t === 'prefix' && e.keys.trim() !== '' && e.seq > k.seq)
    const where = await h.where()
    note(
      `round ${round}: prefix events after the key event: ${late.length}; where has prefix row: ${/is a prefix/.test(where)}`,
    )
    assert(late.length === 0, 'a prefix event followed keyboard-escape-quit')
    assert(!/is a prefix/.test(where), 'Where am I still says ESC is a prefix')
  }
})

await check('B5', 'Start over and honest step ticks', async (h, note) => {
  const r = await post('/api/v1/progress/reset')
  note(`POST /progress/reset with no body -> ${r.status}`)
  assert(r.status === 204, 'reset with no body is not 204 (contract a)')
  let p = await api('/api/v1/progress')
  const started = Object.entries(p.body?.lessons ?? {}).filter(([, l]) =>
    Object.values(l.steps ?? {}).some((s) => s.status !== 'not-started'),
  )
  assert(started.length === 0, `progress not empty: ${started.map(([id]) => id).join(' ')}`)
  const all = await steps(L03)
  for (const s of all) {
    await api('/internal/sessions/local/progress', {
      method: 'POST',
      headers: { authorization: `Bearer ${sessionToken}` },
      body: JSON.stringify({ lessonId: L03, stepId: s.id, status: 'done' }),
    })
  }
  await rpc('scenario', { lessonId: L03, stepId: all[0].id, reset: true })
  await h.open(L03)
  const activeCount = await h.page.locator('li[aria-current="step"]').count()
  const later = await h.page.$$eval('li[aria-current="step"] ~ li', (els) =>
    els.map((e) => `${e.className} ${e.innerText.slice(0, 40)}`),
  )
  const ticked = later.filter((s) => /_done_|Done:/.test(s))
  note(
    `0.3 with every step recorded done: active steps ${activeCount}, ticked after it ${ticked.length}`,
  )
  assert(activeCount === 1, 'the engine’s active step is not shown as active (X1)')
  assert(ticked.length === 0, 'steps after the active one are ticked')
  await pageLoad()
  await h.page.goto(url + '/', { waitUntil: 'load' })
  await h.page.getByRole('button', { name: 'Start over' }).first().click({ timeout: 10000 })
  const dlg = h.page.getByRole('dialog')
  await dlg.waitFor({ timeout: 3000 })
  await dlg.getByRole('button', { name: 'Start over' }).click()
  await sleep(1500)
  p = await api('/api/v1/progress')
  const left = Object.values(p.body?.lessons ?? {}).filter((l) =>
    Object.values(l.steps ?? {}).some((s) => s.status !== 'not-started'),
  ).length
  note(`after the course map's Start over: lessons with progress ${left}`)
  assert(left === 0, 'Start over on the course map left progress')
  await pageLoad()
  await h.page.goto(url + '/session', { waitUntil: 'load' })
  // The Session page is a lazy chunk: wait for it before counting.
  const onSession = await h.page
    .getByRole('button', { name: 'Start over' })
    .first()
    .waitFor({ timeout: 10000 })
    .then(() => h.page.getByRole('button', { name: 'Start over' }).count())
    .catch(() => 0)
  note(`Session page Start over buttons: ${onSession}`)
  assert(onSession > 0, 'the Session page has no Start over')
  await resetAll()
})

await check('B6', 'Watch it demonstrates; Do it yourself', async (h, note) => {
  const st = await h.load(L02, 'search')
  let t = Date.now()
  await h
    .active()
    .getByRole('button', { name: /Watch it/ })
    .click()
  await sleep(1300)
  const caption = await h.page
    .locator('[aria-label="Demo caption"]')
    .innerText({ timeout: 1500 })
    .catch(() => '')
  note(`caption while playing: ${JSON.stringify(caption.slice(0, 80))}`)
  const dem = await h.waitEvent(
    t,
    (e) => e.t === 'step' && e.stepId === 'search' && /demonstrated|complete/.test(e.status),
    20000,
    'step event for search',
  )
  await sleep(500)
  const keys = h.since(t, (e) => e.t === 'key')
  const span = keys.length ? keys.at(-1)._at - keys[0]._at : 0
  note(`step ${dem.status}; demo keys span ${span} ms (${keys.map((k) => k.keys).join(' ')})`)
  assert(dem.status === 'demonstrated', 'Watch it completed the step')
  assert(span >= 3000, 'the demo is not at human pace (600 ms per chord, 120 per character)')
  assert(/C-s/.test(caption), 'no running caption with the keys')
  assert(
    (await h.active().innerText()).includes(text(st.goal).slice(0, 20)),
    'the step is no longer active after the demo',
  )
  t = Date.now()
  await h
    .active()
    .getByRole('button', { name: /Do it yourself/ })
    .click()
  await h.waitEvent(t, (e) => e.t === 'step' && e.status === 'reset', 15000, 'reset event')
  await sleep(800)
  await h.focus()
  t = Date.now()
  await h.press(['Control+s'], 400)
  await h.type('zzz', 120)
  await h.press(['Control+g', 'Control+g'], 500)
  await h.waitEvent(
    t,
    (e) => e.t === 'step' && e.stepId === 'search' && e.status === 'complete',
    6000,
    'complete by the learner',
  )
  await sleep(800)
  const p = await api('/api/v1/progress')
  const status = p.body?.lessons?.[L02]?.steps?.search?.status
  note(`progress for search: ${status}`)
  assert(status === 'done', 'the learner’s completion is not recorded done')
})

await check('B7', 'no terminal resize while a demo plays', async (h, note) => {
  await h.load(L24)
  const before = await h.headerSize()
  const t = Date.now()
  await h
    .active()
    .getByRole('button', { name: /Watch it/ })
    .click()
  await sleep(1500)
  const during = await h.headerSize()
  await h
    .waitEvent(
      t,
      (e) => e.t === 'step' && /demonstrated|complete/.test(e.status),
      45000,
      'end of demo',
    )
    .catch(() => undefined)
  await sleep(1000)
  const after = await h.headerSize()
  const resizes = h.resizes.filter((r) => r.at >= t)
  note(
    `size before ${before?.label}, during ${during?.label}, after ${after?.label}; resize frames ${resizes.length}`,
  )
  assert(
    before && during && before.label === during.label && during.label === after?.label,
    'the terminal size changed',
  )
  assert(resizes.length === 0, 'the page resized the pty during the demo')
})

await check('B8', 'habit cards respect the lesson and reloads', async (h, note) => {
  await h.load(L11)
  await h.focus()
  await h.press(['Control+b', 'Control+l'], 400)
  await sleep(600)
  let cards = await h.cards()
  note(`1.1 after C-b C-l: ${JSON.stringify(cards)}`)
  assert(!cards.some((c) => /C-b is not|C-l is not/.test(c)), 'cards for keys 1.1 teaches')
  // 3.5 build says "clear the offered command (`C-a C-k`)": two commands in
  // one span, both instructed (the newcomer's case, review screenshot 145).
  await h.load(L35, 'build')
  await h.focus()
  await h.press(['Control+x', 'p', 'c'], 600)
  await h.press(['Control+a'], 300)
  await h.press(['Control+k'], 500)
  await h.type('sh build.sh')
  await h.press(['Enter'], 2500)
  cards = await h.cards()
  note(`3.5 build after C-x p c, C-a C-k, sh build.sh: ${JSON.stringify(cards)}`)
  assert(
    !cards.some((c) => /C-a is not|C-k is not/.test(c)),
    'cards for C-a C-k, which 3.5 asks for',
  )
  await h.load(L31, 'split')
  await h.focus()
  await h.press(['Control+x', '2', 'Control+x', '3'], 400)
  await sleep(800)
  cards = await h.cards()
  note(`3.1 after C-x 2 C-x 3: ${JSON.stringify(cards)}`)
  assert(
    !cards.some((c) => /Three windows/.test(c)),
    '"Three windows" card while 3.1 asks for them',
  )
  await h.load(L01)
  await h.focus()
  await h.press(['Control+s'], 800)
  cards = await h.cards()
  note(`0.1 after C-s: ${JSON.stringify(cards)}`)
  assert(
    cards.some((c) => /C-s is not save/.test(c)),
    'the C-s card did not fire in 0.1',
  )
  await h.press(['Control+g'], 400)
  await pageLoad()
  await h.page.reload({ waitUntil: 'load' })
  await h.page.locator('.pg-terminal .xterm-screen').waitFor({ timeout: 30000 })
  await sleep(1500)
  await h.focus()
  await h.press(['Control+s'], 800)
  cards = await h.cards()
  note(`0.1 after a reload and C-s: ${JSON.stringify(cards)}`)
  await h.press(['Control+g'], 300)
  assert(!cards.some((c) => /C-s is not save/.test(c)), 'the C-s card fired again after a reload')
})

await check('B9', 'dismissing one card; no silent expiry; Rescue above cards', async (h, note) => {
  await h.load(L03)
  await h.focus()
  await h.press(['Control+f', 'Control+b', 'Control+v'], 350)
  await sleep(600)
  let cards = await h.cards()
  note(`after C-f C-b C-v: ${JSON.stringify(cards)}`)
  assert(
    ['C-f', 'C-b', 'C-v'].every((k) => cards.some((c) => c.includes(`${k} is not`))),
    'three habit cards expected',
  )
  await h
    .rail('Did you mean')
    .locator('div', { has: h.page.locator('h3', { hasText: 'C-f is not' }) })
    .last()
    .getByRole('button', { name: 'Dismiss' })
    .click()
  await sleep(600)
  cards = await h.cards()
  note(`after dismissing C-f: ${JSON.stringify(cards)}`)
  assert(!cards.some((c) => c.includes('C-f is not')), 'the dismissed card is still there')
  assert(
    cards.some((c) => c.includes('C-b is not')) && cards.some((c) => c.includes('C-v is not')),
    'dismissing removed other cards',
  )
  await h.focus()
  for (let i = 0; i < 6; i++) await h.press(['Control+f', 'Control+b'], 150)
  await sleep(600)
  cards = await h.cards()
  note(`after 12 more commands: ${JSON.stringify(cards)}`)
  assert(
    cards.some((c) => c.includes('C-b is not')) && cards.some((c) => c.includes('C-v is not')),
    'cards expired silently',
  )
  const order = await h.page.$$eval('h2, h3', (els) => els.map((e) => e.textContent.trim()))
  const rescue = order.findIndex((s) => /^Rescue/.test(s))
  const nudges = order.findIndex((s) => /^Did you mean/.test(s))
  note(`rail heading order: Rescue ${rescue}, Did you mean ${nudges}`)
  assert(rescue >= 0 && nudges >= 0 && rescue < nudges, 'Rescue is not above "Did you mean"')
})

await check('B10', 'ticker labels and collapsed typing', async (h, note) => {
  await h.load(L12)
  await h.focus()
  const want = [
    ['Control+k', 'kill-line'],
    ['Control+y', 'yank'],
    ['Alt+y', 'yank-pop'],
  ]
  for (const [k, cmd] of want) {
    const t = Date.now()
    await h.page.keyboard.press(k)
    const ev = await keyAfter(h, t)
    await sleep(300)
    note(`${k} -> ${ev.cmd}`)
    assert(ev.cmd === cmd, `${k} reported as ${ev.cmd}, not ${cmd}`)
  }
  await h.press(['Control+g', 'Control+x', 'Control+f'], 300)
  await h.type('Car')
  const t = Date.now()
  await h.page.keyboard.press('Tab')
  const tab = await h.waitEvent(t, (e) => e.t === 'key' && e.keys === 'TAB', 3000, 'TAB key event')
  note(`TAB in find-file -> ${tab.cmd}`)
  await h.press(['Control+g', 'Control+g'], 300)
  assert(tab.cmd === 'minibuffer-complete', 'minibuffer TAB is not minibuffer-complete')
  await h.type('hello', 60)
  await sleep(700)
  const ticker = await h.ticker()
  note(`ticker after typing hello: ${ticker.slice(0, 120)}`)
  assert(/typed\s*5/i.test(ticker), 'typing is not collapsed into one entry')
})

await check('B11', 'one Where am I card per prompt type', async (h, note) => {
  await h.load(L01)
  await h.focus()
  await h.press(['Alt+x'], 700)
  let w = await h.where()
  note(`M-x: ${w.slice(0, 200)}`)
  await h.press(['Control+g'], 400)
  assert(
    /M-x is waiting/.test(w) && !/A prompt is open at the bottom/.test(w),
    'M-x shows the generic card',
  )
  await h.press(['Alt+g', 'g'], 700)
  w = await h.where()
  note(`M-g g: ${w.slice(0, 200)}`)
  await h.press(['Control+g'], 400)
  assert(!/TAB completes names/.test(w), 'Goto line says TAB completes names')
  await h.press(['Control+x', 'Control+f'], 700)
  w = await h.where()
  note(`C-x C-f: ${w.slice(0, 200)}`)
  await h.press(['Control+g'], 400)
  assert(
    /asking for a file/.test(w) && !/A prompt is open at the bottom/.test(w),
    'the file prompt row does not match',
  )
})

await check('B12', 'GUI then Terminal: no lost X connection', async (h, note) => {
  await h.load(L35)
  const before = h
    .lastState()
    ?.windows?.map((w) => w.buf)
    .join(',')
  await h.page.getByRole('button', { name: 'GUI', exact: true }).click()
  await sleep(8000)
  const t = Date.now()
  await h.page.getByRole('button', { name: 'Terminal', exact: true }).click()
  await sleep(4000)
  const echoes = h.since(t, (e) => e.t === 'echo' && /Connection lost/.test(e.text))
  const screen = (await h.screen()).join('\n')
  const after =
    h
      .lastState(t)
      ?.windows?.map((w) => w.buf)
      .join(',') ?? before
  note(
    `echo "Connection lost": ${echoes.length}; on screen: ${/Connection lost/.test(screen)}; windows ${before} -> ${after}`,
  )
  assert(
    echoes.length === 0 && !/Connection lost/.test(screen),
    'Emacs reported the lost X connection',
  )
  assert(after === before, 'the window layout changed')
})

await check('B13', 'the GUI frame fills the pane at scale 1', async (h, note) => {
  await h.load(L35)
  await h.page.getByRole('button', { name: 'GUI', exact: true }).click()
  await sleep(9000)
  const c = await h.page.evaluate(() => {
    const cs = [...document.querySelectorAll('canvas')].filter(
      (x) => x.offsetParent && x.clientWidth > 100,
    )
    const x = cs.sort((a, b) => b.clientWidth * b.clientHeight - a.clientWidth * a.clientHeight)[0]
    if (!x) return null
    const p = x.parentElement
    return {
      w: x.width,
      h: x.height,
      cw: x.clientWidth,
      ch: x.clientHeight,
      pw: p.clientWidth,
      ph: p.clientHeight,
    }
  })
  await h.page.screenshot({ path: join(out, 'B13-gui.png') })
  await h.page.getByRole('button', { name: 'Terminal', exact: true }).click()
  await sleep(3000)
  note(`canvas ${JSON.stringify(c)}`)
  assert(c, 'no visible VNC canvas')
  assert(
    Math.abs(c.cw - c.pw) <= 4 && Math.abs(c.ch - c.ph) <= 4,
    'the canvas does not fill its pane',
  )
  assert(
    Math.abs(c.w - c.cw) <= 2 && Math.abs(c.h - c.ch) <= 2,
    'the X screen is scaled into the pane',
  )
})

await check('B14', 'Focus mode says what it does', async (h, note) => {
  await h.load(L01)
  const btn = h.page.getByRole('button', { name: 'Focus mode' }).first()
  await btn.click()
  await sleep(1500)
  const pressed = await h.page
    .getByRole('button', { name: /Focus mode/ })
    .first()
    .getAttribute('aria-pressed')
    .catch(() => null)
  const banner = await h.page
    .getByRole('status')
    .filter({ hasText: /Focus mode/ })
    .count()
  const lessonVisible = await h.page
    .locator('aside h1')
    .first()
    .isVisible()
    .catch(() => false)
  note(`aria-pressed ${pressed}; banners ${banner}; lesson pane visible ${lessonVisible}`)
  assert(banner > 0, 'no banner explains focus mode')
  assert(pressed === 'true' || !lessonVisible, 'focus mode did not turn on')
  assert(!lessonVisible, 'the lesson pane is still shown in focus mode')
  await h.page
    .getByRole('button', { name: /Leave focus mode|Focus mode/ })
    .first()
    .click()
  await sleep(1000)
  const back = await h.page
    .locator('aside h1')
    .first()
    .isVisible()
    .catch(() => false)
  note(`after leaving: lesson pane visible ${back}`)
  assert(back, 'leaving focus mode did not restore the layout')
})

await check('B15', 'no M-x abbreviation tip', async (h, note) => {
  await h.load(L01, 'minibuffer')
  await h.focus()
  await h.press(['Alt+x'], 400)
  await h.type('emacs-version')
  const t = Date.now()
  await h.page.keyboard.press('Enter')
  await sleep(4000)
  const tips = h.since(t, (e) => e.t === 'echo' && /You can run the command/.test(e.text))
  note(
    `echo events: ${h
      .since(t, (e) => e.t === 'echo')
      .map((e) => e.text.slice(0, 50))
      .join(' | ')}`,
  )
  assert(tips.length === 0, 'the abbreviation tip still shows')
})

await check('B16', 'the selected mode line is brighter; no wb', async (h, note) => {
  await h.load(L01)
  await sleep(500)
  const rows = await h.screen()
  const ml = rows
    .map((r, i) => [i, r])
    .filter(([, r]) => /^[-:%*UuR]{1,3}[-:%*]{2,4}\s+\S/.test(r) && /\bL\d+\b/.test(r))
  const box = await h.page.locator('.pg-terminal .xterm-screen').first().boundingBox()
  assert(ml.length >= 2 && box, `expected two mode lines, found ${ml.length}`)
  const rh = box.height / rows.length
  const colors = []
  // Sample each mode line over its whole height: a thin strip through the
  // middle of the row lands on the `----` fill and measures the foreground.
  for (const [i] of ml.slice(0, 2)) {
    colors.push(
      await dominantColor(h.page, {
        x: Math.round(box.x + box.width * 0.55),
        y: Math.round(box.y + rh * i + 1),
        width: Math.round(box.width * 0.4),
        height: Math.max(2, Math.floor(rh) - 2),
      }),
    )
  }
  const st = h.lastState()
  const selectedTop = st?.windows?.[0]?.selected === true
  const [lTop, lBottom] = colors.map(luminance)
  const sel = selectedTop ? lTop : lBottom
  const other = selectedTop ? lBottom : lTop
  const ratio = (Math.max(sel, other) + 0.05) / (Math.min(sel, other) + 0.05)
  note(
    `mode-line backgrounds ${JSON.stringify(colors)}; selected ${selectedTop ? 'top' : 'bottom'}; contrast ${ratio.toFixed(2)}`,
  )
  assert(sel > other && ratio >= 2.0, 'the selected mode line is not clearly brighter')
  // Lesson 0.1 runs without the editing module; ws-butler comes with it,
  // so look for its lighter in a lesson that has it (1.1).
  await h.load(L11)
  await sleep(500)
  const wbRows = await h.screen()
  const wb = wbRows.some((r) => / wb\)/.test(r))
  const where = await h.where()
  note(`1.1: " wb)" on screen: ${wb}; Where am I mentions Ws-butler: ${/Ws-butler/.test(where)}`)
  assert(!wb && !/Ws-butler/.test(where), 'ws-butler still shows')
})

await check('B17', '0.2 buffer-list step text and ibuffer', async (h, note) => {
  await h.load(L02, 'buffer-list')
  const detail = (await h.active().innerText()).replace(/\s+/g, ' ')
  note(`step text: ${detail.slice(0, 220)}`)
  assert(
    !/lists every buffer in a second window/.test(detail),
    'the text still promises a second window',
  )
  assert(
    /takes over|this window|same window/i.test(detail),
    'the text does not cover the list taking over the window',
  )
  await h.focus()
  const t = Date.now()
  await h.press(['Control+x', 'Control+b'], 900)
  await h.press(['q'], 300)
  await h.waitEvent(
    t,
    (e) => e.t === 'step' && e.stepId === 'buffer-list' && e.status === 'complete',
    4000,
    'buffer-list complete',
  )
})

await check('B18', 'help menu condition before the second key', async (h, note) => {
  await h.load(L03, 'menu')
  await h.focus()
  const t = Date.now()
  await h.press(['Control+h', '?'], 200)
  const c = await h
    .waitEvent(
      t,
      (e) => e.t === 'check' && e.stepId === 'menu' && e.conditions?.[0]?.ok,
      2000,
      'menu/1 met',
    )
    .catch((e) => e)
  note(`menu/1 met before b: ${!(c instanceof Error)}`)
  await h.press(['b'], 300)
  await h.waitEvent(
    t,
    (e) => e.t === 'step' && e.stepId === 'menu' && e.status === 'complete',
    4000,
    'menu complete',
  )
  assert(!(c instanceof Error), 'the help-menu condition waits for the second key')
})

await check('L1', 'check latency', async (h, note) => {
  await h.load(L01)
  await h.focus()
  const plan = [
    ['Control+x', 'o'],
    ['Control+e', '!'],
    ['Control+n', 'Control+n', 'Control+n', 'Control+n'],
    ['Control+x', 'Control+s'],
  ]
  const deltas = []
  for (const ks of plan) {
    const t = Date.now()
    for (const k of ks) {
      await h.page.keyboard.press(k)
      await sleep(120)
    }
    const c = await h.waitEvent(t, (e) => e.t === 'check' && e.complete, 5000, 'complete check')
    const k = h.since(t, (e) => e.t === 'key' && e.seq < c.seq).at(-1)
    deltas.push(c.ts - k.ts)
    await sleep(800)
  }
  const sorted = [...deltas].sort((a, b) => a - b)
  const median = (sorted[1] + sorted[2]) / 2
  note(`key -> check deltas ${deltas.join(', ')} ms; median ${median}`)
  assert(median < 100, 'median key -> check is not under 100 ms')
})

await check('X3', 'a reset lesson starts README.md at line 1', async (h, note) => {
  await h.load(L01)
  await h.focus()
  await h.press(['Control+x', 'o', 'Control+n', 'Control+n', 'Control+n', 'Control+n'], 250)
  await sleep(500)
  note(`before the reload: line ${h.lastState()?.line}`)
  await h.load(L01)
  await h.focus()
  const t = Date.now()
  await h.press(['Control+x', 'o'], 600)
  const st = h.lastState(t)
  note(`after the reset reload and C-x o: ${st?.buf} line ${st?.line}`)
  assert(st?.buf === 'README.md' && st?.line === 1, 'README.md kept the previous visit’s line')
})

await check('X11', '2.7 compile needs the learner’s own build', async (h, note) => {
  const lastCheck = (t) =>
    h.since(t, (e) => e.t === 'check' && e.stepId === 'compile').at(-1)?.conditions ?? []
  const met = (t) =>
    lastCheck(t)
      .map((r) => `${r.id}:${r.ok ? 'Y' : 'n'}`)
      .join(' ')
  const completed = (t) =>
    h.since(t, (e) => e.t === 'step' && e.stepId === 'compile' && e.status === 'complete').length
  // An earlier build (lesson 3.5 builds the same crate) leaves *compilation*.
  await h.load(L35, 'build')
  await h.focus()
  let t = Date.now()
  await playDemo((await steps(L35))[0].demo)
  await h.waitEvent(
    t,
    (e) => e.t === 'step' && e.stepId === 'build' && e.status === 'complete',
    15000,
    '3.5 build complete',
  )
  // 2.7 loads at its compile step: C-x p c alone must not complete it.
  await h.load(L27, 'compile')
  await h.focus()
  t = Date.now()
  await h.press(['Control+x', 'p', 'c'], 1500)
  note(`after an earlier build, C-x p c alone: complete ${completed(t) > 0}; checks ${met(t)}`)
  assert(completed(t) === 0, 'C-x p c alone completed compile (an earlier build’s *compilation*)')
  await h.press(['Control+g'], 400)
  // Watch it, Do it yourself, then C-x p c alone: still not complete.
  t = Date.now()
  await h
    .active()
    .getByRole('button', { name: /Watch it/ })
    .click()
  await h.waitEvent(
    t,
    (e) => e.t === 'step' && e.stepId === 'compile' && /demonstrated|complete/.test(e.status),
    40000,
    'end of the demo',
  )
  await sleep(1500)
  t = Date.now()
  await h
    .active()
    .getByRole('button', { name: /Do it yourself/ })
    .click()
  await h.waitEvent(t, (e) => e.t === 'step' && e.status === 'reset', 15000, 'reset event')
  await sleep(1000)
  await h.focus()
  t = Date.now()
  await h.press(['Control+x', 'p', 'c'], 1500)
  note(
    `after Watch it and Do it yourself, C-x p c alone: complete ${completed(t) > 0}; checks ${met(t)}`,
  )
  assert(completed(t) === 0, 'C-x p c alone completed compile (the demo’s *compilation*)')
  // The learner's own build completes it, with no card for the instructed keys
  // and no "Three windows" card for the build output's window.
  await h.press(['Control+a'], 300)
  await h.press(['Control+k'], 500)
  await h.type('sh build.sh')
  await h.press(['Enter'], 300)
  await h.waitEvent(
    t,
    (e) => e.t === 'step' && e.stepId === 'compile' && e.status === 'complete',
    15000,
    'compile complete after the learner’s build',
  )
  await sleep(1000)
  const cards = await h.cards()
  note(`windows ${h.lastState()?.windows?.length}; cards ${JSON.stringify(cards)}`)
  assert(
    !cards.some((c) => /C-a is not|C-k is not/.test(c)),
    'cards for C-a C-k, which 2.7 asks for',
  )
  assert(!cards.some((c) => /Three windows/.test(c)), '"Three windows" card for the build output')
})

await check('S1', '2.4 step 1 is split', async (h, note) => {
  const all = await steps(L24)
  const [a, b] = all.map((s) => text(s.goal))
  note(`first goals: ${JSON.stringify([a, b])}`)
  assert(
    /hello/i.test(a) && !/polyglot/i.test(a) && /polyglot/i.test(b),
    'step 1 still covers both projects',
  )
})

await check('S2', 'Skip this step behind I’m stuck', async (h, note) => {
  const all = await steps(L24)
  const first = await h.load(L24)
  await h.page.getByRole('button', { name: /I'm stuck/ }).click()
  const dlg = h.page.getByRole('dialog')
  await dlg.locator('textarea').fill('review.mjs: checking Skip this step')
  await dlg.getByRole('button', { name: 'Send report' }).click()
  const skip = h.page.getByRole('button', { name: 'Skip this step' })
  await skip.waitFor({ timeout: 5000 }).catch(() => {
    throw new Error('no Skip this step after the report')
  })
  const t = Date.now()
  await skip.click()
  await h.waitEvent(
    t,
    (e) => e.t === 'step' && e.stepId === first.id && e.status === 'skipped',
    60000,
    'skipped event',
  )
  await h.waitEvent(
    t,
    (e) => e.t === 'step' && e.stepId === all[1].id && e.status === 'loaded',
    15000,
    'next step loaded',
  )
  const p = await api('/api/v1/progress')
  const status = p.body?.lessons?.[L24]?.steps?.[first.id]?.status
  note(`progress for ${first.id}: ${status}`)
  assert(status === 'skipped', 'progress does not record skipped')
  await pageLoad()
  await h.page.goto(url + '/', { waitUntil: 'load' })
  const row = h.page.locator('li', { has: h.page.locator(`a[href="/lesson/${L24}"]`) }).first()
  const rowText = (await row.innerText({ timeout: 10000 })).replace(/\s+/g, ' ')
  note(`course map row: ${rowText}`)
  assert(/skipped/i.test(rowText), 'the course map does not show the skipped step')
})

await check('S3', 'which-key paging is taught', async (h, note) => {
  const md = (await lesson(L04)).markdown
  const mentions = /C-h[^.\n]{0,80}\bn\b|next page/i.test(md)
  note(`lesson mentions paging: ${mentions}`)
  await h.load(L04)
  await h.focus()
  await h.press(['Control+x', 'r'], 1800)
  await h.press(['Control+h'], 700)
  await h.press(['n'], 900)
  const screen = (await h.screen()).join('\n')
  const page2 = /\(2 of \d+\)/.test(screen)
  note(`after C-x r, C-h, n the popup shows page 2: ${page2}`)
  await h.press(['Control+g', 'Control+g'], 300)
  assert(mentions, 'the lesson does not say how to page which-key')
})

await check('S4', 'level-one hints add information', async (h, note) => {
  const idx = await api('/api/v1/curriculum')
  const ids = idx.body.parts
    .filter((p) => p.n <= 3)
    .flatMap((p) => p.lessons)
    .filter((l) => l.status === 'ready' && !l.id.startsWith('projects/'))
    .map((l) => l.id)
  const keyRe = /(?:[CMSs]-)+[^\s,.;:()"'`]+|\b(?:RET|TAB|SPC|ESC|DEL)\b/g
  const bad = []
  for (const id of ids) {
    for (const st of await steps(id)) {
      const said = new Set(`${text(st.goal)} ${text(st.detail)}`.match(keyRe) ?? [])
      for (const c of st.conditions ?? []) {
        const hint = text(c.hint)
        if (!hint) continue
        const keys = hint.match(keyRe) ?? []
        const words = hint.replace(keyRe, ' ').split(/\s+/).filter(Boolean).length
        if (keys.length && keys.every((k) => said.has(k)) && words <= 8)
          bad.push(`${id}#${st.id}: ${hint}`)
      }
    }
  }
  note(`${bad.length} hints only restate the step's keys`)
  for (const b of bad.slice(0, 8)) note('  ' + b)
  assert(bad.length === 0, 'level-one hints restate the step')
})

await check(
  'S5',
  'the terminal keeps its width at 1280 px',
  Object.assign(
    async (h, note) => {
      await h.load(L01)
      const size = await h.headerSize()
      const toggle = await h.page
        .getByRole('button', { name: 'Where am I' })
        .isVisible()
        .catch(() => false)
      note(`1280x800: terminal ${size?.label}; rail toggle visible ${toggle}`)
      assert(size && size.cols >= 100, 'the terminal is narrower than 100 columns')
      assert(toggle, 'the rail is not collapsed behind its toggle')
    },
    { viewport: { width: 1280, height: 800 } },
  ),
)

await check('R9', 'the ticker belongs to the lesson', async (h, note) => {
  await h.load(L01)
  await h.focus()
  await h.press(['Control+n', 'Control+p'], 300)
  const before = await h.ticker()
  await resetLesson(L02)
  await rpc('scenario', { lessonId: L02, reset: true })
  await h.page.getByRole('link', { name: 'Course' }).first().click()
  await h.page.locator(`a[href="/lesson/${L02}"]`).first().click()
  await h.page.locator('li[aria-current="step"]').first().waitFor({ timeout: 30000 })
  await sleep(1500)
  const after = await h.ticker()
  note(`ticker in 0.1: ${before.slice(0, 60)}; after opening 0.2: ${after.slice(0, 60)}`)
  assert(
    /Press a key in the Emacs pane/.test(after),
    'the ticker still shows the previous lesson’s keys',
  )
})

await check('R10', 'Reset this step asks first', async (h, note) => {
  await h.load(L01)
  const t = Date.now()
  await h.page.getByRole('button', { name: 'Reset this step' }).first().click()
  const dlg = h.page.getByRole('dialog')
  const shown = await dlg
    .filter({ hasText: /Reset this step\?/ })
    .waitFor({ timeout: 1500 })
    .then(() => true)
    .catch(() => false)
  if (shown) await dlg.getByRole('button', { name: 'Cancel' }).click()
  await sleep(1500)
  const resets = h.since(t, (e) => e.t === 'step' && e.status === 'reset').length
  note(`confirmation dialog: ${shown}; reset events: ${resets}`)
  assert(shown, 'Reset this step does not ask for confirmation')
  assert(resets === 0, 'Cancel still reset the step')
})

await check(
  'R1',
  'playctl -watch replays Part 0 as demonstrations',
  async (h, note) => {
    void h
    const r = spawnSync(
      'go',
      [
        'run',
        './cmd/playctl',
        'curriculum',
        'play',
        '-watch',
        'part:0',
        '-session',
        `${url}/s/local`,
      ],
      {
        cwd: repo,
        encoding: 'utf8',
        timeout: 600000,
        env: { ...process.env, PATH: `${process.env.HOME}/.local/go/bin:${process.env.PATH}` },
      },
    )
    const tail = `${r.stdout ?? ''}${r.stderr ?? ''}`.trim().split('\n').slice(-6)
    for (const l of tail) note(l)
    assert(r.status === 0, `playctl -watch exited ${r.status}`)
  },
  620000,
)

// --- Done ----------------------------------------------------------------------

const resetStatus = await resetAll().catch((e) => String(e))
await rpc('mode', { mode: 'terminal' }).catch(() => undefined)
writeFileSync(join(out, 'results.json'), JSON.stringify({ at: Date.now(), results }, null, 2))
const failed = results.filter((r) => !r.pass)
console.error(
  `\n${results.length - failed.length} of ${results.length} checks passed; progress reset (${resetStatus})`,
)
await browser.close().catch(() => undefined)
process.exit(failed.length ? 1 : 0)
