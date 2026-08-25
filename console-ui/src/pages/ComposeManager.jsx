// ComposeManager — 应用管理入口（Issue #2）。
//
// 职责：
//   1. 应用列表：全部 / Docker Compose / Kubernetes / 系统 四类筛选；卡片由后端 phase
//      驱动（observed.phase），显示 runtime + service 数、endpoints / 打开入口、最近
//      operation、catalog 升级提示。前端不自行推 phase。
//   2. 新建 Compose 向导：来源（平台商店入口 / 第三方 catalog 入口 / 粘贴 / 上传 YAML）
//      → 预检（services/images/ports/volumes/network/secrets + 风险 blocked/confirmation/
//      warning/safe）→ 配置 → 部署 Task。上传用 File API 真实读文本。
//   3. 生命周期（start/stop/restart/redeploy）与卸载（走 UninstallDialog / remove-preview）。
//
// 写操作返回 202+Task，前端用 useTask 轮询并 toast 反馈。
import { useState, useEffect, useMemo, useRef } from 'react';
import { T } from '../tokens';
import { Icon } from '../icons';
import { StatusDot, Chip } from '../components/ui';
import { btnSecondary, btnDanger } from '../components/AppWindow';
import { useToast } from '../components/toastContext';
import UninstallDialog from '../components/UninstallDialog';
import { SYSTEM_APPS } from '../data/systemApps';
import {
  useApps, useAppCapability, useTask, appActionAsync,
  validateCompose, applyComposeApp, takeoverApp, useStoreApps, useCatalogApps,
} from '../hooks/useApi';
import {
  PHASE_LABEL, PHASE_TONE, observedPhase, runtimeLabel, appOrigin,
  computeUpgrade, groupRisks, canDeployGivenRisks, slugify, parseEnv,
  formatRelativeTime, serviceInventory, HEALTH_LABEL,
} from '../lib/compose';

export function ComposeManager({ authed, onRequireAuth, onOpenStore, onOpenApp }) {
  const { data: apps, refresh } = useApps(5000);
  const { data: cap } = useAppCapability();
  const { data: storeApps } = useStoreApps();
  const { data: catalogApps } = useCatalogApps();
  const [filter, setFilter] = useState('all');
  const [showCreate, setShowCreate] = useState(false);
  const [activeTask, setActiveTask] = useState(null); // {id, label, appId}
  const [uninstall, setUninstall] = useState(null);   // app | null
  const [takeover, setTakeover] = useState(null);     // discovered app | null
  const toast = useToast();

  const { task } = useTask(activeTask?.id);
  useEffect(() => {
    if (!task || !activeTask) return;
    if (['succeeded', 'failed', 'canceled', 'superseded'].includes(task.status)) {
      if (task.status === 'succeeded') toast.ok(`${activeTask.label}完成`);
      else toast.err(`${activeTask.label}失败：${task.message || task.status}`);
      const t = setTimeout(() => setActiveTask(null), 1500);
      refresh();
      return () => clearTimeout(t);
    }
  }, [task, activeTask, toast, refresh]);

  const composeCap = cap?.compose;
  const composeDown = composeCap && composeCap.available === false;

  const arr = useMemo(() => Array.isArray(apps) ? apps : [], [apps]);
  const counts = {
    all: arr.length,
    compose: arr.filter((a) => (a.runtime || 'kubernetes') === 'compose').length,
    kubernetes: arr.filter((a) => (a.runtime || 'kubernetes') === 'kubernetes').length,
    system: SYSTEM_APPS.length,
  };

  const list = useMemo(() => {
    if (filter === 'system') return [];
    if (filter === 'all') return arr;
    return arr.filter((a) => (a.runtime || 'kubernetes') === filter);
  }, [arr, filter]);

  function guard(fn) {
    return (...args) => {
      if (!authed) { onRequireAuth?.(); return; }
      return fn(...args);
    };
  }

  async function doAction(appId, action, label) {
    try {
      const t = await appActionAsync(appId, action);
      setActiveTask({ id: t.id, label, appId });
      toast.ok(`${label}已提交`);
    } catch (e) { toast.err(`${label}失败：${e.message}`); }
  }

  return (
    <div style={{ padding: 24, height: '100%', overflow: 'auto', background: T.bg }}>
      <Header
        composeCap={composeCap}
        filter={filter}
        setFilter={setFilter}
        counts={counts}
        authed={authed}
        onRequireAuth={onRequireAuth}
        onCreate={() => setShowCreate(true)}
      />

      {activeTask && task && <TaskBanner task={task} label={activeTask.label} />}

      {composeDown && (
        <div style={warnBox}>
          Docker Compose 运行时不可用：{composeCap.reason || ''}。已部署的 Compose 应用仍可观测；K8s 应用与系统工具不受影响。
        </div>
      )}

      <div style={{ display: 'grid', gap: 10, marginTop: 14 }}>
        {/* 系统工具筛选：渲染 SYSTEM_APPS 为可启动卡片 */}
        {filter === 'system' && (
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))', gap: 10 }}>
            {SYSTEM_APPS.map((a) => (
              <SystemCard key={a.id} app={a} onOpen={() => (onOpenApp ? onOpenApp(a) : null)} />
            ))}
          </div>
        )}

        {filter !== 'system' && list.length === 0 && (
          <EmptyState composeDown={composeDown} hasCap={!!cap} onCreate={() => (authed ? setShowCreate(true) : onRequireAuth?.())} />
        )}

        {filter !== 'system' && list.map((app) => (
          app.ownership === 'discovered' ? (
            <DiscoveredCard
              key={app.id}
              app={app}
              disabled={!!activeTask}
              onTakeover={() => (authed ? setTakeover(app) : onRequireAuth?.())}
            />
          ) : (
            <AppCard
              key={app.id}
              app={app}
              storeApps={storeApps}
              catalogApps={catalogApps}
              disabled={!!activeTask}
              onAction={(action, label) => guard(doAction)(app.id, action, label)}
              onUninstall={() => setUninstall(app)}
              onOpenApp={onOpenApp}
            />
          )
        ))}
      </div>

      {showCreate && (
        <CreateDialog
          composeCap={composeCap}
          onClose={() => setShowCreate(false)}
          onOpenStore={() => { setShowCreate(false); onOpenStore?.(); }}
          onDeployed={(t) => {
            setShowCreate(false);
            setActiveTask({ id: t.id, label: '部署', appId: t.appId });
            refresh();
          }}
        />
      )}

      {uninstall && (
        <UninstallDialog
          app={uninstall}
          onClose={() => setUninstall(null)}
          onDone={(taskId) => {
            setUninstall(null);
            if (taskId) setActiveTask({ id: taskId, label: '卸载', appId: uninstall.id });
            else { toast.ok('已卸载'); refresh(); }
          }}
        />
      )}

      {takeover && (
        <TakeoverDialog
          app={takeover}
          onClose={() => setTakeover(null)}
          onDone={(ok) => {
            setTakeover(null);
            if (ok) { toast.ok('已接管，现在可以编辑与部署'); refresh(); }
          }}
        />
      )}
    </div>
  );
}

// ─── Header ─────────────────────────────────────────────────────
function Header({ composeCap, filter, setFilter, counts, authed, onRequireAuth, onCreate }) {
  const filters = [
    { id: 'all', label: '全部', count: counts.all },
    { id: 'compose', label: 'Docker Compose', count: counts.compose },
    { id: 'kubernetes', label: 'Kubernetes', count: counts.kubernetes },
    { id: 'system', label: '系统工具', count: counts.system },
  ];
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
      <div style={{ fontSize: 20, fontWeight: 700, color: T.ink }}>应用管理</div>
      <div style={{ fontSize: 12, color: T.ink3 }}>
        {composeCap?.available ? `Compose ${composeCap.version || '可用'}` : composeCap ? 'Compose 未就绪' : '检测运行时中…'}
      </div>
      <div style={{ flex: 1 }} />
      <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
        {filters.map((f) => (
          <button key={f.id} onClick={() => setFilter(f.id)} title={f.label}
            style={filter === f.id ? chipActive : chip}>
            {f.label}
            <span className="mono tnum" style={{ marginLeft: 5, opacity: 0.8 }}>{f.count}</span>
          </button>
        ))}
      </div>
      <button onClick={() => (authed ? onCreate() : onRequireAuth?.())} className="edge-press" style={primaryBtn}>
        <Icon name="plus" size={13} stroke={2}/>新建 Compose
      </button>
    </div>
  );
}

function TaskBanner({ task, label }) {
  const color = task.status === 'failed' ? '#dc2626' : task.status === 'succeeded' ? '#16a34a' : '#0066ff';
  return (
    <div style={{ marginTop: 12, padding: '10px 14px', borderRadius: 10, background: '#fff', border: `1px solid ${color}33` }}>
      <span style={{ color, fontWeight: 600 }}>{label}</span>
      <span style={{ color: T.ink3, marginLeft: 8, fontSize: 13 }}>
        {task.status} · {task.phase || ''} {task.message ? `· ${task.message}` : ''}
      </span>
    </div>
  );
}

// ─── AppCard：后端 phase 驱动 ────────────────────────────────────
function AppCard({ app, storeApps, catalogApps, disabled, onAction, onUninstall, onOpenApp }) {
  const phase = observedPhase(app);
  const isCompose = (app.runtime || 'kubernetes') === 'compose';
  const running = phase === 'running';
  const origin = appOrigin(app);
  const upgrade = computeUpgrade(app, storeApps, catalogApps);
  const services = app.observed?.services || [];
  const endpoints = app.observed?.endpoints || [];
  const lastTask = app.lastTask;

  return (
    <div style={card}>
      {/* Row 1: phase + name + badges */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
        <StatusDot tone={PHASE_TONE[phase] || 'gray'} size={9} pulse={phase === 'failed' || phase === 'degraded'}/>
        <strong style={{ color: T.ink, fontSize: 14 }}>{app.name}</strong>
        <span style={runtimeBadge(isCompose)} title={runtimeLabel(app.runtime)}>
          {isCompose ? 'Compose' : 'K8s'}
        </span>
        <span style={{ fontSize: 12, color: T.ink3, fontWeight: 600 }}>{PHASE_LABEL[phase] || phase}</span>
        <Chip tone={origin.tone}>{origin.label}</Chip>
        {app.version && <span style={{ fontSize: 11, color: T.ink4 }} className="mono">{app.version}</span>}
        <span style={{ flex: 1 }} />
        {/* runtime + service 数 */}
        <span style={{ fontSize: 11.5, color: T.ink3 }}>
          {services.length > 0
            ? `${services.length} 服务 · ${app.ready || 0}/${app.replicas || services.length} 就绪`
            : (isCompose ? '0 服务' : `${app.ready || 0}/${app.replicas || 0} Pod`)}
        </span>
      </div>

      {/* Row 2: image / endpoints */}
      <div style={{ fontSize: 12, color: T.ink3, marginTop: 6, display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
        <span className="mono" style={{ color: T.ink4 }}>{app.image || '—'}</span>
        {endpoints.length > 0 && endpoints.map((e, i) => (
          <a key={i} href={e.url} target="_blank" rel="noreferrer"
             title={e.name || e.url}
             style={{ display: 'inline-flex', alignItems: 'center', gap: 3, color: T.blue, textDecoration: 'none',
               fontSize: 11.5, padding: '1px 7px', borderRadius: 4, background: T.blueSoft, border: '1px solid #99c7ff' }}>
            <Icon name="external" size={10} stroke={2}/>{e.name || openHost(e.url)}
          </a>
        ))}
        {upgrade.upgradable && (
          <span title={`已装 ${upgrade.installedVersion || '—'} → 可用 ${upgrade.latestVersion}`}
                style={{ fontSize: 10.5, padding: '1px 6px', borderRadius: 3, fontWeight: 600,
                  background: '#fffbeb', color: '#b45309', border: '1px solid #fde68a' }}>
            可升级 → {upgrade.latestVersion}
          </span>
        )}
      </div>

      {/* Row 3: 最近 operation */}
      {lastTask && (
        <div style={{ fontSize: 11, color: T.ink4, marginTop: 5 }}>
          最近：{taskLabel(lastTask)} · {lastTask.status || '—'} · {formatRelativeTime(lastTask.createdAt || lastTask.finishedAt)}
        </div>
      )}

      {/* Row 4: actions */}
      <div style={{ display: 'flex', gap: 8, marginTop: 10, flexWrap: 'wrap' }}>
        <Btn disabled={disabled || running} onClick={() => onAction('start', '启动')} title="启动">启动</Btn>
        <Btn disabled={disabled || !running} onClick={() => onAction('stop', '停止')} title="停止">停止</Btn>
        <Btn disabled={disabled} onClick={() => onAction('restart', '重启')} title="重启">重启</Btn>
        {isCompose && <Btn disabled={disabled} onClick={() => onAction('redeploy', '重部署')} title="重新部署">重部署</Btn>}
        <div style={{ flex: 1 }} />
        {onOpenApp && <Btn onClick={() => onOpenApp(app)} title="打开详情">详情</Btn>}
        <button onClick={onUninstall} disabled={disabled}
          className="edge-press edge-btn-danger"
          style={{ ...btnDanger, height: 28, padding: '0 10px', fontSize: 12 }}>
          <Icon name="trash" size={11} stroke={2}/>卸载
        </button>
      </div>
    </div>
  );
}

// SystemCard：系统工具（来自 SYSTEM_APPS），仅启动，无生命周期
function SystemCard({ app, onOpen }) {
  return (
    <button onClick={onOpen} className="edge-press" style={{
      textAlign: 'left', cursor: 'pointer',
      padding: '12px 14px', borderRadius: 10, background: '#fff',
      border: `1px solid ${T.border}`, display: 'flex', alignItems: 'center', gap: 10,
    }}>
      <div style={{
        width: 36, height: 36, borderRadius: 9, background: app.bg, color: 'white',
        display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0,
      }}>
        <Icon name={app.icon} size={18} stroke={1.7}/>
      </div>
      <div style={{ minWidth: 0, flex: 1 }}>
        <div style={{ fontSize: 13, fontWeight: 600, color: T.ink }}>{app.name}</div>
        <div style={{ fontSize: 11, color: T.ink3 }}>系统工具 · 本地内置</div>
      </div>
      <Icon name="chevRight" size={14} stroke={2} style={{ color: T.ink4 }}/>
    </button>
  );
}

function EmptyState({ composeDown, hasCap, onCreate }) {
  return (
    <div style={emptyBox}>
      <Icon name="apps" size={30} stroke={1.5} style={{ color: T.ink4, marginBottom: 8 }}/>
      <div style={{ fontSize: 14, fontWeight: 600, color: T.ink2 }}>暂无已部署应用</div>
      <div style={{ fontSize: 12, color: T.ink3, marginTop: 6, maxWidth: 420 }}>
        {composeDown
          ? 'Docker Compose 运行时不可用，无法新建 Compose 应用；K8s 应用将由云端下发后显示在此。'
          : '点击右上角「新建 Compose」粘贴或上传一个 compose.yaml，或从应用商店安装。'}
      </div>
      {!composeDown && (
        <button onClick={onCreate} className="edge-press" style={{ ...primaryBtn, marginTop: 12 }}>
          <Icon name="plus" size={13} stroke={2}/>新建 Compose
        </button>
      )}
      {!hasCap && <div style={{ fontSize: 11, color: T.ink4, marginTop: 8 }}>正在检测本机运行时能力…</div>}
    </div>
  );
}

function Btn({ children, onClick, disabled, danger, title }) {
  return (
    <button onClick={onClick} disabled={disabled} title={title}
      className={`edge-press ${danger ? 'edge-btn-danger' : 'edge-btn-secondary'}`}
      style={danger ? { ...btnDanger, height: 28, padding: '0 10px', fontSize: 12 } : { ...btnSecondary, height: 28, padding: '0 10px', fontSize: 12 }}>
      {children}
    </button>
  );
}

function taskLabel(t) {
  const map = { apply: '部署/更新', operate: '操作', remove: '卸载', restore: '回滚' };
  const base = map[t?.type] || t?.type || '操作';
  return t?.action ? `${base}·${t.action}` : base;
}

function openHost(url) {
  try { return new URL(url).host || url; } catch { return url; }
}

// ─── 新建 Compose 向导 ──────────────────────────────────────────
const SAMPLE = `services:
  web:
    image: nginx:1.27
    ports: ["8080:80"]
`;

export function CreateDialog({ composeCap, onClose, onOpenStore, onDeployed }) {
  const [source, setSource] = useState('paste'); // 'paste' | 'upload'
  const [name, setName] = useState('');
  const [compose, setCompose] = useState(SAMPLE);
  const [fileName, setFileName] = useState('');
  const [result, setResult] = useState(null);
  const [validating, setValidating] = useState(false);
  const [busy, setBusy] = useState(false);
  const [secrets, setSecrets] = useState(''); // KEY=VALUE 每行（仅写，不回传）
  const [parameters, setParameters] = useState(''); // 非敏感 KEY=VALUE（进 revision）
  const [dataPath, setDataPath] = useState('');
  const [dataTarget, setDataTarget] = useState('');
  const [cpuLimit, setCpuLimit] = useState('');
  const [memoryLimit, setMemoryLimit] = useState('');
  const [autoStart, setAutoStart] = useState(true);
  const [confirmed, setConfirmed] = useState(false);
  const [error, setError] = useState(null);
  const fileInputRef = useRef(null);
  const toast = useToast();

  const composeAvailable = !composeCap || composeCap.available !== false;
  const invalidate = () => { setResult(null); setConfirmed(false); };
  const settings = { dataPath, dataTarget, cpuLimit, memoryLimit, autoStart };

  // 上传：用 File API 真实读文本（不是 mock 文件名）。
  async function onFile(e) {
    const f = e.target.files?.[0];
    if (!f) return;
    if (f.size > 1 * 1024 * 1024) {
      toast.err('文件过大（>1MB），请粘贴或拆分');
      return;
    }
    try {
      const text = await f.text();
      setCompose(text);
      setFileName(f.name);
      setSource('upload');
      setResult(null);
      setConfirmed(false);
      toast.ok(`已读取 ${f.name}（${text.length} 字符）`);
    } catch (err) {
      toast.err(`读取失败：${err.message}`);
    } finally {
      // 允许同名文件再次触发 change
      if (fileInputRef.current) fileInputRef.current.value = '';
    }
  }

  async function onValidate() {
    setResult(null); setConfirmed(false); setError(null);
    setValidating(true);
    try {
      const r = await validateCompose({ compose, name, parameters: parseEnv(parameters), secrets: parseEnv(secrets), settings });
      setResult(r);
      if (r.ok) toast.ok('预检通过');
      else toast.warn('预检发现阻断 / 错误');
    } catch (e) {
      setError(e.message || '预检失败');
      toast.err(`预检失败：${e.message}`);
    } finally {
      setValidating(false);
    }
  }

  const grouped = result ? groupRisks(result.risks) : { blocked: [], confirmation: [], warning: [] };
  const decision = canDeployGivenRisks(grouped, confirmed);
  const blocked = decision.reason === 'blocked';
  const needConfirm = decision.reason === 'confirmation';
  // 部署门槛：compose 可用 + 预检已跑 + 可部署（blocked 禁止；confirmation 需勾选）。
  const validName = !!slugify(name);
  const canDeploy = composeAvailable && validName && !!result && decision.deployable && !busy;

  async function onDeploy() {
    if (!canDeploy) return;
    setBusy(true); setError(null);
    try {
      const secretMap = parseEnv(secrets);
      const desired = {
        name: name.trim(),
        source: { kind: 'inline' },
        compose,
        parameters: parseEnv(parameters),
        secrets: secretMap,
        confirmRisky: confirmed,
        settings,
      };
      const t = await applyComposeApp(desired);
      toast.ok('已提交部署任务');
      onDeployed(t);
    } catch (e) {
      setError(e.message || '部署失败');
      toast.err(`部署失败：${e.message}`);
      setBusy(false);
    }
  }

  return (
    <div style={overlay} onClick={onClose}>
      <div style={dialog} onClick={(e) => e.stopPropagation()}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <Icon name="plus" size={16} stroke={2} style={{ color: T.blue }}/>
          <strong style={{ fontSize: 16, color: T.ink }}>新建 Compose 应用</strong>
          <span style={{ flex: 1 }} />
          <button onClick={onClose} aria-label="关闭" style={ghostBtn}><Icon name="x" size={15} stroke={2}/></button>
        </div>

        {/* 来源选择 */}
        <div style={fieldLabel}>来源</div>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          <SourceTab active={source === 'paste'} onClick={() => { setSource('paste'); setResult(null); setConfirmed(false); }} icon="edit" label="粘贴 YAML"/>
          <SourceTab active={source === 'upload'} onClick={() => fileInputRef.current?.click()} icon="upload" label={fileName || '上传 YAML 文件'}/>
          <input ref={fileInputRef} type="file" accept=".yaml,.yml,text/yaml,text/plain,application/yaml"
                 onChange={onFile} style={{ display: 'none' }}/>
          <button onClick={onOpenStore} className="edge-press edge-btn-secondary"
            style={{ ...btnSecondary, height: 32, padding: '0 12px', fontSize: 12.5 }}>
            <Icon name="store" size={12} stroke={2}/>从应用商店安装
          </button>
        </div>

        <label style={fieldLabel}>应用名称（生成 ID：小写 / 数字 / 连字符）</label>
        <input value={name} onChange={(e) => { setName(e.target.value); invalidate(); }} placeholder="my-app"
               aria-label="应用名称" style={input} />
        <div style={{ fontSize: 11, color: T.ink4, marginTop: 3 }}>
          {name ? `ID: ${slugify(name) || '（需含字母或数字）'}` : '应用名称为必填项，用于生成稳定的应用 ID'}
        </div>

        <label style={fieldLabel}>Compose YAML {source === 'upload' && fileName ? `· ${fileName}` : ''}</label>
        <textarea value={compose} onChange={(e) => { setCompose(e.target.value); setSource('paste'); setResult(null); setConfirmed(false); }}
                  aria-label="Compose YAML" spellCheck={false} style={textarea} rows={9}/>

        <label style={fieldLabel}>Secret（KEY=VALUE 每行；Compose 中请用 ${'{'}KEY{'}'} 引用；仅写入 .env，不回传 / 不入 revision）</label>
        <textarea value={secrets} onChange={(e) => { setSecrets(e.target.value); invalidate(); }} spellCheck={false}
                  style={textarea} rows={3} placeholder="DB_PASSWORD=change-me"/>

        <div style={fieldLabel}>配置（应用到第一个 service；复杂多服务配置请直接编辑 YAML）</div>
        <label style={{ ...fieldLabel, marginTop: 6 }}>非敏感环境变量（KEY=VALUE，每行；写入 .env 并记录到 revision）</label>
        <textarea value={parameters} onChange={(e) => { setParameters(e.target.value); invalidate(); }} spellCheck={false}
                  style={textarea} rows={2} placeholder="HTTP_PORT=8080"/>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
          <div><label style={fieldLabel}>宿主数据路径</label><input value={dataPath} onChange={(e) => { setDataPath(e.target.value); invalidate(); }} placeholder="/srv/my-app" style={input}/></div>
          <div><label style={fieldLabel}>容器挂载点</label><input value={dataTarget} onChange={(e) => { setDataTarget(e.target.value); invalidate(); }} placeholder="/data" style={input}/></div>
          <div><label style={fieldLabel}>CPU 限制</label><input value={cpuLimit} onChange={(e) => { setCpuLimit(e.target.value); invalidate(); }} placeholder="1.5" style={input}/></div>
          <div><label style={fieldLabel}>内存限制</label><input value={memoryLimit} onChange={(e) => { setMemoryLimit(e.target.value); invalidate(); }} placeholder="512M" style={input}/></div>
        </div>
        <label style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 10, fontSize: 12, color: T.ink2, cursor: 'pointer' }}>
          <input type="checkbox" checked={autoStart} onChange={(e) => { setAutoStart(e.target.checked); invalidate(); }}/>
          <span>Docker 启动后自动恢复应用（restart: unless-stopped）</span>
        </label>
        {dataPath && <div style={{ fontSize: 11, color: T.ink4, marginTop: 4 }}>绝对宿主路径会在预检中标为 confirmation，需明确确认数据与权限边界。</div>}

        <div style={{ display: 'flex', gap: 8, marginTop: 10, alignItems: 'center', flexWrap: 'wrap' }}>
          <button onClick={onValidate} disabled={validating || !composeAvailable || !compose.trim()}
                  className="edge-press edge-btn-secondary" style={{ ...ghostBtn, opacity: (validating || !composeAvailable) ? 0.6 : 1 }}>
            <Icon name="check" size={12} stroke={2}/>{validating ? '预检中…' : '预检'}
          </button>
          <button onClick={onDeploy} disabled={!canDeploy}
                  className="edge-press edge-btn-primary" style={{ ...primaryBtn, opacity: canDeploy ? 1 : 0.6 }}>
            <Icon name="download" size={12} stroke={2}/>{busy ? '部署中…' : '部署'}
          </button>
          {blocked && <span style={{ color: '#dc2626', fontSize: 12 }}>存在阻断级风险，无法部署</span>}
          {!validName && <span style={{ color: '#dc2626', fontSize: 12 }}>请填写有效的应用名称</span>}
          {!composeAvailable && <span style={{ color: '#dc2626', fontSize: 12 }}>Compose 运行时不可用</span>}
        </div>

        {error && <div style={errBox}>{error}</div>}
        {result && <ValidateResultView result={result} grouped={grouped}
          needConfirm={needConfirm} confirmed={confirmed} setConfirmed={setConfirmed} />}
      </div>
    </div>
  );
}

function SourceTab({ active, onClick, icon, label }) {
  return (
    <button onClick={onClick} className={`edge-press ${active ? 'edge-btn-primary' : 'edge-btn-secondary'}`}
      style={active ? { ...primaryBtn, height: 32, padding: '0 12px', fontSize: 12.5 } : { ...btnSecondary, height: 32, padding: '0 12px', fontSize: 12.5 }}>
      <Icon name={icon} size={12} stroke={2}/>{label}
    </button>
  );
}

// 预检结果：services（name/image/ports/volumes）+ 网络 + 风险分级 + 错误/警告。
function ValidateResultView({ result, grouped, needConfirm, confirmed, setConfirmed }) {
  const allPorts = (result.services || []).flatMap((s) => s.ports || []);
  const allVolumes = (result.services || []).flatMap((s) => s.volumes || []);
  return (
    <div style={{ marginTop: 12, fontSize: 12.5, color: T.ink2 }}>
      {/* 摘要：services / images / ports / volumes */}
      <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', marginBottom: 8 }}>
        <SummaryPill icon="apps" label="服务" value={(result.services || []).length} tone="blue"/>
        <SummaryPill icon="download" label="镜像" value={(result.services || []).length} tone="gray"/>
        <SummaryPill icon="port" label="端口" value={allPorts.length} tone="blue"/>
        <SummaryPill icon="database" label="卷" value={allVolumes.length} tone="gray"/>
        <SummaryPill icon="network" label="网络" value={(result.networks || []).length} tone="gray"/>
        <SummaryPill icon="lock" label="Secret" value={(result.secrets || []).length} tone="gray"/>
      </div>

      {/* 服务明细 */}
      {result.services?.length > 0 && (
        <div style={{ ...pane, marginBottom: 8 }}>
          {(result.services || []).map((s, i) => (
            <div key={i} style={{ padding: '6px 0', borderTop: i ? `1px solid ${T.borderSoft}` : 'none' }}>
              <div style={{ fontWeight: 600, color: T.ink }}>{s.name}
                <span className="mono" style={{ fontWeight: 400, color: T.ink3, marginLeft: 8 }}>{s.image || '—'}</span>
              </div>
              {(s.ports?.length || s.volumes?.length) > 0 && (
                <div style={{ fontSize: 11, color: T.ink4, marginTop: 2 }}>
                  {s.ports?.length ? `端口 ${s.ports.join(', ')} ` : ''}
                  {s.volumes?.length ? `卷 ${s.volumes.join(', ')}` : ''}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
      {(result.networks?.length > 0 || result.secrets?.length > 0) && (
        <div style={{ ...pane, marginBottom: 8, fontSize: 11.5, color: T.ink3 }}>
          {result.networks?.length > 0 && <div>网络：<span className="mono">{result.networks.join(", ")}</span></div>}
          {result.secrets?.length > 0 && <div style={{ marginTop: 3 }}>Secret keys：<span className="mono">{result.secrets.join(", ")}</span>（值不回显）</div>}
        </div>
      )}

      {/* 阻断 / 需确认 / 警告 / 安全 */}
      <RiskList tone="#dc2626" label="阻断（禁止部署）" items={grouped.blocked}/>
      <RiskList tone="#d97706" label="需确认" items={grouped.confirmation}/>
      <RiskList tone="#d97706" label="警告" items={grouped.warning}/>

      {/* 风险摘要 */}
      {result.ok && grouped.blocked.length === 0 && grouped.confirmation.length === 0 && grouped.warning.length === 0 && (
        <div style={{ ...pane, color: '#166534', background: '#f0fdf4', border: '1px solid #bbf7d0' }}>
          <Icon name="check" size={12} stroke={2} style={{ verticalAlign: '-2px' }}/> 无风险项（safe）— 可部署
        </div>
      )}

      {result.errors?.length > 0 && (
        <div style={{ color: '#dc2626', marginTop: 6 }}>错误：{result.errors.join('；')}</div>
      )}
      {result.warnings?.length > 0 && (
        <div style={{ marginTop: 6, fontSize: 11.5, color: '#92400e' }}>
          {result.warnings.map((w, i) => <div key={i}>⚠ {w}</div>)}
        </div>
      )}

      {/* 显式确认 confirmation（允许 override；blocked 不可）*/}
      {needConfirm && (
        <label style={{ display: 'flex', alignItems: 'flex-start', gap: 8, marginTop: 10, cursor: 'pointer',
          padding: 10, borderRadius: 8, background: '#fffbeb', border: '1px solid #fde68a' }}>
          <input type="checkbox" checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)} style={{ marginTop: 2 }}/>
          <span style={{ fontSize: 12, color: '#92400e' }}>
            我已知晓上述「需确认」级风险（敏感 capability / IPC 共享等），确认以此配置部署。阻断级风险不可 override。
          </span>
        </label>
      )}
    </div>
  );
}

function SummaryPill({ icon, label, value, tone }) {
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: 4,
      fontSize: 11, padding: '2px 8px', borderRadius: 999,
      background: tone === 'blue' ? '#e6f4ff' : T.surfaceAlt,
      color: tone === 'blue' ? '#005eeb' : T.ink3,
      border: `1px solid ${tone === 'blue' ? '#99c7ff' : T.borderSoft}`,
    }}>
      <Icon name={icon} size={11} stroke={2}/>{label} <strong className="mono tnum" style={{ color: T.ink }}>{value}</strong>
    </span>
  );
}

function RiskList({ tone, label, items }) {
  if (!items || items.length === 0) return null;
  return (
    <div style={{ marginTop: 6 }}>
      <div style={{ fontSize: 11.5, fontWeight: 700, color: tone, marginBottom: 3 }}>{label} · {items.length}</div>
      {items.map((r, i) => (
        <div key={i} style={{ color: tone, fontSize: 11.5, paddingLeft: 10, borderLeft: `2px solid ${tone}55`, marginBottom: 2 }}>
          {r.service ? <strong>{r.service}：</strong> : null}{r.message}
          {r.field && <span style={{ color: T.ink4 }}>（{r.field}）</span>}
        </div>
      ))}
    </div>
  );
}

// ─── DiscoveredCard：外部 compose project，只读，引导接管 ───────
// 一眼可辨「已发现 · 只读」；隐藏全部写操作（不是灰掉）；展示来源路径诊断（等宽 +
// ellipsis + title，不撑破卡片）；不可接管时显示原因；说明「完全 down 不可发现」边界。
function DiscoveredCard({ app, disabled, onTakeover }) {
  const phase = observedPhase(app);
  const di = app.discovered || {};
  const services = app.observed?.services || [];
  const endpoints = app.observed?.endpoints || [];
  const canTakeover = !!di.takeoverAvailable;
  const cfgCount = Array.isArray(di.configFiles) ? di.configFiles.length : 0;

  return (
    <div style={{ ...card, border: `1px dashed ${T.border}`, background: '#fcfcfd' }}>
      {/* Row 1: 只读标识 + project + phase */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
        <span style={discoveredBadge} title="由 Docker Compose 创建、尚未由 devbox 接管的外部 project">
          <Icon name="eye" size={11} stroke={2}/>已发现 · 只读
        </span>
        <strong style={{ color: T.ink, fontSize: 14 }}>{app.name}</strong>
        <span style={runtimeBadge(true)} title={runtimeLabel('compose')}>Compose</span>
        <span style={{ fontSize: 12, color: T.ink3, fontWeight: 600 }}>{PHASE_LABEL[phase] || phase}</span>
        <span style={{ flex: 1 }} />
        <span className="tnum" style={{ fontSize: 11.5, color: T.ink3 }}>
          {services.length > 0
            ? `${services.length} 服务 · ${app.ready || 0}/${app.replicas || services.length} 就绪`
            : '0 服务'}
        </span>
      </div>

      {/* Row 2: 来源路径诊断（等宽 + ellipsis；只展示路径字符串，不读取内容）+ endpoints */}
      <div style={{ fontSize: 12, color: T.ink3, marginTop: 6, display: 'flex', flexDirection: 'column', gap: 3 }}>
        {di.workingDir && (
          <div style={pathRow} title={`工作目录：${di.workingDir}`}>
            <span style={{ color: T.ink4 }}>工作目录</span>
            <span className="mono" style={pathText}>{di.workingDir}</span>
          </div>
        )}
        {cfgCount > 0 && (
          <div style={pathRow} title={`配置文件（${cfgCount}）：${(di.configFiles || []).join(', ')}`}>
            <span style={{ color: T.ink4 }}>配置文件</span>
            <span className="mono" style={pathText}>{cfgCount} 个</span>
          </div>
        )}
        {endpoints.length > 0 && (
          <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
            {endpoints.map((e, i) => (
              <a key={i} href={e.url} target="_blank" rel="noreferrer" title={e.name || e.url}
                 style={{ display: 'inline-flex', alignItems: 'center', gap: 3, color: T.blue, textDecoration: 'none',
                   fontSize: 11.5, padding: '1px 7px', borderRadius: 4, background: T.blueSoft, border: '1px solid #99c7ff' }}>
                <Icon name="external" size={10} stroke={2}/>{e.name || openHost(e.url)}
              </a>
            ))}
          </div>
        )}
      </div>

      {/* 只读服务清单（名称/state/health/端口；tnum 动态数字；长列表截断；无写操作） */}
      <ServiceInventory app={app} />

      {/* 不可接管原因（不含文件内容） */}
      {!canTakeover && di.reason && (
        <div style={{ fontSize: 11.5, color: '#92400e', marginTop: 8, padding: '6px 9px',
          background: '#fffbeb', border: '1px solid #fde68a', borderRadius: 7 }}>
          暂不可接管：{di.reason}
        </div>
      )}

      {/* 完全 down 边界提示 */}
      <div style={{ fontSize: 11, color: T.ink4, marginTop: 8 }}>
        列表仅显示仍有容器记录的 project；若已 <span className="mono">compose down</span> 且容器记录被删除，则无法自动发现，可改用「新建 Compose」粘贴/上传导入。
      </div>

      {/* Row 4: 接管 CTA（min 40px hit area，edge-press）；无任何写操作，不开详情 drawer
          （避免 AppMgmtDrawer 暴露编辑/生命周期/卸载入口；只读诊断已内联在卡片上） */}
      <div style={{ display: 'flex', gap: 8, marginTop: 10, alignItems: 'center' }}>
        <button onClick={onTakeover} disabled={disabled || !canTakeover}
          className="edge-press edge-btn-primary" aria-label={`接管并编辑 ${app.name}`}
          style={{ ...primaryBtn, minHeight: 40, opacity: (disabled || !canTakeover) ? 0.6 : 1 }}>
          <Icon name="download" size={13} stroke={2}/>接管并编辑
        </button>
        {!canTakeover && (
          <span style={{ fontSize: 11.5, color: T.ink4 }}>解决上述问题后即可接管</span>
        )}
      </div>
    </div>
  );
}

// ServiceInventory：discovered 卡片的只读服务清单（名称/state/health/端口）。
// 动态数字用 tnum；长列表截断前若干项 + 「还有 N 个服务」。无写操作。
function ServiceInventory({ app }) {
  const { rows, total, truncated } = serviceInventory(app);
  if (total === 0) {
    return <div style={{ fontSize: 11.5, color: T.ink4, marginTop: 6 }}>0 服务</div>;
  }
  return (
    <div style={{ marginTop: 6, display: 'flex', flexDirection: 'column', gap: 3 }}>
      {rows.map((s, i) => (
        <div key={i} style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 11.5, minWidth: 0 }}>
          <span style={{ color: T.ink2, fontWeight: 600, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: 160 }} title={s.name}>{s.name}</span>
          {s.state && <span className="tnum" style={{ color: T.ink4 }}>{s.state}</span>}
          {s.health && s.health !== 'none' && (
            <span style={{ fontSize: 10.5, padding: '0 6px', borderRadius: 999,
              background: s.health === 'healthy' ? '#ecfdf5' : s.health === 'unhealthy' ? '#fef2f2' : '#fffbeb',
              color: s.health === 'healthy' ? '#047857' : s.health === 'unhealthy' ? '#b91c1c' : '#92400e',
              border: `1px solid ${s.health === 'healthy' ? '#a7f3d0' : s.health === 'unhealthy' ? '#fecaca' : '#fde68a'}` }}>
              {HEALTH_LABEL[s.health] || s.health}
            </span>
          )}
          {s.ports.length > 0 && (
            <span className="mono tnum" style={{ color: T.ink3, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={s.ports.join(', ')}>
              {s.ports.join(', ')}
            </span>
          )}
        </div>
      ))}
      {truncated && <div className="tnum" style={{ fontSize: 11, color: T.ink4 }}>还有 {total - rows.length} 个服务…</div>}
    </div>
  );
}

// TakeoverDialog：接管确认弹层（Esc 关闭 / 初始聚焦 / aria-label；不引入新 motion）。
function TakeoverDialog({ app, onClose, onDone }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const [findings, setFindings] = useState(null); // risk_blocked 时展示
  const [confirmed, setConfirmed] = useState(false);
  const confirmRef = useRef(null);
  const di = app.discovered || {};
  const grouped = findings ? groupRisks(findings) : { blocked: [], confirmation: [], warning: [] };
  const needConfirm = grouped.confirmation.length > 0 && !confirmed;
  const blocked = grouped.blocked.length > 0;
  const canSubmit = !busy && !blocked && !needConfirm;

  // 稳定关闭：busy 时（请求在途）禁止关闭，避免请求状态漂移。busy 同步到 ref（在 effect 中写，
  // 不在 render 中写），Esc 与关闭按钮经 closeGuarded 读 ref 守卫。
  const busyRef = useRef(false);
  useEffect(() => { busyRef.current = busy; }, [busy]);
  const closeGuarded = () => { if (!busyRef.current) onClose(); };

  // 初始聚焦 + 焦点回归（mount/unmount 各一次）。
  useEffect(() => {
    const prev = document.activeElement;
    confirmRef.current?.focus();
    return () => {
      if (prev && typeof prev.focus === 'function') {
        try { prev.focus(); } catch { /* 焦点回归 best-effort */ }
      }
    };
  }, []);

  // Esc 关闭（经 busy 守卫）+ Tab/Shift+Tab focus trap（循环限定在 dialogRef 内，busy 不影响键盘）。
  // 依赖 onClose；onClose 变化只重建 listener，不重新聚焦。
  const dialogRef = useRef(null);
  useEffect(() => {
    const onKey = (e) => {
      if (e.key === 'Escape') {
        if (!busyRef.current) onClose();
        return;
      }
      if (e.key !== 'Tab') return;
      const root = dialogRef.current;
      if (!root) return;
      const focusable = root.querySelectorAll(
        'button:not([disabled]), input:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])',
      );
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  async function submit() {
    if (!canSubmit) return;
    setBusy(true); setError(null); setFindings(null);
    try {
      await takeoverApp(app.id, confirmed, `takeover-${app.id}`);
      onDone(true);
    } catch (e) {
      // 风险阻断（422）携带 findings；其它错误展示 message。
      setFindings(e.findings || null);
      setError(e.message || '接管失败');
      setBusy(false);
    }
  }

  return (
    <div style={overlay} onClick={closeGuarded}>
      <div ref={dialogRef} style={dialog} role="dialog" aria-modal="true"
        aria-label={`接管 Compose project ${app.name}`} onClick={(e) => e.stopPropagation()}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <Icon name="download" size={16} stroke={2} style={{ color: T.blue }}/>
          <strong style={{ fontSize: 16, color: T.ink }}>接管并编辑「{app.name}」</strong>
          <span style={{ flex: 1 }} />
          <button ref={confirmRef} onClick={closeGuarded} disabled={busy} aria-disabled={busy}
            aria-label="关闭接管对话框" style={ghostBtn}>
            <Icon name="x" size={15} stroke={2}/>
          </button>
        </div>

        <div style={{ fontSize: 12.5, color: T.ink2, marginTop: 12, lineHeight: 1.6 }}>
          接管会把该外部 Compose project 转为 devbox 受管：
          <ul style={{ margin: '6px 0 0 18px', padding: 0 }}>
            <li>保留原 project name <span className="mono">{di.project || app.name}</span> 原地管理，容器/网络/named volume 不改名（数据不变）。</li>
            <li>源目录只读：把归一化后的 compose 写入 devbox 托管副本，不修改你的原始文件。</li>
            <li>接管后进入编辑 / 版本 / 日志 / 生命周期 / 卸载边界。</li>
          </ul>
        </div>

        {di.workingDir && (
          <div style={pathRow} title={di.workingDir}>
            <span style={{ color: T.ink4, fontSize: 12 }}>来源</span>
            <span className="mono" style={pathText}>{di.workingDir}</span>
          </div>
        )}

        {error && <div style={errBox}>{error}</div>}
        {findings && (
          <>
            <RiskList tone="#dc2626" label="阻断（不可接管）" items={grouped.blocked}/>
            <RiskList tone="#d97706" label="需确认" items={grouped.confirmation}/>
            <RiskList tone="#d97706" label="警告" items={grouped.warning}/>
          </>
        )}
        {needConfirm && (
          <label style={{ display: 'flex', alignItems: 'flex-start', gap: 8, marginTop: 10, cursor: 'pointer',
            padding: 10, borderRadius: 8, background: '#fffbeb', border: '1px solid #fde68a' }}>
            <input type="checkbox" checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)} style={{ marginTop: 2 }}/>
            <span style={{ fontSize: 12, color: '#92400e' }}>
              我已知晓上述「需确认」级风险，确认接管。阻断级风险不可 override。
            </span>
          </label>
        )}

        <div style={{ display: 'flex', gap: 8, marginTop: 14, alignItems: 'center' }}>
          <button onClick={submit} disabled={!canSubmit}
            className="edge-press edge-btn-primary" aria-label="确认接管"
            style={{ ...primaryBtn, minHeight: 40, opacity: canSubmit ? 1 : 0.6 }}>
            <Icon name="check" size={13} stroke={2}/>{busy ? '接管中…' : '确认接管'}
          </button>
          <button onClick={closeGuarded} className="edge-press edge-btn-secondary"
            style={{ ...btnSecondary, minHeight: 40, padding: '0 14px' }}>
            取消
          </button>
        </div>
      </div>
    </div>
  );
}

// ─── styles ───────────────────────────────────────────────────────
const card = {
  padding: '13px 16px', borderRadius: 12, background: '#fff',
  border: `1px solid ${T.border}`, boxShadow: '0 1px 2px rgba(15,23,42,0.04)',
};
const chip = {
  padding: '5px 11px', fontSize: 12.5, borderRadius: 999, border: `1px solid ${T.border}`,
  background: '#fff', color: T.ink2, cursor: 'pointer',
};
const chipActive = { ...chip, background: T.blue, color: '#fff', borderColor: T.blue };
const primaryBtn = {
  display: 'inline-flex', alignItems: 'center', gap: 6,
  padding: '7px 14px', fontSize: 13, fontWeight: 600, borderRadius: 8, cursor: 'pointer',
  border: `1px solid ${T.blueDeep}`, background: T.blue, color: '#fff',
};
const ghostBtn = {
  display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
  padding: '6px 10px', fontSize: 12.5, borderRadius: 8, cursor: 'pointer',
  border: `1px solid ${T.border}`, background: '#fff', color: T.ink2,
};
const warnBox = {
  marginTop: 12, padding: '10px 14px', borderRadius: 10, background: '#fffbeb',
  border: '1px solid #fde68a', color: '#92400e', fontSize: 13,
};
const emptyBox = {
  padding: 36, textAlign: 'center', color: T.ink3, fontSize: 13,
  border: `1px dashed ${T.border}`, borderRadius: 12, background: '#fff',
};
const overlay = {
  position: 'fixed', inset: 0, background: 'rgba(15,23,42,0.5)', zIndex: 100,
  display: 'flex', alignItems: 'center', justifyContent: 'center', backdropFilter: 'blur(2px)',
};
const dialog = {
  width: 'min(760px, 94vw)', maxHeight: '90vh', overflow: 'auto', background: '#fff',
  borderRadius: 14, padding: 22, boxShadow: '0 12px 40px rgba(15,23,42,0.25)',
};
const fieldLabel = { display: 'block', fontSize: 12, fontWeight: 600, color: T.ink3, marginTop: 12, marginBottom: 5 };
const input = { width: '100%', padding: '8px 10px', borderRadius: 8, border: `1px solid ${T.border}`, fontSize: 13, boxSizing: 'border-box' };
const textarea = { ...input, fontFamily: 'ui-monospace, monospace', resize: 'vertical' };
const pane = { padding: '8px 10px', borderRadius: 7, background: T.surfaceAlt, border: `1px solid ${T.borderSoft}` };
const errBox = { marginTop: 10, padding: '8px 10px', borderRadius: 7, background: '#fef2f2', border: '1px solid #fecaca', fontSize: 12, color: '#b91c1c' };

function runtimeBadge(isCompose) {
  return {
    fontSize: 11, padding: '2px 8px', borderRadius: 999, fontWeight: 600,
    background: isCompose ? '#ecfeff' : '#eef2ff', color: isCompose ? '#0e7490' : '#3730a3',
    border: `1px solid ${isCompose ? '#a5f3fc' : '#c7d2fe'}`,
  };
}
const discoveredBadge = {
  display: 'inline-flex', alignItems: 'center', gap: 4,
  fontSize: 11, padding: '2px 9px', borderRadius: 999, fontWeight: 700,
  background: '#fff7ed', color: '#9a3412', border: '1px solid #fed7aa',
};
// 路径诊断行：等宽 + ellipsis + title，不撑破卡片。
const pathRow = {
  display: 'flex', alignItems: 'center', gap: 8, minWidth: 0,
  fontSize: 11.5, marginTop: 4,
};
const pathText = {
  color: T.ink3, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis',
  whiteSpace: 'nowrap',
};
