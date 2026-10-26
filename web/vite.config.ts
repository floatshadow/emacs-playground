/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The gateway (local: http://127.0.0.1:8080) serves /api, /s (session proxy,
// including WebSockets), /fonts and /internal. `pnpm dev` proxies them so the
// app runs on :5173 against a gateway started from source or by Compose
// (frontend.md §15). Override the target with PG_GATEWAY=http://host:port.
const gateway = process.env.PG_GATEWAY ?? 'http://127.0.0.1:8080'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': { target: gateway, changeOrigin: false },
      '/s': { target: gateway, changeOrigin: false, ws: true },
      '/fonts': { target: gateway, changeOrigin: false },
      '/internal': { target: gateway, changeOrigin: false },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: true,
    target: 'es2023',
  },
  test: {
    environment: 'jsdom',
    globals: false,
    setupFiles: ['./src/test-setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
    css: { modules: { classNameStrategy: 'non-scoped' } },
  },
})
