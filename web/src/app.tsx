import { useState } from 'react'
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query'
import { RouterProvider } from 'react-router'
import { makeQueryClient } from './api/queries'
import { makeRouter } from './routes'
import './i18n'

/**
 * The app: TanStack Query for server state, the data router for screens.
 * The session connection (events, terminal, GUI) lives in module-level hosts
 * above the router, so navigation never drops a WebSocket (frontend.md §2).
 */
export function App({ queryClient }: { queryClient?: QueryClient }) {
  const [qc] = useState(() => queryClient ?? makeQueryClient())
  const [router] = useState(makeRouter)
  return (
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}
