import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AuditLog, { auditDeviceLabel } from './AuditLog'
import { OverlayProvider } from '../overlays/OverlayProvider'

vi.mock('../hooks/useApi', () => ({
  getAuthToken: () => 'test-token',
}))

vi.mock('../lib/visiblePolling', () => ({
  startVisiblePolling: (run) => {
    run()
    return () => {}
  },
}))

describe('AuditLog redacted client fields', () => {
  const rawUA = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36'

  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({
      events: [{
        id: 1,
        level: 'info',
        module: 'auth',
        ts: '2026-09-07T10:00:00Z',
        username: 'admin',
        event: '本地登录成功',
        event_type: 'LOGIN_SUCCESS',
        outcome: 'success',
        source_ip: '203.0.113.x',
        deviceLabel: 'Chrome · macOS',
        deviceType: 'desktop',
        user_agent: rawUA,
      }],
      total: 1,
      limit: 25,
      offset: 0,
    }), { status: 200 })))
  })

  it('renders the masked IP and parsed device label without showing raw User-Agent', async () => {
    const user = userEvent.setup()
    render(<OverlayProvider><AuditLog/></OverlayProvider>)

    await screen.findByText('本地登录成功')
    await user.click(screen.getByText('本地登录成功'))

    await waitFor(() => expect(screen.getByText('203.0.113.x')).toBeInTheDocument())
    expect(screen.getByText('Chrome · macOS (desktop)')).toBeInTheDocument()
    expect(screen.queryByText(rawUA)).not.toBeInTheDocument()
    expect(screen.queryByText('AppleWebKit')).not.toBeInTheDocument()
  })

  it('formats device labels from the audit response fields', () => {
    expect(auditDeviceLabel({ deviceLabel: 'curl · Unknown OS', deviceType: 'unknown' })).toBe('curl · Unknown OS')
    expect(auditDeviceLabel({ device_label: 'Safari · iOS', device_type: 'mobile' })).toBe('Safari · iOS (mobile)')
  })
})
