import { useState, useRef, useEffect } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, useClock } from './ui'
import { AnimatePresence, PopScale } from '../motion'

const CLOUD_LINK_STATES = {
  online: { label: '云端在线' },
  connecting: { label: '云端连接中' },
  offline: { label: '云端离线' },
  unknown: { label: '云端状态未知' },
}

function resolveCloudLinkState(value) {
  if (value === true || value === 'online') return 'online'
  if (value === false || value === 'offline') return 'offline'
  if (value === 'connecting') return 'connecting'
  return 'unknown'
}

export function CloudLinkIndicator({ status }) {
  const state = resolveCloudLinkState(status)
  const label = CLOUD_LINK_STATES[state].label
  const accessibleLabel = `云端连接状态：${label}`

  return (
    <div
      className={`edge-cloud-link edge-cloud-link--${state}`}
      role="status"
      aria-label={accessibleLabel}
      title={accessibleLabel}
      data-cloud-link-state={state}
    >
      <span className="edge-cloud-link-led" aria-hidden="true" />
      <span className="edge-cloud-link-label">{label}</span>
    </div>
  )
}

function MetricPill({ tone, label, value, colors }) {
  return (
    <div className="edge-status-metric" style={{
      display: 'inline-flex', alignItems: 'center', gap: 6,
      padding: '3px 9px 3px 8px', borderRadius: 999,
      background: colors.pillBg,
      fontSize: 12, color: colors.text2,
      whiteSpace: 'nowrap', flexShrink: 0,
    }}>
      <StatusDot tone={tone} size={7}/>
      <span style={{ color: colors.text3 }}>{label}</span>
      <span className="mono tnum" style={{ color: colors.text, fontWeight: 600 }}>{value}</span>
    </div>
  )
}

export function StatusBar({ cpu, gpu, mem, alertCount, online, onOpenAlerts, theme = 'light', deviceLabel, DEVICE, loginUser, onLogout }) {
  const now = useClock();
  const hh = String(now.getHours()).padStart(2, '0');
  const mm = String(now.getMinutes()).padStart(2, '0');
  const ss = String(now.getSeconds()).padStart(2, '0');
  const date = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`;
  const weekday = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'][now.getDay()];

  // Theme palette
  const isDark = theme === 'dark';
  const C = isDark ? {
    bar: '#0b1220',
    barBd: 'rgba(255,255,255,0.06)',
    text: '#f1f5f9',
    text2: 'rgba(241,245,249,0.65)',
    text3: 'rgba(241,245,249,0.45)',
    pillBg: 'rgba(255,255,255,0.06)',
    pillBdNone: 'transparent',
    timeBg: 'rgba(255,255,255,0.06)',
    sep: 'rgba(255,255,255,0.18)',
    logoBg: `linear-gradient(140deg, ${T.blue}, ${T.blueDeep})`,
    chevron: 'rgba(241,245,249,0.55)',
    alertBg: 'rgba(245,158,11,0.18)',
    alertFg: '#fbbf24',
    alertBd: 'rgba(245,158,11,0.45)',
  } : {
    bar: 'rgba(255,255,255,0.85)',
    barBd: 'rgba(226,232,240,0.7)',
    text: T.ink,
    text2: T.ink2,
    text3: T.ink3,
    pillBg: 'rgba(15,23,42,0.04)',
    pillBdNone: 'transparent',
    timeBg: 'rgba(15,23,42,0.04)',
    sep: '#cbd5e1',
    logoBg: `linear-gradient(140deg, ${T.blue}, ${T.blueDeep})`,
    chevron: T.ink4,
    alertBg: '#fef3c7',
    alertFg: '#b45309',
    alertBd: '#fde68a',
  };



  return (
    <div className="edge-status-bar" style={{
      height: 48, paddingLeft: 16, paddingRight: 14,
      display: 'flex', alignItems: 'center', gap: 12,
      background: C.bar,
      backdropFilter: isDark ? 'none' : 'saturate(180%) blur(14px)',
      WebkitBackdropFilter: isDark ? 'none' : 'saturate(180%) blur(14px)',
      borderBottom: `1px solid ${C.barBd}`,
      position: 'relative', zIndex: 30, flexShrink: 0,
    }}>
      {/* Left: device identity (user menu 在右侧 line 169 LF 2026-06-21) */}
      <div className="edge-status-identity" style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0, flexShrink: 0 }}>
        <div className="edge-status-logo" style={{
          width: 28, height: 28, borderRadius: 8,
          background: C.logoBg, color: 'white',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          fontSize: 11, fontWeight: 700, letterSpacing: 0.5,
          boxShadow: '0 2px 4px rgba(37,99,235,0.3)',
        }}>E</div>
        <div className="edge-status-device" style={{ minWidth: 0, lineHeight: 1.15 }}>
          <div className="edge-status-device-name" style={{ fontSize: 13, fontWeight: 600, color: C.text, display: 'flex', alignItems: 'center', gap: 6 }}>
            {deviceLabel || DEVICE.name}
            <span className="edge-status-dept" style={{
              fontSize: 9.5, padding: '1px 5px', borderRadius: 3,
              background: isDark ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.06)',
              color: C.text3, fontWeight: 600,
              letterSpacing: '0.04em',
            }}>{DEVICE.dept}</span>
            {/* chevDown 删除 — 装饰但不触发下拉是误导 (LF 2026-06-21) */}
          </div>
          <div className="edge-status-device-details" style={{ fontSize: 10.5, color: C.text3, marginTop: 1, display: 'flex', alignItems: 'center', gap: 6 }}>
            <span>{DEVICE.site}</span>
            <span style={{ color: C.sep }}>·</span>
            <span className="mono">{DEVICE.sn}</span>
          </div>
        </div>
      </div>

      {/* Center: health pills */}
      <div className="edge-status-health" style={{ flex: 1, display: 'flex', justifyContent: 'center', gap: 6, flexWrap: 'nowrap', overflow: 'hidden' }}>
        <MetricPill tone={cpu < 70 ? 'green' : 'amber'} label="CPU" value={`${cpu}%`} colors={C}/>
        <MetricPill tone={gpu < 80 ? 'green' : 'amber'} label="GPU" value={`${gpu}%`} colors={C}/>
        <MetricPill tone={mem < 80 ? 'green' : 'amber'} label="内存" value={`${mem}%`} colors={C}/>
        <button type="button" className="edge-status-alert" aria-label={`${alertCount} 条告警`} onClick={onOpenAlerts} style={{
          display: 'inline-flex', alignItems: 'center', gap: 6, padding: '3px 9px 3px 8px', borderRadius: 999,
          background: alertCount > 0 ? C.alertBg : C.pillBg,
          border: alertCount > 0 ? `1px solid ${C.alertBd}` : '1px solid transparent',
          color: alertCount > 0 ? C.alertFg : C.text2,
          fontSize: 12, cursor: 'pointer',
          whiteSpace: 'nowrap', flexShrink: 0,
        }}>
          <Icon name="alertTri" size={12} stroke={2}/>
          <span className="tnum" style={{ fontWeight: 600 }}>{alertCount}</span>
          <span>条告警</span>
        </button>
      </div>

      {/* Right: cloud + time */}
      <div className="edge-status-actions" style={{ display: 'flex', alignItems: 'center', gap: 12, flexShrink: 0 }}>
        <CloudLinkIndicator status={online} />
        <div className="edge-status-time" style={{
          display: 'inline-flex', alignItems: 'center', gap: 8,
          padding: '4px 10px', borderRadius: 8,
          background: C.timeBg,
          whiteSpace: 'nowrap',
        }}>
          <div className="mono tnum" style={{ fontSize: 13.5, fontWeight: 600, color: C.text, letterSpacing: 0.5 }}>
            {hh}:{mm}<span style={{ color: C.text3, fontWeight: 500 }}>:{ss}</span>
          </div>
          <div style={{ fontSize: 10.5, color: C.text3, lineHeight: 1.15, whiteSpace: 'nowrap' }}>
            <div>{date}</div>
            <div>{weekday}</div>
          </div>
        </div>

        {/* 用户菜单（含退出登录） */}
        {(loginUser || onLogout) && <UserMenu user={loginUser || 'user'} theme={theme} onLogout={onLogout}/>}
      </div>
    </div>
  );
}

// UserMenu — 顶栏用户头像下拉，提供退出登录入口
function UserMenu({ user, theme, onLogout }) {
  const [open, setOpen] = useState(false)
  const ref = useRef(null)
  useEffect(() => {
    if (!open) return
    function onDoc(e) { if (ref.current && !ref.current.contains(e.target)) setOpen(false) }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])
  const dark = theme === 'dark'
  const initial = (user || '?').charAt(0).toUpperCase()
  return (
    <div ref={ref} className="edge-status-user" style={{ position: 'relative' }}>
      <button className="edge-status-user-button" aria-label={`用户菜单：${user}`} onClick={() => setOpen(o => !o)} title={user} style={{
        display: 'inline-flex', alignItems: 'center', gap: 6,
        height: 28, padding: '0 4px 0 4px', border: 'none',
        background: dark ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.04)',
        borderRadius: 999, cursor: 'pointer',
      }}>
        <span style={{
          width: 22, height: 22, borderRadius: '50%',
          background: 'linear-gradient(135deg, #3b82f6, #1d4ed8)',
          color: 'white', fontSize: 11.5, fontWeight: 600,
          display: 'flex', alignItems: 'center', justifyContent: 'center',
        }}>{initial}</span>
        <Icon name="chevDown" size={11} stroke={2}
          style={{ color: dark ? 'rgba(255,255,255,0.6)' : '#64748b' }}/>
      </button>
      <AnimatePresence>
        {open && (
          <PopScale origin="top right" style={{
            position: 'absolute', top: 34, right: 0, zIndex: 1000,
            minWidth: 180, background: 'white', borderRadius: 10,
            boxShadow: '0 12px 32px -4px rgba(15,23,42,0.18), 0 0 0 1px rgba(15,23,42,0.06)',
            padding: 6, color: '#0f172a',
          }}>
            <div style={{ padding: '8px 12px 10px', borderBottom: '1px solid #f1f5f9' }}>
              <div style={{ fontSize: 12.5, fontWeight: 600, color: '#0f172a' }}>{user}</div>
              <div style={{ fontSize: 10.5, color: '#64748b', marginTop: 2 }}>已登录</div>
            </div>
            <button className="edge-menu-item" onClick={() => { setOpen(false); onLogout && onLogout() }} style={{
              width: '100%', textAlign: 'left', padding: '8px 12px',
              border: 'none', background: 'transparent', cursor: 'pointer',
              fontSize: 13, color: '#dc2626', fontWeight: 500,
              borderRadius: 6, display: 'flex', alignItems: 'center', gap: 8,
            }}>
              <Icon name="lock" size={13} stroke={1.8}/>退出登录
            </button>
          </PopScale>
        )}
      </AnimatePresence>
    </div>
  );
}
