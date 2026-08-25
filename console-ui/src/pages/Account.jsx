import { useState } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'

// Account —「个人设置」系统 app（issue #30 T4 骨架）
//
// 三 tab 骨架：我的账号 / 主题壁纸 / 登录设备。
// 本票只实现可切换的 tab 外壳 + 占位空状态；每个 tab 的真实内容与后端对接由
// 后续子任务补齐（out of scope）。样式沿用 Settings.jsx 的侧栏 + 卡片结构，
// 颜色一律取自 tokens.js 的 T.*，以随调色板自适应。

const tabs = [
  { id: 'profile', label: '我的账号', icon: 'user' },
  { id: 'appearance', label: '主题壁纸', icon: 'palette' },
  { id: 'devices', label: '登录设备', icon: 'shield' },
]

const placeholders = {
  profile: {
    title: '我的账号',
    subtitle: '账户信息与安全设置',
    icon: 'user',
    hint: '账户资料、密码与安全设置即将上线。',
  },
  appearance: {
    title: '主题壁纸',
    subtitle: '外观主题与桌面壁纸',
    icon: 'palette',
    hint: '主题切换与壁纸自定义即将上线。',
  },
  devices: {
    title: '登录设备',
    subtitle: '已登录设备与会话管理',
    icon: 'shield',
    hint: '登录设备列表与会话注销即将上线。',
  },
}

const panel = {
  background: T.surface, border: `1px solid ${T.border}`, borderRadius: 8,
  overflow: 'hidden', boxShadow: '0 1px 2px rgba(15,23,42,0.04)',
}

function EmptyState({ icon, hint }) {
  return (
    <div style={{ padding: '48px 24px', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 14, textAlign: 'center' }}>
      <div style={{ width: 56, height: 56, borderRadius: '50%', display: 'grid', placeItems: 'center', background: T.surfaceAlt, border: `1px solid ${T.border}`, color: T.ink4 }}>
        <Icon name={icon} size={26}/>
      </div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 7, fontSize: 12.5, fontWeight: 600, color: T.ink2 }}>
        <span style={{ padding: '2px 9px', borderRadius: 999, background: T.blueSoft, color: T.blueDeep, fontSize: 10.5, fontWeight: 700, letterSpacing: 0.4 }}>即将上线</span>
      </div>
      <div style={{ fontSize: 12, color: T.ink3, maxWidth: 300, lineHeight: 1.55 }}>{hint}</div>
    </div>
  )
}

function TabPanel({ tab }) {
  const meta = placeholders[tab]
  return (
    <>
      <div style={{ marginBottom: 12 }}>
        <h1 style={{ margin: 0, fontSize: 18, color: T.ink, letterSpacing: 0 }}>{meta.title}</h1>
        <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3 }}>{meta.subtitle}</div>
      </div>
      <section style={panel}>
        <div style={{ minHeight: 43, padding: '0 16px', display: 'flex', alignItems: 'center', gap: 8, background: T.surfaceAlt, borderBottom: `1px solid ${T.borderSoft}` }}>
          <Icon name={meta.icon} size={15} style={{ color: T.blueDeep }}/>
          <h2 style={{ margin: 0, fontSize: 13.5, fontWeight: 700, color: T.ink, letterSpacing: 0 }}>{meta.title}</h2>
        </div>
        <EmptyState icon={meta.icon} hint={meta.hint}/>
      </section>
    </>
  )
}

export default function Account() {
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
          <TabPanel tab={active}/>
        </div>
      </main>
    </div>
  )
}
