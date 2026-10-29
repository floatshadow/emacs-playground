import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useSessionStore } from '../../store/session'
import { Checkpoints } from './Checkpoints'

const rpcMock = vi.hoisted(() => vi.fn())
vi.mock('../../api/session/rpc', () => ({
  rpc: rpcMock,
  sessionBase: (id: string) => `/s/${id}`,
  wsUrl: (p: string) => `ws://localhost${p}`,
}))

const cp = (files: string[]) => ({
  items: [
    { ref: 'refs/playground/m/1', label: 'before', createdAt: '2026-01-01T10:00:00Z', files },
  ],
})

beforeEach(() => {
  useSessionStore.getState().reset()
  useSessionStore.getState().setSessionId('local')
  rpcMock.mockReset()
})

describe('Checkpoints', () => {
  it('refetches when Restore is pressed, so the dialog lists files saved since the last fetch', async () => {
    rpcMock.mockResolvedValueOnce(cp([])).mockResolvedValue(cp(['tempconv/src/main.rs']))
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <Checkpoints />
      </QueryClientProvider>,
    )
    await screen.findByText(/0 files changed since/)
    await userEvent.click(screen.getByRole('button', { name: 'Restore' }))
    expect(await screen.findByText('tempconv/src/main.rs')).toBeInTheDocument()
    expect(screen.queryByText(/No files differ/)).toBeNull()
    expect(rpcMock).toHaveBeenCalledTimes(2)
  })
})
