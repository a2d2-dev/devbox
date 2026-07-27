import { useEffect, useState } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { motion, AnimatePresence, PopScale, springs, useMotionPref } from '../motion'
import { useViewportEnvironment } from '../hooks/useViewportEnvironment'

function DockTooltip({ label }) {
  const pref = useMotionPref()
  return (
    <div style={{
      position: 'absolute', bottom: 'calc(100% + 10px)', left: '50%',
      transform: 'translateX(-50%)', pointerEvents: 'none',
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
  )
}

const dockButtonStyle = {
  position: 'relative', width: 44, height: 44, flex: '0 0 44px',
  borderRadius: 12, border: 'none', padding: 0,
  display: 'flex', alignItems: 'center', justifyContent: 'center',
  cursor: 'pointer', font: 'inherit',
}

export function Dock({ apps, registerDockIconRect, onShowDesktop, onFocusApp, onCloseApp, anyVisible, hidden = false,
                authed, authBadge, onToggleAuth, alertBadge, loginUser, onLogout }) {
  const [hoverId, setHoverId] = useState(null)
  const pref = useMotionPref()
  const { compactWindow, touchEnvironment } = useViewportEnvironment()
  const effectiveHidden = hidden && !compactWindow
  const dockMotion = pref.reduced || touchEnvironment ? {} : {
    whileHover: { y: -4, scale: 1.1 },
    whileTap: { scale: 0.95 },
  }
  const desktopActive = !anyVisible

  useEffect(() => {
    if (!effectiveHidden) return undefined
    const frame = requestAnimationFrame(() => setHoverId(null))
    return () => cancelAnimationFrame(frame)
  }, [effectiveHidden])

  return (
    <div
      className="edge-dock-shell"
      aria-hidden={effectiveHidden}
      inert={effectiveHidden ? true : undefined}
      style={{
        position: 'absolute', bottom: 18, left: '50%', transform: 'translateX(-50%)',
        zIndex: 25, opacity: effectiveHidden ? 0 : 1,
        visibility: effectiveHidden ? 'hidden' : 'visible',
        pointerEvents: effectiveHidden ? 'none' : 'auto',
      }}
    >
      <div className="edge-material-chrome edge-dock-scroll" style={{
        display: 'flex', alignItems: 'center', gap: 6,
        padding: '8px 12px', borderRadius: 20,
        border: '1px solid rgba(255,255,255,0.9)',
        boxShadow: '0 12px 32px -8px rgba(15,23,42,0.18), 0 0 0 1px rgba(15,23,42,0.04)',
        transition: 'border-color 0.15s ease, box-shadow 0.15s ease',
      }}>
        <motion.button
          type="button"
          onClick={onShowDesktop}
          onMouseEnter={() => !touchEnvironment && setHoverId('__home')}
          onMouseLeave={() => setHoverId(null)}
          aria-label="显示桌面"
          title="显示桌面"
          transition={pref.spring('snappy')}
          {...dockMotion}
          style={{
            ...dockButtonStyle,
            background: desktopActive ? '#1f2937' : 'rgba(15,23,42,0.04)',
            color: desktopActive ? 'white' : T.ink2,
            boxShadow: desktopActive ? '0 4px 10px -2px rgba(31,41,55,0.45)' : 'none',
          }}
        >
          <Icon name="home" size={20} stroke={1.7}/>
          {desktopActive && <span style={{
            position: 'absolute', bottom: -6, left: '50%', transform: 'translateX(-50%)',
            width: 5, height: 5, borderRadius: '50%', background: T.ink2,
          }}/>}
          <AnimatePresence>
            {!touchEnvironment && hoverId === '__home' && <DockTooltip label="显示桌面"/>}
          </AnimatePresence>
        </motion.button>

        <div aria-hidden="true" style={{ width: 1, height: 28, flex: '0 0 1px', background: 'rgba(15,23,42,0.08)', margin: '0 4px' }}/>

        {apps.length === 0 ? (
          <div style={{
            height: 44, padding: '0 14px', whiteSpace: 'nowrap',
            display: 'flex', alignItems: 'center', gap: 8,
            color: T.ink3, fontSize: 12,
          }}>
            <Icon name="apps" size={14} stroke={1.7}/>
            <span>从桌面单击应用以启动</span>
          </div>
        ) : apps.map(app => {
          const stateColor = {
            running: T.green, error: T.red, warn: T.amber, stopped: T.ink4,
          }[app.state] || null
          const isError = app.state === 'error'
          const hovered = hoverId === app.id
          const rememberRect = (node) => registerDockIconRect?.(app.id, node)
          return (
            <div
              key={app.id}
              className="edge-dock-app"
              onMouseEnter={() => !touchEnvironment && setHoverId(app.id)}
              onMouseLeave={() => setHoverId(null)}
              style={{ position: 'relative', display: 'flex', alignItems: 'center', gap: 2, flexShrink: 0 }}
            >
              <motion.button
                type="button"
                ref={rememberRect}
                onClick={(event) => { rememberRect(event.currentTarget); onFocusApp(app.id) }}
                onMouseEnter={(event) => rememberRect(event.currentTarget)}
                aria-label={`${app.name}${app.isMinimized ? '，已最小化' : ''}`}
                title={app.name}
                transition={pref.spring('snappy')}
                {...dockMotion}
                style={{
                  ...dockButtonStyle,
                  background: app.isActive ? app.bg : 'rgba(15,23,42,0.04)',
                  color: app.isActive ? 'white' : T.ink2,
                  boxShadow: app.isActive
                    ? `0 4px 10px -2px ${app.color}55, inset 0 1px 0 rgba(255,255,255,0.3)`
                    : 'none',
                  opacity: app.isMinimized ? 0.92 : 1,
                }}
              >
                {!app.isActive && <span style={{
                  position: 'absolute', inset: 6, borderRadius: 8,
                  background: app.bg, color: 'white',
                  display: 'flex', alignItems: 'center', justifyContent: 'center',
                  boxShadow: '0 1px 2px rgba(15,23,42,0.12), inset 0 1px 0 rgba(255,255,255,0.25)',
                }}><Icon name={app.icon} size={18} stroke={1.7}/></span>}
                {app.isActive && <Icon name={app.icon} size={20} stroke={1.7}/>}

                {app.kind === 'app' && stateColor && <span style={{
                  position: 'absolute', right: 1, top: 1,
                  width: 10, height: 10, borderRadius: '50%', background: 'white',
                  display: 'flex', alignItems: 'center', justifyContent: 'center',
                  boxShadow: '0 1px 2px rgba(15,23,42,0.18)',
                }}><span className={isError ? 'edge-live-dot' : ''}
                  style={{ width: 6, height: 6, borderRadius: '50%', background: stateColor }}/></span>}

                {app.id === 'alerts' && alertBadge > 0 && <span style={{
                  position: 'absolute', top: 0, right: 0,
                  minWidth: 18, height: 18, padding: '0 5px', borderRadius: 9,
                  background: T.red, color: 'white', fontSize: 10.5, fontWeight: 700,
                  display: 'flex', alignItems: 'center', justifyContent: 'center',
                  boxShadow: '0 0 0 2px white', transform: 'translate(25%, -25%)',
                }}>{alertBadge}</span>}

                <span aria-hidden="true" style={{
                  position: 'absolute', bottom: -6, left: '50%', transform: 'translateX(-50%)',
                  width: app.isActive ? 5 : 4, height: app.isActive ? 5 : 4,
                  borderRadius: '50%', background: app.isActive ? T.ink2 : T.ink3,
                  opacity: app.isMinimized ? 0.6 : 1,
                }}/>
                <AnimatePresence>
                  {!touchEnvironment && hovered && <DockTooltip label={`${app.name}${app.isMinimized ? ' · 已最小化' : ''}`}/>}
                </AnimatePresence>
              </motion.button>
              {!touchEnvironment && hovered && <button
                type="button"
                onClick={() => onCloseApp(app.id)}
                title="退出应用"
                aria-label={`退出 ${app.name}`}
                style={{
                  position: 'absolute', top: -5, left: -5,
                  width: 18, height: 18, borderRadius: '50%',
                  background: '#0f172a', color: 'white', border: 'none',
                  display: 'flex', alignItems: 'center', justifyContent: 'center',
                  cursor: 'pointer', boxShadow: '0 1px 4px rgba(15,23,42,0.3), 0 0 0 2px white',
                  padding: 0, zIndex: 2,
                }}
              ><Icon name="x" size={11} stroke={2.4}/></button>}
              {touchEnvironment && <button
                type="button"
                className="edge-dock-close"
                onClick={() => onCloseApp(app.id)}
                aria-label={`关闭 ${app.name}`}
                title={`关闭 ${app.name}`}
              ><Icon name="x" size={14} stroke={2.2}/></button>}
            </div>
          )
        })}
      </div>
    </div>
  )
}
