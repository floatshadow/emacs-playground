// Screenshots of every screen against a running app (fake session or real
// gateway), for visual review:
//   PG_CDP=http://127.0.0.1:9222 PG_URL=http://127.0.0.1:8090 PG_OUT=../artifacts node e2e/shots.mjs
// Write screenshots under the repository (artifacts/ is git-ignored): the Docker host may not share /tmp.
import { chromium } from 'playwright-core'

const cdp = process.env.PG_CDP ?? 'http://127.0.0.1:9222'
const url = process.env.PG_URL ?? 'http://127.0.0.1:8090'
const out = process.env.PG_OUT ?? '../artifacts'
const width = Number(process.env.PG_WIDTH ?? 1600)
const height = Number(process.env.PG_HEIGHT ?? 1000)

const browser = await chromium.connectOverCDP(cdp)
const context = browser.contexts()[0] ?? (await browser.newContext())
const page = await context.newPage()
await page.setViewportSize({ width, height })
const errors = []
page.on('pageerror', (e) => errors.push(String(e)))
page.on('console', (m) => {
  if (m.type() === 'error') errors.push('console: ' + m.text())
})

async function shot(path, name, wait = 'h1') {
  await page.goto(url + path, { waitUntil: 'load', timeout: 30000 })
  await page.locator(wait).first().waitFor({ timeout: 15000 })
  await page.waitForTimeout(1200)
  await page.screenshot({ path: `${out}/${name}.png` })
  console.log('shot', name)
}

try {
  await shot('/', 'map')
  await shot('/lesson/02-buffers-and-files/03-opening-files', 'lesson', '.xterm')
  await shot('/project/p6-polyglot', 'project', '.xterm')
  await shot('/session', 'session')
} finally {
  console.log(JSON.stringify({ errors }, null, 1))
  await page.close()
  await browser.close()
}
