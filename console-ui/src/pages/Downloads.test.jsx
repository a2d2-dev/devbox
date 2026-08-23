import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ToastProvider } from '../components/Toast'
import Downloads from './Downloads'

const snapshot = {
  rootDirectory: '/data',
  counts: { all: 3, waiting: 2, downloading: 0, completed: 1, paused: 0, error: 0 },
  statistics: {},
  tasks: [
    { id: 'waiting-1', name: 'waiting.bin', status: 'waiting', downloadedBytes: 0, totalBytes: 10 },
    { id: 'completed-1', name: 'completed.bin', status: 'completed', downloadedBytes: 10, totalBytes: 10 },
  ],
}

vi.mock('../hooks/useApi', () => ({
  downloadRequest: vi.fn(),
  useDownloads: vi.fn(() => ({ data: snapshot, loading: false, error: null, refresh: vi.fn() })),
}))

describe('Downloads status filters', () => {
  beforeEach(() => vi.clearAllMocks())

  it('shows the waiting count and filters waiting tasks', async () => {
    const user = userEvent.setup()
    render(<ToastProvider><Downloads/></ToastProvider>)

    expect(screen.getByText('waiting.bin')).toBeInTheDocument()
    expect(screen.getByText('completed.bin')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /等待\s*2/ }))

    expect(screen.getByText('waiting.bin')).toBeInTheDocument()
    expect(screen.queryByText('completed.bin')).not.toBeInTheDocument()
  })
})
