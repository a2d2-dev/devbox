import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'

const OverlayContext = createContext(null)

let scrollLockCount = 0
let scrollLockPrevious = ''

function lockBodyScroll() {
  if (scrollLockCount === 0) {
    scrollLockPrevious = document.body.style.overflow
    document.body.style.overflow = 'hidden'
  }
  scrollLockCount += 1
  return () => {
    scrollLockCount = Math.max(0, scrollLockCount - 1)
    if (scrollLockCount === 0) document.body.style.overflow = scrollLockPrevious
  }
}

const focusableSelector = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

export function OverlayProvider({ children }) {
  const layers = useRef([])
  const [layerCount, setLayerCount] = useState(0)

  const register = useCallback((layer) => {
    layers.current.push(layer)
    setLayerCount(layers.current.length)
    return () => {
      layers.current = layers.current.filter(item => item.token !== layer.token)
      setLayerCount(layers.current.length)
    }
  }, [])

  const isTop = useCallback((token) => layers.current.at(-1)?.token === token, [])
  const hasLayers = layerCount > 0

  useEffect(() => {
    function onKeyDown(event) {
      if (event.code !== 'Escape' && event.key !== 'Escape') return
      const top = layers.current.at(-1)
      if (!top?.dismissible || typeof top.onDismiss !== 'function') return
      event.preventDefault()
      event.stopPropagation()
      top.onDismiss()
    }
    window.addEventListener('keydown', onKeyDown, true)
    return () => window.removeEventListener('keydown', onKeyDown, true)
  }, [])

  const value = useMemo(() => ({ register, isTop, hasLayers }), [register, isTop, hasLayers])
  return <OverlayContext.Provider value={value}>{children}</OverlayContext.Provider>
}

export function useOverlayStack() {
  const context = useContext(OverlayContext)
  if (!context) throw new Error('useOverlayStack must be used inside OverlayProvider')
  return context
}

function setBackgroundInert(layerElement) {
  const restores = []
  let current = layerElement
  while (current?.parentElement) {
    const parent = current.parentElement
    let sibling = current.previousElementSibling
    while (sibling) {
      const element = sibling
      const hadInert = element.hasAttribute('inert')
      const previousAriaHidden = element.getAttribute('aria-hidden')
      element.setAttribute('inert', '')
      element.setAttribute('aria-hidden', 'true')
      restores.push(() => {
        if (!hadInert) element.removeAttribute('inert')
        if (previousAriaHidden == null) element.removeAttribute('aria-hidden')
        else element.setAttribute('aria-hidden', previousAriaHidden)
      })
      sibling = element.previousElementSibling
    }
    if (parent === document.body) break
    current = parent
  }
  return () => restores.reverse().forEach(restore => restore())
}

export function useOverlayLayer({
  id,
  onDismiss,
  layerRef,
  initialFocusRef,
  modal = false,
  dismissible = true,
  active = true,
}) {
  const context = useContext(OverlayContext)
  if (!context) throw new Error('useOverlayLayer must be used inside OverlayProvider')
  const { register, isTop } = context

  const tokenRef = useRef(Symbol(id || 'overlay'))
  const dismissRef = useRef(onDismiss)
  const previousFocusRef = useRef(null)

  useLayoutEffect(() => {
    dismissRef.current = onDismiss
  }, [onDismiss])

  useLayoutEffect(() => {
    if (!active) return undefined
    const token = tokenRef.current
    const unregister = register({
      token,
      dismissible,
      onDismiss: () => dismissRef.current?.(),
    })
    return () => {
      unregister()
      const previous = previousFocusRef.current
      if (previous?.isConnected) previous.focus({ preventScroll: true })
    }
  }, [active, dismissible, register])

  useLayoutEffect(() => {
    if (!active) return undefined
    const layer = layerRef.current
    if (!layer) return undefined
    previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const focusTarget = initialFocusRef?.current
      || layer.querySelector('[autofocus], [data-initial-focus]')
      || layer.querySelector(focusableSelector)
      || layer
    focusTarget.focus({ preventScroll: true })

    const restoreEnvironment = modal ? setBackgroundInert(layer) : () => {}
    const restoreScroll = modal ? lockBodyScroll() : () => {}

    return () => {
      restoreEnvironment()
      restoreScroll()
    }
  }, [active, initialFocusRef, layerRef, modal])

  const onKeyDown = useCallback((event) => {
    if (event.code !== 'Tab' || !isTop(tokenRef.current)) return
    const layer = layerRef.current
    if (!layer) return
    const focusable = [...layer.querySelectorAll(focusableSelector)]
      .filter(element => !element.hasAttribute('disabled') && element.getAttribute('aria-hidden') !== 'true')
    if (focusable.length === 0) {
      event.preventDefault()
      layer.focus()
      return
    }
    const first = focusable[0]
    const last = focusable.at(-1)
    if (event.shiftKey && (document.activeElement === first || !layer.contains(document.activeElement))) {
      event.preventDefault()
      last.focus()
    } else if (!event.shiftKey && (document.activeElement === last || !layer.contains(document.activeElement))) {
      event.preventDefault()
      first.focus()
    }
  }, [isTop, layerRef])

  const onBackdropClick = useCallback((event) => {
    if (event.target !== event.currentTarget || !dismissible || !isTop(tokenRef.current)) return
    onDismiss?.()
  }, [dismissible, isTop, onDismiss])

  return {
    isTopLayer: () => isTop(tokenRef.current),
    layerProps: { onKeyDown },
    backdropProps: { onClick: onBackdropClick },
  }
}
