// Minimal types for the parts of noVNC 1.7 (core/rfb.js) the app uses.
declare module '@novnc/novnc' {
  export interface RFBOptions {
    shared?: boolean
    credentials?: { username?: string; password?: string; target?: string }
    repeaterID?: string
    wsProtocols?: string[]
  }
  export default class RFB extends EventTarget {
    constructor(target: HTMLElement, urlOrChannel: string | WebSocket, options?: RFBOptions)
    scaleViewport: boolean
    clipViewport: boolean
    resizeSession: boolean
    showDotCursor: boolean
    viewOnly: boolean
    focusOnClick: boolean
    background: string
    qualityLevel: number
    compressionLevel: number
    disconnect(): void
    focus(options?: FocusOptions): void
    blur(): void
    clipboardPasteFrom(text: string): void
    sendCtrlAltDel(): void
    sendKey(keysym: number, code: string | null, down?: boolean): void
  }
}
