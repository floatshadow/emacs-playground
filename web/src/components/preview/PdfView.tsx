import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { PDFDocumentProxy } from 'pdfjs-dist/legacy/build/pdf.mjs'
// The legacy build runs on browsers without the newest APIs (URL.parse, Chrome < 126).
import workerUrl from 'pdfjs-dist/legacy/build/pdf.worker.min.mjs?url'
import styles from './Preview.module.css'
import { errorMessage } from '../../lib/errors'

const MIN_SCALE = 0.4
const MAX_SCALE = 3
const RETRY_MS = 800

/**
 * PDF preview with PDF.js (frontend.md §9): the worker is a separate asset
 * loaded lazily; visible pages are rendered to canvases; scale and scroll
 * are kept across reloads, which happen when the file's version changes.
 * Lazy-loaded so pdfjs stays out of the main chunk.
 */
export default function PdfView({
  url,
  version,
  name,
}: {
  url: string
  version: number
  name: string
}) {
  const { t } = useTranslation()
  const scroller = useRef<HTMLDivElement>(null)
  const pagesEl = useRef<HTMLDivElement>(null)
  const [doc, setDoc] = useState<PDFDocumentProxy | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [scale, setScale] = useState<number | 'fit'>('fit')
  const [effective, setEffective] = useState(1)
  const saved = useRef<{ top: number; left: number } | null>(null)
  // A PDF read while the compiler still writes it fails to parse: one retry.
  const [retry, setRetry] = useState(0)

  // Load (and reload on version change) keeping scroll position.
  useEffect(() => {
    let cancelled = false
    let loaded: PDFDocumentProxy | null = null
    let timer: ReturnType<typeof setTimeout> | undefined
    if (scroller.current)
      saved.current = { top: scroller.current.scrollTop, left: scroller.current.scrollLeft }
    void (async () => {
      try {
        const pdfjs = await import('pdfjs-dist/legacy/build/pdf.mjs')
        pdfjs.GlobalWorkerOptions.workerSrc = workerUrl
        const task = pdfjs.getDocument({
          url: `${url}${url.includes('?') ? '&' : '?'}v=${version}`,
        })
        loaded = await task.promise
        if (cancelled) {
          void loaded.loadingTask.destroy()
          return
        }
        setError(null)
        setDoc((prev) => {
          void prev?.loadingTask.destroy()
          return loaded
        })
      } catch (e) {
        if (cancelled) return
        if (retry === 0) timer = setTimeout(() => setRetry(1), RETRY_MS)
        else setError(errorMessage(e))
      }
    })()
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [url, version, retry])

  // A new version gets its own retry.
  const [seen, setSeen] = useState(version)
  if (seen !== version) {
    setSeen(version)
    setRetry(0)
  }

  useEffect(() => () => void doc?.loadingTask.destroy(), [doc])

  const layout = useCallback(async () => {
    const host = pagesEl.current
    const sc = scroller.current
    if (!doc || !host || !sc) return
    const first = await doc.getPage(1)
    const base = first.getViewport({ scale: 1 })
    const s = scale === 'fit' ? Math.max(MIN_SCALE, (sc.clientWidth - 32) / base.width) : scale
    setEffective(s)
    host.replaceChildren()
    const ratio = window.devicePixelRatio || 1
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (!entry.isIntersecting) continue
          const canvas = entry.target as HTMLCanvasElement
          if (canvas.dataset.rendered) continue
          canvas.dataset.rendered = '1'
          const n = Number(canvas.dataset.page)
          void doc.getPage(n).then((page) => {
            const vp = page.getViewport({ scale: s * ratio })
            canvas.width = vp.width
            canvas.height = vp.height
            void page.render({ canvas, viewport: vp }).promise.catch(() => undefined)
          })
        }
      },
      { root: sc, rootMargin: '200px' },
    )
    for (let n = 1; n <= doc.numPages; n++) {
      const page = n === 1 ? first : await doc.getPage(n)
      const vp = page.getViewport({ scale: s })
      const canvas = document.createElement('canvas')
      canvas.className = styles.pdfPage ?? ''
      canvas.dataset.page = String(n)
      canvas.style.width = `${Math.floor(vp.width)}px`
      canvas.style.height = `${Math.floor(vp.height)}px`
      canvas.setAttribute('role', 'img')
      canvas.setAttribute('aria-label', t('preview.pdfPage', { n, total: doc.numPages, name }))
      host.appendChild(canvas)
      observer.observe(canvas)
    }
    if (saved.current) {
      sc.scrollTop = saved.current.top
      sc.scrollLeft = saved.current.left
      saved.current = null
    }
    return () => observer.disconnect()
  }, [doc, scale, t, name])

  useEffect(() => {
    let cleanup: (() => void) | undefined
    void layout().then((c) => {
      cleanup = c
    })
    return () => cleanup?.()
  }, [layout])

  const zoom = (f: number) =>
    setScale(Math.min(MAX_SCALE, Math.max(MIN_SCALE, Math.round(effective * f * 100) / 100)))

  return (
    <div className={styles.pdf}>
      <div className={styles.pdfBar}>
        <button type="button" onClick={() => zoom(1 / 1.2)} aria-label={t('preview.zoomOut')}>
          −
        </button>
        <span>{Math.round(effective * 100)}%</span>
        <button type="button" onClick={() => zoom(1.2)} aria-label={t('preview.zoomIn')}>
          +
        </button>
        <button type="button" onClick={() => setScale('fit')} aria-pressed={scale === 'fit'}>
          {t('preview.fit')}
        </button>
        {doc && <span className={styles.dim}>{t('preview.pages', { count: doc.numPages })}</span>}
      </div>
      <div ref={scroller} className={styles.pdfScroll}>
        {error ? <p className={styles.error}>{t('preview.pdfError', { error })}</p> : null}
        <div ref={pagesEl} className={styles.pdfPages} />
      </div>
    </div>
  )
}
