/**
 * Typed session RPC (api.md §3.4): POST /s/{id}/rpc/{op} → {ok, result}.
 * Errors become toasts unless the caller passes { silent: true } and handles
 * them itself.
 */
import type { RpcOk, RpcOps } from '../../protocol.gen'
import { useSessionStore, whenSessionId } from '../../store/session'
import { toast } from '../../store/ui'
import { ApiError, request } from '../http'

export type RpcOp = keyof RpcOps
export type RpcRequest<K extends RpcOp> = RpcOps[K]['request']
export type RpcResult<K extends RpcOp> = RpcOps[K]['result']

export interface RpcOptions {
  /** Do not toast errors; the caller handles them. */
  silent?: boolean
  signal?: AbortSignal
}

/** The session path prefix, /s/{id}. Local and server differ only here (frontend.md §4). */
export function sessionBase(id: string): string {
  return `/s/${encodeURIComponent(id)}`
}

export function wsUrl(path: string): string {
  const { protocol, host } = window.location
  return `${protocol === 'https:' ? 'wss' : 'ws'}://${host}${path}`
}

export async function rpc<K extends RpcOp>(
  op: K,
  body: RpcRequest<K>,
  opts: RpcOptions = {},
): Promise<RpcResult<K>> {
  try {
    const id = useSessionStore.getState().sessionId ?? (await whenSessionId(15000))
    const res = await request<RpcOk<RpcResult<K>> | undefined>(`${sessionBase(id)}/rpc/${op}`, {
      method: 'POST',
      body: body ?? {},
      ...(opts.signal ? { signal: opts.signal } : {}),
    })
    return (res?.result ?? {}) as RpcResult<K>
  } catch (e) {
    if (!opts.silent && !(e instanceof DOMException && e.name === 'AbortError')) {
      const msg = e instanceof ApiError ? e.message : e instanceof Error ? e.message : String(e)
      toast(`${op}: ${msg}`, 'error')
    }
    throw e
  }
}
