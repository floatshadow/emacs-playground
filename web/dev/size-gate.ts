/**
 * Bundle size gate (frontend.md §15): the JavaScript loaded up front (the
 * entry script and its modulepreloads in dist/index.html) must stay under
 * 600 KB gzipped. Lazy chunks (course map, project, pdf.js, noVNC) are
 * listed for information.
 *
 *   pnpm build && node dev/size-gate.ts [dist] [--limit 600]
 */
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { gzipSync } from 'node:zlib'

const dist = process.argv[2] && !process.argv[2].startsWith('--') ? process.argv[2] : 'dist'
const li = process.argv.indexOf('--limit')
const limitKb = li >= 0 ? Number(process.argv[li + 1]) : 600

const html = readFileSync(join(dist, 'index.html'), 'utf8')
const initial = new Set<string>()
for (const m of html.matchAll(/<script[^>]+src="\/([^"]+\.js)"/g)) initial.add(m[1] as string)
for (const m of html.matchAll(/<link[^>]+rel="modulepreload"[^>]+href="\/([^"]+\.js)"/g))
  initial.add(m[1] as string)
for (const m of html.matchAll(/<link[^>]+href="\/([^"]+\.js)"[^>]+rel="modulepreload"/g))
  initial.add(m[1] as string)

const gz = (f: string) => gzipSync(readFileSync(join(dist, f))).length
const kb = (n: number) => (n / 1024).toFixed(1) + ' KB'

let total = 0
console.log('initial JavaScript (gzipped):')
for (const f of [...initial].sort()) {
  const n = gz(f)
  total += n
  console.log(`  ${kb(n).padStart(10)}  ${f}`)
}
console.log(`  ${kb(total).padStart(10)}  total (limit ${limitKb} KB)`)

const lazy = readdirSync(join(dist, 'assets'))
  .filter((f) => f.endsWith('.js') && !initial.has('assets/' + f))
  .map((f) => [f, gz('assets/' + f)] as const)
  .sort((a, b) => b[1] - a[1])
console.log('lazy chunks (gzipped):')
for (const [f, n] of lazy.slice(0, 8)) console.log(`  ${kb(n).padStart(10)}  assets/${f}`)

if (total > limitKb * 1024) {
  console.error(`size gate: initial JavaScript ${kb(total)} exceeds ${limitKb} KB`)
  process.exit(1)
}
