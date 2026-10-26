/**
 * Persistent hosts for the terminal (xterm.js + PtyClient) and the GUI frame
 * (noVNC). They live above the router like the events stream: a pane
 * component only moves the host element into its container, so navigating
 * between lessons and projects keeps the sockets open and the screen intact
 * (frontend.md §2, §4).
 */
import { Terminal, type ITerminalOptions } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { FitAddon } from '@xterm/addon-fit'
import { WebglAddon } from '@xterm/addon-webgl'
import { Unicode11Addon } from '@xterm/addon-unicode11'
import { ClipboardAddon } from '@xterm/addon-clipboard'
import { WebLinksAddon } from '@xterm/addon-web-links'
import { PtyClient } from '../../api/session/pty'
import { sessionBase, wsUrl } from '../../api/session/rpc'
import { VncClient } from '../../api/session/vnc'
import { debounce } from '../../lib/debounce'
import { useSessionStore } from '../../store/session'
import { toast, useUiStore } from '../../store/ui'
import i18n from '../../i18n'
import { MODUS_VIVENDI_TINTED, TERMINAL_FONT } from './theme'
import { emitTerminalInput, registerTerminalFocus } from './focus'
import { LESSON_PANE_ID } from './paneIds'

/** The exact xterm.js options from frontend.md §5. */
export function terminalOptions(screenReaderMode: boolean): ITerminalOptions {
  return {
    fontFamily: TERMINAL_FONT,
    fontSize: 14,
    lineHeight: 1.2,
    theme: MODUS_VIVENDI_TINTED,
    allowProposedApi: true,
    scrollback: 0,
    macOptionIsMeta: true,
    cursorBlink: false,
    vtExtensions: { kittyKeyboard: true },
    screenReaderMode,
  }
}

const isFirefox = () => typeof navigator !== 'undefined' && /firefox/i.test(navigator.userAgent)

/**
 * The custom key handler's decision (frontend.md §5). It is used for three
 * things only: Shift+Escape leaves the terminal; outside focus mode a
 * browser-reserved combination triggers a one-time hint about Focus mode;
 * everything else passes through to xterm.js, which encodes it.
 */
export function classifyKey(
  e: Pick<KeyboardEvent, 'type' | 'key' | 'ctrlKey' | 'altKey' | 'shiftKey' | 'metaKey' | 'code'>,
  focusMode: boolean,
  firefox = isFirefox(),
): 'leave' | 'reserved' | 'pass' {
  if (e.type !== 'keydown') return 'pass'
  if (e.key === 'Escape' && e.shiftKey && !e.ctrlKey && !e.altKey && !e.metaKey) return 'leave'
  if (focusMode) return 'pass'
  const k = e.key.toLowerCase()
  if (e.ctrlKey && !e.altKey && !e.shiftKey && (k === 'w' || k === 't' || k === 'n'))
    return 'reserved'
  if (e.ctrlKey && e.shiftKey && !['control', 'shift'].includes(k)) return 'reserved'
  if (firefox && e.altKey && !e.ctrlKey && /^Key[A-Z]$/.test(e.code)) return 'reserved'
  return 'pass'
}

/** True when the learner has not put focus anywhere yet (or it fell back to the page). */
function nothingFocused(): boolean {
  const a = document.activeElement
  return !a || a === document.body || a === document.documentElement
}

function focusLessonPane() {
  const el = document.getElementById(LESSON_PANE_ID)
  if (el) {
    if (!el.hasAttribute('tabindex')) el.setAttribute('tabindex', '-1')
    el.focus()
  }
}

class TerminalHost {
  readonly el: HTMLDivElement
  term: Terminal | null = null
  private fit: FitAddon | null = null
  pty: PtyClient | null = null
  private container: HTMLElement | null = null
  private observer: ResizeObserver | null = null
  private sessionId: string | null = null
  private fontsReady = false
  constructor() {
    this.el = document.createElement('div')
    this.el.className = 'pg-terminal'
    this.el.style.width = '100%'
    this.el.style.height = '100%'
  }

  private create() {
    const ui = useUiStore.getState()
    const term = new Terminal(terminalOptions(ui.screenReaderMode))
    const fit = new FitAddon()
    term.loadAddon(fit)
    const unicode = new Unicode11Addon()
    term.loadAddon(unicode)
    term.unicode.activeVersion = '11'
    term.loadAddon(new ClipboardAddon())
    term.loadAddon(
      new WebLinksAddon((_e, uri) => {
        window.open(uri, '_blank', 'noopener,noreferrer')
      }),
    )
    term.open(this.el)
    try {
      const webgl = new WebglAddon()
      webgl.onContextLoss(() => webgl.dispose()) // falls back to the DOM renderer
      term.loadAddon(webgl)
    } catch {
      // No WebGL (headless, old GPU): the DOM renderer is used.
    }
    term.attachCustomKeyEventHandler((e) => {
      const verdict = classifyKey(e, useUiStore.getState().focusMode)
      if (verdict === 'leave') {
        e.preventDefault()
        focusLessonPane()
        return false
      }
      if (verdict === 'reserved') {
        const s = useUiStore.getState()
        if (!s.reservedKeyToastShown) {
          s.markReservedKeyToast()
          toast(i18n.t('terminal.reservedKey'))
        }
      }
      return true
    })
    term.onData((d) => {
      emitTerminalInput()
      this.pty?.send(d)
    })
    term.onBinary((d) => {
      const bytes = new Uint8Array(d.length)
      for (let i = 0; i < d.length; i++) bytes[i] = d.charCodeAt(i) & 0xff
      this.pty?.send(bytes)
    })
    this.term = term
    this.fit = fit
    useUiStore.subscribe((s, prev) => {
      if (s.screenReaderMode !== prev.screenReaderMode && this.term) {
        this.term.options.screenReaderMode = s.screenReaderMode
      }
    })
  }

  private readonly refit = debounce(() => this.doFit(), 100)

  private doFit() {
    if (!this.term || !this.fit || !this.container || !this.fontsReady) return
    if (this.container.clientWidth === 0 || this.container.clientHeight === 0) return
    try {
      this.fit.fit()
    } catch {
      return
    }
    const { cols, rows } = this.term
    useUiStore.getState().setTermSize(cols, rows)
    this.pty?.resize(cols, rows)
  }

  attach(container: HTMLElement) {
    if (!this.term) this.create()
    this.container = container
    container.appendChild(this.el)
    this.observer?.disconnect()
    this.observer = new ResizeObserver(() => this.refit())
    this.observer.observe(container)
    if (!this.fontsReady) {
      // A missing font (404) must not keep the terminal from starting: any
      // outcome, or 2.5 s, counts as ready and the fallback font is used.
      const fonts = document.fonts
      const ready = fonts
        ? Promise.race([
            // Both faces of TERMINAL_FONT: when the gateway has no Iosevka,
            // the bundled JetBrains Mono must be loaded before cells are measured.
            Promise.all([
              fonts.load('14px "Iosevka Nerd Font"').catch(() => []),
              fonts.load('14px "JetBrains Mono"').catch(() => []),
            ])
              .then(() => fonts.ready)
              .catch(() => undefined),
            new Promise((r) => setTimeout(r, 2500)),
          ])
        : Promise.resolve()
      void ready.then(() => {
        this.fontsReady = true
        // Cell metrics depend on the font: re-apply it so xterm measures again.
        if (this.term) this.term.options.fontFamily = TERMINAL_FONT
        this.doFit()
        this.maybeConnect()
      })
    } else {
      this.refit()
      this.maybeConnect()
      if (this.pty?.state === 'open') this.focusIfIdle()
    }
  }

  detach(container: HTMLElement) {
    if (this.container !== container) return
    this.observer?.disconnect()
    this.observer = null
    this.container = null
    if (this.el.parentElement === container) container.removeChild(this.el)
  }

  setSession(id: string | null) {
    if (id === this.sessionId) return
    this.pty?.close()
    this.pty = null
    this.sessionId = id
    this.maybeConnect()
  }

  private maybeConnect() {
    if (!this.sessionId || !this.term || !this.fontsReady || this.pty) return
    const id = this.sessionId
    const term = this.term
    this.pty = new PtyClient({
      url: (cols, rows) => wsUrl(`${sessionBase(id)}/pty?cols=${cols}&rows=${rows}`),
      onData: (bytes) => term.write(bytes),
      onState: (s) => {
        useSessionStore.getState().setConnection('pty', s)
        // First keystrokes go to Emacs, not to the browser (Ctrl+F would open find).
        if (s === 'open') this.focusIfIdle()
      },
    })
    this.pty.connect(term.cols, term.rows)
  }

  reattach() {
    this.pty?.reattach()
    this.term?.focus()
  }

  /** Focuses the terminal when it is on screen and nothing else holds focus. */
  focusIfIdle() {
    if (!this.term || !this.container || !nothingFocused()) return
    if (useSessionStore.getState().mode === 'gui') return
    this.term.focus()
  }

  focus() {
    this.term?.focus()
  }
}

class VncHost {
  readonly el: HTMLDivElement
  private client: VncClient | null = null
  private sessionId: string | null = null

  constructor() {
    this.el = document.createElement('div')
    this.el.className = 'pg-vnc'
    this.el.style.width = '100%'
    this.el.style.height = '100%'
    this.el.addEventListener('paste', (e) => {
      const text = e.clipboardData?.getData('text/plain')
      if (text) this.client?.paste(text)
    })
  }

  attach(container: HTMLElement) {
    container.appendChild(this.el)
  }

  detach(container: HTMLElement) {
    if (this.el.parentElement === container) container.removeChild(this.el)
  }

  setSession(id: string | null) {
    if (id === this.sessionId) return
    this.client?.disconnect()
    this.client = null
    this.sessionId = id
  }

  /** Connects when the session is in GUI mode (noVNC is loaded on first use). */
  ensure() {
    const id = this.sessionId
    if (!id) return
    if (!this.client) {
      this.client = new VncClient({
        url: () => wsUrl(`${sessionBase(id)}/vnc`),
        onState: (s) => useSessionStore.getState().setConnection('vnc', s),
        onClipboard: (text) => {
          void navigator.clipboard?.writeText(text).catch(() => undefined)
        },
        wanted: () => useSessionStore.getState().mode === 'gui',
      })
    }
    void this.client.connect(this.el)
  }

  focus() {
    this.client?.focus()
  }
}

let terminalHost: TerminalHost | null = null
let vncHost: VncHost | null = null

export function getTerminalHost(): TerminalHost {
  if (!terminalHost) {
    const host = new TerminalHost()
    terminalHost = host
    host.setSession(useSessionStore.getState().sessionId)
    useSessionStore.subscribe((s, prev) => {
      if (s.sessionId !== prev.sessionId) host.setSession(s.sessionId)
    })
  }
  return terminalHost
}

export function getVncHost(): VncHost {
  if (!vncHost) {
    const host = new VncHost()
    vncHost = host
    host.setSession(useSessionStore.getState().sessionId)
    useSessionStore.subscribe((s, prev) => {
      if (s.sessionId !== prev.sessionId) host.setSession(s.sessionId)
    })
  }
  return vncHost
}

registerTerminalFocus(() => {
  if (useSessionStore.getState().mode === 'gui' && vncHost) vncHost.focus()
  else terminalHost?.focus()
})
