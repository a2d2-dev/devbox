import { beforeEach, describe, expect, it, vi } from 'vitest'
import { authFetch, clearAuth, getAuthToken, setAuthRequired, setAuthToken, setOnAuthExpired } from './useApi'

describe('auth session storage', () => {
  beforeEach(() => {
    clearAuth()
    setAuthRequired(true)
    setOnAuthExpired(null)
    vi.restoreAllMocks()
  })

  it('stores persistent and browser-session tokens in separate storage', () => {
    setAuthToken('persistent-token', 'persistent')
    expect(localStorage.getItem('edge_token')).toBe('persistent-token')
    expect(sessionStorage.getItem('edge_token')).toBeNull()

    setAuthToken('session-token', 'session')
    expect(localStorage.getItem('edge_token')).toBeNull()
    expect(sessionStorage.getItem('edge_token')).toBe('session-token')
    expect(getAuthToken()).toBe('session-token')
  })

  it('clears an expired token and notifies only once across concurrent 401s', async () => {
    setAuthToken('expired-token')
    const expired = vi.fn()
    setOnAuthExpired(expired)
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 401 })))

    await Promise.all([authFetch('/api/v1/metrics'), authFetch('/api/v1/apps')])

    expect(getAuthToken()).toBe('')
    expect(expired).toHaveBeenCalledOnce()
    expect(localStorage.getItem('edge_token')).toBeNull()
  })
})
