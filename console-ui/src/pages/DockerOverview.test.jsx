import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import DockerOverview from './DockerOverview'

const api = vi.hoisted(() => ({
  serviceAction: vi.fn(async () => ({})),
  refresh: vi.fn(async () => {}),
  overview: null,
}))

vi.mock('../hooks/useApi', () => ({
  dockerServiceAction: api.serviceAction,
  executeDockerMigration: vi.fn(),
  planDockerMigration: vi.fn(),
  setDockerAutostart: vi.fn(),
  useDockerOverview: () => ({
    data: api.overview,
    loading: false,
    refresh: api.refresh,
  }),
  useDockerStats: () => ({ data: null }),
}))

vi.mock('../components/toastContext', () => ({
  useToast: () => ({ ok: vi.fn(), err: vi.fn() }),
}))

describe('DockerOverview authentication wiring', () => {
  beforeEach(() => {
    api.serviceAction.mockClear()
    api.refresh.mockClear()
    api.overview = {
      service: { state: 'stopped', controlSupported: true, autostartSupported: true },
      storage: { path: '/data/docker', configured: true, valid: true, migrationSupported: true, disk: {} },
      composeProjects: {},
      containers: {},
    }
  })

  it('executes the first service action under the login-only auth policy', async () => {
    const user = userEvent.setup()
    const requireAuth = vi.fn()
    render(<DockerOverview authed={false} onRequireAuth={requireAuth} onOpenCompose={vi.fn()}/>)

    await user.click(screen.getByRole('button', { name: '启动' }))

    expect(requireAuth).toHaveBeenCalledWith('启动 Docker')
    await waitFor(() => expect(api.serviceAction).toHaveBeenCalledWith('start'))
  })

  it.each([
    ['permission_denied', '权限不足'],
    ['timeout', '连接超时'],
    ['unreachable', '无法连接'],
  ])('renders %s daemon failures with a distinct label', (state, label) => {
    api.overview.service.state = state
    render(<DockerOverview onRequireAuth={vi.fn()} onOpenCompose={vi.fn()}/>)
    expect(screen.getAllByText(label).length).toBeGreaterThan(0)
  })

  it('disables migration for a remote read-only daemon and shows the reason', () => {
    api.overview.service = {
      state: 'running',
      controlSupported: false,
      autostartSupported: false,
      diagnostic: '远程 Docker daemon 仅支持只读概览',
    }
    api.overview.storage.migrationSupported = false
    api.overview.storage.migrationDiagnostic = '远程 Docker daemon 仅支持只读概览'
    render(<DockerOverview onRequireAuth={vi.fn()} onOpenCompose={vi.fn()}/>)

    expect(screen.getByRole('button', { name: '迁移' })).toBeDisabled()
    expect(screen.getAllByText('远程 Docker daemon 仅支持只读概览').length).toBeGreaterThan(0)
  })

  it('requires storage confirmation before enabling start', () => {
    api.overview.storage.configured = false
    render(<DockerOverview onRequireAuth={vi.fn()} onOpenCompose={vi.fn()}/>)

    expect(screen.getByRole('button', { name: '启动' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '设置' })).toBeEnabled()
    expect(screen.getByText(/启动前请先完成设置/)).toBeInTheDocument()
  })
})
