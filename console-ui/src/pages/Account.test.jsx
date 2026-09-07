import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, test, vi } from 'vitest'
import Account from './Account'
import { clearAuth, setAuthToken } from '../hooks/useApi'

describe('Account devices tab', () => {
  beforeEach(() => {
    clearAuth()
    setAuthToken('current-token')
    global.fetch = vi.fn(async (url, opts = {}) => {
      if (url === '/api/v1/account/sessions') {
        return Response.json([
          {
            id: 'current-session-id',
            deviceLabel: 'Chrome · macOS',
            deviceType: 'desktop',
            loginAt: '2026-09-07T08:00:00Z',
            lastActiveAt: '2026-09-07T08:10:00Z',
            ipMasked: '203.0.113.x',
            current: true,
          },
          {
            id: 'other-session-id',
            deviceLabel: 'Firefox · Linux',
            deviceType: 'desktop',
            loginAt: '2026-09-07T07:00:00Z',
            lastActiveAt: '2026-09-07T07:20:00Z',
            ipMasked: '198.51.100.x',
            current: false,
          },
        ])
      }
      if (url === '/api/v1/account/sessions/other-session-id' && opts.method === 'DELETE') {
        return new Response(null, { status: 204 })
      }
      return new Response(null, { status: 404 })
    })
  })

  test('revokes a non-current session after confirmation and refreshes the list', async () => {
    const user = userEvent.setup()
    render(<Account />)

    await user.click(screen.getByRole('button', { name: '登录设备' }))
    expect(await screen.findByText('Chrome · macOS')).toBeInTheDocument()
    expect(screen.getByText('Firefox · Linux')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '退出 Chrome · macOS' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '退出 Firefox · Linux' }))
    expect(screen.getByRole('dialog', { name: '确认退出指定设备' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '确认退出' }))

    await waitFor(() => {
      expect(global.fetch).toHaveBeenCalledWith('/api/v1/account/sessions/other-session-id', expect.objectContaining({ method: 'DELETE' }))
    })
    await waitFor(() => {
      const listCalls = global.fetch.mock.calls.filter(([url]) => url === '/api/v1/account/sessions')
      expect(listCalls.length).toBeGreaterThanOrEqual(2)
    })
    expect(await screen.findByText('已退出指定设备。')).toBeInTheDocument()
  })
})
