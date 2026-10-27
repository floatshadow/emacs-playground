// End-to-end scenarios from frontend.md §14, through a remote Chrome over CDP:
// open lesson 2.3, type C-x C-f and see find-file in the ticker, complete a
// step with keys, watch a demo, reset the step, switch to the GUI frame and
// back, and watch the preview reload on save in project P6.
//
//   node dev/fake-session.ts --port 8090 --static dist &   # or a real gateway
//   PG_CDP=http://127.0.0.1:9222 PG_URL=http://127.0.0.1:8090 PG_OUT=../artifacts node e2e/lesson.mjs
//
// Run it against a fresh session: it expects lesson 2.3 to start at step 1.
import { chromium } from 'playwright-core'

const cdp = process.env.PG_CDP ?? 'http://127.0.0.1:9222'
const url = process.env.PG_URL ?? 'http://127.0.0.1:8090'
const out = process.env.PG_OUT
const browser = await chromium.connectOverCDP(cdp)
const context = browser.contexts()[0] ?? (await browser.newContext())
const page = await context.newPage()
await page.setViewportSize({ width: 1600, height: 1000 })
const errors = []
page.on('pageerror', (e) => errors.push(String(e)))
const results = []

async function step(name, fn) {
  const t0 = Date.now()
  try {
    await fn()
    results.push({ name, ok: true, ms: Date.now() - t0 })
  } catch (e) {
    results.push({ name, ok: false, error: String(e).split('\n')[0] })
    if (out) await page.screenshot({ path: `${out}/fail-${name.replace(/\W+/g, '-')}.png` })
  }
}
const shot = async (name) => {
  if (out) await page.screenshot({ path: `${out}/${name}.png` })
}
const ticker = () => page.locator('[aria-label^="Key ticker"]')
const activeStep = () => page.locator('li[aria-current="step"]')
async function focusTerminal() {
  await page
    .locator('.xterm')
    .first()
    .click({ position: { x: 200, y: 200 } })
}

try {
  await step('open lesson 2.3', async () => {
    await page.goto(url + '/lesson/02-buffers-and-files/03-opening-files', { waitUntil: 'load' })
    await page.locator('h1', { hasText: 'Opening files' }).waitFor()
    await page.locator('.xterm-screen').waitFor()
    await activeStep().filter({ hasText: 'Open src/main.rs' }).waitFor({ timeout: 15000 })
    await page.waitForTimeout(800)
    await shot('e2e-1-lesson')
  })

  await step('the terminal has focus on arrival', async () => {
    const inTerminal = await page.evaluate(() => !!document.activeElement?.closest('.xterm'))
    if (!inTerminal) throw new Error('focus is not in the terminal')
  })

  await step('C-x C-f shows find-file in the ticker', async () => {
    await focusTerminal()
    await page.keyboard.press('Control+x')
    await ticker().getByText('waiting for the next key').waitFor({ timeout: 5000 })
    await page.keyboard.press('Control+f')
    await ticker().getByText('find-file', { exact: true }).waitFor({ timeout: 5000 })
    await page.getByText('A prompt is open: Find file:').first().waitFor({ timeout: 5000 })
    await shot('e2e-2-find-file')
  })

  await step('complete step 1 with keys', async () => {
    await page.keyboard.type('src/ma', { delay: 40 })
    await page.keyboard.press('Tab')
    await page.keyboard.press('Enter')
    await activeStep().filter({ hasText: 'Change the greeting' }).waitFor({ timeout: 5000 })
  })

  await step('show a hint', async () => {
    await page.getByRole('button', { name: 'Show a hint' }).click()
    await page.getByRole('note').filter({ hasText: 'hint for:' }).waitFor({ timeout: 5000 })
    await shot('e2e-3-hint')
  })

  await step('watch it demonstrates step 2, with a caption', async () => {
    await page.getByRole('button', { name: 'Watch it' }).click()
    await page.locator('[aria-label="Demo caption"]').getByText('M-<').waitFor({ timeout: 5000 })
    await shot('e2e-3b-caption')
    // A demonstration, not a completion: the step stays, watched.
    await activeStep()
      .filter({ hasText: 'Change the greeting' })
      .getByText('You watched this step')
      .waitFor({ timeout: 20000 })
    await page.locator('[aria-label="Demo caption"]').waitFor({ state: 'detached', timeout: 20000 })
  })

  await step('do it yourself, then the keys complete step 2', async () => {
    await activeStep().getByRole('button', { name: 'Do it yourself' }).click()
    await activeStep()
      .getByText('You watched this step')
      .waitFor({ state: 'detached', timeout: 5000 })
    await focusTerminal()
    await page.keyboard.press('Alt+Shift+Comma')
    await page.keyboard.press('Control+s')
    await page.keyboard.type('Hello', { delay: 30 })
    await page.keyboard.press('Enter')
    for (let i = 0; i < 5; i++) await page.keyboard.press('Backspace')
    await page.keyboard.type('Hi', { delay: 30 })
    await activeStep().filter({ hasText: 'Save it' }).waitFor({ timeout: 15000 })
  })

  await step('reset this step asks first', async () => {
    await page.getByRole('button', { name: 'Reset this step' }).click()
    await page.getByRole('dialog').getByRole('button', { name: 'Reset', exact: true }).click()
    await page
      .getByText(/^Restored .*(checkpoint|no files had changed)/)
      .first()
      .waitFor({ timeout: 5000 })
  })

  await step('a habit card appears for C-s', async () => {
    // Key events up to 600 ms after a demo ends count as the demo's (no cards).
    await page.waitForTimeout(700)
    await focusTerminal()
    await page.keyboard.press('Control+s')
    await page.getByText('Coming from VS Code? C-s is not save.').waitFor({ timeout: 5000 })
    await page.keyboard.press('Control+g')
    await shot('e2e-4-habit')
  })

  await step('switch to the GUI frame and back', async () => {
    await page.getByRole('button', { name: 'GUI' }).click()
    await page.locator('.pg-vnc canvas').waitFor({ timeout: 15000 })
    await page.waitForTimeout(800)
    await shot('e2e-5-gui')
    await page.getByRole('button', { name: 'Terminal', exact: true }).click()
    await page.locator('.pg-terminal').waitFor({ state: 'visible', timeout: 5000 })
  })

  await step('project P6: preview reloads on save', async () => {
    await page.goto(url + '/project/p6-polyglot', { waitUntil: 'load' })
    await page.locator('.xterm-screen').waitFor()
    await page.getByText('Look around the project').first().waitFor({ timeout: 15000 })
    await page.locator('canvas[aria-label^="Page 1"]').waitFor({ timeout: 15000 })
    await page.waitForTimeout(1500)
    await shot('e2e-6a-project')
    await focusTerminal()
    await page.keyboard.press('Control+x')
    await page.keyboard.press('Control+f')
    await page.keyboard.type('~/polyglot/docs/report.typ', { delay: 10 })
    await page.keyboard.press('Enter')
    await page.waitForTimeout(300)
    await page.keyboard.press('Alt+Shift+Comma')
    for (let i = 0; i < 3; i++) await page.keyboard.press('Control+n')
    await page.keyboard.press('Control+e')
    await page.keyboard.type(' twice', { delay: 20 })
    await page.keyboard.press('Control+x')
    await page.keyboard.press('Control+s')
    await page.getByText(/report\.pdf updated/).waitFor({ timeout: 10000 })
    await page.waitForTimeout(1000)
    await shot('e2e-6-project-preview')
  })
} finally {
  console.log(JSON.stringify({ results, errors }, null, 1))
  await page.close()
  await browser.close()
  if (results.some((r) => !r.ok) || errors.length) process.exitCode = 1
}
