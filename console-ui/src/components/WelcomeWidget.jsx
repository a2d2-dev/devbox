import { useCallback, useEffect, useMemo, useState } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { authFetch } from '../hooks/useApi'

const GUIDES = [
  { id: 'storage', appId: 'disks', icon: 'hardDrive', title: '初始化存储', desc: '确认工作区与磁盘状态，准备文件和应用数据空间。', action: '打开磁盘管理' },
  { id: 'recommendedApps', appId: 'store', icon: 'store', title: '安装推荐应用', desc: '从应用商店选择开发环境和常用服务。', action: '浏览应用商店' },
  { id: 'remoteAccess', appId: 'links', icon: 'network', title: '了解远程访问', desc: '在服务导航中确认可访问入口与网络地址。', action: '查看服务入口' },
  { id: 'securityContact', appId: 'settings', icon: 'shield', title: '设置安全联系邮箱', desc: '保存用于安全通知和运维联系的邮箱地址。', action: '查看系统设置' },
]

export function WelcomeWidget({ onOpenApp, deviceName }) {
  const [state, setState] = useState(null)
  const [email, setEmail] = useState('')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const load = useCallback(async () => {
    try {
      const response = await authFetch('/api/v1/onboarding')
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const data = await response.json()
      setState(data)
      setEmail(data.contactEmail || '')
      setError('')
    } catch {
      setError('引导状态暂时无法读取')
    }
  }, [])

  // load() only updates state after its first await; this is an initial external-store sync.
  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => { load() }, [load])

  const pending = useMemo(() => GUIDES.filter((guide) => (state?.steps?.[guide.id] || 'pending') === 'pending'), [state])
  const skipped = useMemo(() => GUIDES.filter((guide) => state?.steps?.[guide.id] === 'skipped'), [state])
  const completedCount = GUIDES.filter((guide) => state?.steps?.[guide.id] === 'completed').length
  const current = pending[0]

  const update = async (guide, status) => {
    setSaving(true)
    setError('')
    try {
      const payload = { step: guide.id, status }
      if (guide.id === 'securityContact' && status === 'completed') payload.contactEmail = email.trim()
      const response = await authFetch('/api/v1/onboarding', {
        method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload),
      })
      if (!response.ok) throw new Error((await response.text()).trim() || `HTTP ${response.status}`)
      setState(await response.json())
    } catch (err) {
      setError(err.message === 'contact email is required' || err.message === 'invalid contact email' ? '请输入有效的安全联系邮箱' : '引导状态保存失败，请重试')
    } finally {
      setSaving(false)
    }
  }

  const restoreSkipped = async () => {
    setSaving(true)
    setError('')
    try {
      for (const guide of skipped) {
        const response = await authFetch('/api/v1/onboarding', {
          method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ step: guide.id, status: 'pending' }),
        })
        if (!response.ok) throw new Error(`HTTP ${response.status}`)
      }
      await load()
    } catch {
      setError('恢复引导失败，请重试')
    } finally {
      setSaving(false)
    }
  }

  if (!state && !error) return <div style={shellStyle}><div style={{ color: T.ink4, fontSize: 12 }}>正在读取首次使用引导...</div></div>
  if (!current && skipped.length === 0) return null

  return (
    <section aria-label="首次使用引导" style={shellStyle}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 13, fontWeight: 700, color: T.ink }}>欢迎来到 {deviceName || 'DevBox'}</div>
          <div style={{ marginTop: 3, fontSize: 10.5, color: T.ink3 }}>{completedCount}/{GUIDES.length} 项已完成</div>
        </div>
        <div style={{ width: 54, height: 5, borderRadius: 3, background: '#e2e8f0', overflow: 'hidden' }}>
          <div style={{ width: `${completedCount / GUIDES.length * 100}%`, height: '100%', background: T.green }}/>
        </div>
      </div>

      {current ? <div style={{ marginTop: 12, paddingTop: 12, borderTop: `1px solid ${T.borderSoft}` }}>
        <div style={{ display: 'flex', gap: 10 }}>
          <div style={{ width: 34, height: 34, borderRadius: 7, flexShrink: 0, display: 'grid', placeItems: 'center', background: T.blueSoft, color: T.blue }}><Icon name={current.icon} size={17} stroke={1.8}/></div>
          <div style={{ minWidth: 0 }}><div style={{ fontSize: 12.5, fontWeight: 700, color: T.ink }}>{current.title}</div><div style={{ marginTop: 3, fontSize: 11, lineHeight: 1.55, color: T.ink3 }}>{current.desc}</div></div>
        </div>
        {current.id === 'storage' && state?.readiness?.storageConfigured === false && <div style={warningStyle}>存储空间未创建：{state.readiness.storageReason || '请先检查工作区目录'}</div>}
        {current.id === 'securityContact' && <input type="email" value={email} onChange={(event) => setEmail(event.target.value)} placeholder="name@example.com" aria-label="安全联系邮箱" style={emailStyle}/>}
        <button type="button" onClick={() => onOpenApp?.({ id: current.appId })} style={openButton}><Icon name={current.icon} size={12} stroke={1.8}/>{current.action}</button>
        <div style={{ display: 'flex', gap: 8, marginTop: 10 }}>
          <button type="button" disabled={saving} onClick={() => update(current, 'skipped')} style={secondaryButton}>跳过</button>
          <button type="button" disabled={saving} onClick={() => update(current, 'completed')} style={primaryButton}><Icon name="check" size={12} stroke={2}/>{saving ? '保存中...' : '标记完成'}</button>
        </div>
        {skipped.length > 0 && <button type="button" disabled={saving} onClick={restoreSkipped} style={{ ...secondaryButton, marginTop: 8, width: '100%' }}>
          恢复已跳过步骤（{skipped.length}）
        </button>}
      </div> : <div style={{ marginTop: 12, paddingTop: 12, borderTop: `1px solid ${T.borderSoft}` }}>
        <div style={{ color: T.ink3, fontSize: 11.5 }}>有 {skipped.length} 个步骤已跳过，可随时恢复继续。</div>
        <button type="button" disabled={saving} onClick={restoreSkipped} style={{ ...secondaryButton, marginTop: 10, width: '100%' }}>恢复已跳过步骤</button>
      </div>}
      {error && <div role="alert" style={{ marginTop: 9, color: T.red, fontSize: 10.5 }}>{error}</div>}
    </section>
  )
}

const shellStyle = { padding: 15, borderRadius: 8, background: 'rgba(255,255,255,0.78)', border: '1px solid rgba(255,255,255,0.94)', boxShadow: '0 6px 20px -8px rgba(15,23,42,0.16)', backdropFilter: 'blur(12px)' }
const warningStyle = { marginTop: 10, padding: '8px 9px', borderRadius: 6, background: '#fffbeb', border: '1px solid #fde68a', color: '#92400e', fontSize: 10.5, lineHeight: 1.5 }
const emailStyle = { width: '100%', boxSizing: 'border-box', height: 34, marginTop: 10, padding: '0 9px', borderRadius: 6, border: `1px solid ${T.border}`, background: '#fff', color: T.ink, fontSize: 12, outline: 'none' }
const openButton = { width: '100%', height: 32, marginTop: 10, borderRadius: 6, border: `1px solid ${T.border}`, background: '#fff', color: T.ink2, display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 6, fontSize: 11.5, fontWeight: 600, cursor: 'pointer' }
const secondaryButton = { flex: 1, height: 32, borderRadius: 6, border: `1px solid ${T.border}`, background: '#fff', color: T.ink3, fontSize: 11.5, cursor: 'pointer' }
const primaryButton = { flex: 1.4, height: 32, borderRadius: 6, border: 'none', background: T.blue, color: '#fff', display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 5, fontSize: 11.5, fontWeight: 600, cursor: 'pointer' }

export default WelcomeWidget
