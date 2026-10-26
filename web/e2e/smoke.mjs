// Smoke test through a remote Chrome (no local browser on the dev host):
//   docker run -d --rm --name pg-chrome --network host zenika/alpine-chrome:latest \
//     --headless --no-sandbox --disable-gpu --remote-debugging-address=0.0.0.0 \
//     --remote-debugging-port=9222 about:blank
//   PG_URL=http://127.0.0.1:8080 pnpm e2e
// Environment: PG_CDP (default http://127.0.0.1:9222), PG_URL (default
// http://127.0.0.1:8080), PG_SHOT (optional screenshot path).
// The frontend.md §14 scenarios (lesson 2.3, ticker, steps, reset, GUI
// frame, preview reload) are in e2e/lesson.mjs (pnpm e2e:lesson).
import { chromium } from 'playwright-core'

const cdp = process.env.PG_CDP ?? 'http://127.0.0.1:9222'
const url = process.env.PG_URL ?? 'http://127.0.0.1:8080'

const browser = await chromium.connectOverCDP(cdp)
try {
  const context = browser.contexts()[0] ?? (await browser.newContext())
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', (e) => errors.push(String(e)))
  await page.goto(url, { waitUntil: 'load', timeout: 30000 })
  await page.locator('h1').first().waitFor({ timeout: 15000 })
  const title = await page.title()
  const heading = await page.locator('h1').first().textContent()
  if (process.env.PG_SHOT) await page.screenshot({ path: process.env.PG_SHOT, fullPage: true })
  console.log(JSON.stringify({ url, title, heading, errors }))
  await page.close()
  if (errors.length) process.exitCode = 1
} finally {
  await browser.close()
}
