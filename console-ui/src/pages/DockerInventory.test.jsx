// DockerInventory — 网络 / 存储 tab（T3）渲染测试：
//   有数据（字段映射）/ 空清单 / daemon 不可用（available:false 显示诊断，不白屏）。
//   存储 tab 另验证 data-root 卡与「切回概览」入口（不搬迁移代码）。
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DockerNetworksTab, DockerStorageTab } from './DockerInventory'

const api = vi.hoisted(() => ({
  networks: null,
  volumes: null,
  overview: null,
}))

vi.mock('../hooks/useApi', () => ({
  useDockerNetworks: () => ({ data: api.networks }),
  useDockerVolumes: () => ({ data: api.volumes }),
  useDockerOverview: () => ({ data: api.overview }),
}))

describe('DockerNetworksTab', () => {
  beforeEach(() => { api.networks = null })

  it('renders the network table with field mapping', () => {
    api.networks = { available: true, networks: [
      { name: 'alpha_default', driver: 'bridge', scope: 'local', internal: false, containers: 2 },
      { name: 'backend', driver: 'overlay', scope: 'swarm', internal: true, containers: 0 },
    ] }
    render(<DockerNetworksTab/>)

    expect(screen.getByText('alpha_default')).toBeInTheDocument()
    expect(screen.getByText('backend')).toBeInTheDocument()
    expect(screen.getByText('overlay')).toBeInTheDocument()
    expect(screen.getByText('是')).toBeInTheDocument() // internal
    expect(screen.getByText('2')).toBeInTheDocument()  // 容器数
    // 只读：无操作按钮列
    expect(screen.queryByRole('button', { name: /删除|创建|清理/ })).not.toBeInTheDocument()
  })

  it('shows the empty state for an empty list', () => {
    api.networks = { available: true, networks: [] }
    render(<DockerNetworksTab/>)
    expect(screen.getByText('暂无 docker network')).toBeInTheDocument()
  })

  it('shows the daemon diagnostic instead of a blank screen when unavailable', () => {
    api.networks = { available: false, diagnostic: 'Docker daemon 不可用: connection refused' }
    render(<DockerNetworksTab/>)
    expect(screen.getByRole('status')).toHaveTextContent('Docker daemon 不可用')
    expect(screen.getByText(/connection refused/)).toBeInTheDocument()
  })
})

describe('DockerStorageTab', () => {
  beforeEach(() => {
    api.volumes = null
    api.overview = {
      storage: { path: '/data/docker', configured: true, valid: true, migrationSupported: true,
        disk: { totalBytes: 1000, availableBytes: 400 } },
    }
  })

  it('renders the data-root card and volume table with compose project attribution', () => {
    api.volumes = { available: true, volumes: [
      { name: 'alpha_db', driver: 'local', mountpoint: '/var/lib/docker/volumes/alpha_db/_data', containers: 2, composeProject: 'alpha' },
      { name: 'scratch', driver: 'local', mountpoint: '/var/lib/docker/volumes/scratch/_data', containers: 0 },
    ] }
    render(<DockerStorageTab onOpenOverview={vi.fn()}/>)

    expect(screen.getByText('/data/docker')).toBeInTheDocument()
    expect(screen.getByText('alpha_db')).toBeInTheDocument()
    expect(screen.getByText('alpha')).toBeInTheDocument()  // compose project 归属
    expect(screen.getByText('scratch')).toBeInTheDocument()
  })

  it('routes the migration entry back to the overview tab instead of duplicating the dialog', async () => {
    const user = userEvent.setup()
    const onOpenOverview = vi.fn()
    api.volumes = { available: true, volumes: [] }
    render(<DockerStorageTab onOpenOverview={onOpenOverview}/>)

    await user.click(screen.getByRole('button', { name: /存储设置与迁移/ }))
    expect(onOpenOverview).toHaveBeenCalled()
    expect(screen.getByText('暂无 docker volume')).toBeInTheDocument()
  })

  it('shows the daemon diagnostic for the volume list when unavailable', () => {
    api.volumes = { available: false, diagnostic: 'Docker daemon 不可用: no such socket' }
    render(<DockerStorageTab onOpenOverview={vi.fn()}/>)
    expect(screen.getByRole('status')).toHaveTextContent('Docker daemon 不可用')
    expect(screen.getByText(/no such socket/)).toBeInTheDocument()
  })
})
