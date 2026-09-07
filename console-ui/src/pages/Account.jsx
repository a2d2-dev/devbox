import { useState } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import ProfilePanel from './account/ProfilePanel'
import AppearanceSettings from '../components/AppearanceSettings'
import { useSessions, logoutOthers, revokeSession } from '../hooks/useApi'

// Account —「个人设置」系统 app（issue #30 T4 骨架 → T5/T6/T7 全量接入）
//
// 三 tab：我的账号（T5 <ProfilePanel/>）/ 主题壁纸（T6 <AppearanceSettings/>，
// 读写 App.jsx 的 useTweaks 偏好并防抖写回后端）/ 登录设备（T7）：
//   GET  /api/v1/account/sessions       活跃会话（倒序，已脱敏）
//   DELETE /api/v1/account/sessions/{id} 退出指定非当前会话
//   POST /api/v1/account/logout-others  退出本人除当前外全部会话
// 安全：后端已脱敏（IP 打码 / UA 归纳 / 无 token）。前端只展示，绝不还原、
// 拼接或打印任何完整 IP / 原始 UA / token。
// 样式沿用 Settings.jsx 的侧栏 + 卡片结构，颜色一律取自 tokens.js 的 T.*。

const tabs = [
  { id: 'profile', label: '我的账号', icon: 'user' },
  { id: 'appearance', label: '主题壁纸', icon: 'palette' },
  { id: 'devices', label: '登录设备', icon: 'shield' },
]

const panel = {
  background: T.surface, border: `1px solid ${T.border}`, borderRadius: 8,
  overflow: 'hidden', boxShadow: '0 1px 2px rgba(15,23,42,0.04)',
}

// ─── 登录设备（T7）──────────────────────────────────────────────

// deviceType → 现有 Icon 名（icons.jsx 无独立手机/平板字形，取语义最接近者）。
const deviceIcon = {
  desktop: 'cpu',
  mobile: 'wifi',
  tablet: 'sidebar',
  unknown: 'globe',
}

// 把后端 RFC3339 时间戳格式化成 "2026-08-24 09:30"（按本地时区）。
// 后端时间为权威值，前端只做展示格式化，不做任何拼接/还原。
function formatLoginAt(iso) {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function CurrentBadge() {
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: 4, padding: '2px 8px',
      borderRadius: 999, background: T.blue, color: 'white',
      fontSize: 10.5, fontWeight: 700, letterSpacing: 0.4, flex: '0 0 auto',
    }}>
      <Icon name="check" size={11}/>本机
    </span>
  )
}

function DeviceRow({ session, onRevoke, busy }) {
  const icon = deviceIcon[session.deviceType] || deviceIcon.unknown
  const current = !!session.current
  return (
    <div style={{
      display: 'flex', alignItems: 'center', gap: 12, padding: '12px 16px',
      borderTop: `1px solid ${T.borderSoft}`,
      background: current ? T.blueSoft : 'transparent',
    }}>
      <div style={{
        width: 38, height: 38, borderRadius: 8, flex: '0 0 auto',
        display: 'grid', placeItems: 'center',
        background: current ? 'white' : T.surfaceAlt,
        border: `1px solid ${current ? '#cfe4ff' : T.border}`,
        color: current ? T.blueDeep : T.ink3,
      }}>
        <Icon name={icon} size={19}/>
      </div>
      <div style={{ minWidth: 0, flex: 1 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
          <span style={{ fontSize: 13, fontWeight: 650, color: T.ink, letterSpacing: 0 }}>
            {session.deviceLabel || '未知设备'}
          </span>
          {current && <CurrentBadge/>}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginTop: 4, fontSize: 11.5, color: T.ink3, flexWrap: 'wrap' }}>
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
            <Icon name="globe" size={12}/>{session.ipMasked || 'IP 未知'}
          </span>
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
            <Icon name="clock" size={12}/>{formatLoginAt(session.loginAt)}
          </span>
        </div>
      </div>
      {!current && (
        <button
          type="button"
          onClick={() => onRevoke(session)}
          disabled={busy}
          aria-label={`退出 ${session.deviceLabel || '未知设备'}`}
          style={{
            height: 30, padding: '0 11px', borderRadius: 6, flex: '0 0 auto',
            display: 'inline-flex', alignItems: 'center', gap: 5,
            background: T.surface, color: T.red, border: `1px solid #fecaca`,
            fontSize: 12, fontWeight: 600, cursor: busy ? 'not-allowed' : 'pointer',
          }}
        >
          <Icon name="logout" size={13}/>退出
        </button>
      )}
    </div>
  )
}

// 二次确认弹层：作用域限定在卡片内（position:absolute），沿用审计日志/进程页
// 的确认弹层样式，避免覆盖整屏。
function ConfirmLogoutOthers({ busy, onCancel, onConfirm }) {
  return (
    <div role="dialog" aria-modal="true" aria-label="确认退出其他全部设备" onClick={busy ? undefined : onCancel}
      style={{ position: 'absolute', inset: 0, zIndex: 40, background: 'rgba(15,23,42,.35)', display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 16 }}>
      <div onClick={(e) => e.stopPropagation()} style={{ width: 400, maxWidth: '100%', padding: 20, background: T.surface, border: `1px solid ${T.border}`, borderRadius: 8, boxShadow: '0 18px 48px rgba(15,23,42,.2)' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 9 }}>
          <div style={{ width: 34, height: 34, borderRadius: '50%', display: 'grid', placeItems: 'center', background: T.redSoft, color: T.red, flex: '0 0 auto' }}>
            <Icon name="logout" size={17}/>
          </div>
          <div style={{ fontSize: 15, fontWeight: 700, color: T.ink }}>退出其他全部设备？</div>
        </div>
        <div style={{ marginTop: 12, fontSize: 12.5, color: T.ink2, lineHeight: 1.7 }}>
          将吊销你在其他设备上的全部登录会话，那些设备需要重新登录。当前设备（本机）不受影响。
        </div>
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 18 }}>
          <button type="button" disabled={busy} onClick={onCancel} style={dialogBtn}>取消</button>
          <button type="button" disabled={busy} onClick={onConfirm} style={{ ...dialogBtn, color: '#fff', background: T.red, borderColor: T.red }}>
            {busy ? '正在退出…' : '确认退出'}
          </button>
        </div>
      </div>
    </div>
  )
}

const dialogBtn = { height: 32, padding: '0 14px', border: `1px solid ${T.border}`, borderRadius: 6, background: T.surface, color: T.ink2, cursor: 'pointer', fontSize: 12.5, fontWeight: 600 }

function ConfirmRevokeSession({ session, busy, onCancel, onConfirm }) {
  if (!session) return null
  return (
    <div role="dialog" aria-modal="true" aria-label="确认退出指定设备" onClick={busy ? undefined : onCancel}
      style={{ position: 'absolute', inset: 0, zIndex: 40, background: 'rgba(15,23,42,.35)', display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 16 }}>
      <div onClick={(e) => e.stopPropagation()} style={{ width: 400, maxWidth: '100%', padding: 20, background: T.surface, border: `1px solid ${T.border}`, borderRadius: 8, boxShadow: '0 18px 48px rgba(15,23,42,.2)' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 9 }}>
          <div style={{ width: 34, height: 34, borderRadius: '50%', display: 'grid', placeItems: 'center', background: T.redSoft, color: T.red, flex: '0 0 auto' }}>
            <Icon name="logout" size={17}/>
          </div>
          <div style={{ minWidth: 0 }}>
            <div style={{ fontSize: 15, fontWeight: 700, color: T.ink }}>退出这台设备？</div>
            <div style={{ marginTop: 3, fontSize: 12, color: T.ink3 }}>{session.deviceLabel || '未知设备'}</div>
          </div>
        </div>
        <div style={{ marginTop: 12, fontSize: 12.5, color: T.ink2, lineHeight: 1.7 }}>
          将吊销该设备上的登录会话，它需要重新登录。当前设备不受影响。
        </div>
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 18 }}>
          <button type="button" disabled={busy} onClick={onCancel} style={dialogBtn}>取消</button>
          <button type="button" disabled={busy} onClick={onConfirm} style={{ ...dialogBtn, color: '#fff', background: T.red, borderColor: T.red }}>
            {busy ? '正在退出…' : '确认退出'}
          </button>
        </div>
      </div>
    </div>
  )
}

function DevicesPanel() {
  const { data: sessions, loading, error, refresh } = useSessions()
  const [confirming, setConfirming] = useState(false)
  const [revokeTarget, setRevokeTarget] = useState(null)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState(null) // { tone: 'ok'|'err', text }

  const onConfirm = async () => {
    setBusy(true)
    try {
      await logoutOthers()
      setConfirming(false)
      setNotice({ tone: 'ok', text: '已退出其他全部设备。' })
      refresh()
    } catch (e) {
      setConfirming(false)
      setNotice({ tone: 'err', text: '退出失败：' + (e?.message || '请稍后重试') })
    } finally {
      setBusy(false)
    }
  }

  const onRevokeConfirm = async () => {
    if (!revokeTarget) return
    setBusy(true)
    try {
      await revokeSession(revokeTarget.id)
      setRevokeTarget(null)
      setNotice({ tone: 'ok', text: '已退出指定设备。' })
      refresh()
    } catch (e) {
      setRevokeTarget(null)
      setNotice({ tone: 'err', text: '退出失败：' + (e?.message || '请稍后重试') })
    } finally {
      setBusy(false)
    }
  }

  const list = Array.isArray(sessions) ? sessions : []
  const hasList = list.length > 0

  return (
    <>
      <div style={{ display: 'flex', alignItems: 'flex-start', marginBottom: 12, gap: 12 }}>
        <div style={{ minWidth: 0 }}>
          <h1 style={{ margin: 0, fontSize: 18, color: T.ink, letterSpacing: 0 }}>登录设备</h1>
          <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3 }}>显示当前活跃会话。IP 与设备信息已脱敏。</div>
        </div>
        <div style={{ flex: 1 }}/>
        <button
          type="button"
          onClick={() => { setNotice(null); setConfirming(true) }}
          disabled={!hasList}
          style={{
            display: 'inline-flex', alignItems: 'center', gap: 6, flex: '0 0 auto',
            height: 32, padding: '0 12px', borderRadius: 7, fontSize: 12.5, fontWeight: 600,
            background: 'white', color: hasList ? T.red : T.ink4,
            border: `1px solid ${hasList ? '#fecaca' : T.border}`,
            cursor: hasList ? 'pointer' : 'default',
          }}
        >
          <Icon name="logout" size={14}/>退出其他全部设备
        </button>
      </div>

      {notice && (
        <div role="status" style={{
          marginBottom: 12, padding: '9px 12px', borderRadius: 7, fontSize: 12,
          display: 'flex', alignItems: 'center', gap: 8,
          background: notice.tone === 'ok' ? T.greenSoft : T.redSoft,
          color: notice.tone === 'ok' ? T.green : T.red,
          border: `1px solid ${notice.tone === 'ok' ? '#b9e6cd' : '#fecaca'}`,
        }}>
          <Icon name={notice.tone === 'ok' ? 'check' : 'alertTri'} size={14}/>{notice.text}
        </div>
      )}

      <section style={{ ...panel, position: 'relative' }}>
        <div style={{ minHeight: 43, padding: '0 16px', display: 'flex', alignItems: 'center', gap: 8, background: T.surfaceAlt, borderBottom: `1px solid ${T.borderSoft}` }}>
          <Icon name="shield" size={15} style={{ color: T.blueDeep }}/>
          <h2 style={{ margin: 0, fontSize: 13.5, fontWeight: 700, color: T.ink, letterSpacing: 0 }}>活跃会话</h2>
          <div style={{ flex: 1 }}/>
          {hasList && <span style={{ fontSize: 11, color: T.ink4 }}>{list.length} 条</span>}
        </div>

        {loading && !sessions ? (
          <div style={{ padding: '40px 24px', display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 9, color: T.ink3, fontSize: 12.5 }}>
            <Icon name="refresh" size={15}/>正在加载活跃会话…
          </div>
        ) : error && !hasList ? (
          <div style={{ padding: '40px 24px', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 12, textAlign: 'center' }}>
            <div style={{ width: 48, height: 48, borderRadius: '50%', display: 'grid', placeItems: 'center', background: T.redSoft, color: T.red }}>
              <Icon name="alertTri" size={22}/>
            </div>
            <div style={{ fontSize: 12.5, color: T.ink2 }}>加载活跃会话失败，请稍后重试。</div>
            <button type="button" onClick={refresh} style={dialogBtn}>
              <Icon name="refresh" size={13} style={{ marginRight: 5, verticalAlign: '-2px' }}/>重新加载
            </button>
          </div>
        ) : !hasList ? (
          <EmptyStateText icon="shield" text="暂无活跃会话"/>
        ) : (
          <div>
            {list.map((s) => <DeviceRow key={s.id} session={s} busy={busy} onRevoke={(target) => { setNotice(null); setRevokeTarget(target) }}/>)}
          </div>
        )}

        {confirming && (
          <ConfirmLogoutOthers busy={busy} onCancel={() => setConfirming(false)} onConfirm={onConfirm}/>
        )}
        {revokeTarget && (
          <ConfirmRevokeSession session={revokeTarget} busy={busy} onCancel={() => setRevokeTarget(null)} onConfirm={onRevokeConfirm}/>
        )}
      </section>

      <div style={{ marginTop: 10, fontSize: 11, color: T.ink4, lineHeight: 1.6 }}>
        以上为当前活跃会话。「退出其他全部设备」仅吊销其他设备的登录会话，当前设备不受影响。
      </div>
    </>
  )
}

function EmptyStateText({ icon, text }) {
  return (
    <div style={{ padding: '44px 24px', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 12, textAlign: 'center' }}>
      <div style={{ width: 52, height: 52, borderRadius: '50%', display: 'grid', placeItems: 'center', background: T.surfaceAlt, border: `1px solid ${T.border}`, color: T.ink4 }}>
        <Icon name={icon} size={24}/>
      </div>
      <div style={{ fontSize: 12.5, color: T.ink3 }}>{text}</div>
    </div>
  )
}

export default function Account({ t = {}, setT = () => {} }) {
  const [active, setActive] = useState('profile')

  return (
    <div className="account-shell" style={{ flex: 1, minHeight: 0, display: 'grid', gridTemplateColumns: '178px minmax(0,1fr)', background: T.surfaceAlt }}>
      <style>{`@media (max-width: 700px) {
        .account-shell { grid-template-columns: minmax(0,1fr) !important; grid-template-rows: auto minmax(0,1fr); }
        .account-sidebar { padding: 8px !important; }
        .account-sidebar-title { display: none; }
        .account-nav { flex-direction: row !important; overflow-x: auto; }
        .account-nav button { flex: 0 0 auto; }
        .account-main { padding: 14px 10px !important; }
      }`}</style>
      <aside className="account-sidebar" style={{ background: '#172033', color: 'white', padding: '18px 10px', minHeight: 0 }}>
        <div className="account-sidebar-title" style={{ padding: '0 10px 14px', fontSize: 12.5, fontWeight: 700, letterSpacing: 0 }}>个人设置</div>
        <nav className="account-nav" style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
          {tabs.map(tab => (
            <button
              key={tab.id}
              type="button"
              aria-pressed={active === tab.id}
              onClick={() => setActive(tab.id)}
              style={{ border: 0, borderRadius: 6, height: 36, padding: '0 10px', display: 'flex', alignItems: 'center', gap: 9, cursor: 'pointer', background: active === tab.id ? 'rgba(255,255,255,0.13)' : 'transparent', color: active === tab.id ? 'white' : '#aab5c7', fontSize: 12.5, fontWeight: active === tab.id ? 650 : 500, textAlign: 'left', letterSpacing: 0 }}
            >
              <Icon name={tab.icon} size={15}/>{tab.label}
            </button>
          ))}
        </nav>
      </aside>
      <main className="account-main" style={{ minWidth: 0, minHeight: 0, overflow: 'auto', padding: '20px clamp(16px, 3vw, 30px)' }}>
        <div style={{ maxWidth: 980, margin: '0 auto' }}>
          {active === 'profile' && <ProfilePanel/>}
          {active === 'appearance' && <AppearanceSettings t={t} setT={setT}/>}
          {active === 'devices' && <DevicesPanel/>}
        </div>
      </main>
    </div>
  )
}
