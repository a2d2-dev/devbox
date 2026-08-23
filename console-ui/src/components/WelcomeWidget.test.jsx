import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { setAuthRequired, setAuthToken } from '../hooks/useApi'
import { WelcomeWidget } from './WelcomeWidget'

const completedSteps = {
  storage: 'pending',
  recommendedApps: 'completed',
  remoteAccess: 'completed',
  securityContact: 'completed',
}

function installOnboardingAPI(initialSteps = completedSteps) {
  let steps = { ...initialSteps }
  const fetchMock = vi.fn(async (_url, options = {}) => {
    if (options.method === 'PATCH') {
      const update = JSON.parse(options.body)
      steps[update.step] = update.status
    }
    return new Response(JSON.stringify({
      steps,
      contactEmail: 'ops@example.com',
      readiness: { storageConfigured: true },
    }), { status: 200, headers: { 'Content-Type': 'application/json' } })
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('WelcomeWidget persistence flow', () => {
  beforeEach(() => {
    setAuthRequired(true)
    setAuthToken('test-token')
    vi.restoreAllMocks()
  })

  it('skips and restores a pending step through the backend', async () => {
    const user = userEvent.setup()
    const fetchMock = installOnboardingAPI()
    render(<WelcomeWidget onOpenApp={vi.fn()} deviceName="devbox-test"/>)

    expect(await screen.findByText('初始化存储')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '跳过' }))
    expect(await screen.findByText(/1 个步骤已跳过/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '恢复已跳过步骤' }))
    expect(await screen.findByText('初始化存储')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/onboarding', expect.objectContaining({ method: 'PATCH' }))
  })

  it('does not show a completed step again', async () => {
    const user = userEvent.setup()
    installOnboardingAPI()
    render(<WelcomeWidget onOpenApp={vi.fn()} deviceName="devbox-test"/>)

    await user.click(await screen.findByRole('button', { name: /标记完成/ }))
    await waitFor(() => expect(screen.queryByLabelText('首次使用引导')).not.toBeInTheDocument())
  })

  it('can restore a skipped step while another step is pending', async () => {
    const user = userEvent.setup()
    installOnboardingAPI({
      storage: 'skipped',
      recommendedApps: 'pending',
      remoteAccess: 'completed',
      securityContact: 'completed',
    })
    render(<WelcomeWidget onOpenApp={vi.fn()} deviceName="devbox-test"/>)

    expect(await screen.findByText('安装推荐应用')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /恢复已跳过步骤/ }))
    expect(await screen.findByText('初始化存储')).toBeInTheDocument()
  })
})
