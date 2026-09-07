import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { clearAuth, getAuthToken, setAuthRequired, setAuthToken, setOnAuthExpired } from '../../hooks/useApi'
import ProfilePanel from './ProfilePanel'

describe('ProfilePanel password change', () => {
  beforeEach(() => {
    clearAuth()
    setAuthRequired(true)
    setOnAuthExpired(null)
    vi.restoreAllMocks()
  })

  it('shows current-password errors from authFetch without expiring the session', async () => {
    setAuthToken('issue-37-token')
    const expired = vi.fn()
    setOnAuthExpired(expired)

    const fetchMock = vi.fn(async (url, opts = {}) => {
      if (url === '/api/v1/account') {
        return new Response(JSON.stringify({
          id: 'u1',
          username: 'developer',
          displayName: 'Dev Eloper',
          role: 'user',
          avatarKind: 'initials',
          createdAt: '2026-01-01T00:00:00Z',
        }), { status: 200, headers: { 'content-type': 'application/json' } })
      }
      if (url === '/api/v1/account/password') {
        expect(opts.headers.Authorization).toBe('Bearer issue-37-token')
        return new Response(JSON.stringify({
          error: '当前密码错误',
          reason: 'invalid_current_password',
        }), { status: 400, headers: { 'content-type': 'application/json' } })
      }
      throw new Error(`unexpected fetch ${url}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const user = userEvent.setup()
    render(<ProfilePanel/>)

    await screen.findByRole('heading', { name: '账户资料' })
    await user.type(screen.getByLabelText('当前密码'), 'Wrong-pass-2026')
    await user.type(screen.getByLabelText('新密码'), 'Brand-new-2027')
    await user.type(screen.getByLabelText('确认新密码'), 'Brand-new-2027')
    await user.click(screen.getByRole('button', { name: '修改密码' }))

    expect(await screen.findByText('当前密码错误。')).toBeInTheDocument()
    expect(getAuthToken()).toBe('issue-37-token')
    expect(expired).not.toHaveBeenCalled()
  })
})
