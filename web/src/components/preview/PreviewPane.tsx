import { lazy, Suspense, useEffect, useMemo, useRef, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'
import type { PreviewSpec } from '../../protocol.gen'
import { useSessionStore } from '../../store/session'
import { useUiStore } from '../../store/ui'
import { ArtefactGate } from './Artefact'
import { HtmlView } from './HtmlView'
import { ImageView } from './ImageView'
import { MarkdownView, TextView } from './MarkdownView'
import { PreviewStatus } from './PreviewStatus'
import { baseName, followSource, previewKind, previewUrl, workspaceRelative } from './kinds'
import styles from './Preview.module.css'

const PdfView = lazy(() => import('./PdfView'))

/**
 * The preview pane (frontend.md §9): tabs from the scenario's preview.tabs
 * plus files opened from the file tree; preview.follow switches to the
 * artefact of the source being edited. Views reload on file events for
 * their path.
 */
export function PreviewPane({ owner, spec }: { owner: string; spec: PreviewSpec | undefined }) {
  const { t } = useTranslation()
  const sessionId = useSessionStore((s) => s.sessionId)
  const extra = useUiStore((s) => s.previewExtra[owner])
  const chosen = useUiStore((s) => s.previewTab[owner])
  const setTab = useUiStore((s) => s.setPreviewTab)
  const addTab = useUiStore((s) => s.addPreviewTab)
  const closeTab = useUiStore((s) => s.removePreviewTab)
  const file = useSessionStore((s) => workspaceRelative(s.state?.file))
  const tabs = useMemo(() => {
    const out: string[] = []
    for (const p of [...(spec?.tabs ?? []), ...(extra ?? [])]) if (!out.includes(p)) out.push(p)
    return out
  }, [spec?.tabs, extra])
  const current = chosen && tabs.includes(chosen) ? chosen : tabs[0]
  const version = useSessionStore((s) => (current ? (s.files[current]?.version ?? 0) : 0))

  // preview.follow: show the artefact of the source in the current buffer.
  const lastFollowed = useRef<string | null>(null)
  useEffect(() => {
    if (!file || !spec?.follow) return
    const target = spec.follow[file]
    if (target && lastFollowed.current !== file) {
      lastFollowed.current = file
      if (!tabs.includes(target)) addTab(owner, target)
      else setTab(owner, target)
    }
    if (!target) lastFollowed.current = null
  }, [file, spec?.follow, tabs, owner, addTab, setTab])

  const source = useSessionStore((s) =>
    current ? followSource(current, spec?.follow, file, s.files) : undefined,
  )

  if (tabs.length === 0 || !current) {
    return (
      <div className={styles.preview}>
        <p className={styles.empty}>{t('preview.empty')}</p>
      </div>
    )
  }
  const url = sessionId ? previewUrl(sessionId, current) : ''
  const kind = previewKind(current)
  const name = baseName(current)
  const tabId = (p: string) => `ptab-${owner}-${p}`.replace(/[^A-Za-z0-9_-]/g, '_')
  return (
    <div className={styles.preview}>
      <div className={styles.tabs} role="tablist" aria-label={t('preview.tabs')}>
        {tabs.map((p) => {
          // Tabs opened from the tree can be closed (× or Delete); the scenario's stay.
          const closable = !spec?.tabs?.includes(p) && !!extra?.includes(p)
          const close = t('preview.close', { name: baseName(p) })
          return (
            <button
              key={p}
              id={tabId(p)}
              type="button"
              role="tab"
              aria-selected={p === current}
              className={`${styles.ptab} ${p === current ? styles.on : ''}`}
              title={closable ? `${p} · ${close}` : p}
              onClick={() => setTab(owner, p)}
              {...(closable
                ? {
                    'aria-keyshortcuts': 'Delete',
                    onKeyDown: (e: KeyboardEvent<HTMLButtonElement>) => {
                      if (e.key === 'Delete') closeTab(owner, p)
                    },
                  }
                : {})}
            >
              {baseName(p)}
              {closable && (
                <span
                  className={styles.ptabClose}
                  aria-hidden="true"
                  data-testid={`close-${baseName(p)}`}
                  onClick={(e) => {
                    e.stopPropagation()
                    closeTab(owner, p)
                  }}
                >
                  ×
                </span>
              )}
            </button>
          )
        })}
      </div>
      <div className={styles.body} role="tabpanel" aria-labelledby={tabId(current)}>
        {!sessionId ? (
          <p className={styles.dim}>{t('preview.noSession')}</p>
        ) : (
          <ArtefactGate url={url} version={version} path={current} source={source}>
            {kind === 'pdf' ? (
              <Suspense fallback={<p className={styles.dim}>{t('common.loading')}</p>}>
                <PdfView url={url} version={version} name={name} />
              </Suspense>
            ) : kind === 'html' ? (
              <HtmlView url={url} version={version} name={name} />
            ) : kind === 'markdown' ? (
              <MarkdownView url={url} version={version} />
            ) : kind === 'image' ? (
              <ImageView url={url} version={version} name={name} />
            ) : (
              <TextView url={url} version={version} />
            )}
          </ArtefactGate>
        )}
      </div>
      <PreviewStatus path={current} source={source} />
    </div>
  )
}
