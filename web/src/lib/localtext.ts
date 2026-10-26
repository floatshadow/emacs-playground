import type { LocalText } from '../protocol.gen'

/**
 * Resolves a LocalText (a plain string or a locale map) for a locale, falling
 * back to the base language, then "en", then the first entry.
 */
export function lt(text: LocalText | undefined | null, locale = 'en'): string {
  if (text == null) return ''
  if (typeof text === 'string') return text
  const base = locale.split('-')[0] ?? locale
  return text[locale] ?? text[base] ?? text['en'] ?? Object.values(text)[0] ?? ''
}
