/** Preview helpers: which view a file gets, and its URL under /s/{id}/preview/. */
import { workspacePath } from '../../api/http'
import { sessionBase } from '../../api/session/rpc'

export type PreviewKind = 'pdf' | 'html' | 'markdown' | 'image' | 'text'

export function previewKind(path: string): PreviewKind {
  const ext = path.toLowerCase().split('.').pop() ?? ''
  if (ext === 'pdf') return 'pdf'
  if (ext === 'html' || ext === 'htm') return 'html'
  if (ext === 'md' || ext === 'markdown') return 'markdown'
  if (['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp', 'avif', 'bmp'].includes(ext)) return 'image'
  return 'text'
}

/** True for artefacts worth a preview tab (the tree offers "Preview" for these). */
export function isPreviewable(path: string): boolean {
  return previewKind(path) !== 'text'
}

export function previewUrl(sessionId: string, path: string, version?: number): string {
  const base = `${sessionBase(sessionId)}/preview/${workspacePath(path)}`
  return version ? `${base}?v=${version}` : base
}

export function baseName(path: string): string {
  return path.split('/').filter(Boolean).pop() ?? path
}

/** Workspace-relative path for an absolute file in /work (state.file). */
export function workspaceRelative(file: string | null | undefined): string | null {
  if (!file) return null
  const m = /^\/work\/(.*)$/.exec(file)
  return m ? (m[1] ?? null) : null
}
