import { T } from '../tokens'
import { Icon } from '../icons'

export function AppIcon({ app, onOpen, size = 76, dense = false, iconStyle = 'gradient', accent = '#2563eb', labelSize = 12.5 }) {
  const isError = app.state === 'error';
  const stateColor = {
    running: T.green, error: T.red, warn: T.amber, stopped: T.ink4,
  }[app.state] || null;

  // Resolve icon presentation based on style
  // - 'gradient': original macOS-style colored gradient + white icon
  // - 'flat': solid color (last stop) + white icon, no inner highlight
  let iconBg = app.bg;
  let iconShadow = `0 4px 10px -2px ${app.color}33, 0 0 0 1px rgba(15,23,42,0.04), inset 0 1px 0 rgba(255,255,255,0.4)`;
  let highlightOverlay = true;

  if (iconStyle === 'flat') {
    iconBg = app.color;
    iconShadow = `0 0 0 1px rgba(15,23,42,0.08)`;
    highlightOverlay = false;
  }

  return (
    <div onClick={() => onOpen(app)}
         draggable={!app.locked}
         onDragStart={(e) => {
           if (app.locked) { e.preventDefault(); return; }
           e.dataTransfer.setData('application/x-edgex-app', app.id);
           e.dataTransfer.effectAllowed = 'copy';
         }}
         className="edge-row-hover"
         style={{
           display: 'flex', flexDirection: 'column', alignItems: 'center',
           gap: 8, cursor: 'pointer', userSelect: 'none',
           padding: '8px 4px', borderRadius: 14,
           background: 'transparent',
           transition: 'background 0.15s ease',
           width: size + 28,
           '--edge-row-hover-bg': 'rgba(255,255,255,0.55)',
         }}>
      <div className={`edge-icon-hover ${isError ? 'edge-pulse' : ''}`}
           style={{
             position: 'relative', width: size, height: size,
             borderRadius: iconStyle === 'flat' ? 14 : 18,
             background: iconBg, color: 'white',
             display: 'flex', alignItems: 'center', justifyContent: 'center',
             boxShadow: iconShadow,
             '--edge-icon-hover-shadow': `0 10px 22px -6px ${app.color}55, 0 0 0 1px rgba(15,23,42,0.06)${highlightOverlay ? ', inset 0 1px 0 rgba(255,255,255,0.4)' : ''}`,
           }}>
        <Icon name={app.icon} size={size * 0.46} stroke={1.6}/>

        {/* small inner highlight */}
        {highlightOverlay && (
          <div style={{
            position: 'absolute', inset: 1, borderRadius: 17, pointerEvents: 'none',
            background: 'radial-gradient(ellipse at 30% 0%, rgba(255,255,255,0.35), transparent 60%)',
          }}/>
        )}

        {/* status dot for app-kind */}
        {app.kind === 'app' && stateColor && (
          <div style={{
            position: 'absolute', right: -3, bottom: -3,
            width: 18, height: 18, borderRadius: '50%',
            background: 'white',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            boxShadow: '0 1px 3px rgba(15,23,42,0.18)',
          }}>
            <span style={{
              width: 10, height: 10, borderRadius: '50%', background: stateColor,
              boxShadow: isError ? `0 0 0 0 ${stateColor}` : 'none',
            }} className={isError ? 'edge-live-dot' : ''}/>
          </div>
        )}

        {/* badge (numeric) */}
        {app.badge && (
          <div style={{
            position: 'absolute', top: -5, right: -5, minWidth: 20, height: 20,
            padding: '0 6px', borderRadius: 10,
            background: T.red, color: 'white', fontSize: 11.5, fontWeight: 700,
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            boxShadow: '0 2px 6px rgba(239,68,68,0.4), 0 0 0 2px white',
            fontFamily: 'inherit',
          }}>{app.badge}</div>
        )}

        {/* lock badge */}
        {app.locked && (
          <div style={{
            position: 'absolute', top: -4, right: -4,
            width: 20, height: 20, borderRadius: 6,
            background: 'white', color: T.slate,
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            boxShadow: '0 2px 4px rgba(15,23,42,0.18)',
          }}>
            <Icon name="lock" size={12} stroke={2}/>
          </div>
        )}
      </div>

      <div style={{
        fontSize: labelSize, color: T.ink, lineHeight: 1.3,
        textAlign: 'center', fontWeight: 500, maxWidth: size + 24,
        whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis',
      }}>{app.name}</div>

      {app.kind === 'app' && (
        <div style={{
          marginTop: -4, fontSize: 10.5, color: T.ink3,
          display: 'flex', alignItems: 'center', gap: 4,
        }} className="tnum">
          {app.state === 'running' && app.gpuPct > 0 && (
            <span className="mono">GPU {app.gpuPct}%</span>
          )}
          {app.state === 'running' && app.gpuPct === 0 && <span>仅 CPU</span>}
          {app.state === 'error' && <span style={{ color: T.red, fontWeight: 600 }}>异常 · {app.lastOk}</span>}
        </div>
      )}
    </div>
  );
}
