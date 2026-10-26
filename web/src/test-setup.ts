import '@testing-library/jest-dom/vitest'
import { afterEach } from 'vitest'
import { cleanup } from '@testing-library/react'
import './i18n'

// jsdom lacks these; the app only feature-detects them.
if (typeof window !== 'undefined') {
  window.HTMLElement.prototype.scrollIntoView ??= function () {}
  if (!('ResizeObserver' in window)) {
    ;(window as unknown as { ResizeObserver: unknown }).ResizeObserver = class {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
  }
}

afterEach(() => cleanup())
