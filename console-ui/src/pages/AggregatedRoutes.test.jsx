import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { ToastProvider } from '../components/Toast'
import Files from './Files'
import NetworkSecurity from './NetworkSecurity'

vi.mock('../hooks/useApi', () => ({
  authFetch: vi.fn(() => Promise.resolve({
    ok: true,
    json: () => Promise.resolve([]),
  })),
}))

vi.mock('./Downloads', () => ({
  default: () => <div>下载任务列表</div>,
}))

vi.mock('./Backup', () => ({
  default: () => <div>备份任务列表</div>,
}))

vi.mock('./NetworkConnections', () => ({
  default: () => <div>网络连接列表</div>,
}))

vi.mock('./Links', () => ({
  default: () => <div>服务导航列表</div>,
}))

describe('aggregated desktop routes', () => {
  it('opens the downloads tab inside Files', () => {
    render(<ToastProvider><Files initialTab="downloads" /></ToastProvider>)
    expect(screen.getByText('下载任务列表')).toBeInTheDocument()
  })

  it('opens the backup tab inside Files', () => {
    render(<ToastProvider><Files initialTab="backup" /></ToastProvider>)
    expect(screen.getByText('备份任务列表')).toBeInTheDocument()
  })

  it('opens the connections tab inside NetworkSecurity', () => {
    render(<NetworkSecurity initialTab="connections" />)
    expect(screen.getByText('网络连接列表')).toBeInTheDocument()
  })

  it('opens the links tab inside NetworkSecurity', () => {
    render(<NetworkSecurity initialTab="links" />)
    expect(screen.getByText('服务导航列表')).toBeInTheDocument()
  })
})
