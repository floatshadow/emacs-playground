// Regression check for the demo player (review fix, 2026-10-27): "Watch it"
// keeps playing when the terminal loses focus. tmux's focus-events make
// xterm.js send a focus-out report (ESC [ O) on blur; that is not learner
// input. A real keypress still stops the demo. Watch it is a demonstration
// (fix plan contract b): the step ends up watched, not done.
//
//   cd web && node e2e/demo-focus.mjs      (stack on :8080, Chrome on CDP :9222)
import { chromium } from 'playwright-core'

const cdp = process.env.PG_CDP ?? 'http://127.0.0.1:9222'
const url = (process.env.PG_URL ?? 'http://127.0.0.1:8080').replace(/\/$/, '')
const LESSON = '02-buffers-and-files/03-opening-files'

async function api(path, body) {
  const res = await fetch(url + path, {
    method: 'POST',
    headers: { 'content-type': 'application/json', origin: url },
    body: JSON.stringify(body),
  })
  return res.status
}

const browser = await chromium.connectOverCDP(cdp)
const context = browser.contexts()[0] ?? (await browser.newContext())
const page = await context.newPage()
await page.setViewportSize({ width: 1600, height: 1000 })
const sent = []
page.on('websocket', (ws) => {
  if (!/\/pty/.test(ws.url())) return
  ws.on('framesent', (f) => sent.push({ at: Date.now(), data: String(f.payload) }))
})
const toasts = []
const fail = (msg) => {
  console.error('FAIL ' + msg)
  process.exitCode = 1
}
try {
  await api('/api/v1/progress/reset', { lessonId: LESSON })
  await page.goto(`${url}/lesson/${LESSON}`, { waitUntil: 'load' })
  const active = () => page.locator('li[aria-current="step"]')
  await active().filter({ hasText: 'Open src/main.rs' }).waitFor({ timeout: 30000 })
  await page.locator('.pg-terminal .xterm').first().waitFor({ timeout: 20000 })
  await page.waitForTimeout(1500)
  page.on('console', (m) => {
    if (/Demo stopped/.test(m.text())) toasts.push(m.text())
  })

  // 1. Watch it, then click the lesson text: the terminal blurs.
  const t0 = Date.now()
  const caption = page.locator('[aria-label="Demo caption"]')
  await active().getByRole('button', { name: 'Watch it' }).click()
  await caption.waitFor({ timeout: 5000 })
  await page.waitForTimeout(400)
  await page.getByRole('heading', { level: 1 }).first().click()
  await page.waitForTimeout(300)
  const focusOut = sent.some((f) => f.at >= t0 && f.data.includes('\x1b[O'))
  const stopped = await page
    .getByText(/Demo stopped/)
    .first()
    .isVisible()
    .catch(() => false)
  if (stopped) fail('the demo stopped when the terminal lost focus')
  const completed = await active()
    .filter({ hasText: 'Open src/main.rs' })
    .getByText('You watched this step')
    .waitFor({ state: 'attached', timeout: 30000 })
    .then(() => true)
    .catch(() => false)
  if (!completed) fail('the first step was not demonstrated after the blur')
  console.log(
    `blur during the demo: focus-out report sent=${focusOut}, demo stopped=${stopped}, step demonstrated=${completed}`,
  )
  if (stopped || !completed) throw new Error('stopping after the blur check')

  // 2. A real key during a demo still stops it.
  await caption.waitFor({ state: 'detached', timeout: 15000 })
  await active().getByRole('button', { name: 'Watch it' }).click()
  await caption.waitFor({ timeout: 5000 })
  await page.keyboard.press('Shift') // a lone modifier is not input
  await page.waitForTimeout(300)
  const afterShift = await caption.isVisible()
  if (!afterShift) fail('a lone Shift stopped the demo')
  await page.keyboard.press('ArrowRight')
  await page
    .getByText(/Demo stopped/)
    .first()
    .waitFor({ timeout: 3000 })
    .then(() => console.log('a keypress during the demo stops it (toast shown)'))
    .catch(() => fail('a keypress did not stop the demo'))
} catch (e) {
  fail(String(e).split('\n')[0])
} finally {
  await api('/s/local/rpc/rescue', {})
  await api('/api/v1/progress/reset', {})
  await page.close()
  await browser.close().catch(() => undefined)
}
if (!process.exitCode) console.log('PASS demo-focus')
