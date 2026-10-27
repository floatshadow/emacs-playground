// Browser smoke scenarios for the whole stack (AGENTS.md §4, frontend.md §14),
// run against `docker compose` in local mode through a remote headless Chrome:
//
//   docker run -d --rm --name pg-chrome --network host --shm-size=1g zenika/alpine-chrome:latest \
//     --headless --no-sandbox --disable-gpu --remote-debugging-address=0.0.0.0 \
//     --remote-debugging-port=9222 about:blank
//   cd web && pnpm e2e:smoke
//
// Scenarios (screenshots in artifacts/verify/<n>.png):
//   1  compose services healthy; /api/v1/me and /api/v1/curriculum
//   2  course map: 15 parts, the lesson rows of Parts 0-3
//   3  lesson 2.3: within 20 s of navigating, the mode line of the file its
//      setup opens (lib.rs) and that buffer in Where am I
//   4  the focusing click adds no ticker entry; C-x shows a pending prefix,
//      C-f find-file and the prompt, C-g clears it
//   5  Watch it demonstrates the first step (step demonstrated, the caption),
//      also when the terminal loses focus; Do it yourself, then the keys complete it
//   6  lesson 0.2 with Part 0's modules: C-x C-b alone opens the buffer list in
//      a second window and Where am I names it; Rescue leaves one window with
//      main.rs and the toast names what it did. (From Part 2 on the files
//      module makes C-x C-b ibuffer, which takes over the window on purpose,
//      so the second-window exit belongs to Parts 0-1.)
//   7  Reset this step: main.rs on disk equals the fixture again, the step's
//      conditions start over
//   8  GUI frame: VNC canvas, a key typed there reaches the ticker with
//      frame=gui; back to Terminal, frame=terminal
//   9  lesson 3.8: the Markdown preview pane shows a line saved from Emacs
//   10 playctl curriculum play for every lesson of Parts 0-3 (PG_SMOKE_PLAY=0 skips)
//
// Scenario 6 runs after 9 because it and 10 both start from Part 0's module
// set (base agent); the results are reported in scenario order.
//
// Environment: PG_CDP (http://127.0.0.1:9222), PG_URL (http://127.0.0.1:8080),
// PG_OUT (artifacts/verify), PG_SMOKE_ONLY (comma-separated scenario numbers),
// PG_SMOKE_PLAY (1), PG_COMPOSE_DIR (the repository root), PG_WORKSPACE (the
// workspace bind mount, <repo>/workspace).
//
// The script resets the progress of lessons 0.2, 2.3 and 3.8 first, so it
// can be re-run against the same session, and all progress when it ends.
import { chromium } from 'playwright-core'
import { execFileSync, spawnSync } from 'node:child_process'
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const repo = process.env.PG_COMPOSE_DIR ?? resolve(here, '../..')
const cdp = process.env.PG_CDP ?? 'http://127.0.0.1:9222'
const url = (process.env.PG_URL ?? 'http://127.0.0.1:8080').replace(/\/$/, '')
const out = process.env.PG_OUT ?? join(repo, 'artifacts/verify')
const only = (process.env.PG_SMOKE_ONLY ?? '')
  .split(',')
  .map((s) => s.trim())
  .filter(Boolean)
const runPlay = (process.env.PG_SMOKE_PLAY ?? '1') !== '0'
const workspace = process.env.PG_WORKSPACE ?? join(repo, 'workspace')
mkdirSync(out, { recursive: true })

const LESSON_02 = '00-orientation/02-getting-unstuck'
const LESSON_23 = '02-buffers-and-files/03-opening-files'
const LESSON_38 = '03-windows-and-layouts/08-focus-and-layout-recipes'

const browser = await chromium.connectOverCDP(cdp)
const consoleErrors = []
const context = browser.contexts()[0] ?? (await browser.newContext())
const page = await context.newPage()
await page.setViewportSize({ width: 1600, height: 1000 })
const pageErrors = []
page.on('pageerror', (e) => pageErrors.push(String(e)))
page.on('console', (m) => {
  if (m.type() === 'error') consoleErrors.push(m.text().slice(0, 300))
})
page.on('crash', () => pageErrors.push('the page crashed'))
page.on('requestfailed', (r) =>
  consoleErrors.push(`request failed: ${r.method()} ${r.url()} ${r.failure()?.errorText ?? ''}`),
)
const requests = new Map()
const pending = new Map()
page.on('request', (r) => pending.set(r, r.url()))
page.on('requestfinished', (r) => pending.delete(r))
page.on('requestfailed', (r) => pending.delete(r))
page.on('websocket', (ws) => {
  const u =
    'ws ' +
    ws
      .url()
      .replace(/[?#].*$/, '')
      .replace(/^ws:\/\/[^/]+/, '')
  requests.set(u, (requests.get(u) ?? 0) + 1)
})
page.on('request', (r) => {
  const u = r
    .url()
    .replace(/[?#].*$/, '')
    .replace(url, '')
  requests.set(u, (requests.get(u) ?? 0) + 1)
})
const results = []

// The /events frames the page receives (api.md §4), for checks the DOM
// cannot prove on its own (a check event, the frame a key was typed in).
const events = []
page.on('websocket', (ws) => {
  if (!/\/events/.test(ws.url())) return
  ws.on('framereceived', (f) => {
    try {
      const e = JSON.parse(String(f.payload))
      e._at = Date.now()
      events.push(e)
    } catch {
      // not JSON: ignore
    }
  })
})
const eventsSince = (t, pred) => events.filter((e) => e._at >= t && pred(e))
async function waitEvent(t, pred, timeout, what) {
  const until = Date.now() + timeout
  for (;;) {
    const hit = eventsSince(t, pred)
    if (hit.length) return hit[0]
    if (Date.now() > until) throw new Error(`no ${what} event within ${timeout} ms`)
    await page.waitForTimeout(100)
  }
}

async function scenario(n, name, fn) {
  if (only.length && !only.includes(String(n))) return
  if (process.env.PG_SMOKE_DEBUG)
    console.error(`before ${n}: ${pending.size} pending`, [...pending.values()].slice(0, 5))
  const t0 = Date.now()
  let detail = ''
  try {
    detail = (await fn()) ?? ''
    results.push({ n, name, ok: true, ms: Date.now() - t0, detail })
  } catch (e) {
    results.push({ n, name, ok: false, ms: Date.now() - t0, error: String(e).split('\n')[0] })
  }
  await page.screenshot({ path: join(out, `${n}.png`) }).catch(() => undefined)
  const r = results[results.length - 1]
  console.error(`${r.ok ? 'PASS' : 'FAIL'} ${n} ${name} (${r.ms} ms)${r.ok ? '' : ': ' + r.error}`)
}

// ---- helpers ---------------------------------------------------------------

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
  return { status: res.status, body, type: res.headers.get('content-type') ?? '' }
}

/** Sends a demo string through rpc keys as learner input, at playctl pacing (170/70 ms). */
async function playKeys(demo) {
  const re = /"((?:[^"\\]|\\.)*)"|~(\d+)|(\S+)/g
  for (const m of demo.matchAll(re)) {
    if (m[1] !== undefined) {
      for (const ch of JSON.parse(`"${m[1]}"`)) {
        await api('/s/local/rpc/keys', { method: 'POST', body: JSON.stringify({ text: ch }) })
        await page.waitForTimeout(70)
      }
    } else if (m[2] !== undefined) await page.waitForTimeout(Number(m[2]))
    else {
      await api('/s/local/rpc/keys', { method: 'POST', body: JSON.stringify({ keys: m[3] }) })
      await page.waitForTimeout(170)
    }
  }
}

const ticker = () => page.locator('[aria-label^="Key ticker"]')
const where = () =>
  page.locator('section', { has: page.getByRole('heading', { name: 'Where am I' }) })
const activeStep = () => page.locator('li[aria-current="step"]')
const toastText = (re) => page.getByText(re).first()

/** The terminal screen as text rows (hosts.ts exposes it for tests). */
async function screenText() {
  return page.evaluate(() => {
    const f = /** @type {any} */ (window).__pgScreenText
    return typeof f === 'function' ? f() : []
  })
}

async function waitScreen(pred, timeout, what) {
  const until = Date.now() + timeout
  let rows = []
  while (Date.now() < until) {
    rows = await screenText()
    if (pred(rows)) return rows
    await page.waitForTimeout(250)
  }
  throw new Error(`terminal never showed ${what}; last screen:\n${rows.filter(Boolean).join('\n')}`)
}

async function focusTerminal() {
  await page
    .locator('.pg-terminal .xterm')
    .first()
    .click({ position: { x: 300, y: 200 } })
  await page.waitForTimeout(150)
}

/** Waits until a ticker entry for cmd is the newest (left-most) entry. */
async function waitTicker(text, timeout = 5000) {
  await ticker().getByText(text, { exact: true }).first().waitFor({ timeout })
}

async function whereText() {
  return (await where().first().innerText()).replace(/\s+/g, ' ')
}

async function waitWhere(pred, timeout, what) {
  const until = Date.now() + timeout
  let txt = ''
  while (Date.now() < until) {
    txt = await whereText().catch(() => '')
    if (pred(txt)) return txt
    await page.waitForTimeout(150)
  }
  throw new Error(`Where am I never showed ${what}; it said: ${txt.slice(0, 400)}`)
}

/** Opens a lesson and waits for its scenario to be loaded into Emacs. */
async function openLesson(id, firstGoal) {
  await page.goto(`${url}/lesson/${id}`, { waitUntil: 'load' })
  await page.locator('.pg-terminal .xterm-screen').waitFor({ timeout: 20000 })
  if (firstGoal) await activeStep().filter({ hasText: firstGoal }).waitFor({ timeout: 30000 })
  // The scenario is loaded once the step list has an active step and the
  // loading note is gone.
  await page
    .getByText('Loading the lesson into Emacs…')
    .waitFor({ state: 'detached', timeout: 30000 })
}

/**
 * Sets the configuration modules (the session page's `rpc modules`) and waits
 * for Emacs to come back with exactly that set and the agent connected.
 */
async function setModules(enabled) {
  const mods = await api('/s/local/rpc/modules', {
    method: 'POST',
    body: JSON.stringify({ enabled }),
  })
  if (mods.status !== 200) throw new Error('rpc modules: ' + JSON.stringify(mods.body))
  const until = Date.now() + 90000
  for (;;) {
    const st = await api('/s/local/rpc/status', { method: 'POST', body: '{}' })
    const m = st.body?.result?.modules ?? []
    if (st.status === 200 && st.body?.result?.emacs?.agent && m.join(',') === enabled.join(','))
      return
    if (Date.now() > until) throw new Error(`Emacs did not restart with ${enabled.join(' ')}`)
    await page.waitForTimeout(1000)
  }
}

// ---- scenarios -------------------------------------------------------------

try {
  // Fresh progress for the lessons the browser scenarios walk through.
  for (const lessonId of [LESSON_02, LESSON_23, LESSON_38]) {
    await api('/api/v1/progress/reset', { method: 'POST', body: JSON.stringify({ lessonId }) })
  }

  await scenario(1, 'services healthy, /me and /curriculum', async () => {
    const ps = execFileSync('docker', ['compose', 'ps', '--format', 'json'], {
      cwd: repo,
      encoding: 'utf8',
    })
    const services = ps
      .trim()
      .split('\n')
      .filter(Boolean)
      .flatMap((l) => {
        const v = JSON.parse(l)
        return Array.isArray(v) ? v : [v]
      })
    const health = Object.fromEntries(services.map((s) => [s.Service, s.Health || s.State]))
    for (const svc of ['gateway', 'session', 'remote-box']) {
      if (health[svc] !== 'healthy') throw new Error(`${svc} is ${health[svc] ?? 'missing'}`)
    }
    const me = await api('/api/v1/me')
    if (me.status !== 200 || !/json/.test(me.type) || me.body?.mode !== 'local')
      throw new Error(`/me: ${JSON.stringify(me)}`)
    const cur = await api('/api/v1/curriculum')
    const parts = cur.body?.parts ?? []
    if (cur.status !== 200 || !/json/.test(cur.type) || parts.length !== 15)
      throw new Error(`/curriculum: ${parts.length} parts`)
    await page.goto(`${url}/api/v1/me`, { waitUntil: 'load' })
    return `health ${JSON.stringify(health)}; mode ${me.body.mode}; ${parts.length} parts`
  })

  await scenario(2, 'course map lists 15 parts and the lessons of Parts 0-3', async () => {
    const cur = (await api('/api/v1/curriculum')).body
    await page.goto(url + '/', { waitUntil: 'load' })
    await page.locator('h1').first().waitFor({ timeout: 15000 })
    await page.locator('section[aria-labelledby^="part-"]').nth(14).waitFor({ timeout: 15000 })
    const partCount = await page.locator('section[aria-labelledby^="part-"]').count()
    if (partCount !== 15) throw new Error(`${partCount} part sections`)
    let rows = 0
    for (const part of cur.parts.filter((p) => p.n <= 3)) {
      for (const l of part.lessons) {
        const link = page.locator(`section[aria-labelledby^="part-"] a[href="/lesson/${l.id}"]`)
        if ((await link.count()) !== 1) throw new Error(`no row for ${l.n} ${l.id}`)
        // Rendered for the learner, not only in the DOM (a collapsed part).
        if (!(await link.isVisible())) throw new Error(`the row for ${l.n} is not visible`)
        rows++
      }
    }
    return `${partCount} parts, ${rows} lesson rows in Parts 0-3`
  })

  await scenario(3, 'lesson 2.3 shows Emacs and Where am I within 20 s', async () => {
    // Timed from navigation: the learner sees Emacs and its state in 20 s.
    const t0 = Date.now()
    await page.goto(`${url}/lesson/${LESSON_23}`, { waitUntil: 'load' })
    // The lesson's own setup (it opens src/lib.rs at the top), not what the
    // previous lesson left on the screen: a re-run after 2.3 still shows
    // lib.rs, with the cursor wherever the last run left it.
    const isModeLine = (l) => /\blib\.rs\b/.test(l) && /\bL1 C0\b/.test(l)
    let rows = []
    let txt = ''
    while (Date.now() - t0 < 20000) {
      rows = await screenText()
      txt = await whereText().catch(() => '')
      if (rows.some(isModeLine) && /Buffer lib\.rs/.test(txt)) break
      await page.waitForTimeout(200)
    }
    const ms = Date.now() - t0
    const modeLine = rows.find(isModeLine)
    if (!modeLine)
      throw new Error(
        `no mode line naming lib.rs ${ms} ms after navigating:\n${rows.filter(Boolean).join('\n')}`,
      )
    if (!/Buffer lib\.rs/.test(txt))
      throw new Error(
        `Where am I did not name lib.rs ${ms} ms after navigating: ${txt.slice(0, 300)}`,
      )
    // The rest of the scenarios need the lesson loaded into Emacs.
    await activeStep().filter({ hasText: 'Open src/main.rs' }).waitFor({ timeout: 30000 })
    await page
      .getByText('Loading the lesson into Emacs…')
      .waitFor({ state: 'detached', timeout: 30000 })
    return `${ms} ms after navigating: mode line "${modeLine.trim()}"; ${txt.match(/Buffer \S+/)[0]}`
  })

  await scenario(4, 'C-x prefix, C-f find-file and prompt, C-g', async () => {
    await focusTerminal()
    // Clicking into the terminal to focus it is not a key: no ticker entry.
    if (/mouse-set-point|mouse-drag-region/.test(await ticker().first().innerText()))
      throw new Error('the click that focused the terminal shows in the key ticker')
    const t0 = Date.now()
    await page.keyboard.press('Control+x')
    await ticker().getByText('waiting for the next key').waitFor({ timeout: 1000 })
    const prefixMs = Date.now() - t0
    await page.keyboard.press('Control+f')
    await waitTicker('find-file')
    await waitEvent(t0, (e) => e.t === 'key' && e.cmd === 'find-file', 3000, 'find-file key')
    await waitWhere((t) => /Prompt Find file/.test(t), 5000, 'the Find file prompt')
    await page.keyboard.press('Control+g')
    await waitWhere((t) => /Prompt none/.test(t), 5000, 'the prompt closed')
    return `prefix shown after ${prefixMs} ms`
  })

  await scenario(5, 'Watch it demonstrates the first step; the learner does it', async () => {
    const first = page.locator('ol li', { hasText: 'Open src/main.rs' }).first()
    await activeStep().filter({ hasText: 'Open src/main.rs' }).waitFor({ timeout: 5000 })
    const t0 = Date.now()
    await activeStep().getByRole('button', { name: 'Watch it' }).click()
    // Clicking the lesson text blurs the terminal: xterm.js sends a focus-out
    // report (tmux focus-events), which is not a keypress and must not stop
    // the demo (e2e/demo-focus.mjs checks this in more detail).
    const caption = page.locator('[aria-label="Demo caption"]')
    await caption.waitFor({ timeout: 5000 })
    await page.waitForTimeout(300)
    await page.getByRole('heading', { level: 1 }).first().click()
    // A demonstration, not a completion (fix plan contract b): sessiond
    // records it "demonstrated" and the step stays active.
    const ev = await waitEvent(
      t0,
      (e) => e.t === 'step' && e.stepId === 'open' && /demonstrated|complete/.test(e.status),
      30000,
      'step demonstrated for step open',
    )
    if (ev.status !== 'demonstrated') throw new Error('Watch it completed the step')
    await caption.waitFor({ state: 'detached', timeout: 30000 })
    if (
      await toastText(/Demo stopped/)
        .isVisible()
        .catch(() => false)
    )
      throw new Error('the demo stopped when the terminal lost focus')
    await activeStep().filter({ hasText: 'Open src/main.rs' }).waitFor({ timeout: 5000 })
    // Do it yourself: the step goes back to its checkpoint; the learner's
    // keys (here the demo's, sent as input) complete it for real.
    const t1 = Date.now()
    await activeStep().getByRole('button', { name: 'Do it yourself' }).click()
    await waitEvent(t1, (e) => e.t === 'step' && e.status === 'reset', 15000, 'step reset')
    await page.waitForTimeout(600)
    const lesson = await api('/api/v1/curriculum/lessons/' + LESSON_23)
    await playKeys(lesson.body.scenario.steps[0].demo)
    const done = await waitEvent(
      t1,
      (e) => e.t === 'step' && e.stepId === 'open' && e.status === 'complete',
      15000,
      'step complete for step open',
    )
    await first.getByText('Done:').waitFor({ state: 'attached', timeout: 15000 })
    await activeStep().filter({ hasText: 'Change the greeting' }).waitFor({ timeout: 5000 })
    return `demonstrated after ${ev._at - t0} ms (terminal blurred), done by the learner after ${done._at - t1} ms`
  })

  await scenario(7, 'Reset this step restores files and the step', async () => {
    const fixture = readFileSync(
      join(repo, 'curriculum', LESSON_23, 'fixtures/hello/src/main.rs'),
      'utf8',
    )
    const mainRs = join(workspace, 'hello/src/main.rs')
    const onDisk = () => (existsSync(mainRs) ? readFileSync(mainRs, 'utf8') : '')
    if (onDisk() !== fixture)
      throw new Error('main.rs on disk differs from the fixture before the step')
    // Make the active step's first condition true, then reset it.
    await focusTerminal()
    const step = activeStep()
    await step.filter({ hasText: 'Change the greeting' }).waitFor({ timeout: 5000 })
    await waitWhere((t) => /Buffer main\.rs/.test(t), 5000, 'main.rs as the buffer')
    // The step starts with nothing met: main.rs opened at the top, not
    // where an earlier attempt left the cursor (save-place).
    const metAtStart = await activeStep().getByText('met:', { exact: true }).count()
    if (metAtStart !== 0) throw new Error(`${metAtStart} conditions met before doing anything`)
    await page.keyboard.press('Alt+Shift+Comma') // M-<
    await page.keyboard.press('Control+s')
    await page.keyboard.type('Hello', { delay: 30 })
    await page.keyboard.press('Enter')
    await step.getByText('met:').first().waitFor({ state: 'attached', timeout: 8000 })
    // Change the file on disk too, so the reset has a file to restore.
    await page.keyboard.type('XYZ', { delay: 30 })
    await page.keyboard.press('Control+x')
    await page.keyboard.press('Control+s')
    const saved = Date.now() + 5000
    while (Date.now() < saved && !onDisk().includes('XYZ')) await page.waitForTimeout(200)
    if (!onDisk().includes('XYZ')) throw new Error('the edit never reached main.rs on disk')
    const t0 = Date.now()
    await page.getByRole('button', { name: 'Reset this step' }).click()
    // It asks first (R10).
    await page.getByRole('dialog').getByRole('button', { name: 'Reset', exact: true }).click()
    const toast = toastText(/^Restored /)
    await toast.waitFor({ timeout: 8000 })
    const said = await toast.innerText()
    await waitEvent(
      t0,
      (e) => e.t === 'step' && e.stepId === 'edit' && e.status === 'reset',
      8000,
      'step reset',
    )
    await activeStep().filter({ hasText: 'Change the greeting' }).waitFor({ timeout: 5000 })
    // Every condition of the step is unmet again once the reset check arrives.
    const until = Date.now() + 8000
    let met = -1
    while (Date.now() < until) {
      met = await activeStep().getByText('met:', { exact: true }).count()
      if (met === 0) break
      await page.waitForTimeout(200)
    }
    if (met !== 0) throw new Error(`${met} conditions still met after the reset`)
    if (onDisk() !== fixture)
      throw new Error('main.rs on disk differs from the fixture after the reset')
    const rows = await screenText()
    if (rows.some((l) => l.includes('XYZ'))) throw new Error('the typed text survived the reset')
    return `toast "${said}"; main.rs on disk equals the fixture`
  })

  await scenario(8, 'GUI frame and back', async () => {
    await page.getByRole('button', { name: 'GUI', exact: true }).click()
    const canvas = page.locator('.pg-vnc canvas')
    await canvas.first().waitFor({ state: 'visible', timeout: 20000 })
    await page.waitForTimeout(2500)
    // A real framebuffer, not an empty element: a usable size and more
    // than one colour on it once the frame has been drawn.
    const box = await canvas.first().boundingBox()
    const drawn = await canvas.first().evaluate((c) => {
      const d = c.getContext('2d')?.getImageData(0, 0, c.width, c.height).data
      const colours = new Set()
      for (let i = 0; d && i < d.length; i += 4 * 97) colours.add(`${d[i]},${d[i + 1]},${d[i + 2]}`)
      return { w: c.width, h: c.height, colours: colours.size }
    })
    if (!box || box.width < 200 || box.height < 200 || drawn.w < 200 || drawn.colours < 2)
      throw new Error(`the VNC canvas is empty: ${JSON.stringify({ box, drawn })}`)
    await canvas.first().click({ position: { x: 400, y: 300 } })
    await page.waitForTimeout(300)
    const t1 = Date.now()
    await page.keyboard.press('Control+n')
    await waitTicker('next-line', 8000)
    const gui = await waitEvent(
      t1,
      (e) => e.t === 'key' && e.cmd === 'next-line',
      3000,
      'next-line key',
    )
    if (gui.frame !== 'gui')
      throw new Error(`C-n in the GUI frame was reported from frame ${gui.frame}`)
    await page.screenshot({ path: join(out, '8-gui.png') })
    await page.getByRole('button', { name: 'Terminal', exact: true }).click()
    await page.locator('.pg-terminal').waitFor({ state: 'visible', timeout: 5000 })
    await page.waitForTimeout(1000)
    await focusTerminal()
    const t2 = Date.now()
    await page.keyboard.press('Control+p')
    await waitTicker('previous-line', 8000)
    const term = await waitEvent(
      t2,
      (e) => e.t === 'key' && e.cmd === 'previous-line',
      3000,
      'previous-line key',
    )
    if (term.frame !== 'terminal')
      throw new Error(`C-p in the terminal was reported from frame ${term.frame}`)
    return `canvas ${drawn.w}x${drawn.h}, ${drawn.colours} colours sampled; next-line frame=gui; previous-line frame=terminal`
  })

  await scenario(9, 'Markdown preview reloads on save', async () => {
    await openLesson(LESSON_38, 'Read reading.md as one long page')
    const status = page.locator('[role="status"]', { hasText: 'reading.md' })
    await status.first().waitFor({ timeout: 15000 })
    await page.getByRole('heading').filter({ hasText: /\S/ }).first().waitFor()
    await focusTerminal()
    await page.keyboard.press('Control+x')
    await page.keyboard.press('Control+f')
    await waitWhere((t) => /Prompt Find file/.test(t), 5000, 'the Find file prompt')
    await page.keyboard.type('/work/projects/notes/reading.md', { delay: 15 })
    await page.keyboard.press('Enter')
    await waitWhere((t) => /Buffer reading\.md/.test(t), 5000, 'reading.md as the buffer')
    await page.keyboard.press('Alt+Shift+Period') // M->
    const marker = `Smoke test line ${Date.now() % 100000}`
    await page.keyboard.press('Enter')
    await page.keyboard.type(marker, { delay: 15 })
    // The status has aged since the lesson loaded, so "just now" below can
    // only come from this save.
    await page
      .locator('[role="status"]', { hasText: /reading\.md updated \d+ (s|min) ago/ })
      .first()
      .waitFor({ timeout: 15000 })
    const t0 = Date.now()
    await page.keyboard.press('Control+x')
    await page.keyboard.press('Control+s')
    await waitEvent(t0, (e) => e.t === 'file' && /reading\.md$/.test(e.path ?? ''), 10000, 'file')
    await page
      .locator('[role="status"]', { hasText: /reading\.md updated just now/ })
      .first()
      .waitFor({ timeout: 10000 })
    // The marker in the preview pane, not in the terminal's own text.
    await page.locator('[role="tabpanel"]').getByText(marker).first().waitFor({ timeout: 10000 })
    const disk = readFileSync(join(workspace, 'projects/notes/reading.md'), 'utf8')
    if (!disk.includes(marker)) throw new Error('the saved line is not on disk')
    return `status "${(await status.first().innerText()).trim()}"`
  })

  await scenario(6, 'C-x C-b opens a second window; Rescue closes it', async () => {
    // Part 0's module set: C-x C-b is the plain buffer list, which opens in
    // a second window (lesson 0.2, "An extra window").
    await setModules(['base', 'agent'])
    await api('/api/v1/progress/reset', {
      method: 'POST',
      body: JSON.stringify({ lessonId: LESSON_02 }),
    })
    await openLesson(LESSON_02, 'Open a prompt, then cancel it')
    await waitWhere(
      (t) => /Buffer main\.rs/.test(t) && /Windows one/.test(t),
      20000,
      'main.rs in one window',
    )
    await page.waitForTimeout(500)
    await focusTerminal()
    const t0 = Date.now()
    await page.keyboard.press('Control+x')
    await page.keyboard.press('Control+b')
    const key = await waitEvent(
      t0,
      (e) => e.t === 'key' && /^(list-buffers|ibuffer)$/.test(e.cmd ?? ''),
      5000,
      'C-x C-b key',
    )
    if (key.cmd !== 'list-buffers')
      throw new Error(`C-x C-b ran ${key.cmd}; Part 0's modules bind it to list-buffers`)
    // C-x C-b alone, no extra keys: the list is in a second window.
    const txt = await waitWhere(
      (t) => /Another window shows \*Buffer List\*/.test(t),
      5000,
      'the second-window exit',
    )
    const exit = txt.match(/Another window shows \*Buffer List\*[^.]*/)[0]
    await page.screenshot({ path: join(out, '6a-second-window.png') })
    const t1 = Date.now()
    await page.getByRole('button', { name: 'Rescue me' }).last().click()
    const toast = toastText(/^Rescue: /)
    await toast.waitFor({ timeout: 5000 })
    const said = await toast.innerText()
    // The toast names what Rescue did: here, closing the list's window.
    if (!/window/.test(said) || /already in a normal state/.test(said))
      throw new Error(`the Rescue toast does not name what it did: "${said}"`)
    const after = await waitWhere((t) => /Windows one/.test(t), 5000, 'one window')
    if (!/Buffer main\.rs/.test(after))
      throw new Error('Rescue left another buffer than main.rs: ' + after.slice(0, 200))
    await waitEvent(t1, (e) => e.t === 'state' && e.windows?.length === 1, 5000, 'one-window state')
    return `${exit}; toast "${said}"`
  })

  await scenario(10, 'playctl curriculum play, Parts 0-3', async () => {
    if (!runPlay) return 'skipped (PG_SMOKE_PLAY=0)'
    // Replay as a learner going through the course in order: configuration
    // modules unlock monotonically (emacs.md §4), and the scenarios above
    // already turned on Part 3's. Start again from Part 0's set.
    await setModules(['base', 'agent'])
    const r = spawnSync(
      'go',
      [
        'run',
        './cmd/playctl',
        'curriculum',
        'play',
        'part:0',
        'part:1',
        'part:2',
        'part:3',
        '-session',
        `${url}/s/local`,
      ],
      { cwd: repo, encoding: 'utf8', timeout: 60 * 60 * 1000, maxBuffer: 64 << 20 },
    )
    writeFileSync(join(out, '10-play.txt'), r.stdout + '\n' + r.stderr)
    await page.goto(url + '/', { waitUntil: 'load' })
    await page.locator('h1').first().waitFor({ timeout: 15000 })
    const summary = (r.stdout.match(/^\d+ steps, .*$/m) ?? ['no summary'])[0]
    if (r.status !== 0) {
      const failed = r.stdout
        .split('\n')
        .filter((l) => /\s(fail|timeout|error)\s/.test(l))
        .slice(0, 20)
      throw new Error(`${summary}\n${failed.join('\n')}`)
    }
    return summary
  })
} finally {
  // Start over: the scenarios and the replay recorded progress for the
  // single local learner (fix plan contract a).
  await api('/api/v1/progress/reset', { method: 'POST' }).catch(() => undefined)
  results.sort((a, b) => a.n - b.n)
  const table = results.map(
    (r) => `${r.ok ? 'PASS' : 'FAIL'}  ${r.n}  ${r.name}  ${r.ok ? r.detail : r.error}`,
  )
  console.log(table.join('\n'))
  writeFileSync(
    join(out, 'results.json'),
    JSON.stringify(
      {
        results,
        pageErrors,
        consoleErrors,
        topRequests: [...requests].sort((a, b) => b[1] - a[1]).slice(0, 15),
        pendingRequests: [...pending.values()].slice(0, 20),
      },
      null,
      1,
    ),
  )
  if (pageErrors.length) console.log('page errors:\n' + pageErrors.join('\n'))
  await page.close()
  await browser.close()
  if (results.some((r) => !r.ok)) process.exitCode = 1
}
