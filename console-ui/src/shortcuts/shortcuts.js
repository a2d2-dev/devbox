const modAlt = { primary: true, alt: true }

export const shortcutRegistry = [
  {
    id: 'shortcut-help',
    action: 'toggle-shortcut-help',
    name: '快捷键帮助',
    description: '打开或关闭此快捷键面板',
    category: 'general',
    bindings: [
      { code: 'Slash', shift: true, displayKey: '?' },
      { code: 'Slash', primary: true, displayKey: '/' },
    ],
  },
  {
    id: 'show-desktop',
    action: 'show-desktop',
    name: '显示桌面',
    description: '最小化当前窗口并返回桌面',
    category: 'navigation',
    bindings: [{ code: 'KeyD', ...modAlt, displayKey: 'D' }],
  },
  {
    id: 'dock-app',
    action: 'focus-dock-app',
    name: '聚焦 Dock 应用',
    description: '按当前运行中的 Dock 顺序聚焦第 1–9 个应用',
    category: 'navigation',
    bindings: Array.from({ length: 9 }, (_, index) => ({
      code: `Digit${index + 1}`,
      alt: true,
      displayKey: String(index + 1),
      argument: index,
    })),
    enabled: ({ dockApps = [] }, binding) => Boolean(dockApps[binding.argument]),
  },
  {
    id: 'window-minimize',
    action: 'minimize-window',
    name: '最小化当前应用',
    description: '最小化当前活动窗口',
    category: 'window',
    bindings: [{ code: 'KeyM', ...modAlt, displayKey: 'M' }],
    enabled: ({ activeId }) => Boolean(activeId),
  },
  {
    id: 'window-maximize',
    action: 'toggle-maximized',
    name: '最大化或还原',
    description: '切换当前活动窗口的显示模式',
    category: 'window',
    bindings: [{ code: 'KeyF', ...modAlt, displayKey: 'F' }],
    enabled: ({ activeId }) => Boolean(activeId),
  },
  {
    id: 'window-close',
    action: 'close-window',
    name: '关闭当前应用',
    description: '关闭当前活动窗口并退出运行',
    category: 'window',
    bindings: [{ code: 'KeyW', ...modAlt, displayKey: 'W' }],
    enabled: ({ activeId }) => Boolean(activeId),
  },
]

export const shortcutCategories = [
  { id: 'general', label: '通用' },
  { id: 'navigation', label: '导航' },
  { id: 'window', label: '窗口' },
]

export function getPlatform(userAgent = navigator.userAgent, platform = navigator.platform) {
  const value = `${userAgent} ${platform}`.toLowerCase()
  if (/mac|iphone|ipad|ipod/.test(value)) return 'mac'
  if (/win/.test(value)) return 'windows'
  return 'linux'
}

function matchesModifier(event, binding, key) {
  return Boolean(event[key]) === Boolean(binding[key === 'ctrlKey' || key === 'metaKey' ? 'primary' : key.replace('Key', '')])
}

export function bindingMatches(event, binding) {
  const primaryMatches = binding.primary
    ? (event.ctrlKey || event.metaKey) && !(event.ctrlKey && event.metaKey)
    : !event.ctrlKey && !event.metaKey
  return event.code === binding.code
    && primaryMatches
    && matchesModifier(event, binding, 'altKey')
    && matchesModifier(event, binding, 'shiftKey')
}

export function isEditableContext(target) {
  if (!(target instanceof Element)) return false
  return Boolean(target.closest([
    'input',
    'textarea',
    'select',
    '[contenteditable]:not([contenteditable="false"])',
    '[role="textbox"]',
    '.xterm',
    '[data-shortcut-scope]',
  ].join(',')))
}

export function matchShortcut(event, registry = shortcutRegistry, context = {}) {
  if (event.repeat || isEditableContext(event.target)) return null
  for (const shortcut of registry) {
    for (const binding of shortcut.bindings) {
      if (!bindingMatches(event, binding)) continue
      if (shortcut.enabled && !shortcut.enabled(context, binding)) continue
      return { shortcut, binding }
    }
  }
  return null
}

export function formatBinding(binding, platform = getPlatform()) {
  const parts = []
  if (binding.primary) parts.push(platform === 'mac' ? '⌘' : 'Ctrl')
  if (binding.alt) parts.push(platform === 'mac' ? 'Option' : 'Alt')
  if (binding.shift && binding.displayKey !== '?') parts.push('Shift')
  parts.push(binding.displayKey)
  return parts.join(' + ')
}

export function formatShortcut(shortcut, platform = getPlatform()) {
  return shortcut.bindings.map(binding => formatBinding(binding, platform)).join(' / ')
}
