import { T } from '../tokens'
import { Icon } from '../icons'
import { WALLPAPERS, resolveWallpaper } from '../data/wallpapers'

// AppearanceSettings —「个人设置 → 主题壁纸」tab 的真实内容（issue #30 T6）。
//
// 单一偏好状态源来自 App.jsx 的 useTweaks（t / setT）。这里只读 t、调 setT：
// setT 负责写 localStorage + 防抖回写 /api/v1/account/preferences（见
// TweaksPanel.useTweaks），所以这个组件本身不碰任何持久化 / 网络逻辑，点一下
// 即时改 t，桌面背景与主色随之立刻变化。
//
// 视觉上刻意做成「正式设置页」的大预览卡片，而非调试面板的小控件：每张壁纸
// 卡片渲染对应背景的真实缩略预览（与 App.jsx bgClass/bgStyle 逐一对应）。

// 壁纸缩略预览复用 data/wallpapers.js 的注册表与 resolveWallpaper：与 App.jsx 桌面
// 背景解析完全同源（含 NASA 照片壁纸），保证缩略图「所见即所得」。

// 主色候选 — 与 TweaksPanel/App.jsx 的 TweakColor 选项一致（首项即 fnOS 蓝）。
const ACCENTS = [
  { value: '#0066ff', label: 'fnOS 蓝' },
  { value: '#06b6d4', label: '青' },
  { value: '#10b981', label: '绿' },
  { value: '#8b5cf6', label: '紫' },
  { value: '#0f172a', label: '墨' },
]

const THEME_MODES = [
  { value: 'light',  label: '浅色' },
  { value: 'dark',   label: '深色' },
  { value: 'system', label: '跟随系统' },
]

function CheckBadge() {
  return (
    <div style={{
      position: 'absolute', top: 8, right: 8, width: 22, height: 22, borderRadius: '50%',
      background: T.blue, color: '#fff', display: 'grid', placeItems: 'center',
      boxShadow: '0 2px 6px rgba(0,102,255,0.4)', zIndex: 2,
    }}>
      <Icon name="check" size={13} stroke={3}/>
    </div>
  )
}

function SectionCard({ icon, title, subtitle, children }) {
  return (
    <section style={{
      background: T.surface, border: `1px solid ${T.border}`, borderRadius: 10,
      overflow: 'hidden', boxShadow: '0 1px 2px rgba(15,23,42,0.04)', marginBottom: 16,
    }}>
      <div style={{
        minHeight: 46, padding: '10px 16px', display: 'flex', alignItems: 'center', gap: 9,
        background: T.surfaceAlt, borderBottom: `1px solid ${T.borderSoft}`,
      }}>
        <Icon name={icon} size={15} style={{ color: T.blueDeep }}/>
        <div>
          <h2 style={{ margin: 0, fontSize: 13.5, fontWeight: 700, color: T.ink }}>{title}</h2>
          {subtitle && <div style={{ fontSize: 11, color: T.ink3, marginTop: 1 }}>{subtitle}</div>}
        </div>
      </div>
      <div style={{ padding: 16 }}>{children}</div>
    </section>
  )
}

function WallpaperCard({ wp, selected, onSelect }) {
  // 缩略预览与桌面同源：resolveWallpaper 给出 className（programmatic）或
  // cover 图 style（photo），保证所见即所得。
  const preview = resolveWallpaper(wp.id, T)
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      aria-label={`${wp.label} — ${wp.desc}`}
      onClick={() => onSelect(wp.id)}
      style={{
        position: 'relative', padding: 0, border: 0, background: 'transparent',
        cursor: 'pointer', textAlign: 'left', borderRadius: 10, display: 'block',
      }}
    >
      <div
        className={preview.className}
        style={{
          position: 'relative', height: 108, borderRadius: 10, overflow: 'hidden',
          border: selected ? `2px solid ${T.blue}` : `1px solid ${T.border}`,
          boxShadow: selected ? '0 0 0 3px rgba(0,102,255,0.18)' : '0 1px 2px rgba(15,23,42,0.05)',
          transition: 'box-shadow 0.15s, border-color 0.15s',
          ...(preview.style || {}),
        }}
      >
        {selected && <CheckBadge/>}
      </div>
      <div style={{ padding: '8px 2px 0' }}>
        <div style={{ fontSize: 12.5, fontWeight: 650, color: selected ? T.blueDeep : T.ink }}>{wp.label}</div>
        <div style={{ fontSize: 11, color: T.ink3, marginTop: 1 }}>{wp.desc}</div>
      </div>
    </button>
  )
}

export default function AppearanceSettings({ t, setT }) {
  const wallpaper = t.wallpaper
  const accent = t.accent
  const theme = t.theme || 'light'

  return (
    <>
      <div style={{ marginBottom: 12 }}>
        <h1 style={{ margin: 0, fontSize: 18, color: T.ink }}>主题壁纸</h1>
        <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3 }}>外观主题与桌面壁纸</div>
      </div>

      {/* 顶部说明：外观会跟随账号同步 */}
      <div style={{
        display: 'flex', alignItems: 'center', gap: 8, marginBottom: 16,
        padding: '9px 13px', borderRadius: 8, background: T.blueSoft,
        border: `1px solid ${T.blueBorder}`, color: T.blueDeep, fontSize: 12, lineHeight: 1.5,
      }}>
        <Icon name="cloud" size={15} style={{ flexShrink: 0 }}/>
        <span>外观设置会保存到你的账号，任何设备登录后自动恢复。</span>
      </div>

      {/* 壁纸选择区 — 内置壁纸缩略卡片（含 NASA 照片壁纸），自适应多列 */}
      <SectionCard icon="palette" title="桌面壁纸" subtitle="点击即时预览，选择自动保存">
        <div role="radiogroup" aria-label="桌面壁纸" style={{
          display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(180px, 1fr))', gap: 14,
        }}>
          {WALLPAPERS.map((wp) => (
            <WallpaperCard key={wp.id} wp={wp} selected={wallpaper === wp.id}
                           onSelect={(id) => setT('wallpaper', id)}/>
          ))}
        </div>
      </SectionCard>

      {/* 主题色 accent — 一排色块 */}
      <SectionCard icon="sparkle" title="主题色" subtitle="应用于图标、按钮与高亮">
        <div role="radiogroup" aria-label="主题色" style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
          {ACCENTS.map((a) => {
            const on = accent === a.value
            return (
              <button
                key={a.value}
                type="button"
                role="radio"
                aria-checked={on}
                aria-label={a.label}
                title={a.label}
                onClick={() => setT('accent', a.value)}
                style={{
                  position: 'relative', width: 44, height: 44, borderRadius: '50%',
                  background: a.value, cursor: 'pointer', padding: 0,
                  border: `2px solid ${T.surface}`,
                  boxShadow: on
                    ? `0 0 0 3px ${a.value}, 0 2px 8px rgba(15,23,42,0.2)`
                    : '0 0 0 1px rgba(15,23,42,0.12), 0 1px 3px rgba(15,23,42,0.12)',
                  transition: 'box-shadow 0.15s',
                  display: 'grid', placeItems: 'center',
                }}
              >
                {on && <Icon name="check" size={18} stroke={3} style={{ color: '#fff' }}/>}
              </button>
            )
          })}
        </div>
      </SectionCard>

      {/* 主题模式 — 浅色/深色/跟随系统 */}
      <SectionCard icon="eye" title="主题模式" subtitle="选择后立即应用到整个控制台">
        <div role="radiogroup" aria-label="主题模式" style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
          {THEME_MODES.map((m) => {
            const on = theme === m.value
            return (
              <button
                key={m.value}
                type="button"
                role="radio"
                aria-checked={on}
                onClick={() => setT('theme', m.value)}
                style={{
                  position: 'relative', minWidth: 120, padding: '12px 14px', borderRadius: 9,
                  cursor: 'pointer', textAlign: 'left',
                  background: on ? T.blueSoft : T.surface,
                  border: `1px solid ${on ? T.blueBorder : T.border}`,
                  boxShadow: on ? '0 0 0 2px rgba(0,102,255,0.12)' : 'none',
                  transition: 'box-shadow 0.15s, border-color 0.15s',
                  display: 'flex', alignItems: 'center', gap: 9,
                }}
              >
                <span style={{
                  width: 18, height: 18, borderRadius: '50%', flexShrink: 0,
                  border: `2px solid ${on ? T.blue : T.ink4}`,
                  display: 'grid', placeItems: 'center',
                }}>
                  {on && <span style={{ width: 8, height: 8, borderRadius: '50%', background: T.blue }}/>}
                </span>
                <span style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
                  <span style={{ fontSize: 13, fontWeight: 650, color: on ? T.blueDeep : T.ink }}>{m.label}</span>
                </span>
              </button>
            )
          })}
        </div>
      </SectionCard>
    </>
  )
}
