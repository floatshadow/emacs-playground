/**
 * A minimal RFC 6455 WebSocket server for the fake session: handshake,
 * masked client frames (text, binary, close, ping, pong, continuation),
 * unmasked server frames. Enough for the browser and for tests; not for
 * production.
 */
import { createHash } from 'node:crypto'
import type { IncomingMessage } from 'node:http'
import type { Duplex } from 'node:stream'

const GUID = '258EAFA5-E914-47DA-95CA-C5AB0DC85B11'

export type WsMessage = { text: string } | { binary: Buffer }

export class WsConn {
  readonly path: string
  readonly query: URLSearchParams
  private socket: Duplex
  private buf = Buffer.alloc(0)
  private frag: { op: number; parts: Buffer[] } | null = null
  closed = false
  onMessage: (m: WsMessage) => void = () => {}
  onClose: (code: number) => void = () => {}

  constructor(socket: Duplex, url: URL) {
    this.socket = socket
    this.path = url.pathname
    this.query = url.searchParams
    socket.on('data', (d: Buffer) => this.feed(d))
    socket.on('close', () => this.finish(1006))
    socket.on('error', () => this.finish(1006))
  }

  private finish(code: number) {
    if (this.closed) return
    this.closed = true
    this.onClose(code)
  }

  private feed(d: Buffer) {
    this.buf = Buffer.concat([this.buf, d])
    for (;;) {
      if (this.buf.length < 2) return
      const b0 = this.buf[0] as number
      const b1 = this.buf[1] as number
      const fin = (b0 & 0x80) !== 0
      const op = b0 & 0x0f
      const masked = (b1 & 0x80) !== 0
      let len = b1 & 0x7f
      let off = 2
      if (len === 126) {
        if (this.buf.length < 4) return
        len = this.buf.readUInt16BE(2)
        off = 4
      } else if (len === 127) {
        if (this.buf.length < 10) return
        len = Number(this.buf.readBigUInt64BE(2))
        off = 10
      }
      const maskOff = off
      if (masked) off += 4
      if (this.buf.length < off + len) return
      let payload = Buffer.from(this.buf.subarray(off, off + len))
      if (masked) {
        const mask = this.buf.subarray(maskOff, maskOff + 4)
        for (let i = 0; i < payload.length; i++)
          payload[i] = (payload[i] as number) ^ (mask[i % 4] as number)
      }
      this.buf = this.buf.subarray(off + len)
      if (op === 0x8) {
        const code = payload.length >= 2 ? payload.readUInt16BE(0) : 1005
        this.close(code === 1005 ? 1000 : code)
        this.finish(code)
        return
      }
      if (op === 0x9) {
        this.frame(0xa, payload)
        continue
      }
      if (op === 0xa) continue
      if (op === 0x0 && this.frag) {
        this.frag.parts.push(payload)
        if (!fin) continue
        payload = Buffer.concat(this.frag.parts)
        const fop = this.frag.op
        this.frag = null
        this.deliver(fop, payload)
        continue
      }
      if (!fin) {
        this.frag = { op, parts: [payload] }
        continue
      }
      this.deliver(op, payload)
    }
  }

  private deliver(op: number, payload: Buffer) {
    if (op === 0x1) this.onMessage({ text: payload.toString('utf8') })
    else if (op === 0x2) this.onMessage({ binary: payload })
  }

  private frame(op: number, payload: Buffer) {
    if (this.closed) return
    const len = payload.length
    let head: Buffer
    if (len < 126) {
      head = Buffer.from([0x80 | op, len])
    } else if (len < 65536) {
      head = Buffer.alloc(4)
      head[0] = 0x80 | op
      head[1] = 126
      head.writeUInt16BE(len, 2)
    } else {
      head = Buffer.alloc(10)
      head[0] = 0x80 | op
      head[1] = 127
      head.writeBigUInt64BE(BigInt(len), 2)
    }
    this.socket.write(Buffer.concat([head, payload]))
  }

  sendText(s: string) {
    this.frame(0x1, Buffer.from(s, 'utf8'))
  }

  sendJSON(v: unknown) {
    this.sendText(JSON.stringify(v))
  }

  sendBinary(b: Buffer | Uint8Array | string) {
    this.frame(0x2, typeof b === 'string' ? Buffer.from(b, 'utf8') : Buffer.from(b))
  }

  close(code = 1000, reason = '') {
    if (this.closed) return
    const r = Buffer.from(reason, 'utf8')
    const p = Buffer.alloc(2 + r.length)
    p.writeUInt16BE(code, 0)
    r.copy(p, 2)
    this.frame(0x8, p)
    this.closed = true
    this.socket.end()
    this.onClose(code)
  }
}

/** Completes the handshake for an HTTP upgrade request and returns the connection. */
export function acceptUpgrade(req: IncomingMessage, socket: Duplex): WsConn | null {
  const key = req.headers['sec-websocket-key']
  if (typeof key !== 'string' || (req.headers.upgrade ?? '').toLowerCase() !== 'websocket') {
    socket.end('HTTP/1.1 400 Bad Request\r\n\r\n')
    return null
  }
  const accept = createHash('sha1')
    .update(key + GUID)
    .digest('base64')
  const proto = req.headers['sec-websocket-protocol']
  const lines = [
    'HTTP/1.1 101 Switching Protocols',
    'Upgrade: websocket',
    'Connection: Upgrade',
    `Sec-WebSocket-Accept: ${accept}`,
  ]
  // noVNC asks for "binary"; echo the first offered subprotocol.
  if (typeof proto === 'string' && proto.trim())
    lines.push(`Sec-WebSocket-Protocol: ${proto.split(',')[0]?.trim()}`)
  socket.write(lines.join('\r\n') + '\r\n\r\n')
  const url = new URL(req.url ?? '/', 'http://localhost')
  if ('setNoDelay' in socket && typeof socket.setNoDelay === 'function') socket.setNoDelay(true)
  return new WsConn(socket, url)
}
