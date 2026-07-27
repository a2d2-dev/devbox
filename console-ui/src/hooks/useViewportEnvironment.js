import { useEffect, useState } from 'react'

const MEDIA = {
  phone: '(max-width: 639px)',
  tablet: '(min-width: 640px) and (max-width: 1023px)',
  desktop: '(min-width: 1024px)',
  coarsePointer: '(pointer: coarse)',
  noHover: '(hover: none)',
  portrait: '(orientation: portrait)',
}

function readEnvironment() {
  if (typeof window === 'undefined') {
    return {
      width: 1024,
      height: 768,
      visualViewportHeight: 768,
      isPhone: false,
      isTablet: false,
      isDesktop: true,
      isPortrait: false,
      coarsePointer: false,
      noHover: false,
    }
  }

  const matches = (query) => window.matchMedia(query).matches
  return {
    width: window.innerWidth,
    height: window.innerHeight,
    visualViewportHeight: window.visualViewport?.height || window.innerHeight,
    isPhone: matches(MEDIA.phone),
    isTablet: matches(MEDIA.tablet),
    isDesktop: matches(MEDIA.desktop),
    isPortrait: matches(MEDIA.portrait),
    coarsePointer: matches(MEDIA.coarsePointer),
    noHover: matches(MEDIA.noHover),
  }
}

export function useViewportEnvironment() {
  const [environment, setEnvironment] = useState(readEnvironment)

  useEffect(() => {
    const mediaQueries = Object.values(MEDIA).map(query => window.matchMedia(query))
    let frame = 0
    const update = () => {
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => setEnvironment(readEnvironment()))
    }

    mediaQueries.forEach(query => query.addEventListener('change', update))
    window.addEventListener('resize', update)
    window.addEventListener('orientationchange', update)
    window.visualViewport?.addEventListener('resize', update)
    window.visualViewport?.addEventListener('scroll', update)

    update()
    return () => {
      cancelAnimationFrame(frame)
      mediaQueries.forEach(query => query.removeEventListener('change', update))
      window.removeEventListener('resize', update)
      window.removeEventListener('orientationchange', update)
      window.visualViewport?.removeEventListener('resize', update)
      window.visualViewport?.removeEventListener('scroll', update)
    }
  }, [])

  const compactWindow = environment.isPhone || (environment.isTablet && environment.isPortrait)
  return {
    ...environment,
    compactWindow,
    touchEnvironment: environment.coarsePointer || environment.noHover,
  }
}

export { MEDIA as viewportMedia }
