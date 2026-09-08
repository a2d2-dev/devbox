// DockerApp — 容器域 IA 合并（T1）集成测试：
//   1. Docker 应用默认显示「概览」tab（DockerOverview 全量内容）。
//   2. 概览页「Compose 管理」按钮切换到「Compose 应用」tab，而非另开窗口。
//   3. initialTab='compose'（旧 compose-manager 打开请求经 appRoutes 别名）直达 Compose tab。
//   4. Compose tab 只显示 runtime=compose 应用；Kubernetes / 系统工具筛选已移除。
// DockerOverview 与 ComposeManager 用真实组件，仅 mock API 层与 toast。
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import DockerApp from './DockerApp'

const api = vi.hoisted(() => ({
  apps: [],
  refresh: vi.fn(async () => {}),
}))

vi.mock('../hooks/useApi', () => ({
  // DockerOverview
  dockerServiceAction: vi.fn(async () => ({})),
  executeDockerMigration: vi.fn(),
  planDockerMigration: vi.fn(),
  setDockerAutostart: vi.fn(),
  useDockerOverview: () => ({
    data: {
      service: { state: 'running', controlSupported: true, autostartSupported: true },
      storage: { path: '/data/docker', configured: true, valid: true, migrationSupported: true, disk: {} },
      composeProjects: {},
      containers: {},
    },
    loading: false,
    refresh: api.refresh,
  }),
  useDockerStats: () => ({ data: null }),
  // ComposeManager
  useApps: () => ({ data: api.apps, refresh: api.refresh }),
  useAppCapability: () => ({ data: { compose: { available: true, version: 'v2.27' } } }),
  useStoreApps: () => ({ data: [] }),
  useCatalogApps: () => ({ data: [] }),
  useTask: () => ({ task: null }),
  appActionAsync: vi.fn(),
  validateCompose: vi.fn(),
  applyComposeApp: vi.fn(),
  takeoverApp: vi.fn(),
  // UninstallDialog（经 ComposeManager 间接引入）
  getRemovePreview: vi.fn(),
  removeAppEx: vi.fn(),
}))

vi.mock('../components/toastContext', () => ({
  useToast: () => ({ ok: vi.fn(), err: vi.fn(), warn: vi.fn() }),
}))

describe('DockerApp container-domain tabs', () => {
  beforeEach(() => {
    api.apps = []
  })

  it('shows the overview tab by default with full DockerOverview content', () => {
    render(<DockerApp authed onRequireAuth={vi.fn()}/>)

    expect(screen.getByRole('heading', { name: 'Docker' })).toBeInTheDocument()
    expect(screen.getByText('服务控制')).toBeInTheDocument()
    expect(screen.getByText('开机自动启动')).toBeInTheDocument()
    expect(screen.getByText('数据存储')).toBeInTheDocument()
    expect(screen.getByText('实时监控')).toBeInTheDocument()
    // Compose tab 内容未渲染
    expect(screen.queryAllByText('新建 Compose')).toHaveLength(0)
  })

  it('switches to the compose tab via the overview「Compose 管理」button instead of opening a window', async () => {
    const user = userEvent.setup()
    render(<DockerApp authed onRequireAuth={vi.fn()}/>)

    await user.click(screen.getByRole('button', { name: /Compose 管理/ }))

    expect(screen.getAllByText('新建 Compose').length).toBeGreaterThan(0)
    expect(screen.queryByText('服务控制')).not.toBeInTheDocument()
  })

  it('opens the compose tab directly for legacy compose-manager launches (initialTab)', () => {
    render(<DockerApp authed onRequireAuth={vi.fn()} initialTab="compose"/>)

    expect(screen.getAllByText('新建 Compose').length).toBeGreaterThan(0)
    expect(screen.queryByText('实时监控')).not.toBeInTheDocument()
  })

  it('lists only compose-runtime apps and drops the Kubernetes / system-tool filters', async () => {
    api.apps = [
      { id: 'blog', name: 'my-compose-app', runtime: 'compose', ownership: 'managed', observed: { phase: 'running' } },
      { id: 'cloud', name: 'k8s-only-app', runtime: 'kubernetes', ownership: 'managed', observed: { phase: 'running' } },
    ]
    const user = userEvent.setup()
    render(<DockerApp authed onRequireAuth={vi.fn()} initialTab="compose"/>)

    expect(screen.getByText('my-compose-app')).toBeInTheDocument()
    // runtime=kubernetes 应用暂不显示（云端应用页是后续票）
    expect(screen.queryByText('k8s-only-app')).not.toBeInTheDocument()
    // 旧四类筛选（全部 / Docker Compose / Kubernetes / 系统工具）已移除
    expect(screen.queryByRole('button', { name: /Kubernetes/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /系统工具/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^全部/ })).not.toBeInTheDocument()

    // tab 导航可来回切换
    await user.click(screen.getByRole('button', { name: '概览' }))
    expect(screen.getByText('服务控制')).toBeInTheDocument()
  })
})
