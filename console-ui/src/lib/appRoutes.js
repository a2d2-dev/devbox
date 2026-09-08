export const APP_LAUNCH_ALIASES = {
  downloads: { id: 'files', tab: 'downloads' },
  backup: { id: 'files', tab: 'backup' },
  'network-connections': { id: 'network-security', tab: 'connections' },
  links: { id: 'network-security', tab: 'links' },
  // 容器域 IA 合并：Compose 管理并入 Docker 应用（DockerApp.jsx）的 compose tab，
  // 旧 compose-manager 打开请求（桌面遗留入口 / Desktop.jsx 跳转）重定向，不留死链。
  'compose-manager': { id: 'docker', tab: 'compose' },
}

export function resolveAppLaunch(app) {
  if (!app?.id) return null
  const alias = APP_LAUNCH_ALIASES[app.id]
  return alias ? { ...app, ...alias, sourceId: app.id } : app
}
