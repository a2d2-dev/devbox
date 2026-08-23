import { useEffect, useState } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { motion, AnimatePresence, PopScale, springs, useMotionPref } from '../motion'

function DockTooltip({ label }) {
  const pref = useMotionPref()
  return (
    <div style={{
      position: 'absolute', bottom: 'calc(100% + 10px)', left: '50%',
      transform: 'translateX(-50%)',
      pointerEvents: 'none',
    }}>
      <motion.div
        initial={pref.reduced ? { opacity: 0 } : { opacity: 0, y: 4 }}
        animate={pref.reduced ? { opacity: 1 } : { opacity: 1, y: 0 }}
        exit={pref.reduced ? { opacity: 0 } : { opacity: 0, y: 4 }}
        transition={pref.reduced ? pref.fadeTransition : springs.snappy}
      >
        <PopScale origin="bottom center" style={{
          background: 'rgba(15,23,42,0.92)', color: 'white',
          fontSize: 11.5, fontWeight: 500,
          padding: '4px 9px', borderRadius: 6, whiteSpace: 'nowrap',
          boxShadow: '0 4px 12px -2px rgba(15,23,42,0.3)',
        }}>{label}</PopScale>
      </motion.div>
    </div>
  );
}

export function Dock({ apps, registerDockIconRect, onShowDesktop, onFocusApp, onCloseApp, anyVisible, hidden = false,
                authed, authBadge, onToggleAuth, alertBadge, loginUser, onLogout }) {
  const [hoverId, setHoverId] = useState(null);
  const pref = useMotionPref();
  const dockMotion = pref.reduced ? {} : {
    whileHover: { y: -4, scale: 1.1 },
    whileTap: { scale: 0.95 },
  };

  // "Show desktop" is the home button — it lives on the left,
  // and is shown as "active" only when no window is visible.
  const desktopActive = !anyVisible;

  useEffect(() => {
    if (!hidden) return undefined;
    const frame = requestAnimationFrame(() => setHoverId(null));
    return () => cancelAnimationFrame(frame);
  }, [hidden]);

  return (
    <div
      className={`desktop-dock${apps.length === 0 ? ' desktop-dock-empty' : ''}`}
      aria-hidden={hidden}
      inert={hidden ? true : undefined}
      style={{
      position: 'absolute', bottom: 18, left: '50%', transform: 'translateX(-50%)',
      zIndex: 25,
      opacity: hidden ? 0 : 1,
      visibility: hidden ? 'hidden' : 'visible',
      pointerEvents: hidden ? 'none' : 'auto',
    }}>
      <div className="edge-material-chrome"
        style={{
          display: 'flex', alignItems: 'center', gap: 6,
          padding: '8px 12px', borderRadius: 20,
          border: '1px solid rgba(255,255,255,0.9)',
          boxShadow: '0 12px 32px -8px rgba(15,23,42,0.18), 0 0 0 1px rgba(15,23,42,0.04)',
          transition: 'border-color 0.15s ease, box-shadow 0.15s ease',
        }}>
        {/* Show-desktop button */}
        <motion.div onClick={onShowDesktop}
             onMouseEnter={() => setHoverId('__home')}
             onMouseLeave={() => setHoverId(null)}
             title="显示桌面"
             transition={pref.spring('snappy')}
             {...dockMotion}
             style={{
               position: 'relative', width: 44, height: 44, borderRadius: 12,
               background: desktopActive ? '#1f2937' : 'rgba(15,23,42,0.04)',
               color: desktopActive ? 'white' : T.ink2,
               display: 'flex', alignItems: 'center', justifyContent: 'center',
               cursor: 'pointer',
               boxShadow: desktopActive ? '0 4px 10px -2px rgba(31,41,55,0.45)' : 'none',
             }}>
          <Icon name="home" size={20} stroke={1.7}/>
          {desktopActive && (
            <div style={{
              position: 'absolute', bottom: -6, left: '50%', transform: 'translateX(-50%)',
              width: 5, height: 5, borderRadius: '50%', background: T.ink2,
            }}/>
          )}
          <AnimatePresence>
            {hoverId === '__home' && (
              <DockTooltip label="显示桌面"/>
            )}
          </AnimatePresence>
        </motion.div>

        {/* Divider between system controls and running apps */}
        <div style={{ width: 1, height: 28, background: 'rgba(15,23,42,0.08)', margin: '0 4px' }}/>

        {/* Running apps — dynamic */}
        {apps.length === 0 ? (
          <div style={{
            height: 44, padding: '0 14px',
            display: 'flex', alignItems: 'center', gap: 8,
            color: T.ink3, fontSize: 12,
          }}>
            <Icon name="apps" size={14} stroke={1.7}/>
            <span>从桌面单击应用以启动</span>
          </div>
        ) : (
          apps.map(app => {
            const stateColor = {
              running: T.green, error: T.red, warn: T.amber, stopped: T.ink4,
            }[app.state] || null;
            const isError = app.state === 'error';
            const hovered = hoverId === app.id;
            const rememberRect = (node) => registerDockIconRect?.(app.id, node);
            return (
              <motion.div key={app.id}
                   ref={rememberRect}
                   onClick={(e) => { rememberRect(e.currentTarget); onFocusApp(app.id); }}
                   onMouseEnter={(e) => { rememberRect(e.currentTarget); setHoverId(app.id); }}
                   onMouseLeave={() => setHoverId(null)}
                   onContextMenu={(e) => { e.preventDefault(); onCloseApp(app.id); }}
                   title={app.name}
                   transition={pref.spring('snappy')}
                   {...dockMotion}
                   style={{
                     position: 'relative', width: 44, height: 44, borderRadius: 12,
                     background: app.isActive ? app.bg : 'rgba(15,23,42,0.04)',
                     color: app.isActive ? 'white' : T.ink2,
                     display: 'flex', alignItems: 'center', justifyContent: 'center',
                     cursor: 'pointer',
                     boxShadow: app.isActive
                       ? `0 4px 10px -2px ${app.color}55, inset 0 1px 0 rgba(255,255,255,0.3)`
                       : 'none',
                     opacity: app.isMinimized ? 0.92 : 1,
                   }}>
                {/* When inactive, show the app's own gradient as a tiny tile
                    so it still looks like *the* app, not a generic chip */}
                {!app.isActive && (
                  <div style={{
                    position: 'absolute', inset: 6, borderRadius: 8,
                    background: app.bg, color: 'white',
                    display: 'flex', alignItems: 'center', justifyContent: 'center',
                    boxShadow: '0 1px 2px rgba(15,23,42,0.12), inset 0 1px 0 rgba(255,255,255,0.25)',
                  }}>
                    <Icon name={app.icon} size={18} stroke={1.7}/>
                  </div>
                )}
                {app.isActive && (
                  <Icon name={app.icon} size={20} stroke={1.7}/>
                )}

                {/* App-state pip (running/error) on running app-kind items */}
                {app.kind === 'app' && stateColor && (
                  <div style={{
                    position: 'absolute', right: 1, top: 1,
                    width: 10, height: 10, borderRadius: '50%',
                    background: 'white',
                    display: 'flex', alignItems: 'center', justifyContent: 'center',
                    boxShadow: '0 1px 2px rgba(15,23,42,0.18)',
                  }}>
                    <span className={isError ? 'edge-live-dot' : ''}
                          style={{ width: 6, height: 6, borderRadius: '50%', background: stateColor }}/>
                  </div>
                )}

                {/* Alert center badge */}
                {app.id === 'alerts' && alertBadge > 0 && (
                  <div style={{
                    position: 'absolute', top: 0, right: 0,
                    minWidth: 18, height: 18, padding: '0 5px', borderRadius: 9,
                    background: T.red, color: 'white', fontSize: 10.5, fontWeight: 700,
                    display: 'flex', alignItems: 'center', justifyContent: 'center',
                    boxShadow: '0 0 0 2px white', transform: 'translate(25%, -25%)',
                  }}>{alertBadge}</div>
                )}

                {/* Hover-close (x) button — Mac-like close hint */}
                {hovered && (
                  <button onClick={(e) => { e.stopPropagation(); onCloseApp(app.id); }}
                    title="退出应用"
                    style={{
                      position: 'absolute', top: -5, left: -5,
                      width: 18, height: 18, borderRadius: '50%',
                      background: '#0f172a', color: 'white', border: 'none',
                      display: 'flex', alignItems: 'center', justifyContent: 'center',
                      cursor: 'pointer', boxShadow: '0 1px 4px rgba(15,23,42,0.3), 0 0 0 2px white',
                      padding: 0, zIndex: 2,
                    }}>
                    <Icon name="x" size={11} stroke={2.4}/>
                  </button>
                )}

                {/* Running indicator dot:
                    - filled larger for the active window
                    - smaller hollow for minimized (still running) */}
                <div style={{
                  position: 'absolute', bottom: -6, left: '50%', transform: 'translateX(-50%)',
                  width: app.isActive ? 5 : 4,
                  height: app.isActive ? 5 : 4,
                  borderRadius: '50%',
                  background: app.isActive ? T.ink2 : T.ink3,
                  opacity: app.isMinimized ? 0.6 : 1,
                }}/>

                <AnimatePresence>
                  {hovered && <DockTooltip label={`${app.name}${app.isMinimized ? ' · 已最小化' : ''}`}/>}
                </AnimatePresence>
              </motion.div>
            );
          })
        )}

        {/* 用户菜单挪到 StatusBar 左上角 (Apple-style 2026-06-21 LF) */}
      </div>
    </div>
  );
}
