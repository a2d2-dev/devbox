// CloudApps — 容器域 IA 重组 T2 最小渲染测试：
//   1. runtime=kubernetes 应用显示（K8s 徽标 + Pod 就绪口径），compose 应用不显示。
//   2. 空态文案说明「由云端下发后显示」。
//   3. 操作语义对齐 T1 前 ComposeManager K8s 分支：启动/停止/重启/卸载有、
//      「重部署」（compose 专属）与「新建」入口没有。
import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CloudApps from './CloudApps'

const api = vi.hoisted(() => ({
  apps: [],
  refresh: vi.fn(async () => {}),
}))

vi.mock('../hooks/useApi', () => ({
  useApps: () => ({ data: api.apps, refresh: api.refresh }),
  useStoreApps: () => ({ data: [] }),
  useCatalogApps: () => ({ data: [] }),
  useTask: () => ({ task: null }),
  appActionAsync: vi.fn(),
  // UninstallDialog（间接引入）
  getRemovePreview: vi.fn(),
  removeAppEx: vi.fn(),
}))

vi.mock('../components/toastContext', () => ({
  useToast: () => ({ ok: vi.fn(), err: vi.fn(), warn: vi.fn() }),
}))

describe('CloudApps kubernetes-only page', () => {
  beforeEach(() => {
    api.apps = []
  })

  it('renders only kubernetes-runtime apps with the K8s pod-readiness copy', () => {
    api.apps = [
      { id: 'cloud', name: 'k8s-cloud-app', runtime: 'kubernetes', ownership: 'managed',
        observed: { phase: 'running' }, ready: 2, replicas: 3 },
      { id: 'blog', name: 'local-compose-app', runtime: 'compose', ownership: 'managed',
        observed: { phase: 'running' } },
    ]
    render(<CloudApps authed onRequireAuth={vi.fn()}/>)

    expect(screen.getByText('k8s-cloud-app')).toBeInTheDocument()
    // runtime 徽标（title=Kubernetes）+ 来源 Chip 均显示 K8s
    expect(screen.getAllByText('K8s').length).toBeGreaterThan(0)
    expect(screen.getByTitle('Kubernetes')).toBeInTheDocument()
    expect(screen.getByText('2/3 Pod')).toBeInTheDocument()
    expect(screen.queryByText('local-compose-app')).not.toBeInTheDocument()
  })

  it('keeps the pre-T1 K8s action set: start/stop/restart/uninstall, no redeploy and no create entry', () => {
    api.apps = [
      { id: 'cloud', name: 'k8s-cloud-app', runtime: 'kubernetes', ownership: 'managed',
        observed: { phase: 'running' } },
    ]
    render(<CloudApps authed onRequireAuth={vi.fn()}/>)

    expect(screen.getByRole('button', { name: '启动' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '停止' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重启' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /卸载/ })).toBeInTheDocument()
    // 「重部署」是 compose 专属；云端应用无本地新建路径
    expect(screen.queryByRole('button', { name: '重部署' })).not.toBeInTheDocument()
    expect(screen.queryByText(/新建/)).not.toBeInTheDocument()
  })

  it('shows the cloud-delivery empty state when no kubernetes apps exist', () => {
    api.apps = [
      { id: 'blog', name: 'local-compose-app', runtime: 'compose', ownership: 'managed',
        observed: { phase: 'running' } },
    ]
    render(<CloudApps authed onRequireAuth={vi.fn()}/>)

    expect(screen.getByText('暂无云端应用')).toBeInTheDocument()
    expect(screen.getByText(/由云端下发后显示/)).toBeInTheDocument()
    expect(screen.getByText('0 个应用')).toBeInTheDocument()
  })
})
