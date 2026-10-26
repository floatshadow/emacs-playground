/**
 * UI state (frontend.md §3): rail open, preview tab, focus mode, locale,
 * reduced motion, toasts, hint levels and the demo player. Preferences
 * persist in localStorage.
 */
import { create } from 'zustand'

export interface Toast {
  id: number
  text: string
  tone?: 'info' | 'error'
}

export interface Prefs {
  locale: string
  reducedMotion: boolean
  screenReaderMode: boolean
}

const PREFS_KEY = 'pg.prefs'

function loadPrefs(): Prefs {
  const def: Prefs = {
    locale:
      typeof navigator !== 'undefined' && navigator.language?.startsWith('zh') ? 'zh-CN' : 'en',
    reducedMotion: false,
    screenReaderMode: false,
  }
  try {
    const raw = typeof localStorage !== 'undefined' ? localStorage.getItem(PREFS_KEY) : null
    return raw ? { ...def, ...(JSON.parse(raw) as Partial<Prefs>) } : def
  } catch {
    return def
  }
}

function savePrefs(p: Prefs) {
  try {
    localStorage.setItem(PREFS_KEY, JSON.stringify(p))
  } catch {
    // private mode: preferences live for the tab only
  }
}

export interface DemoState {
  stepId: string
  goal: string
}

export interface UiStore extends Prefs {
  railOpen: boolean
  previewHidden: boolean
  /** Preview tab per lesson or project id. */
  previewTab: Record<string, string>
  /** Extra preview tabs opened from the file tree, per lesson or project id. */
  previewExtra: Record<string, string[]>
  focusMode: boolean
  /** The browser-reserved-key toast is shown once per tab. */
  reservedKeyToastShown: boolean
  toasts: Toast[]
  /** Hint level per "<lessonId>#<stepId>". */
  hintLevels: Record<string, number>
  demo: DemoState | null
  guiStarting: boolean
  /** Current terminal size in cells, for the screen header. */
  termSize: { cols: number; rows: number } | null
  setTermSize(cols: number, rows: number): void
  setRailOpen(open: boolean): void
  togglePreview(): void
  setPreviewTab(owner: string, path: string): void
  addPreviewTab(owner: string, path: string): void
  setFocusMode(on: boolean): void
  markReservedKeyToast(): void
  toast(text: string, tone?: Toast['tone']): void
  dismissToast(id: number): void
  setHintLevel(key: string, level: number): void
  setDemo(d: DemoState | null): void
  setGuiStarting(on: boolean): void
  setPrefs(p: Partial<Prefs>): void
}

let toastId = 1
const TOAST_MS = 4200

export const useUiStore = create<UiStore>()((set, get) => ({
  ...loadPrefs(),
  railOpen: false,
  previewHidden: false,
  previewTab: {},
  previewExtra: {},
  focusMode: false,
  reservedKeyToastShown: false,
  toasts: [],
  hintLevels: {},
  demo: null,
  guiStarting: false,
  termSize: null,
  setTermSize: (cols, rows) =>
    set((s) =>
      s.termSize?.cols === cols && s.termSize.rows === rows ? s : { termSize: { cols, rows } },
    ),
  setRailOpen: (railOpen) => set({ railOpen }),
  togglePreview: () => set((s) => ({ previewHidden: !s.previewHidden })),
  setPreviewTab: (owner, path) => set((s) => ({ previewTab: { ...s.previewTab, [owner]: path } })),
  addPreviewTab: (owner, path) =>
    set((s) => {
      const cur = s.previewExtra[owner] ?? []
      return {
        previewExtra: cur.includes(path)
          ? s.previewExtra
          : { ...s.previewExtra, [owner]: [...cur, path] },
        previewTab: { ...s.previewTab, [owner]: path },
        previewHidden: false,
      }
    }),
  setFocusMode: (focusMode) => set({ focusMode }),
  markReservedKeyToast: () => set({ reservedKeyToastShown: true }),
  toast: (text, tone = 'info') => {
    const id = toastId++
    set((s) => ({ toasts: [...s.toasts.slice(-2), { id, text, tone }] }))
    setTimeout(() => get().dismissToast(id), TOAST_MS)
  },
  dismissToast: (id) => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
  setHintLevel: (key, level) => set((s) => ({ hintLevels: { ...s.hintLevels, [key]: level } })),
  setDemo: (demo) => set({ demo }),
  setGuiStarting: (guiStarting) => set({ guiStarting }),
  setPrefs: (p) => {
    set(p)
    const { locale, reducedMotion, screenReaderMode } = get()
    savePrefs({ locale, reducedMotion, screenReaderMode })
  },
}))

/** Shorthand for components and clients outside React. */
export const toast = (text: string, tone?: Toast['tone']) => useUiStore.getState().toast(text, tone)
