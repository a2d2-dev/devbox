import { useState, useEffect, useMemo, useRef, useCallback } from 'react'
import { T } from './tokens'
import { Icon } from './icons'

// ─── Watermark overlay (canvas-based, updates every minute) ────
function Watermark({ text }) {
  const ref = useRef(null);
  const draw = useCallback(() => {
    const el = ref.current;
    if (!el) return;
    const now = new Date();
    const ts = `${String(now.getHours()).padStart(2,'0')}:${String(now.getMinutes()).padStart(2,'0')}`;
    const label = `${text}  ${ts}`;
    const c = document.createElement('canvas');
    const dpr = window.devicePixelRatio || 1;
    c.width = 280 * dpr; c.height = 160 * dpr;
    const ctx = c.getContext('2d');
    ctx.scale(dpr, dpr);
    ctx.rotate(-22 * Math.PI / 180);
    ctx.font = '13px -apple-system, BlinkMacSystemFont, sans-serif';
    ctx.fillStyle = 'rgba(0,0,0,0.06)';
    ctx.fillText(label, 20, 120);
    el.style.backgroundImage = `url(${c.toDataURL()})`;
    el.style.backgroundSize = `${280}px ${160}px`;
  }, [text]);

  useEffect(() => {
    draw();
    const id = setInterval(draw, 60_000);
    return () => clearInterval(id);
  }, [draw]);

  return <div ref={ref} style={{
    position: 'fixed', inset: 0, zIndex: 9999,
    pointerEvents: 'none',
  }}/>;
}
import { StatusBar } from './components/StatusBar'
import { Desktop } from './components/Desktop'
import { Dock } from './components/Dock'
import AppWindow, { btnSecondary, btnPrimary } from './components/AppWindow'
import DashboardApp from './pages/Dashboard'
import AppStore from './pages/AppStore'
import AlertCenter from './pages/AlertCenter'
import AuditLog from './pages/AuditLog'
import Supervisor from './pages/Supervisor'
import Hardware from './pages/Hardware'
import Links from './pages/Links'
import Diagnostics from './pages/Settings'
import { AppShell } from './components/AppShell'
import AppMgmtDrawer from './components/AppMgmtDrawer'
import { AuthModal } from './components/AuthModal'
import { LoginScreen } from './components/LoginScreen'
import { TweaksPanel, useTweaks, TweakSection, TweakRadio, TweakToggle, TweakColor } from './components/TweaksPanel'
import { useMetrics, useMetricsHistory, useApps, useAlerts, useDevice, setAuthToken, getAuthToken, clearAuth, setOnAuthExpired } from './hooks/useApi'
import { SYSTEM_APPS } from './data/systemApps'

const TWEAK_DEFAULTS = {
  "preset": "platform-dark",
  "wallpaper": "grid",
  "iconStyle": "gradient",
  "topbar": "dark",
  "accent": "#2563eb",
  "layout": "workstation",
  "iconSize": "md",
  // deviceLabel 已删 — 从 /api/v1/device 拿真实 deviceName (K8s node name，如 edge-004)
  // 登录前 device data 还没拿到时，组件需要自行处理空值显示
  "showRecent": true,
  "online": true
};

// Style presets — each one sets multiple sub-tweaks at once
const PRESETS = {
  'platform-dark': {
    label: '云端同款',
    desc: '深色顶栏 · 与管理平台一致',
    set: { topbar: 'dark', iconStyle: 'gradient', accent: '#2563eb', wallpaper: 'grid' },
  },
  'aurora-light': {
    label: '极光浅色',
    desc: '浅色玻璃顶栏 · 渐变图标',
    set: { topbar: 'light', iconStyle: 'gradient', accent: '#2563eb', wallpaper: 'grid' },
  },
  'field-cyan': {
    label: '现场青绿',
    desc: '工业青色调 · 拓扑背景',
    set: { topbar: 'dark', iconStyle: 'gradient', accent: '#06b6d4', wallpaper: 'topo' },
  },
  'minimal-flat': {
    label: '极简扁平',
    desc: '扁平图标 · 灰底无渐变',
    set: { topbar: 'light', iconStyle: 'flat', accent: '#0f172a', wallpaper: 'plain' },
  },
  'sunrise': {
    label: '晨曦渐变',
    desc: '暖色调 · 紫橙光晕',
    set: { topbar: 'light', iconStyle: 'gradient', accent: '#8b5cf6', wallpaper: 'topo' },
  },
  'mono-dev': {
    label: '研发暗调',
    desc: '深色顶栏 + 灰底 + 扁平',
    set: { topbar: 'dark', iconStyle: 'flat', accent: '#10b981', wallpaper: 'plain' },
  },
};

function clamp(v, lo, hi) { return Math.max(lo, Math.min(hi, v)); }

export default function App() {
  const [loggedIn, setLoggedIn] = useState(() => !!getAuthToken());
  const [loginUser, setLoginUser] = useState('');

  const handleLogin = (token, username) => {
    setAuthToken(token);
    setLoginUser(username);
    setLoggedIn(true);
  };

  const handleLogout = () => {
    clearAuth();
    setLoggedIn(false);
    setLoginUser('');
  };

  useEffect(() => {
    setOnAuthExpired(() => {
      setLoggedIn(false);
      setLoginUser('');
    });
  }, []);

  const [t, setT] = useTweaks(TWEAK_DEFAULTS);

  // Apply preset -> individual tweaks
  const applyPreset = (id) => {
    const p = PRESETS[id];
    if (!p) return;
    setT({ preset: id, ...p.set });
  };

  // Resolve current theme
  const theme = useMemo(() => ({
    topbar: t.topbar,
    iconStyle: t.iconStyle,
    accent: t.accent,
    wallpaper: t.wallpaper,
  }), [t.topbar, t.iconStyle, t.accent, t.wallpaper]);

  // ─── Mac/Windows-style session state ──────────────────────────
  // openApps  : ids of apps currently "running" in this browser session
  //             (always present in the dock until explicitly closed)
  // activeId  : id of the app whose window is currently shown,
  //             or null when the desktop is visible (all windows minimized)
  // minimized : set of ids whose window is hidden but still running
  const [openApps, setOpenApps] = useState([]);
  const [activeId, setActiveId] = useState(null);
  const [minimized, setMinimized] = useState(() => new Set());
  // Per-app maximized state — each app remembers whether it was full-screen
  // or framed before it was minimized / unfocused.
  const [maxByApp, setMaxByApp] = useState({}); // { [appId]: boolean }
  const maximized = activeId ? (maxByApp[activeId] ?? true) : true;
  const setMaximized = (next) => {
    if (!activeId) return;
    setMaxByApp(m => {
      const cur = m[activeId] ?? true;
      const val = typeof next === 'function' ? next(cur) : next;
      if (val === cur) return m;
      return { ...m, [activeId]: val };
    });
  };
  const [mgmtOpen, setMgmtOpen] = useState(false);
  const [authed, setAuthed] = useState(false);
  const [authReason, setAuthReason] = useState(null);
  const [pendingAction, setPendingAction] = useState(null);
  const [uninstalled, setUninstalled] = useState(() => new Set());

  // Launch an app: add to running set + focus its window.
  // New apps default to full-screen; already-running apps keep their last mode.
  const launchApp = (app) => {
    if (!app || !app.id) return;
    setOpenApps(p => p.includes(app.id) ? p : [...p, app.id]);
    setMinimized(prev => {
      if (!prev.has(app.id)) return prev;
      const next = new Set(prev); next.delete(app.id); return next;
    });
    setMaxByApp(m => (app.id in m) ? m : { ...m, [app.id]: true });
    setActiveId(app.id);
    setMgmtOpen(false);
  };

  // Click a dock icon: toggle focus / restore from minimize / minimize if active
  const focusApp = (id) => {
    if (!openApps.includes(id)) return;
    if (activeId === id) {
      // Toggle minimize on the currently-active app
      setMinimized(prev => { const n = new Set(prev); n.add(id); return n; });
      setActiveId(null);
      setMgmtOpen(false);
      return;
    }
    setMinimized(prev => {
      if (!prev.has(id)) return prev;
      const n = new Set(prev); n.delete(id); return n;
    });
    // Don't touch maxByApp — restore whatever mode the window was in.
    setActiveId(id);
    setMgmtOpen(false);
  };

  // Minimize the active window — keep the app in the dock
  const minimizeWindow = () => {
    if (!activeId) return;
    setMinimized(prev => { const n = new Set(prev); n.add(activeId); return n; });
    setActiveId(null);
    setMgmtOpen(false);
  };

  // Close window: remove the app from the running set
  const closeWindow = () => {
    if (!activeId) return;
    const id = activeId;
    setOpenApps(p => p.filter(x => x !== id));
    setMinimized(prev => { const n = new Set(prev); n.delete(id); return n; });
    setMaxByApp(m => { if (!(id in m)) return m; const n = { ...m }; delete n[id]; return n; });
    setActiveId(null);
    setMgmtOpen(false);
  };

  // Close any open app (e.g. from dock right-click / hover X)
  const closeApp = (id) => {
    setOpenApps(p => p.filter(x => x !== id));
    setMinimized(prev => { const n = new Set(prev); n.delete(id); return n; });
    setMaxByApp(m => { if (!(id in m)) return m; const n = { ...m }; delete n[id]; return n; });
    if (activeId === id) { setActiveId(null); setMgmtOpen(false); }
  };

  // Show the desktop: minimize all open windows
  const showDesktop = () => {
    if (activeId) {
      setMinimized(prev => { const n = new Set(prev); n.add(activeId); return n; });
    }
    setActiveId(null);
    setMgmtOpen(false);
  };

  // Live data from API hooks
  const metricsHook = useMetrics();
  const { data: metricsHistoryData } = useMetricsHistory();
  const { data: liveApps } = useApps();
  const { data: liveAlerts } = useAlerts();
  const { data: liveDevice } = useDevice();

  const liveAppById = useMemo(() => {
    if (!liveApps) return {};
    return Object.fromEntries(liveApps.map(a => [a.id, a]));
  }, [liveApps]);

  // Bug fix (LF report 2026-06-20): "Rendered more hooks than during previous render"
  // 原 appById useMemo 在 if (!loggedIn) return <LoginScreen/> 之后调用，登录前后
  // hook 数量不一致 → 必须移到 early return 之前。
  // SYSTEM_APPS 是 const import；liveAppById 已在上一行计算。
  const appById = useMemo(() => ({
    ...Object.fromEntries(SYSTEM_APPS.map(a => [a.id, a])),
    ...(liveAppById || {}),
  }), [liveAppById])

  const m = metricsHook?.data?.metrics;
  const [cpu, setCpu] = useState(0);
  const [gpu, setGpu] = useState(0);
  const [mem, setMem] = useState(0);
  useEffect(() => {
    if (m) {
      setCpu(m.cpu?.used || 0);
      setGpu(m.gpu?.used || 0);
      setMem(m.mem?.pct || 0);
    }
  }, [m]);

  // [Story 4.2 Disabled 2026-06-20] AuthModal 二次确认在受控开发界面下移除。
  // 登录是唯一身份门槛（AD-3），通过登录的用户调用 requireAuth 直接放行。
  // 业务路径（应用安装/卸载、告警 ack、系统设置等）调用方继续传 action，
  // 但不再触发模态确认弹窗。AuthModal 组件文件保留供历史追溯。
  const requireAuth = (action) => {
    setAuthed(true);
  };

  const onAuthSuccess = () => {
    setAuthed(true);
    setAuthReason(null);
    setPendingAction(null);
  };

  // device 必须在 LoginScreen early-return 前定义（line 290 引用它）
  const device = liveDevice || {};

  if (!loggedIn) {
    return <LoginScreen onLogin={handleLogin} deviceName={device.deviceName || device.hostname}/>;
  }

  // 系统工具是本地内置能力（永远显示），不依赖任何 API。
  // /api/v1/apps 仅返回云端已部署应用（kind:'app'），失败只影响"已部署应用"段。
  // appById useMemo 已上移到 early return 之前（hooks 顺序约束）
  const liveDeployedApps = Array.isArray(liveApps) ? liveApps.filter(a => a.kind === 'app') : []
  const apps = [...SYSTEM_APPS, ...liveDeployedApps]
  const alerts = liveAlerts || [];

  const activeApp = activeId ? appById[activeId] : null;

  const sysApps = SYSTEM_APPS;
  const deployedApps = liveDeployedApps.filter(a => !uninstalled.has(a.id));
  const alertCount = Array.isArray(alerts) ? alerts.filter(a => a.state === 'active').length : 0;

  // Custom wallpaper
  const bgClass = t.wallpaper === 'grid' ? 'edge-bg' : '';
  const bgStyle = t.wallpaper === 'topo'
    ? { background: '#eef3fa', backgroundImage: 'radial-gradient(circle at 20% 20%, rgba(59,130,246,0.15), transparent 40%), radial-gradient(circle at 80% 70%, rgba(20,184,166,0.12), transparent 45%), radial-gradient(circle at 50% 100%, rgba(99,102,241,0.10), transparent 50%)' }
    : t.wallpaper === 'plain'
      ? { background: '#f1f5f9' }
      : {};

  // Dock receives the running apps (resolved to full app objects), with
  // each one tagged "active" / "minimized" so it can render correctly.
  const dockApps = openApps
    .map(id => appById[id])
    .filter(Boolean)
    .map(a => ({
      ...a,
      isActive: activeId === a.id,
      isMinimized: minimized.has(a.id),
    }));

  // Window is "visible" iff there is an active app and it's not minimized
  const showWindow = !!activeApp && !minimized.has(activeApp.id);

  return (
    <div style={{
      width: '100vw', height: '100vh', overflow: 'hidden',
      position: 'relative', display: 'flex', flexDirection: 'column',
    }} className={bgClass}>
      <div style={{ position: 'absolute', inset: 0, ...bgStyle, zIndex: 0 }} className={bgClass}/>
      <Watermark text={device.deviceName || device.hostname || ''}/>

      {/* Status bar */}
      <div style={{ position: 'relative', zIndex: 30 }}>
        <StatusBar
          theme={theme.topbar}
          // 优先用真实 K8s node name (deviceName='edge-004')
          // hostname (edge-probe-node) / mock label (GB10-DEV-01) 都不直观
          deviceLabel={device.deviceName || device.hostname || t.deviceLabel}
          DEVICE={device}
          cpu={Math.round(cpu)}
          gpu={Math.round(gpu)}
          mem={Math.round(mem)}
          alertCount={alertCount}
          online={t.online}
          lastSync="5 分钟前"
          onOpenAlerts={() => launchApp({ id: 'alerts' })}
          loginUser={loginUser}
          onLogout={handleLogout}
        />
      </div>

      {/* Main content area */}
      <div style={{ position: 'relative', flex: 1, overflow: 'hidden' }}>
        <div style={{ position: 'absolute', inset: 0, display: 'flex', flexDirection: 'column' }}>
          <Desktop
            onOpenApp={(app) => {
              if (app.locked) {
                requireAuth(`打开「${app.name}」`);
                return;
              }
              launchApp(app);
            }}
            sysApps={sysApps}
            deployedApps={deployedApps}
            APPS={apps}
            RECENT_IDS={[]}
            DEVICE={device}
            showRecent={t.showRecent}
            iconStyle={theme.iconStyle}
            accent={theme.accent}
            layout={t.layout}
            iconSize={t.iconSize}
          />
        </div>

        {/* Windows — all open apps stay mounted, hidden via display:none */}
        {openApps.map(appId => {
          const app = appById[appId];
          if (!app) return null;
          const isVisible = activeId === appId && !minimized.has(appId);
          const isMax = maxByApp[appId] ?? true;
          return (
            <div key={appId} style={{ display: isVisible ? 'contents' : 'none' }}>
              <AppWindow
                app={app}
                maximized={isMax}
                onMaximize={() => setMaxByApp(m => ({ ...m, [appId]: !(m[appId] ?? true) }))}
                onMinimize={minimizeWindow}
                onClose={closeWindow}
                canManage={app.kind === 'app'}
                mgmtOpen={activeId === appId && mgmtOpen}
                onToggleMgmt={() => setMgmtOpen(o => !o)}
              >
                {appId === 'dashboard' && <DashboardApp onOpenApp={launchApp}/>}
                {appId === 'store'     && <AppStore onOpenApp={launchApp} authed={authed} onRequireAuth={requireAuth}/>}
                {appId === 'alerts'    && <AlertCenter authed={authed} onRequireAuth={requireAuth}/>}
                {appId === 'audit'     && <AuditLog/>}
                {appId === 'supervisor'&& <Supervisor/>}
                {appId === 'hardware'  && <Hardware/>}
                {appId === 'links'     && <Links/>}
                {(appId === 'diag' || appId === 'settings') && <Diagnostics/>}
                {!['dashboard','store','alerts','audit','supervisor','hardware','links','diag','settings'].includes(appId)
                  && <AppShell appId={appId} app={app} authed={authed} onRequireAuth={requireAuth}
                       onOpenManagement={() => setMgmtOpen(true)}/>}

                {app.kind === 'app' && (
                  <AppMgmtDrawer
                    app={app}
                    open={activeId === appId && mgmtOpen}
                    onClose={() => setMgmtOpen(false)}
                    authed={authed}
                    onRequireAuth={requireAuth}
                    onUninstall={(a) => { setUninstalled(s => new Set([...s, a.id])); closeApp(a.id); }}
                    metricsData={metricsHook?.data}
                    historyData={metricsHistoryData}
                  />
                )}
              </AppWindow>
            </div>
          );
        })}

        {/* Dock — visible on desktop and when window is in framed (non-maximized) mode */}
        {(!showWindow || !maximized) && (
          <Dock
            apps={dockApps}
            onShowDesktop={showDesktop}
            onFocusApp={focusApp}
            onCloseApp={closeApp}
            anyVisible={showWindow}
            authed={authed}
            alertBadge={alertCount}
          />
        )}
      </div>

      {/* [Story 4.2 Disabled] AuthModal "节点访问授权" 入口删除 (LF 报告冲突 2026-06-21)
          受控开发界面下登录是唯一身份门槛，退出在右上头像下拉 */}

      {/* Tweaks panel */}
      <TweaksPanel title="Tweaks">
        <TweakSection label="主题预设"/>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6, padding: '0 0 6px' }}>
          {Object.entries(PRESETS).map(([id, p]) => (
            <button key={id} onClick={() => applyPreset(id)} style={{
              display: 'flex', alignItems: 'center', gap: 10,
              padding: '8px 10px', borderRadius: 8, textAlign: 'left',
              background: t.preset === id ? T.blueSoft : 'white',
              border: `1px solid ${t.preset === id ? '#bfdbfe' : T.border}`,
              cursor: 'pointer',
            }}>
              <div style={{
                width: 28, height: 28, borderRadius: 6, flexShrink: 0,
                background: p.set.topbar === 'dark' ? '#0b1220' : '#f1f5f9',
                border: `1px solid ${T.borderSoft}`,
                position: 'relative', overflow: 'hidden',
              }}>
                <div style={{
                  position: 'absolute', top: 0, left: 0, right: 0, height: 8,
                  background: p.set.topbar === 'dark' ? '#0b1220' : 'rgba(255,255,255,0.9)',
                  borderBottom: `1px solid ${p.set.topbar === 'dark' ? 'rgba(255,255,255,0.1)' : '#e2e8f0'}`,
                }}/>
                <div style={{
                  position: 'absolute', left: 5, top: 12,
                  width: 6, height: 6, borderRadius: p.set.iconStyle === 'flat' ? 1.5 : 2,
                  background: p.set.accent,
                }}/>
                <div style={{
                  position: 'absolute', left: 14, top: 12,
                  width: 6, height: 6, borderRadius: p.set.iconStyle === 'flat' ? 1.5 : 2,
                  background: '#fb7185',
                }}/>
              </div>
              <div style={{ minWidth: 0, flex: 1 }}>
                <div style={{ fontSize: 12.5, fontWeight: 600,
                  color: t.preset === id ? T.blueDeep : T.ink }}>{p.label}</div>
                <div style={{ fontSize: 10.5, color: T.ink3, marginTop: 2 }}>{p.desc}</div>
              </div>
              {t.preset === id && <Icon name="check" size={13} stroke={2.4} style={{ color: T.blueDeep, flexShrink: 0 }}/>}
            </button>
          ))}
        </div>

        <TweakSection label="桌面布局"/>
        <TweakRadio  label="排列"     value={t.layout}    options={[{ value: 'workstation', label: '工作站' }, { value: 'launcher', label: '启动器' }, { value: 'compact', label: '紧凑' }]}
                     onChange={(v) => setT('layout', v)}/>
        <TweakRadio  label="图标尺寸" value={t.iconSize}  options={[{ value: 'sm', label: '小' }, { value: 'md', label: '中' }, { value: 'lg', label: '大' }]}
                     onChange={(v) => setT('iconSize', v)}/>

        <TweakSection label="单项覆盖"/>
        <TweakRadio  label="顶栏"   value={t.topbar}    options={[{ value: 'dark', label: '深色' }, { value: 'light', label: '浅色' }]}
                     onChange={(v) => setT('topbar', v)}/>
        <TweakRadio  label="图标"   value={t.iconStyle} options={[{ value: 'gradient', label: '渐变' }, { value: 'flat', label: '扁平' }]}
                     onChange={(v) => setT('iconStyle', v)}/>
        <TweakRadio  label="背景"   value={t.wallpaper} options={[{ value: 'grid', label: '网格' }, { value: 'topo', label: '光晕' }, { value: 'plain', label: '纯色' }]}
                     onChange={(v) => setT('wallpaper', v)}/>
        <TweakColor  label="主色"   value={t.accent}    options={['#2563eb', '#06b6d4', '#10b981', '#8b5cf6', '#0f172a']}
                     onChange={(v) => setT('accent', v)}/>
        <TweakToggle label="显示最近使用" value={t.showRecent} onChange={(v) => setT('showRecent', v)}/>

        <TweakSection label="状态"/>
        <TweakToggle label="云端在线" value={t.online} onChange={(v) => setT('online', v)}/>
        <button onClick={handleLogout} style={{
          width: '100%', height: 32, borderRadius: 7, marginTop: 4,
          background: 'white', color: '#dc2626', border: `1px solid #fecaca`,
          fontSize: 12, fontWeight: 600, cursor: 'pointer',
        }}>退出登录 ({loginUser || 'user'})</button>

        <TweakSection label="演示快捷"/>
        {[
          ['dashboard', '仪表盘'],
          ['ai-activity', 'AI 活动'],
          ['store',     '应用商店'],
          ['vscode',    'VS Code Server'],
          ['jupyter',   'JupyterLab'],
          ['vllm',      'vLLM (异常)'],
          ['comfyui',   'ComfyUI'],
          ['sdwebui',   'SD WebUI'],
          ['training',  '训练任务'],
          ['terminal',  '终端'],
          ['files',     '文件浏览器'],
          ['ports',     '端口与公网'],
          ['models',    '模型仓库'],
          ['alerts',    '告警中心'],
        ].map(([id, label]) => (
          <button key={id} onClick={() => launchApp({ id })} style={{
            width: '100%', height: 30, borderRadius: 6, marginBottom: 4,
            background: activeId === id ? T.blueSoft : openApps.includes(id) ? '#f1f5f9' : 'white',
            color: activeId === id ? T.blueDeep : T.ink2,
            border: `1px solid ${activeId === id ? '#bfdbfe' : T.border}`,
            fontSize: 12, fontWeight: 500, textAlign: 'left', paddingLeft: 10, cursor: 'pointer',
            display: 'flex', alignItems: 'center', gap: 6,
          }}>
            {openApps.includes(id) && (
              <span style={{ width: 5, height: 5, borderRadius: '50%',
                background: activeId === id ? T.blueDeep : T.ink3 }}/>
            )}
            {label}
          </button>
        ))}
        {/* [Story 4.2 Disabled 2026-06-20] 演示验证弹窗按钮移除（AuthModal 无业务触发） */}

        {openApps.length > 0 && (
          <button onClick={() => { openApps.forEach(id => closeApp(id)); }} style={{
            width: '100%', height: 30, borderRadius: 6, marginTop: 4,
            background: 'white', color: T.ink3, border: `1px solid ${T.border}`,
            fontSize: 12, fontWeight: 500, textAlign: 'left', paddingLeft: 10, cursor: 'pointer',
          }}>退出全部 ({openApps.length}) 个运行中应用</button>
        )}
      </TweaksPanel>
    </div>
  );
}
