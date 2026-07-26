import { describe, expect, it, vi } from 'vitest'
import { formatShortcut, isEditableContext, matchShortcut, shortcutRegistry } from './shortcuts'

describe('keyboard shortcut matcher', () => {
  it('matches KeyboardEvent.code instead of the visible key', () => {
    const event = new KeyboardEvent('keydown', { code: 'Slash', key: '§', ctrlKey: true })
    expect(matchShortcut(event, shortcutRegistry, {})).toMatchObject({ shortcut: { id: 'shortcut-help' } })
  })

  it('does not match repeated keydown events by default', () => {
    const event = new KeyboardEvent('keydown', { code: 'KeyD', ctrlKey: true, altKey: true, repeat: true })
    expect(matchShortcut(event, shortcutRegistry, {})).toBeNull()
  })

  it('only returns enabled shortcuts', () => {
    const event = new KeyboardEvent('keydown', { code: 'KeyM', ctrlKey: true, altKey: true })
    expect(matchShortcut(event, shortcutRegistry, { activeId: null })).toBeNull()
    expect(matchShortcut(event, shortcutRegistry, { activeId: 'dashboard' })?.shortcut.id).toBe('window-minimize')
  })
})

describe('editable shortcut contexts', () => {
  it.each([
    ['input', '<input>'],
    ['textarea', '<textarea></textarea>'],
    ['select', '<select></select>'],
    ['contenteditable', '<div contenteditable="true"></div>'],
    ['ARIA textbox', '<div role="textbox"></div>'],
    ['xterm', '<div class="xterm"><span></span></div>'],
    ['declared shortcut scope', '<div data-shortcut-scope><button></button></div>'],
  ])('suppresses shortcuts inside %s', (_label, markup) => {
    document.body.innerHTML = markup
    const target = document.body.querySelector('span, button, input, textarea, select, [contenteditable], [role="textbox"], .xterm')
    expect(isEditableContext(target)).toBe(true)
  })

  it('allows shortcuts from ordinary controls', () => {
    document.body.innerHTML = '<button>Open</button>'
    expect(isEditableContext(document.querySelector('button'))).toBe(false)
  })
})

describe('platform shortcut labels', () => {
  it('uses Apple modifier names on macOS', () => {
    expect(formatShortcut(shortcutRegistry.find(item => item.id === 'window-close'), 'mac')).toBe('⌘ + Option + W')
  })

  it('uses Ctrl and Alt on Windows and Linux', () => {
    expect(formatShortcut(shortcutRegistry.find(item => item.id === 'window-close'), 'windows')).toBe('Ctrl + Alt + W')
  })
})

describe('shortcut event consumption', () => {
  it('leaves unmatched events untouched', () => {
    const event = new KeyboardEvent('keydown', { code: 'KeyZ', ctrlKey: true, cancelable: true })
    const preventDefault = vi.spyOn(event, 'preventDefault')
    expect(matchShortcut(event, shortcutRegistry, {})).toBeNull()
    expect(preventDefault).not.toHaveBeenCalled()
  })
})
