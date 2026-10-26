/**
 * A minimal RFB 3.8 server over the fake session's /vnc WebSocket, so the
 * GUI pane (noVNC) has something to connect to. It draws an abstract GUI
 * frame (menu bar, windows and mode lines following the mock Emacs layout)
 * with raw encoding, and decodes KeyEvent messages into Emacs key names,
 * like sessiond's RFB tap (backend.md §3.6).
 */
import type { WsConn } from './ws.ts'

const W = 960
const H = 540

type Rgb = [number, number, number]

export interface RfbScene {
  /** Windows to draw: side-by-side or stacked, and which one is selected. */
  windows(): { selected: boolean }[]
  side(): boolean
  title(): string
}

interface PixelFormat {
  bpp: number
  bigEndian: boolean
  rshift: number
  gshift: number
  bshift: number
}

const KEYSYMS: Record<number, string> = {
  0xff0d: 'RET',
  0xff08: 'DEL',
  0xff09: 'TAB',
  0xff1b: 'ESC',
  0xff51: '<left>',
  0xff52: '<up>',
  0xff53: '<right>',
  0xff54: '<down>',
  0xff50: '<home>',
  0xff57: '<end>',
  0xff63: '<insert>',
  0xffff: '<deletechar>',
  0xffbe: '<f1>',
  0xffbf: '<f2>',
  0xffc0: '<f3>',
  0xffc1: '<f4>',
  0x20: 'SPC',
}
const MODS: Record<number, 'C' | 'M' | 'S'> = {
  0xffe3: 'C',
  0xffe4: 'C',
  0xffe9: 'M',
  0xffea: 'M',
  0xffe7: 'M',
  0xffe8: 'M',
  0xffe1: 'S',
  0xffe2: 'S',
}

/** Serves one RFB connection on ws. onKey receives decoded key names. */
export function serveRfb(
  ws: WsConn,
  scene: RfbScene,
  onKey: (key: string) => void,
): { refresh(): void } {
  let stage: 'version' | 'security' | 'init' | 'normal' = 'version'
  let buf = Buffer.alloc(0)
  let pf: PixelFormat = { bpp: 32, bigEndian: false, rshift: 16, gshift: 8, bshift: 0 }
  let pending = false
  let dirty = true
  const mods = new Set<'C' | 'M' | 'S'>()

  ws.sendBinary(Buffer.from('RFB 003.008\n', 'latin1'))

  function frame(): Buffer {
    const bytes = pf.bpp / 8
    const out = Buffer.alloc(W * H * bytes)
    const px = (x: number, y: number, c: Rgb) => {
      const v = ((c[0] << pf.rshift) | (c[1] << pf.gshift) | (c[2] << pf.bshift)) >>> 0
      const o = (y * W + x) * bytes
      if (bytes === 4) {
        if (pf.bigEndian) out.writeUInt32BE(v, o)
        else out.writeUInt32LE(v, o)
      } else if (bytes === 2) {
        if (pf.bigEndian) out.writeUInt16BE(v & 0xffff, o)
        else out.writeUInt16LE(v & 0xffff, o)
      } else out[o] = v & 0xff
    }
    const rect = (x0: number, y0: number, w: number, h: number, c: Rgb) => {
      for (let y = y0; y < Math.min(H, y0 + h); y++)
        for (let x = x0; x < Math.min(W, x0 + w); x++) px(x, y, c)
    }
    rect(0, 0, W, H, [13, 14, 28])
    rect(0, 0, W, 26, [233, 233, 239])
    for (let i = 0; i < 6; i++) rect(14 + i * 64, 9, 44, 8, [120, 120, 130])
    const wins = scene.windows()
    const side = scene.side()
    const top = 26
    const avail = H - top - 22
    wins.forEach((w, i) => {
      const n = wins.length
      const x0 = side ? Math.floor((W / n) * i) : 0
      const y0 = side ? top : top + Math.floor((avail / n) * i)
      const ww = side ? Math.floor(W / n) - (i < n - 1 ? 2 : 0) : W
      const hh = side ? avail : Math.floor(avail / n)
      // text lines as soft bars
      for (let l = 0; l < Math.floor((hh - 24) / 18); l++) {
        const len = 80 + ((l * 137 + i * 53) % 420)
        rect(
          x0 + 14,
          y0 + 10 + l * 18,
          Math.min(len, ww - 28),
          7,
          l % 4 === 0 ? [182, 160, 255] : [120, 168, 255],
        )
      }
      rect(x0, y0 + hh - 20, ww, 20, w.selected ? [72, 77, 103] : [41, 45, 72])
      rect(x0 + 10, y0 + hh - 13, 120, 6, w.selected ? [255, 255, 255] : [154, 154, 176])
    })
    rect(0, H - 22, W, 22, [13, 14, 28])
    rect(10, H - 15, 260, 6, [111, 75, 181])
    return out
  }

  function sendUpdate() {
    const pixels = frame()
    const head = Buffer.alloc(4 + 12)
    head[0] = 0
    head.writeUInt16BE(1, 2)
    head.writeUInt16BE(0, 4)
    head.writeUInt16BE(0, 6)
    head.writeUInt16BE(W, 8)
    head.writeUInt16BE(H, 10)
    head.writeInt32BE(0, 12)
    ws.sendBinary(Buffer.concat([head, pixels]))
    pending = false
    dirty = false
  }

  function keyName(sym: number): string | null {
    let base = KEYSYMS[sym]
    if (!base) {
      if (sym >= 0x21 && sym <= 0x7e) base = String.fromCharCode(sym)
      else if (sym >= 0xa0 && sym <= 0xff) base = String.fromCharCode(sym)
      else return null
    }
    let prefix = ''
    if (mods.has('C')) prefix += 'C-'
    if (mods.has('M')) prefix += 'M-'
    if (prefix && base.length === 1) base = base.toLowerCase()
    return prefix + base
  }

  ws.onMessage = (m) => {
    if (!('binary' in m)) return
    buf = Buffer.concat([buf, m.binary])
    for (;;) {
      if (stage === 'version') {
        if (buf.length < 12) return
        buf = buf.subarray(12)
        ws.sendBinary(Buffer.from([1, 1])) // one security type: None
        stage = 'security'
        continue
      }
      if (stage === 'security') {
        if (buf.length < 1) return
        buf = buf.subarray(1)
        ws.sendBinary(Buffer.from([0, 0, 0, 0]))
        stage = 'init'
        continue
      }
      if (stage === 'init') {
        if (buf.length < 1) return
        buf = buf.subarray(1)
        const name = Buffer.from(scene.title(), 'utf8')
        const si = Buffer.alloc(24 + name.length)
        si.writeUInt16BE(W, 0)
        si.writeUInt16BE(H, 2)
        si[4] = 32
        si[5] = 24
        si[6] = 0
        si[7] = 1
        si.writeUInt16BE(255, 8)
        si.writeUInt16BE(255, 10)
        si.writeUInt16BE(255, 12)
        si[14] = 16
        si[15] = 8
        si[16] = 0
        si.writeUInt32BE(name.length, 20)
        name.copy(si, 24)
        ws.sendBinary(si)
        stage = 'normal'
        continue
      }
      if (buf.length < 1) return
      const type = buf[0] as number
      if (type === 0) {
        if (buf.length < 20) return
        pf = {
          bpp: buf[4] as number,
          bigEndian: buf[6] !== 0,
          rshift: buf[14] as number,
          gshift: buf[15] as number,
          bshift: buf[16] as number,
        }
        buf = buf.subarray(20)
        dirty = true
      } else if (type === 2) {
        if (buf.length < 4) return
        const n = buf.readUInt16BE(2)
        if (buf.length < 4 + 4 * n) return
        buf = buf.subarray(4 + 4 * n)
      } else if (type === 3) {
        if (buf.length < 10) return
        const incremental = buf[1] !== 0
        buf = buf.subarray(10)
        if (!incremental || dirty) sendUpdate()
        else pending = true
      } else if (type === 4) {
        if (buf.length < 8) return
        const down = buf[1] !== 0
        const sym = buf.readUInt32BE(4)
        buf = buf.subarray(8)
        const mod = MODS[sym]
        if (mod) {
          if (down) mods.add(mod)
          else mods.delete(mod)
        } else if (down) {
          const k = keyName(sym)
          if (k) onKey(k)
        }
      } else if (type === 5) {
        if (buf.length < 6) return
        buf = buf.subarray(6)
      } else if (type === 6) {
        if (buf.length < 8) return
        const len = buf.readUInt32BE(4)
        if (buf.length < 8 + len) return
        buf = buf.subarray(8 + len)
      } else {
        // Unknown message: drop the buffer rather than desynchronise.
        buf = Buffer.alloc(0)
      }
    }
  }

  return {
    refresh() {
      dirty = true
      if (pending) sendUpdate()
    },
  }
}
