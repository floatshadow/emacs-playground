import { describe, expect, it } from 'vitest'
import en from './en.json'
import { missingKeys, translationKeys } from './keys'
import i18n, { LOCALES } from './index'

describe('translation keys', () => {
  it('folds plural forms and nests with dots', () => {
    const keys = translationKeys({ a: { typed_one: 'x', typed_other: 'y' }, b: 'z' })
    expect([...keys].sort()).toEqual(['a.typed', 'b'])
  })
  it('lists what a translation lacks', () => {
    expect(missingKeys({ a: { b: '1', c: '2' } }, { a: { b: '1' } })).toEqual(['a.c'])
    expect(missingKeys({ n_one: '1', n_other: '2' }, { n_other: '2' })).toEqual([])
  })
})

describe('locales', () => {
  it('offers only complete translations, English always', () => {
    expect(LOCALES.map((l) => l.id)).toContain('en')
    expect(missingKeys(en, en)).toEqual([])
  })
  it('sets the page language from the start', () => {
    expect(document.documentElement.lang).toBe(i18n.language)
  })
})
