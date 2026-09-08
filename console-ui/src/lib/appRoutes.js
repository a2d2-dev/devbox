export const APP_LAUNCH_ALIASES = {
  downloads: { id: 'files', tab: 'downloads' },
  backup: { id: 'files', tab: 'backup' },
  'network-connections': { id: 'network-security', tab: 'connections' },
  links: { id: 'network-security', tab: 'links' },
}

export function resolveAppLaunch(app) {
  if (!app?.id) return null
  const alias = APP_LAUNCH_ALIASES[app.id]
  return alias ? { ...app, ...alias, sourceId: app.id } : app
}
