import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import PdfView from './PdfView'

const { getDocument } = vi.hoisted(() => ({ getDocument: vi.fn() }))
vi.mock('pdfjs-dist/legacy/build/pdf.mjs', () => ({ getDocument, GlobalWorkerOptions: {} }))
vi.mock('pdfjs-dist/legacy/build/pdf.worker.min.mjs?url', () => ({ default: 'worker.js' }))

afterEach(() => vi.unstubAllGlobals())

it.each(['resolve', 'reject'])('cancels an unfinished layout before zoom (%s)', async (outcome) => {
  const observers: { disconnect: ReturnType<typeof vi.fn> }[] = []
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      disconnect = vi.fn()
      observe = vi.fn()
      constructor() {
        observers.push(this)
      }
    },
  )
  const page = {
    getViewport: ({ scale }: { scale: number }) => ({ width: 600 * scale, height: 800 * scale }),
  }
  let finish!: (value: typeof page) => void
  let reject!: (err: Error) => void
  const pending = new Promise<typeof page>((resolve, fail) => {
    finish = resolve
    reject = fail
  })
  let secondPages = 0
  const doc = {
    numPages: 2,
    loadingTask: { destroy: vi.fn() },
    getPage: vi.fn((n: number) =>
      n === 2 && secondPages++ === 0 ? pending : Promise.resolve(page),
    ),
  }
  getDocument.mockReturnValue({ promise: Promise.resolve(doc) })
  const { container, unmount } = render(<PdfView url="/preview/a.pdf" version={1} name="a.pdf" />)
  await waitFor(() => expect(container.querySelectorAll('canvas')).toHaveLength(1))
  fireEvent.click(screen.getByRole('button', { name: 'Zoom in' }))
  await waitFor(() => expect(container.querySelectorAll('canvas')).toHaveLength(2))
  expect(observers[0]!.disconnect).toHaveBeenCalledOnce()
  await act(async () => {
    if (outcome === 'resolve') finish(page)
    else reject(new Error('document destroyed'))
    await Promise.resolve()
  })
  expect(container.querySelectorAll('canvas')).toHaveLength(2)
  unmount()
  expect(observers[1]!.disconnect).toHaveBeenCalledOnce()
})
