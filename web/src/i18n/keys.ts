/**
 * Comparing translation files: i18next plural forms (key_one, key_other,
 * …) count as one key, since languages have different sets of them.
 */

const PLURAL = /_(zero|one|two|few|many|other)$/

type Strings = { readonly [key: string]: unknown }

/** The dotted keys of a translation file, plural forms folded into their base key. */
export function translationKeys(strings: Strings, prefix = ''): Set<string> {
  const out = new Set<string>()
  for (const [k, v] of Object.entries(strings)) {
    const key = prefix + k.replace(PLURAL, '')
    if (v !== null && typeof v === 'object') {
      for (const sub of translationKeys(v as Strings, key + '.')) out.add(sub)
    } else out.add(key)
  }
  return out
}

/** Keys of base that other lacks. */
export function missingKeys(base: Strings, other: Strings): string[] {
  const have = translationKeys(other)
  return [...translationKeys(base)].filter((k) => !have.has(k))
}
