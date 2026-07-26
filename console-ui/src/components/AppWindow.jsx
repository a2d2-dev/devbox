import { useState } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { motion, springs, useMotionPref } from '../motion'
import { useViewportEnvironment } from '../hooks/useViewportEnvironment'

export const btnSecondary = {
  display: 'inline-flex', alignItems: 'center', gap: 6,
  height: 32, padding: '0 12px', borderRadius: 7,
  background: T.surface, border: `1px solid ${T.border}`,
  fontSize: 12.5, fontWeight: 500, color: T.ink2,
  cursor: 'pointer',
}
export const btnPrimary = {
  ...btnSecondary,
  background: T.blue, color: 'white', border: `1px solid ${T.blueDeep}`,
  fontWeight: 600,
}
export const btnDanger = {
  ...btnSecondary,
  background: 'white', color: T.red, border: '1px solid #fecaca',
}

const WINDOW_SHADOW = '0 24px 60px -12px rgba(15,23,42,0.32), 0 0 0 1px rgba(15,23,42,0.08)'
const WINDOW_SHADOW_CLEAR = '0 24px 60px -12px rgba(15,23,42,0), 0 0 0 1px rgba(15,23,42,0)'

function windowFrame(maximized, compactWindow) {
  if (compactWindow) {
    return { inset: 0, borderRadius: 0, boxShadow: WINDOW_SHADOW_CLEAR }
  }
  return maximized
    ? { top: '0%', left: '0%', right: '0%', bottom: '0%', borderRadius: 0, boxShadow: WINDOW_SHADOW_CLEAR }
    : { top: '5.5%', left: '7.5%', right: '7.5%', bottom: '12%', borderRadius: 14, boxShadow: WINDOW_SHADOW }
}

function dockOffset(appId, windowNode, getDockIconRect) {
  if (!windowNode) return { x: 0, y: 0 }
  const dockRect = getDockIconRect?.(appId)
  const offsetParentRect = windowNode.offsetParent?.getBoundingClientRect?.()
  const sourceX = (offsetParentRect?.left || 0) + windowNode.offsetLeft + windowNode.offsetWidth / 2
  const sourceY = (offsetParentRect?.top || 0) + windowNode.offsetTop + windowNode.offsetHeight / 2
  const targetX = dockRect ? dockRect.left + dockRect.width / 2 : window.innerWidth / 2
  const targetY = dockRect ? dockRect.top + dockRect.height / 2 : window.innerHeight - 42
  return { x: targetX - sourceX, y: targetY - sourceY }
}

export default function AppWindow({
  app, active = false, visible = true, minimized = false, maximized,
  getDockIconRect, onMaximize, onMinimize, onClose, onShowDesktop,
  children, headerActions, breadcrumb, mgmtOpen, onToggleMgmt, canManage,
}) {
  const pref = useMotionPref()
  const { compactWindow } = useViewportEnvironment()
  const [windowNode, setWindowNode] = useState(null)
  const showDesktop = onShowDesktop || onMinimize

  const trafficBtn = (label, color, onClick, icon, className = 'edge-btn-secondary', extraClass = '') => (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onClick}
      className={`edge-window-control edge-press ${className} ${extraClass}`}
      style={{
        width: 22, height: 22, borderRadius: 6, cursor: 'pointer',
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        color, transition: 'background 0.15s, color 0.15s',
        border: 'none', background: 'transparent', padding: 0,
      }}
    >{icon}</button>
  )

  const frame = windowFrame(maximized, compactWindow)
  const minimizeOffset = minimized && !compactWindow
    ? dockOffset(app.id, windowNode, getDockIconRect)
    : { x: 0, y: 0 }
  const activeTarget = {
    ...frame,
    opacity: visible ? 1 : 0,
    visibility: 'visible',
    transitionEnd: visible ? { visibility: 'visible' } : { visibility: 'hidden' },
    ...(pref.reduced || compactWindow ? {} : {
      x: minimized ? minimizeOffset.x : 0,
      y: minimized ? minimizeOffset.y : 0,
      scale: visible ? 1 : minimized ? 0.35 : 0.98,
    }),
  }
  const transition = pref.reduced || compactWindow
    ? {
        opacity: pref.fadeTransition,
        top: { duration: 0 }, left: { duration: 0 }, right: { duration: 0 }, bottom: { duration: 0 },
        borderRadius: { duration: 0 }, boxShadow: { duration: 0 },
      }
    : {
        top: springs.gentle, left: springs.gentle, right: springs.gentle, bottom: springs.gentle,
        borderRadius: springs.gentle, boxShadow: springs.gentle,
        x: springs.default, y: springs.default, scale: springs.default,
        opacity: { duration: 0.2 },
      }

  return (
    <motion.div
      ref={setWindowNode}
      className={`edge-app-window${compactWindow ? ' edge-app-window-compact' : ''}`}
      initial={pref.reduced || compactWindow ? { opacity: 0 } : { opacity: 0, scale: 0.96 }}
      animate={activeTarget}
      exit={pref.reduced || compactWindow ? { opacity: 0 } : { opacity: 0, scale: 0.97, x: 0, y: 0 }}
      transition={transition}
      onAnimationComplete={() => {
        if (visible) window.dispatchEvent(new Event('resize'))
      }}
      style={{
        position: 'absolute', ...frame, background: T.windowBg,
        display: 'flex', flexDirection: 'column', overflow: 'hidden',
        zIndex: active ? 21 : 20, pointerEvents: visible ? 'auto' : 'none',
      }}
    >
      <div className="edge-window-titlebar" style={{
        minHeight: 44, display: 'flex', alignItems: 'center',
        paddingLeft: 14, paddingRight: 8, gap: 10,
        background: T.titleBar, borderBottom: `1px solid ${T.borderSoft}`,
        flexShrink: 0,
      }}>
        {compactWindow && trafficBtn('返回桌面', T.ink2, showDesktop, <Icon name="chevDown" size={16} stroke={2}/>, 'edge-btn-secondary', 'edge-window-back')}
        <div style={{
          width: 22, height: 22, borderRadius: 6, color: 'white', flexShrink: 0,
          background: app.bg, display: 'flex', alignItems: 'center', justifyContent: 'center',
        }}><Icon name={app.icon} size={13} stroke={1.8}/></div>
        <div className="edge-window-title" style={{ ...T.type.body, fontWeight: 600, color: T.ink }}>{app.name}</div>
        {breadcrumb && <div className="edge-window-breadcrumb" style={{ display: 'flex', alignItems: 'center', gap: 6, color: T.ink3, fontSize: 12.5 }}>
          <Icon name="chevRight" size={12} stroke={2}/>{breadcrumb}
        </div>}
        <div style={{ flex: 1 }}/>
        <div className="edge-window-header-actions">{headerActions}</div>
        {canManage && <button
          type="button"
          onClick={onToggleMgmt}
          title="应用运维信息"
          className="edge-window-manage edge-press edge-btn-secondary"
          style={{
            display: 'flex', alignItems: 'center', gap: 5,
            minHeight: 44, padding: '0 10px', borderRadius: 6, appearance: 'none',
            background: mgmtOpen ? T.blueSoft : undefined,
            border: `1px solid ${mgmtOpen ? '#bfdbfe' : 'transparent'}`,
            color: mgmtOpen ? T.blueDeep : T.ink3,
            cursor: 'pointer', fontSize: 11.5, fontWeight: 600, marginRight: 4,
          }}
        ><Icon name="info" size={12} stroke={2}/><span>运维信息</span></button>}
        <div className="edge-window-controls" style={{ display: 'flex', alignItems: 'center', gap: 2, marginLeft: 4 }}>
          {!compactWindow && trafficBtn('最小化', T.ink3, onMinimize, <Icon name="minus" size={14} stroke={2}/>)}
          {!compactWindow && trafficBtn(maximized ? '还原窗口' : '最大化', T.ink3, onMaximize, <Icon name={maximized ? 'restore' : 'maximize'} size={12} stroke={2}/>)}
          {trafficBtn('关闭', T.red, onClose, <Icon name="x" size={14} stroke={2}/>, 'edge-btn-danger')}
        </div>
      </div>
      <div className="edge-window-body" style={{ flex: 1, minHeight: 0, overflow: 'hidden', display: 'flex', position: 'relative' }}>
        {children}
      </div>
    </motion.div>
  )
}
