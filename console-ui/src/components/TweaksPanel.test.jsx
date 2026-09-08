import { render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { useTweaks } from './TweaksPanel'

vi.mock('../hooks/useApi', () => ({
  authFetch: vi.fn(),
  getAuthToken: vi.fn(() => null),
}))

function Probe({ defaults }) {
  const [prefs] = useTweaks(defaults)
  return <div data-testid="icon-size">{prefs.iconSize}</div>
}

describe('useTweaks defaults', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('uses the new small icon default when no user preference exists', async () => {
    render(<Probe defaults={{ iconSize: 'sm', theme: 'light' }} />)

    await waitFor(() => expect(screen.getByTestId('icon-size')).toHaveTextContent('sm'))
  })

  it('keeps a user-selected icon size instead of overwriting it with the default', async () => {
    localStorage.setItem('edgex-user-prefs', JSON.stringify({ iconSize: 'md' }))

    render(<Probe defaults={{ iconSize: 'sm', theme: 'light' }} />)

    await waitFor(() => expect(screen.getByTestId('icon-size')).toHaveTextContent('md'))
  })
})
