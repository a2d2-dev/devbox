import { render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { useGlobalShortcuts } from './useGlobalShortcuts'

function Harness({ enabled = true, actions, context = {} }) {
  useGlobalShortcuts({ enabled, actions, context })
  return null
}

function keydown(init) {
  const event = new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init })
  document.dispatchEvent(event)
  return event
}

describe('useGlobalShortcuts', () => {
  it('dispatches an enabled action and prevents its browser default', () => {
    const actions = { 'show-desktop': vi.fn() }
    render(<Harness actions={actions} context={{}}/>)
    const event = keydown({ code: 'KeyD', ctrlKey: true, altKey: true })
    expect(actions['show-desktop']).toHaveBeenCalledOnce()
    expect(event.defaultPrevented).toBe(true)
  })

  it('does not preventDefault when disabled or in an editable context', () => {
    const actions = { 'show-desktop': vi.fn() }
    const { rerender } = render(<Harness enabled={false} actions={actions}/>)
    expect(keydown({ code: 'KeyD', ctrlKey: true, altKey: true }).defaultPrevented).toBe(false)
    expect(actions['show-desktop']).not.toHaveBeenCalled()

    rerender(<Harness enabled actions={actions}/>)
    const input = document.createElement('input')
    document.body.append(input)
    const event = new KeyboardEvent('keydown', { code: 'KeyD', ctrlKey: true, altKey: true, bubbles: true, cancelable: true })
    input.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(false)
    expect(actions['show-desktop']).not.toHaveBeenCalled()
  })

  it('suppresses repeated actions', () => {
    const actions = { 'show-desktop': vi.fn() }
    render(<Harness actions={actions}/>)
    const event = keydown({ code: 'KeyD', ctrlKey: true, altKey: true, repeat: true })
    expect(event.defaultPrevented).toBe(false)
    expect(actions['show-desktop']).not.toHaveBeenCalled()
  })
})
