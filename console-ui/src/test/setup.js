import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

afterEach(() => {
  cleanup()
  document.body.style.overflow = ''
  document.body.removeAttribute('aria-hidden')
  document.body.removeAttribute('inert')
})

if (!Object.prototype.hasOwnProperty.call(HTMLElement.prototype, 'inert')) {
  Object.defineProperty(HTMLElement.prototype, 'inert', {
    configurable: true,
    get() { return this.hasAttribute('inert') },
    set(value) { value ? this.setAttribute('inert', '') : this.removeAttribute('inert') },
  })
}
