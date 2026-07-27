import { useEffect, useEffectEvent } from 'react'
import { matchShortcut, shortcutRegistry } from './shortcuts'

export function useGlobalShortcuts({ enabled, actions, context, registry = shortcutRegistry }) {
  const onKeyDown = useEffectEvent((event) => {
    if (!enabled) return
    const match = matchShortcut(event, registry, context)
    if (!match) return
    const action = actions[match.shortcut.action]
    if (typeof action !== 'function') return
    event.preventDefault()
    action(match.binding.argument, match)
  })

  useEffect(() => {
    window.addEventListener('keydown', onKeyDown, true)
    return () => window.removeEventListener('keydown', onKeyDown, true)
  }, [])
}
