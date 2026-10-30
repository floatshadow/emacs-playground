import type { TFunction } from 'i18next'
import { ErrGroupQuotaExceeded } from '../protocol.gen'
import { ApiError } from '../api/http'
import { errorMessage } from './errors'

/**
 * Why the session could not start, for the overlay and the session page:
 * a full group quota (429 group_quota_exceeded, api.md §1) names the group,
 * its limit and who can raise it; anything else shows the server's message.
 */
export function sessionStartError(e: unknown, t: TFunction): { title: string; body: string } {
  if (e instanceof ApiError && e.code === ErrGroupQuotaExceeded) {
    const d = (e.details ?? {}) as { group?: unknown; maxActiveSessions?: unknown }
    const group = typeof d.group === 'string' ? d.group : ''
    const max = typeof d.maxActiveSessions === 'number' ? d.maxActiveSessions : 0
    return {
      title: t('overlay.quotaTitle'),
      body: group ? t('overlay.quotaGroup', { group, max }) : t('overlay.quotaDefault', { max }),
    }
  }
  return { title: t('overlay.sessionFailed'), body: errorMessage(e) }
}
