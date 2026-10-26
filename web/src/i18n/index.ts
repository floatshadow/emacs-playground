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

export const LOCALES = [
  { id: 'en', label: 'English' },
  { id: 'zh-CN', label: '简体中文' },
] as const

const initial = useUiStore.getState().locale

void i18n.use(initReactI18next).init({
  resources: { en: { translation: en }, 'zh-CN': { translation: zhCN } },
  lng: initial,
  fallbackLng: 'en',
  interpolation: { escapeValue: false },
  returnNull: false,
})
setRequestLocale(initial)

useUiStore.subscribe((s, prev) => {
  if (s.locale !== prev.locale) {
    setRequestLocale(s.locale)
    void i18n.changeLanguage(s.locale)
    if (typeof document !== 'undefined') document.documentElement.lang = s.locale
  }
})

export default i18n
