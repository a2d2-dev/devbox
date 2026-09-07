import { afterEach, describe, expect, test, vi } from 'vitest'
import { applyThemePreference, resolveThemePreference, T } from './tokens'

function mockMatchMedia(matches) {
  const listeners = new Set()
  const media = {
    matches,
    media: '(prefers-color-scheme: dark)',
    addEventListener: vi.fn((event, cb) => { if (event === 'change') listeners.add(cb) }),
    removeEventListener: vi.fn((event, cb) => { if (event === 'change') listeners.delete(cb) }),
    addListener: vi.fn((cb) => listeners.add(cb)),
    removeListener: vi.fn((cb) => listeners.delete(cb)),
    dispatch(nextMatches) {
      this.matches = nextMatches
      const event = { matches: nextMatches, media: this.media }
      listeners.forEach((cb) => cb(event))
    },
  }
  window.matchMedia = vi.fn(() => media)
  return media
}

describe('theme tokens', () => {
  afterEach(() => {
    document.documentElement.removeAttribute('data-theme')
    document.documentElement.removeAttribute('data-theme-preference')
    document.documentElement.removeAttribute('style')
    vi.restoreAllMocks()
  })

  test('dark preference applies data-theme and token variables resolve', () => {
    document.documentElement.style.setProperty('--edge-bg', '#0f131a')

    const resolved = applyThemePreference('dark')

    expect(resolved).toBe('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(document.documentElement.dataset.themePreference).toBe('dark')
    expect(T.bg).toBe('var(--edge-bg)')
    expect(getComputedStyle(document.documentElement).getPropertyValue('--edge-bg').trim()).toBe('#0f131a')
  })

  test('system preference follows matchMedia at runtime', () => {
    const media = mockMatchMedia(false)

    expect(resolveThemePreference('system')).toBe('light')
    expect(applyThemePreference('system')).toBe('light')

    media.dispatch(true)
    expect(applyThemePreference('system')).toBe('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(document.documentElement.dataset.themePreference).toBe('system')
  })
})
