import { afterEach, describe, expect, it, vi } from 'vitest'
import { startVisiblePolling } from './visiblePolling'

describe('startVisiblePolling', () => {
  afterEach(() => vi.useRealTimers())

  it('pauses in the background and refreshes immediately when visible again', () => {
    vi.useFakeTimers()
    const doc = new EventTarget()
    doc.hidden = false
    const run = vi.fn()
    const stop = startVisiblePolling(run, 1000, doc)

    expect(run).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(2000)
    expect(run).toHaveBeenCalledTimes(3)

    doc.hidden = true
    doc.dispatchEvent(new Event('visibilitychange'))
    vi.advanceTimersByTime(5000)
    expect(run).toHaveBeenCalledTimes(3)

    doc.hidden = false
    doc.dispatchEvent(new Event('visibilitychange'))
    expect(run).toHaveBeenCalledTimes(4)
    vi.advanceTimersByTime(1000)
    expect(run).toHaveBeenCalledTimes(5)
    stop()
  })
})
