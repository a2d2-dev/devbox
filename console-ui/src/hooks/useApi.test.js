import { afterEach, describe, expect, it, vi } from 'vitest'
import { authFetch, clearAuth, setAuthToken } from './useApi'

describe('authFetch authentication state', () => {
  afterEach(() => {
    clearAuth()
    vi.unstubAllGlobals()
  })

  it('does not send protected requests before login', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    const response = await authFetch('/api/v1/downloads')

    expect(response.status).toBe(401)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('sends requests without an Authorization header after no-auth login', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('{}', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    setAuthToken('')

    const response = await authFetch('/api/v1/downloads')

    expect(response.status).toBe(200)
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/downloads', { headers: {} })
  })

  it('returns to the pre-login state after logout', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('{}', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    setAuthToken('')
    clearAuth()

    const response = await authFetch('/api/v1/downloads')

    expect(response.status).toBe(401)
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
