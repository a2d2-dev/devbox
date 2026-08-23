import { useRef, useState } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { OverlayProvider, useOverlayLayer } from './OverlayProvider'

function Layer({ name, onDismiss, modal = true }) {
  const ref = useRef(null)
  const { backdropProps, layerProps } = useOverlayLayer({
    id: name,
    onDismiss,
    modal,
    layerRef: ref,
    initialFocusRef: ref,
  })
  return (
    <div data-testid={`${name}-backdrop`} {...backdropProps}>
      <div ref={ref} tabIndex={-1} data-testid={name} {...layerProps}>
        <button>{name} first</button>
        <button>{name} last</button>
      </div>
    </div>
  )
}

function Stack({ firstDismiss, secondDismiss }) {
  return (
    <OverlayProvider>
      <main data-testid="background"><button>launcher</button></main>
      <Layer name="first" onDismiss={firstDismiss}/>
      <Layer name="second" onDismiss={secondDismiss}/>
    </OverlayProvider>
  )
}

describe('overlay stack', () => {
  it('dismisses only the last registered layer and consumes Escape', () => {
    const firstDismiss = vi.fn()
    const secondDismiss = vi.fn()
    render(<Stack firstDismiss={firstDismiss} secondDismiss={secondDismiss}/>)

    const event = new KeyboardEvent('keydown', { code: 'Escape', key: 'Escape', bubbles: true, cancelable: true })
    window.dispatchEvent(event)

    expect(secondDismiss).toHaveBeenCalledOnce()
    expect(firstDismiss).not.toHaveBeenCalled()
    expect(event.defaultPrevented).toBe(true)
  })

  it('dismisses only when the top layer backdrop itself is clicked', async () => {
    const user = userEvent.setup()
    const firstDismiss = vi.fn()
    const secondDismiss = vi.fn()
    render(<Stack firstDismiss={firstDismiss} secondDismiss={secondDismiss}/>)

    await user.click(screen.getByTestId('second'))
    expect(secondDismiss).not.toHaveBeenCalled()
    await user.click(screen.getByTestId('second-backdrop'))
    expect(secondDismiss).toHaveBeenCalledOnce()
  })

  it('traps focus inside the top layer', () => {
    render(<Stack firstDismiss={vi.fn()} secondDismiss={vi.fn()}/>)
    const [first, last] = screen.getAllByRole('button', { name: /second/, hidden: true })
    last.focus()
    fireEvent.keyDown(last, { code: 'Tab', key: 'Tab' })
    expect(document.activeElement).toBe(first)
    fireEvent.keyDown(first, { code: 'Tab', key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(last)
  })

  it('locks scrolling, hides background, and restores focus after close', async () => {
    const user = userEvent.setup()
    function Harness() {
      const [open, setOpen] = useState(false)
      return (
        <OverlayProvider>
          <main data-testid="background"><button onClick={() => setOpen(true)}>launcher</button></main>
          {open && <Layer name="dialog" onDismiss={() => setOpen(false)}/>}
        </OverlayProvider>
      )
    }
    render(<Harness/>)
    const launcher = screen.getByRole('button', { name: 'launcher' })
    await user.click(launcher)

    expect(document.body.style.overflow).toBe('hidden')
    expect(screen.getByTestId('background')).toHaveAttribute('aria-hidden', 'true')
    expect(screen.getByTestId('background')).toHaveAttribute('inert')

    fireEvent.keyDown(window, { code: 'Escape', key: 'Escape' })
    expect(screen.queryByTestId('dialog')).not.toBeInTheDocument()
    expect(launcher).toHaveFocus()
    expect(document.body.style.overflow).toBe('')
    expect(screen.getByTestId('background')).not.toHaveAttribute('aria-hidden')
  })
})
