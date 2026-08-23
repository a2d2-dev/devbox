import { useState } from 'react'
import { createPortal } from 'react-dom'
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
const WINDOW_SHADOW_DIM = '0 10px 28px -14px rgba(15,23,42,0.20), 0 0 0 1px rgba(15,23,42,0.05)';
const WINDOW_SHADOW_CLEAR = '0 24px 60px -12px rgba(15,23,42,0), 0 0 0 1px rgba(15,23,42,0)';

// 窗口最小可缩放尺寸（与 App.jsx 的 clampGeo 保持一致）
const MIN_W = 420, MIN_H = 300;

// 8 向 resize handle：贴窗口边缘/角，cursor 按方向
//   边 handle 留出 10px 给角；角 handle 12×12
const HANDLES = [
  { dir: 'n',  css: { top: 0, left: 10, right: 10, height: 6 }, cursor: 'ns-resize' },
  { dir: 's',  css: { bottom: 0, left: 10, right: 10, height: 6 }, cursor: 'ns-resize' },
  { dir: 'e',  css: { right: 0, top: 10, bottom: 10, width: 6 }, cursor: 'ew-resize' },
  { dir: 'w',  css: { left: 0, top: 10, bottom: 10, width: 6 }, cursor: 'ew-resize' },
  { dir: 'ne', css: { top: 0, right: 0, width: 12, height: 12 }, cursor: 'nesw-resize' },
  { dir: 'nw', css: { top: 0, left: 0, width: 12, height: 12 }, cursor: 'nwse-resize' },
  { dir: 'se', css: { bottom: 0, right: 0, width: 12, height: 12 }, cursor: 'nwse-resize' },
  { dir: 'sw', css: { bottom: 0, left: 0, width: 12, height: 12 }, cursor: 'nesw-resize' },
];

// 最小化飞回 Dock 图标的 transform 偏移（genie 效果）
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

export default function AppWindow({
  app, active = false, visible = true, minimized = false, maximized,
  geo, zIndex = 10, interacting,
  getDockIconRect, onMaximize, onMinimize, onClose, onBringToFront,
  onChangeGeo, onInteractStart, onInteractEnd,
  children, headerActions, breadcrumb, mgmtOpen, onToggleMgmt, canManage,
}) {
  const pref = useMotionPref();
  const [windowNode, setWindowNode] = useState(null);
  const [overlayCursor, setOverlayCursor] = useState('default');

  // 防御：geo 万一没传，给个兜底（父级已 fallback，这里双保险）
  const g = geo || { x: 40, y: 40, w: 820, h: 560 };

  // traffic 按钮：onPointerDown stop 防冒泡到标题栏触发拖拽
  const trafficBtn = (color, onClick, icon, className = 'edge-btn-secondary') => (
    <div onClick={onClick} onPointerDown={(e) => e.stopPropagation()} className={`edge-press ${className}`} style={{
      width: 22, height: 22, borderRadius: 6, cursor: 'pointer',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      color, transition: 'background 0.15s, color 0.15s',
    }}>
      {icon}
    </div>
  );

  // minimize genie：transform x/y 偏移到 dock 图标 + 缩小
  // 注意：拖拽改的是 left/top，最小化改的是 transform x/y —— 两个 CSS 通道互不干扰
  const minimizeOffset = minimized
    ? dockOffset(app.id, windowNode, getDockIconRect)
    : { x: 0, y: 0 };

  const activeTarget = {
    left: g.x,
    top: g.y,
    width: g.w,
    height: g.h,
    opacity: visible ? 1 : 0,
    visibility: 'visible',
    transitionEnd: visible ? { visibility: 'visible' } : { visibility: 'hidden' },
    ...(pref.reduced ? {} : {
      x: minimized ? minimizeOffset.x : 0,
      y: minimized ? minimizeOffset.y : 0,
      scale: visible ? 1 : minimized ? 0.35 : 0.98,
    }),
  };

  // 拖拽/缩放期间：几何走 duration:0（精确跟手）；否则走 spring（maximize/restore 丝滑过渡）
  const geoTransition = interacting
    ? { left: { duration: 0 }, top: { duration: 0 }, width: { duration: 0 }, height: { duration: 0 } }
    : { left: springs.gentle, top: springs.gentle, width: springs.gentle, height: springs.gentle };
  const transition = pref.reduced
    ? {
        opacity: pref.fadeTransition,
        left: { duration: 0 }, top: { duration: 0 },
        width: { duration: 0 }, height: { duration: 0 },
        borderRadius: { duration: 0 }, boxShadow: { duration: 0 },
      }
    : {
        ...geoTransition,
        x: springs.default, y: springs.default, scale: springs.default,
        opacity: { duration: 0.2 },
      };

  // ─── 拖拽（标题栏） ─── 直接改 geo.x/y（→ left/top）
  const startDrag = (e) => {
    if (maximized || e.button !== 0) return;
    onBringToFront?.();
    onInteractStart?.();
    setOverlayCursor('grabbing');
    const sx = e.clientX, sy = e.clientY;
    const orig = { x: g.x, y: g.y };
    const onMove = (ev) => {
      onChangeGeo?.({ x: orig.x + (ev.clientX - sx), y: orig.y + (ev.clientY - sy) });
    };
    const onUp = () => {
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
      setOverlayCursor('default');
      onInteractEnd?.();
      window.dispatchEvent(new Event('resize'));
    };
    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
  };

  // ─── 缩放（8 向） ─── 左/上方向同步移动 x/y；尺寸触底后位置不再变（更接近原生 WM）
  const startResize = (dir, cursor) => (e) => {
    if (maximized || e.button !== 0) return;
    onBringToFront?.();
    onInteractStart?.();
    setOverlayCursor(cursor);
    const sx = e.clientX, sy = e.clientY;
    const orig = { x: g.x, y: g.y, w: g.w, h: g.h };
    const onMove = (ev) => {
      const dx = ev.clientX - sx, dy = ev.clientY - sy;
      const next = { x: orig.x, y: orig.y, w: orig.w, h: orig.h };
      if (dir.includes('e')) next.w = Math.max(MIN_W, orig.w + dx);
      if (dir.includes('s')) next.h = Math.max(MIN_H, orig.h + dy);
      if (dir.includes('w')) {
        const nw = Math.max(MIN_W, orig.w - dx);
        next.x = orig.x + (orig.w - nw);
        next.w = nw;
      }
      if (dir.includes('n')) {
        const nh = Math.max(MIN_H, orig.h - dy);
        next.y = orig.y + (orig.h - nh);
        next.h = nh;
      }
      onChangeGeo?.(next);
    };
    const onUp = () => {
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
      setOverlayCursor('default');
      onInteractEnd?.();
      window.dispatchEvent(new Event('resize'));
    };
    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
  };

  return (
    <motion.div ref={setWindowNode}
      initial={pref.reduced ? { opacity: 0 } : { opacity: 0, scale: 0.96 }}
      animate={activeTarget}
      exit={pref.reduced ? { opacity: 0 } : { opacity: 0, scale: 0.97, x: 0, y: 0 }}
      transition={transition}
      onPointerDown={() => onBringToFront?.()}
      onAnimationComplete={() => { if (visible) window.dispatchEvent(new Event('resize')); }}
      style={{
        position: 'absolute',
        left: g.x, top: g.y, width: g.w, height: g.h,
        borderRadius: maximized ? 0 : 14,
        boxShadow: maximized ? WINDOW_SHADOW_CLEAR : (active ? WINDOW_SHADOW : WINDOW_SHADOW_DIM),
        background: T.windowBg,
        display: 'flex', flexDirection: 'column', overflow: 'hidden',
        zIndex,
        pointerEvents: visible ? 'auto' : 'none',
      }}>

      {/* 拖拽/缩放期间的全屏覆盖层：屏蔽 iframe 吞掉 pointermove（Browser 等应用拖拽关键）。
          portal 到 body —— 脱离本窗口的 transform 上下文，fixed 才能真正覆盖整个视口。 */}
      {interacting && createPortal(
        <div style={{ position: 'fixed', inset: 0, zIndex: 9998, cursor: overlayCursor }} />,
        document.body
      )}

      {/* 标题栏：拖拽 + 双击最大化 */}
      <div
        onPointerDown={startDrag}
        onDoubleClick={onMaximize}
        style={{
          height: 44, display: 'flex', alignItems: 'center',
          paddingLeft: 14, paddingRight: 8, gap: 10,
          background: T.titleBar, borderBottom: `1px solid ${T.borderSoft}`,
          flexShrink: 0, touchAction: 'none', userSelect: 'none',
          cursor: maximized ? 'default' : 'grab',
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
          <button onPointerDown={(e) => e.stopPropagation()} onClick={onToggleMgmt} title="应用运维信息" className="edge-press edge-btn-secondary" style={{
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

      {/* 8 向 resize handle —— 最大化时隐藏 */}
      {!maximized && HANDLES.map(h => (
        <div key={h.dir} onPointerDown={startResize(h.dir, h.cursor)} style={{
          position: 'absolute', zIndex: 50, cursor: h.cursor, ...h.css,
        }}/>
      ))}

      {/* Body */}
      <div style={{ flex: 1, overflow: 'hidden', display: 'flex', position: 'relative' }}>
        {children}
      </div>
    </motion.div>
  );
}
