import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, useClock } from './ui'
import { AppIcon } from './AppIcon'
import { MemoWidget } from './MemoWidget'
import { SystemStatusWidget } from './SystemStatusWidget'
import { WelcomeWidget } from './WelcomeWidget'

// ─── Clock + Calendar widget ────────────────────────────────────
function ClockCalendarWidget() {
  const now = useClock();
  const hh = String(now.getHours()).padStart(2, '0');
  const mm = String(now.getMinutes()).padStart(2, '0');
  const ss = String(now.getSeconds()).padStart(2, '0');
  const yy = now.getFullYear();
  const mo = now.getMonth();
  const today = now.getDate();
  const weekday = ['星期日', '星期一', '星期二', '星期三', '星期四', '星期五', '星期六'][now.getDay()];

  // Build month grid
  const firstDay = new Date(yy, mo, 1).getDay(); // 0..6 (Sun..Sat)
  const daysInMonth = new Date(yy, mo + 1, 0).getDate();
  const cells = [];
  for (let i = 0; i < firstDay; i++) cells.push(null);
  for (let d = 1; d <= daysInMonth; d++) cells.push(d);
  while (cells.length % 7 !== 0) cells.push(null);

  return (
    <div style={{
      width: '100%', padding: 18,
      background: 'rgba(255,255,255,0.7)',
      backdropFilter: 'blur(12px)',
      WebkitBackdropFilter: 'blur(12px)',
      border: `1px solid rgba(255,255,255,0.9)`,
      borderRadius: 14,
      boxShadow: '0 6px 20px -6px rgba(15,23,42,0.12)',
    }}>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 8 }}>
        <div className="mono tnum" style={{
          fontSize: 40, fontWeight: 700, color: T.ink, letterSpacing: '-0.04em',
          lineHeight: 1,
        }}>
          {hh}:{mm}<span style={{ color: T.ink4, fontWeight: 500 }}>:{ss}</span>
        </div>
        <div style={{ flex: 1 }}/>
        <div style={{ textAlign: 'right', lineHeight: 1.25 }}>
          <div style={{ fontSize: 12, color: T.ink2, fontWeight: 600 }}>{weekday}</div>
          <div className="mono tnum" style={{ fontSize: 11, color: T.ink3, marginTop: 2 }}>
            {yy}-{String(mo + 1).padStart(2, '0')}-{String(today).padStart(2, '0')}
          </div>
        </div>
      </div>

      <div style={{
        display: 'flex', alignItems: 'center', gap: 10, marginTop: 14, marginBottom: 8,
        paddingTop: 12, borderTop: `1px solid ${T.borderSoft}`,
      }}>
        <div style={{ fontSize: 11.5, fontWeight: 600, color: T.ink, letterSpacing: '0.02em' }}>
          {yy} 年 {mo + 1} 月
        </div>
        <div style={{ flex: 1 }}/>
        <div style={{
          fontSize: 10, color: T.ink3, padding: '1px 6px', borderRadius: 3,
          background: T.surfaceAlt, border: `1px solid ${T.borderSoft}`,
        }}>第 {Math.ceil((today + firstDay) / 7)} 周</div>
      </div>

      {/* Weekday header */}
      <div style={{
        display: 'grid', gridTemplateColumns: 'repeat(7, 1fr)',
        marginBottom: 4, gap: 2,
      }}>
        {['日', '一', '二', '三', '四', '五', '六'].map((d, i) => (
          <div key={d} style={{
            textAlign: 'center', fontSize: 10, color: i === 0 || i === 6 ? T.red : T.ink3,
            fontWeight: 600, padding: '3px 0',
          }}>{d}</div>
        ))}
      </div>

      {/* Days */}
      <div style={{
        display: 'grid', gridTemplateColumns: 'repeat(7, 1fr)', gap: 2,
      }}>
        {cells.map((d, i) => {
          if (d == null) return <div key={i}/>;
          const isToday = d === today;
          const dayOfWeek = (firstDay + d - 1) % 7;
          const isWeekend = dayOfWeek === 0 || dayOfWeek === 6;
          return (
            <div key={i} style={{
              aspectRatio: '1',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              fontSize: 11.5,
              color: isToday ? 'white' : isWeekend ? T.red : T.ink2,
              fontWeight: isToday ? 700 : 500,
              background: isToday ? T.blue : 'transparent',
              borderRadius: 6,
              fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
              boxShadow: isToday ? `0 2px 6px -1px ${T.blue}55` : 'none',
            } } className="tnum">{d}</div>
          );
        })}
      </div>
    </div>
  );
}

// ─── Recent apps widget ─────────────────────────────────────────
export function RecentWidget({ apps, onOpen, variant = 'compact' }) {
  if (variant === 'row') {
    // Wide row layout — pill chips, suits the bottom-of-grid placement
    return (
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        {apps.map(app => (
          <div key={app.id} onClick={() => onOpen(app)} style={{
            display: 'inline-flex', alignItems: 'center', gap: 8,
            padding: '6px 12px 6px 6px', borderRadius: 999,
            background: 'rgba(255,255,255,0.7)',
            border: '1px solid rgba(226,232,240,0.85)',
            cursor: 'pointer',
            boxShadow: '0 1px 2px rgba(15,23,42,0.04)',
          }}>
            <div style={{
              width: 26, height: 26, borderRadius: 7,
              background: app.bg, color: 'white',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              flexShrink: 0,
            }}>
              <Icon name={app.icon} size={14} stroke={1.8}/>
            </div>
            <span style={{ fontSize: 12, color: T.ink, fontWeight: 500 }}>{app.name}</span>
          </div>
        ))}
      </div>
    );
  }
  // Compact (legacy) 2-col grid
  return (
    <div style={{
      width: 188, padding: 12,
      background: 'rgba(255,255,255,0.55)',
      backdropFilter: 'blur(10px)',
      WebkitBackdropFilter: 'blur(10px)',
      border: `1px solid rgba(255,255,255,0.8)`,
      borderRadius: 14,
      boxShadow: '0 4px 14px -4px rgba(15,23,42,0.10)',
    }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 10,
        fontSize: 10.5, color: T.ink3, fontWeight: 600,
        letterSpacing: '0.06em', textTransform: 'uppercase' }}>
        <Icon name="history" size={11} stroke={2}/>
        最近使用
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, 1fr)', gap: 6 }}>
        {apps.map(app => (
          <div key={app.id} onClick={() => onOpen(app)} style={{
            display: 'flex', alignItems: 'center', gap: 8,
            padding: 6, borderRadius: 8,
            background: 'rgba(255,255,255,0.7)',
            border: '1px solid rgba(226,232,240,0.7)',
            cursor: 'pointer',
          }}>
            <div style={{
              width: 26, height: 26, borderRadius: 7,
              background: app.bg, color: 'white',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              flexShrink: 0,
            }}>
              <Icon name={app.icon} size={14} stroke={1.8}/>
            </div>
            <div style={{ minWidth: 0, fontSize: 10.5, color: T.ink, fontWeight: 500,
              whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
              {app.name}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

// ─── Compact device info card (right column) ────────────────────
function DeviceInfoCard({ DEVICE }) {
  return (
    <div style={{
      padding: 16,
      background: 'rgba(255,255,255,0.7)',
      backdropFilter: 'blur(12px)',
      WebkitBackdropFilter: 'blur(12px)',
      border: `1px solid rgba(255,255,255,0.9)`,
      borderRadius: 14,
      boxShadow: '0 6px 20px -6px rgba(15,23,42,0.10)',
    }}>
      <div style={{
        display: 'flex', alignItems: 'center', gap: 8, marginBottom: 12,
      }}>
        <Icon name="cpu" size={14} stroke={1.8} style={{ color: T.blueDeep }}/>
        <div style={{ fontSize: 12, fontWeight: 700, color: T.ink,
          letterSpacing: '0.02em' }}>设备信息</div>
        <div style={{ flex: 1 }}/>
        <span style={{
          fontSize: 10, fontWeight: 600, padding: '2px 6px', borderRadius: 999,
          background: '#ecfdf5', color: '#047857', border: '1px solid #a7f3d0',
        }}>在线</span>
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 7 }}>
        {[
          ['硬件',  DEVICE.model],
          ['系统',  DEVICE.os],
          ['Agent', DEVICE.agent],
          ['持续在线', DEVICE.uptime],
        ].map(([k, v]) => (
          <div key={k} style={{ display: 'flex', alignItems: 'baseline', gap: 10, fontSize: 11.5 }}>
            <span style={{ color: T.ink3, width: 56, flexShrink: 0 }}>{k}</span>
            <span style={{ color: T.ink, flex: 1, fontWeight: 500,
              overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{v}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

function RunningChip({ error }) {
  if (error > 0) {
    return (
      <span style={{
        display: 'inline-flex', alignItems: 'center', gap: 5,
        fontSize: 11, fontWeight: 600, color: '#b91c1c',
        padding: '2px 8px', borderRadius: 999,
        background: '#fef2f2', border: '1px solid #fecaca',
        whiteSpace: 'nowrap', flexShrink: 0,
      }}>
        <StatusDot tone="red" size={6} pulse/>
        <span className="tnum">{error}</span>
        <span>个应用异常</span>
      </span>
    );
  }
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: 5,
      fontSize: 11, color: '#047857',
      whiteSpace: 'nowrap', flexShrink: 0,
    }}>
      <StatusDot tone="green" size={6}/>
      <span style={{ fontWeight: 600 }}>全部运行中</span>
    </span>
  );
}

function Section({ label, meta, right, children }) {
  return (
    <div>
      <div style={{
        display: 'flex', alignItems: 'baseline', gap: 10, marginBottom: 14,
      }}>
        <div style={{ fontSize: 12, fontWeight: 700, color: T.ink,
          letterSpacing: '0.04em' }}>{label}</div>
        <div style={{ fontSize: 11.5, color: T.ink3 }}>{meta}</div>
        <div style={{ flex: 1 }}/>
        {right}
      </div>
      {children}
    </div>
  );
}

function AppGrid({ apps, onOpen, iconStyle, accent, iconPx = 76, tilePx = 104, labelSize = 12.5 }) {
  return (
    <div style={{
      display: 'grid',
      gridTemplateColumns: `repeat(auto-fill, minmax(${tilePx + 8}px, ${tilePx + 8}px))`,
      gap: 8,
    }}>
      {apps.map(app => <AppIcon key={app.id} app={app} onOpen={onOpen}
        iconStyle={iconStyle} accent={accent}
        size={iconPx} labelSize={labelSize}/>)}
    </div>
  );
}

function DeployedApps({ apps, loading, error, onRetry, onOpen, iconStyle, accent, iconPx, tilePx, labelSize }) {
  if (apps.length > 0) {
    return <AppGrid apps={apps} onOpen={onOpen} iconStyle={iconStyle} accent={accent}
      iconPx={iconPx} tilePx={tilePx} labelSize={labelSize}/>;
  }
  const unavailable = !!error;
  const title = loading ? '正在读取服务状态' : unavailable ? '服务状态暂不可用' : '服务未配置';
  const description = loading ? '正在从 DevBox 获取已部署应用。' : unavailable
    ? '应用列表接口请求失败，其他桌面功能仍可使用。'
    : '尚未部署应用，可从应用商店选择服务。';
  return (
    <div className="desktop-deployed-empty" style={{ minHeight: 96, padding: '18px 20px', borderRadius: 8,
      border: `1px dashed ${T.border}`, background: 'rgba(255,255,255,0.55)',
      display: 'flex', alignItems: 'center', gap: 14 }}>
      <div style={{ width: 38, height: 38, borderRadius: 8, background: '#f1f5f9',
        color: T.ink3, display: 'grid', placeItems: 'center' }}>
        <Icon name="apps" size={18} stroke={1.7}/>
      </div>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ fontSize: 12.5, fontWeight: 700, color: T.ink }}>{title}</div>
        <div style={{ marginTop: 3, fontSize: 11, color: T.ink3 }}>{description}</div>
      </div>
      {!loading && <button type="button" onClick={() => unavailable ? onRetry?.() : onOpen({ id: 'store' })} style={{
        height: 32, padding: '0 12px', border: 'none', borderRadius: 6,
        background: T.blue, color: '#fff', fontSize: 11.5, fontWeight: 600, cursor: 'pointer',
      }}>{unavailable ? '重试' : '打开应用商店'}</button>}
    </div>
  );
}

function DesktopWidgets({ onOpenApp, deviceName }) {
  return <>
    <SystemStatusWidget
      onOpenMonitoring={() => onOpenApp({ id: 'monitoring' })}
      onOpenStorage={() => onOpenApp({ id: 'disks' })}/>
    <WelcomeWidget onOpenApp={onOpenApp} deviceName={deviceName}/>
  </>;
}

// ─── Desktop ────────────────────────────────────────────────────
export function Desktop({ onOpenApp, sysApps, deployedApps, deployedAppsLoading, deployedAppsError, onRetryDeployedApps, showRecent = true, iconStyle = 'gradient', accent = '#2563eb', layout = 'workstation', iconSize = 'md', APPS, RECENT_IDS, DEVICE }) {
  const runningCount = deployedApps.filter(a => a.state === 'running').length;
  const errorCount   = deployedApps.filter(a => a.state === 'error').length;

  // Icon size presets
  const iconPx = { sm: 60, md: 76, lg: 96 }[iconSize] || 76;
  const tilePx = iconPx + (iconSize === 'sm' ? 22 : iconSize === 'lg' ? 36 : 28);
  const labelSize = iconSize === 'sm' ? 11.5 : iconSize === 'lg' ? 13.5 : 12.5;

  // ─── Launcher layout: single full-width column, larger icons, no right sidebar
  if (layout === 'launcher') {
    return (
      <div className="desktop-layout desktop-layout-launcher" style={{
        flex: 1, padding: '40px 56px 110px',
        display: 'flex', flexDirection: 'column', gap: 32,
        overflow: 'auto', alignContent: 'start',
      }}>
        <Section label="系统工具" meta="本地固定">
          <AppGrid apps={sysApps} onOpen={onOpenApp} iconStyle={iconStyle} accent={accent}
            iconPx={Math.max(iconPx, 88)} tilePx={tilePx + 12} labelSize={labelSize}/>
        </Section>
        <Section label="已部署应用" meta="云端下发"
          right={<RunningChip running={runningCount} error={errorCount}/>}>
          <DeployedApps apps={deployedApps} loading={deployedAppsLoading} error={deployedAppsError} onRetry={onRetryDeployedApps} onOpen={onOpenApp} iconStyle={iconStyle} accent={accent}
            iconPx={Math.max(iconPx, 88)} tilePx={tilePx + 12} labelSize={labelSize}/>
        </Section>
        {showRecent && (
          <Section label="最近使用" meta="按访问时间">
            <RecentWidget
              apps={RECENT_IDS.map(id => APPS.find(a => a.id === id)).filter(Boolean)}
              onOpen={onOpenApp} variant="row"/>
          </Section>
        )}
        <div className="desktop-launcher-widgets" style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(280px, 1fr))', gap: 14 }}>
          <DesktopWidgets onOpenApp={onOpenApp} deviceName={DEVICE.name}/>
        </div>
      </div>
    );
  }

  // ─── Compact: denser grid, smaller spacing, dual column with smaller widgets
  if (layout === 'compact') {
    return (
      <div className="desktop-layout desktop-layout-grid" style={{
        flex: 1, padding: '20px 36px 110px',
        display: 'grid', gridTemplateColumns: '1fr 280px', gap: 22,
        overflow: 'auto', alignContent: 'start',
      }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 18, minWidth: 0 }}>
          <Section label="系统工具" meta="本地固定">
            <AppGrid apps={sysApps} onOpen={onOpenApp} iconStyle={iconStyle} accent={accent}
              iconPx={Math.min(iconPx, 64)} tilePx={Math.min(tilePx, 94)} labelSize={11}/>
          </Section>
          <Section label="已部署应用" meta="云端下发"
            right={<RunningChip running={runningCount} error={errorCount}/>}>
            <DeployedApps apps={deployedApps} loading={deployedAppsLoading} error={deployedAppsError} onRetry={onRetryDeployedApps} onOpen={onOpenApp} iconStyle={iconStyle} accent={accent}
              iconPx={Math.min(iconPx, 64)} tilePx={Math.min(tilePx, 94)} labelSize={11}/>
          </Section>
          {showRecent && (
            <Section label="最近使用" meta="按访问时间">
              <RecentWidget
                apps={RECENT_IDS.map(id => APPS.find(a => a.id === id)).filter(Boolean)}
                onOpen={onOpenApp} variant="row"/>
            </Section>
          )}
        </div>
        <div className="desktop-sidebar" style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          <ClockCalendarWidget/>
          <DesktopWidgets onOpenApp={onOpenApp} deviceName={DEVICE.name}/>
          <DeviceInfoCard DEVICE={DEVICE}/>
          <MemoWidget/>
        </div>
      </div>
    );
  }

  // ─── Workstation (default): two-column with full widgets
  return (
    <div className="desktop-layout desktop-layout-grid" style={{
      flex: 1, position: 'relative',
      padding: '28px 48px 110px',
      display: 'grid', gridTemplateColumns: '1fr 340px', gap: 32,
      overflow: 'auto', alignContent: 'start',
    }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 26, minWidth: 0 }}>
        <Section label="系统工具" meta="本地固定 · 始终可用">
          <AppGrid apps={sysApps} onOpen={onOpenApp} iconStyle={iconStyle} accent={accent}
            iconPx={iconPx} tilePx={tilePx} labelSize={labelSize}/>
        </Section>

        <Section label="已部署应用" meta="云端下发"
          right={<RunningChip running={runningCount} error={errorCount}/>}>
          <DeployedApps apps={deployedApps} loading={deployedAppsLoading} error={deployedAppsError} onRetry={onRetryDeployedApps} onOpen={onOpenApp} iconStyle={iconStyle} accent={accent}
            iconPx={iconPx} tilePx={tilePx} labelSize={labelSize}/>
        </Section>

        {showRecent && (
          <Section label="最近使用" meta="按访问时间">
            <RecentWidget
              apps={RECENT_IDS.map(id => APPS.find(a => a.id === id)).filter(Boolean)}
              onOpen={onOpenApp} variant="row"/>
          </Section>
        )}
      </div>

      <div className="desktop-sidebar" style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
        <ClockCalendarWidget/>
        <DesktopWidgets onOpenApp={onOpenApp} deviceName={DEVICE.name}/>
        <DeviceInfoCard DEVICE={DEVICE}/>
        <MemoWidget/>
      </div>
    </div>
  );
}
