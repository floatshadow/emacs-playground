/**
 * UI strings through react-i18next (frontend.md §12). Key names and command
 * names are never translated. Lesson content and tables are localised by the
 * gateway (Accept-Language, set from the same locale).
 */
import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import en from './en.json'
import zhCN from './zh-CN.json'
import { setRequestLocale } from '../api/http'
import { useUiStore } from '../store/ui'
import { missingKeys } from './keys'

const ALL_LOCALES = [
  { id: 'en', label: 'English', strings: en },
  { id: 'zh-CN', label: '简体中文', strings: zhCN },
]

/**
 * The locales offered in the preferences: those whose strings cover every
 * English key. A partial translation stays in the bundle but is not
 * offered, so a page is never half in one language.
 */
export const LOCALES = ALL_LOCALES.filter((l) => missingKeys(en, l.strings).length === 0).map(
  ({ id, label }) => ({ id, label }),
)

// A stored (or browser-derived) locale that is not offered falls back to English.
if (!LOCALES.some((l) => l.id === useUiStore.getState().locale)) {
  useUiStore.setState({ locale: 'en' })
}

const initial = useUiStore.getState().locale

function applyLocale(locale: string) {
  setRequestLocale(locale)
  if (typeof document !== 'undefined') document.documentElement.lang = locale
}

void i18n.use(initReactI18next).init({
  resources: Object.fromEntries(ALL_LOCALES.map((l) => [l.id, { translation: l.strings }])),
  lng: initial,
  fallbackLng: 'en',
  interpolation: { escapeValue: false },
  returnNull: false,
})
applyLocale(initial)

useUiStore.subscribe((s, prev) => {
  if (s.locale !== prev.locale) {
    applyLocale(s.locale)
    void i18n.changeLanguage(s.locale)
  }
})

export default i18n
