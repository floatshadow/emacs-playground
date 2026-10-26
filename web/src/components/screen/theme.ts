import type { ITheme } from '@xterm/xterm'

/**
 * modus-vivendi-tinted, the palette the demo's screen uses (frontend.md §5).
 * Emacs draws its own faces in true colour; these are the 16 ANSI colours,
 * the cursor and the selection.
 */
export const MODUS_VIVENDI_TINTED: ITheme = {
  background: '#0d0e1c',
  foreground: '#ffffff',
  cursor: '#ffffff',
  cursorAccent: '#0d0e1c',
  selectionBackground: '#555a66',
  selectionForeground: '#ffffff',
  black: '#0d0e1c',
  red: '#ff5f59',
  green: '#44bc44',
  yellow: '#d0bc00',
  blue: '#2fafff',
  magenta: '#feacd0',
  cyan: '#00d3d0',
  white: '#c8c8d8',
  brightBlack: '#61647a',
  brightRed: '#ff6b55',
  brightGreen: '#00c06f',
  brightYellow: '#fec43f',
  brightBlue: '#79a8ff',
  brightMagenta: '#b6a0ff',
  brightCyan: '#6ae4b9',
  brightWhite: '#ffffff',
}

export const TERMINAL_FONT = '"Iosevka Nerd Font", "JetBrains Mono", monospace'
