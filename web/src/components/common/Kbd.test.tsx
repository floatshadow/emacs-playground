/// <reference types="node" />
import { describe, expect, it } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { render, screen } from '@testing-library/react'
import { Kbd } from './Kbd'

// The stylesheets as written (the test build stubs CSS); tests run in web/.
const src = join(process.cwd(), 'src')
const css = (rel: string) => readFileSync(join(src, rel), 'utf8')
const sheets = (readdirSync(src, { recursive: true }) as string[]).filter((f) =>
  f.endsWith('.module.css'),
)

describe('ligatures', () => {
  it('a keycap draws M-> as three keys: no ligatures', () => {
    const style = document.createElement('style')
    style.textContent = css('components/common/Kbd.module.css')
    document.head.append(style)
    render(<Kbd keys="M->" />)
    expect(getComputedStyle(screen.getByText('M->')).fontVariantLigatures).toBe('none')
    style.remove()
  })

  it('every module rule that sets a monospace font turns ligatures off', () => {
    for (const file of sheets) {
      for (const rule of css(file).match(/[^{}]+\{[^}]*font:[^;}]*var\(--mono\)[^}]*\}/g) ?? []) {
        expect(rule, file).toContain('font-variant-ligatures: none')
      }
    }
  })
})
