import { useState, useEffect, useRef } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'

export const btnSecondary = {
  display: 'inline-flex', alignItems: 'center', gap: 6,
  height: 32, padding: '0 12px', borderRadius: 7,
  background: T.surface, border: `1px solid ${T.border}`,
  fontSize: 12.5, fontWeight: 500, color: T.ink2,
  cursor: 'pointer',
};
export const btnPrimary = {
  ...btnSecondary,
  background: T.blue, color: 'white', border: `1px solid ${T.blueDeep}`,
  fontWeight: 600,
};
export const btnDanger = {
  ...btnSecondary,
  background: 'white', color: T.red, border: '1px solid #fecaca',
};

export default function AppWindow({ app, maximized, onMaximize, onMinimize, onClose, children, headerActions, breadcrumb, mgmtOpen, onToggleMgmt, canManage }) {
  const trafficBtn = (color, hover, onClick, icon) => (
    <div onClick={onClick} style={{
      width: 22, height: 22, borderRadius: 6, cursor: 'pointer',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      color: T.ink3, transition: 'background 0.15s',
    }}
    onMouseEnter={(e) => { e.currentTarget.style.background = hover; e.currentTarget.style.color = color; }}
    onMouseLeave={(e) => { e.currentTarget.style.background = 'transparent'; e.currentTarget.style.color = T.ink3; }}>
      {icon}
    </div>
  );

  return (
    <div className="edge-fade-in" style={{
      position: 'absolute',
      ...(maximized
        ? { top: 0, left: 0, right: 0, bottom: 0, borderRadius: 0 }
        : { top: '5.5%', left: '7.5%', right: '7.5%', bottom: '12%', borderRadius: 14 }),
      background: T.windowBg,
      boxShadow: maximized ? 'none' : '0 24px 60px -12px rgba(15,23,42,0.32), 0 0 0 1px rgba(15,23,42,0.08)',
      display: 'flex', flexDirection: 'column', overflow: 'hidden',
      zIndex: 20,
    }}>
      {/* Title bar */}
      <div style={{
        height: 44, display: 'flex', alignItems: 'center',
        paddingLeft: 14, paddingRight: 8, gap: 10,
        background: T.titleBar, borderBottom: `1px solid ${T.borderSoft}`,
        flexShrink: 0,
      }}>
        <div style={{
          width: 22, height: 22, borderRadius: 6, color: 'white',
          background: app.bg, display: 'flex', alignItems: 'center', justifyContent: 'center',
        }}>
          <Icon name={app.icon} size={13} stroke={1.8}/>
        </div>
        <div style={{ fontSize: 13, fontWeight: 600, color: T.ink }}>{app.name}</div>
        {breadcrumb && (
          <div style={{ display: 'flex', alignItems: 'center', gap: 6, color: T.ink3, fontSize: 12.5 }}>
            <Icon name="chevRight" size={12} stroke={2}/>
            {breadcrumb}
          </div>
        )}
        <div style={{ flex: 1 }}/>
        {headerActions}
        {canManage && (
          <button onClick={onToggleMgmt} title="应用运维信息" style={{
            display: 'flex', alignItems: 'center', gap: 5,
            height: 26, padding: '0 10px', borderRadius: 6,
            background: mgmtOpen ? T.blueSoft : 'transparent',
            border: `1px solid ${mgmtOpen ? '#bfdbfe' : 'transparent'}`,
            color: mgmtOpen ? T.blueDeep : T.ink3,
            cursor: 'pointer', fontSize: 11.5, fontWeight: 600,
            marginRight: 4,
          }}
          onMouseEnter={(e) => !mgmtOpen && (e.currentTarget.style.background = T.surfaceAlt)}
          onMouseLeave={(e) => !mgmtOpen && (e.currentTarget.style.background = 'transparent')}>
            <Icon name="info" size={12} stroke={2}/>
            运维信息
          </button>
        )}
        <div style={{ display: 'flex', alignItems: 'center', gap: 2, marginLeft: 4 }}>
          {trafficBtn(T.ink, '#f1f5f9', onMinimize, <Icon name="minus" size={14} stroke={2}/>)}
          {trafficBtn(T.ink, '#f1f5f9', onMaximize, <Icon name={maximized ? 'restore' : 'maximize'} size={12} stroke={2}/>)}
          {trafficBtn(T.red, '#fee2e2', onClose, <Icon name="x" size={14} stroke={2}/>)}
        </div>
      </div>

      {/* Body */}
      <div style={{ flex: 1, overflow: 'hidden', display: 'flex', position: 'relative' }}>
        {children}
      </div>
    </div>
  );
}
