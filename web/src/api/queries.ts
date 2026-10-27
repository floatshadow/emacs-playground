/**
 * Server state through TanStack Query (frontend.md §3): me, auth config,
 * curriculum, lesson, progress, session and status. Progress is invalidated
 * on step events (see useProgressInvalidation).
 */
import { useEffect } from 'react'
import { QueryClient, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  APIPrefix,
  type AuthConfigInfo,
  type CheckpointsResult,
  type CurriculumIndex,
  type LessonBundle,
  type Me,
  type Progress,
  type SessionInfo,
} from '../protocol.gen'
import { useSessionStore } from '../store/session'
import { useUiStore } from '../store/ui'
import { ApiError, api, lessonPath, request } from './http'
import { ensureSession, onSessionEvent } from './session/connection'
import { rpc } from './session/rpc'

export function makeQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        refetchOnWindowFocus: false,
        retry: (count, err) => {
          if (err instanceof ApiError && err.status >= 400 && err.status < 500) return false
          return count < 2
        },
      },
    },
  })
}

export const qk = {
  me: ['me'] as const,
  authConfig: ['auth-config'] as const,
  curriculum: (locale: string) => ['curriculum', locale] as const,
  lesson: (id: string, locale: string) => ['lesson', id, locale] as const,
  progress: ['progress'] as const,
  session: ['session'] as const,
  sessionInfo: (id: string) => ['session-info', id] as const,
  status: (id: string) => ['status', id] as const,
  checkpoints: (id: string, v: number) => ['checkpoints', id, v] as const,
}

export function useMe() {
  return useQuery({
    queryKey: qk.me,
    queryFn: ({ signal }) => api.get<Me>(`${APIPrefix}/me`, signal),
    staleTime: 5 * 60_000,
  })
}

export function useAuthConfig() {
  return useQuery({
    queryKey: qk.authConfig,
    queryFn: ({ signal }) => api.get<AuthConfigInfo>(`${APIPrefix}/auth/config`, signal),
    staleTime: Infinity,
  })
}

export function useCurriculum() {
  const locale = useUiStore((s) => s.locale)
  return useQuery({
    queryKey: qk.curriculum(locale),
    queryFn: ({ signal }) => api.get<CurriculumIndex>(`${APIPrefix}/curriculum`, signal),
    staleTime: 10 * 60_000,
  })
}

export function useLesson(lessonId: string | undefined) {
  const locale = useUiStore((s) => s.locale)
  return useQuery({
    queryKey: qk.lesson(lessonId ?? '', locale),
    queryFn: ({ signal }) =>
      api.get<LessonBundle>(
        `${APIPrefix}/curriculum/lessons/${lessonPath(lessonId ?? '')}`,
        signal,
      ),
    enabled: !!lessonId,
    staleTime: 10 * 60_000,
  })
}

export function useProgress(enabled = true) {
  return useQuery({
    queryKey: qk.progress,
    queryFn: ({ signal }) => api.get<Progress>(`${APIPrefix}/progress`, signal),
    enabled,
  })
}

/**
 * POST /progress/reset (api.md §2.4): with a lesson id
 * that lesson's records, without one every record of the user ("Start
 * over", posted with no body). The page then forgets what it remembers of
 * that progress (outcomes, hint levels, habit cards and keys seen) and
 * refetches progress; sessiond drops the active step and says so with a
 * "dropped" step event.
 */
export async function resetProgress(qc: QueryClient, lessonId?: string): Promise<void> {
  if (lessonId) await api.post<void>(`${APIPrefix}/progress/reset`, { lessonId })
  else await request<void>(`${APIPrefix}/progress/reset`, { method: 'POST' })
  useSessionStore.getState().forgetProgress(lessonId)
  useUiStore.getState().clearHintLevels(lessonId)
  await qc.invalidateQueries({ queryKey: qk.progress })
}

/** Ensures the session (POST /api/v1/sessions, idempotent) for screens that need Emacs. */
export function useEnsureSession(enabled = true) {
  return useQuery({
    queryKey: qk.session,
    queryFn: () => ensureSession(),
    enabled,
    staleTime: Infinity,
    retry: (count, err) =>
      !(err instanceof ApiError && err.status < 500 && err.status !== 0) && count < 3,
    retryDelay: (n) => Math.min(8000, 1000 * 2 ** n),
  })
}

export function useSessionInfo(id: string | null, poll = false) {
  return useQuery({
    queryKey: qk.sessionInfo(id ?? ''),
    queryFn: ({ signal }) =>
      api.get<SessionInfo>(`${APIPrefix}/sessions/${encodeURIComponent(id ?? '')}`, signal),
    enabled: !!id,
    refetchInterval: poll ? 5000 : false,
  })
}

export function useStatus(id: string | null, poll = false) {
  return useQuery({
    queryKey: qk.status(id ?? ''),
    queryFn: () => rpc('status', {}, { silent: true }),
    enabled: !!id,
    refetchInterval: poll ? 5000 : false,
  })
}

export function useCheckpoints(enabled = true) {
  const id = useSessionStore((s) => s.sessionId)
  const v = useSessionStore((s) => s.checkpointsVersion)
  return useQuery<CheckpointsResult>({
    queryKey: qk.checkpoints(id ?? '', v),
    queryFn: () => rpc('checkpoints', {}, { silent: true }),
    enabled: enabled && !!id,
    staleTime: 10_000,
  })
}

/** Invalidates progress whenever a step loads, completes or resets. */
export function useProgressInvalidation() {
  const qc = useQueryClient()
  useEffect(
    () =>
      onSessionEvent((ev) => {
        if (ev.t === 'step') void qc.invalidateQueries({ queryKey: qk.progress })
      }),
    [qc],
  )
}
