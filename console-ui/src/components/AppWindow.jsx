import { useState } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { motion, springs, useMotionPref } from '../motion'

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

const WINDOW_SHADOW = '0 24px 60px -12px rgba(15,23,42,0.32), 0 0 0 1px rgba(15,23,42,0.08)';
const WINDOW_SHADOW_CLEAR = '0 24px 60px -12px rgba(15,23,42,0), 0 0 0 1px rgba(15,23,42,0)';

function windowFrame(maximized) {
  return maximized
    ? { top: '0%', left: '0%', right: '0%', bottom: '0%', borderRadius: 0, boxShadow: WINDOW_SHADOW_CLEAR }
    : { top: '5.5%', left: '7.5%', right: '7.5%', bottom: '12%', borderRadius: 14, boxShadow: WINDOW_SHADOW };
}

function dockOffset(appId, windowNode, getDockIconRect) {
  if (!windowNode) return { x: 0, y: 0 };

  const dockRect = getDockIconRect?.(appId);
  const offsetParentRect = windowNode.offsetParent?.getBoundingClientRect?.();
  const sourceX = (offsetParentRect?.left || 0) + windowNode.offsetLeft + windowNode.offsetWidth / 2;
  const sourceY = (offsetParentRect?.top || 0) + windowNode.offsetTop + windowNode.offsetHeight / 2;
  const targetX = dockRect
    ? dockRect.left + dockRect.width / 2
    : window.innerWidth / 2;
  const targetY = dockRect
    ? dockRect.top + dockRect.height / 2
    : window.innerHeight - 42;

  return {
    x: targetX - sourceX,
    y: targetY - sourceY,
  };
}

export default function AppWindow({ app, active = false, visible = true, minimized = false, maximized, getDockIconRect, onMaximize, onMinimize, onClose, children, headerActions, breadcrumb, mgmtOpen, onToggleMgmt, canManage }) {
  const pref = useMotionPref();
  const [windowNode, setWindowNode] = useState(null);

  const trafficBtn = (color, onClick, icon, className = 'edge-btn-secondary') => (
    <div onClick={onClick} className={`edge-press ${className}`} style={{
      width: 22, height: 22, borderRadius: 6, cursor: 'pointer',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      color, transition: 'background 0.15s, color 0.15s',
    }}>
      {icon}
    </div>
  );

  const frame = windowFrame(maximized);
  const minimizeOffset = minimized
    ? dockOffset(app.id, windowNode, getDockIconRect)
    : { x: 0, y: 0 };
  const activeTarget = {
    ...frame,
    opacity: visible ? 1 : 0,
    visibility: 'visible',
    transitionEnd: visible ? { visibility: 'visible' } : { visibility: 'hidden' },
    ...(pref.reduced ? {} : {
      x: minimized ? minimizeOffset.x : 0,
      y: minimized ? minimizeOffset.y : 0,
      scale: visible ? 1 : minimized ? 0.35 : 0.98,
    }),
  };

  const transition = pref.reduced
    ? {
        opacity: pref.fadeTransition,
        top: { duration: 0 },
        left: { duration: 0 },
        right: { duration: 0 },
        bottom: { duration: 0 },
        borderRadius: { duration: 0 },
        boxShadow: { duration: 0 },
      }
    : {
        top: springs.gentle,
        left: springs.gentle,
        right: springs.gentle,
        bottom: springs.gentle,
        borderRadius: springs.gentle,
        boxShadow: springs.gentle,
        x: springs.default,
        y: springs.default,
        scale: springs.default,
        opacity: { duration: 0.2 },
      };

  return (
    <motion.div ref={setWindowNode} initial={pref.reduced ? { opacity: 0 } : { opacity: 0, scale: 0.96 }}
      animate={activeTarget}
      exit={pref.reduced ? { opacity: 0 } : { opacity: 0, scale: 0.97, x: 0, y: 0 }}
      transition={transition}
      onAnimationComplete={() => {
        if (visible) window.dispatchEvent(new Event('resize'));
      }}
      style={{
      position: 'absolute',
      ...frame,
      background: T.windowBg,
      display: 'flex', flexDirection: 'column', overflow: 'hidden',
      zIndex: active ? 21 : 20,
      pointerEvents: visible ? 'auto' : 'none',
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
        <div style={{ ...T.type.body, fontWeight: 600, color: T.ink }}>{app.name}</div>
        {breadcrumb && (
          <div style={{ display: 'flex', alignItems: 'center', gap: 6, color: T.ink3, fontSize: 12.5 }}>
            <Icon name="chevRight" size={12} stroke={2}/>
            {breadcrumb}
          </div>
        )}
        <div style={{ flex: 1 }}/>
        {headerActions}
        {canManage && (
          <button onClick={onToggleMgmt} title="应用运维信息" className="edge-press edge-btn-secondary" style={{
            display: 'flex', alignItems: 'center', gap: 5,
            height: 26, padding: '0 10px', borderRadius: 6,
            appearance: 'none',
            background: mgmtOpen ? T.blueSoft : undefined,
            border: `1px solid ${mgmtOpen ? '#bfdbfe' : 'transparent'}`,
            color: mgmtOpen ? T.blueDeep : T.ink3,
            cursor: 'pointer', fontSize: 11.5, fontWeight: 600,
            marginRight: 4,
          }}>
            <Icon name="info" size={12} stroke={2}/>
            运维信息
          </button>
        )}
        <div style={{ display: 'flex', alignItems: 'center', gap: 2, marginLeft: 4 }}>
          {trafficBtn(T.ink3, onMinimize, <Icon name="minus" size={14} stroke={2}/>)}
          {trafficBtn(T.ink3, onMaximize, <Icon name={maximized ? 'restore' : 'maximize'} size={12} stroke={2}/>)}
          {trafficBtn(T.red, onClose, <Icon name="x" size={14} stroke={2}/>, 'edge-btn-danger')}
        </div>
      </div>

      {/* Body */}
      <div style={{ flex: 1, overflow: 'hidden', display: 'flex', position: 'relative' }}>
        {children}
      </div>
    </motion.div>
  );
}
