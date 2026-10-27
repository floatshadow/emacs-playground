/**
 * fetch wrapper (frontend.md §1): JSON in and out, problem+json errors as
 * ApiError, same-origin credentials. State-changing requests are plain
 * same-origin fetches, so the browser sends the Origin header the gateway
 * checks (api.md §1); nothing here may switch to no-cors or a foreign origin.
 */
import type { ErrorCode, Problem } from '../protocol.gen'

export class ApiError extends Error {
  readonly status: number
  readonly code: ErrorCode | string
  readonly details?: unknown
  constructor(status: number, code: string, message: string, details?: unknown) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.details = details
  }
}

let acceptLanguage = 'en'

/** Sets the Accept-Language sent with every request (lesson and table locale, frontend.md §12). */
export function setRequestLocale(locale: string) {
  acceptLanguage = locale
}

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  body?: unknown
  signal?: AbortSignal
  /** Expect text instead of JSON. */
  text?: boolean
}

const STATUS_CODES: Record<number, string> = {
  400: 'invalid_request',
  401: 'unauthenticated',
  403: 'forbidden',
  404: 'not_found',
  409: 'conflict',
  429: 'rate_limited',
  501: 'not_implemented',
  503: 'session_unavailable',
  504: 'timeout',
}

async function toError(res: Response): Promise<ApiError> {
  const ct = res.headers.get('content-type') ?? ''
  if (ct.includes('json')) {
    try {
      const p = (await res.json()) as Partial<Problem>
      return new ApiError(
        res.status,
        p.code ?? STATUS_CODES[res.status] ?? 'internal',
        p.message ?? res.statusText,
        p.details,
      )
    } catch {
      // fall through
    }
  }
  let text = ''
  try {
    text = (await res.text()).slice(0, 300)
  } catch {
    // ignore
  }
  return new ApiError(
    res.status,
    STATUS_CODES[res.status] ?? 'internal',
    text || res.statusText || `HTTP ${res.status}`,
  )
}

export async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {
    Accept: opts.text ? '*/*' : 'application/json',
    'Accept-Language': acceptLanguage,
  }
  const init: RequestInit = {
    method: opts.method ?? (opts.body !== undefined ? 'POST' : 'GET'),
    headers,
    credentials: 'same-origin',
  }
  if (opts.signal) init.signal = opts.signal
  if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(opts.body)
  }
  let res: Response
  try {
    res = await fetch(path, init)
  } catch (e) {
    if (e instanceof DOMException && e.name === 'AbortError') throw e
    throw new ApiError(
      0,
      'session_unavailable',
      'Cannot reach the playground server. Is it running?',
    )
  }
  if (!res.ok) throw await toError(res)
  if (res.status === 204) return undefined as T
  if (opts.text) return (await res.text()) as T
  const ct = res.headers.get('content-type') ?? ''
  if (!ct.includes('json')) return (await res.text()) as T
  return (await res.json()) as T
}

export const api = {
  get: <T>(path: string, signal?: AbortSignal) => request<T>(path, signal ? { signal } : {}),
  post: <T>(path: string, body: unknown = {}) => request<T>(path, { method: 'POST', body }),
  del: <T>(path: string) => request<T>(path, { method: 'DELETE' }),
}

/** Lesson ids contain one slash and go into URLs literally (api.md §1). */
export function lessonPath(lessonId: string): string {
  return lessonId
    .split('/')
    .map((s) => encodeURIComponent(s))
    .join('/')
}

/** A workspace path for /preview/: each segment encoded, slashes kept. */
export function workspacePath(p: string): string {
  return p
    .replace(/^\/+/, '')
    .split('/')
    .map((s) => encodeURIComponent(s))
    .join('/')
}
