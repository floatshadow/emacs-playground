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

/** True for artefacts a build makes (PDF, HTML, images), not files people write. */
export function isBuildOutput(path: string): boolean {
  const kind = previewKind(path)
  return kind === 'pdf' || kind === 'html' || kind === 'image'
}

/**
 * The source the artefact follows (preview.follow) when it is built from
 * one: the file being edited if it is a candidate (several sources may map
 * to one artefact, before and after a rename), else the one changed last
 * and not deleted, else the first. A followed file that is not a build
 * output (a README shown next to the code) has no source: it is never
 * "being rebuilt".
 */
export function followSource(
  artefact: string,
  follow: Record<string, string> | undefined,
  editing: string | null,
  files: Record<string, { event: string; ts: number } | undefined>,
): string | undefined {
  if (!follow || !isBuildOutput(artefact)) return undefined
  const candidates = Object.entries(follow)
    .filter(([, v]) => v === artefact)
    .map(([k]) => k)
  if (editing && candidates.includes(editing)) return editing
  let best: string | undefined
  let bestTs = -1
  for (const c of candidates) {
    const mark = files[c]
    if (mark && mark.event !== 'deleted' && mark.ts > bestTs) {
      best = c
      bestTs = mark.ts
    }
  }
  return best ?? candidates[0]
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
