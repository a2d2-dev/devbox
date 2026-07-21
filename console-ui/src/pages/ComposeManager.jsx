// ComposeManager — Docker Compose 应用管理入口（Issue #2 MVP UI）。
//
// 刻意做成独立页面，不重写 Desktop / AppMgmtDrawer：
//   - 列表按 runtime（全部 / Compose / Kubernetes）筛选
//   - 生命周期：启动 / 停止 / 重启 / 重部署 / 卸载（默认保留数据，purge 显式）
//   - 新建：粘贴 inline Compose → 预检（服务/镜像/风险）→ 部署
//   - 任务进度：写操作返回 202+Task，前端轮询并反馈
//   - 受管应用以事实源 compose.yaml 驱动；external volume 永不删（后端保证）
import { useState, useEffect, useMemo } from 'react';
import { T } from '../tokens';
import {
  useApps, useAppCapability, useTask, appActionAsync, removeAppEx,
  validateCompose, applyComposeApp,
} from '../hooks/useApi';
import { useToast } from '../components/toastContext';

const PHASE_LABEL = {
  running: '运行中', stopped: '已停止', degraded: '降级', deploying: '部署中',
  failed: '失败', pending: '等待', removing: '卸载中', unknown: '未知',
};
const PHASE_COLOR = {
  running: '#16a34a', stopped: '#64748b', degraded: '#d97706', deploying: '#2563eb',
  failed: '#dc2626', pending: '#2563eb', removing: '#7c3aed', unknown: '#94a3b8',
};

function observedPhase(app) {
  return app?.observed?.phase || app?.state || 'unknown';
}

export function ComposeManager({ authed, onRequireAuth }) {
  const { data: apps, refresh } = useApps(5000);
  const { data: cap } = useAppCapability();
  const [filter, setFilter] = useState('all');
  const [showCreate, setShowCreate] = useState(false);
  const [activeTask, setActiveTask] = useState(null); // {id, label, appId}
  const toast = useToast();

  const { task } = useTask(activeTask?.id);
  useEffect(() => {
    if (!task || !activeTask) return;
    if (['succeeded', 'failed', 'canceled', 'superseded'].includes(task.status)) {
      if (task.status === 'succeeded') toast.ok(`${activeTask.label} 完成`);
      else toast.err(`${activeTask.label} 失败：${task.message || task.status}`);
      const t = setTimeout(() => setActiveTask(null), 1500);
      refresh();
      return () => clearTimeout(t);
    }
  }, [task, activeTask, toast, refresh]);

  const composeCap = cap?.compose;
  const composeDown = composeCap && composeCap.available === false;

  const list = useMemo(() => {
    const arr = Array.isArray(apps) ? apps : [];
    return filter === 'all' ? arr : arr.filter((a) => (a.runtime || 'kubernetes') === filter);
  }, [apps, filter]);

  function guard(fn) {
    return async (...args) => {
      if (!authed) { onRequireAuth?.(); return; }
      return fn(...args);
    };
  }

  async function doAction(appId, action, label) {
    try {
      const t = await appActionAsync(appId, action);
      setActiveTask({ id: t.id, label, appId });
      toast.ok(`${label} 已提交`);
    } catch (e) { toast.err(`${label} 失败：${e.message}`); }
  }

  async function doRemove(app, purge) {
    const confirmText = purge
      ? `确认卸载「${app.name}」并删除其受管数据？external volume 不会被删除。`
      : `确认卸载「${app.name}」？（默认保留数据）`;
    if (!window.confirm(confirmText)) return;
    try {
      const t = await removeAppEx(app.id, purge);
      // 兼容同步接口：返回 {status, taskId?}；若有 taskId 则跟踪，否则直接刷新。
      if (t && t.taskId) setActiveTask({ id: t.taskId, label: '卸载', appId: app.id });
      else { toast.ok('已卸载'); refresh(); }
    } catch (e) { toast.err(`卸载失败：${e.message}`); }
  }

  return (
    <div style={{ padding: 28, height: '100%', overflow: 'auto', background: T.bg || '#f8fafc' }}>
      <Header
        composeCap={composeCap}
        filter={filter}
        setFilter={setFilter}
        onCreate={() => (authed ? setShowCreate(true) : onRequireAuth?.())}
      />

      {activeTask && task && (
        <TaskBanner task={task} label={activeTask.label} />
      )}

      {composeDown && (
        <div style={warnBox}>
          Docker Compose 运行时不可用：{composeCap.reason}。K8s 应用不受影响。
        </div>
      )}

      <div style={{ display: 'grid', gap: 12, marginTop: 16 }}>
        {list.length === 0 && (
          <div style={emptyBox}>暂无应用。点击右上角「新建 Compose」粘贴一个 compose.yaml 试试。</div>
        )}
        {list.map((app) => (
          <AppCard
            key={app.id}
            app={app}
            disabled={!!activeTask}
            onAction={(action, label) => guard(doAction)(app.id, action, label)}
            onRemove={(purge) => guard(doRemove)(app, purge)}
          />
        ))}
      </div>

      {showCreate && (
        <CreateDialog
          onClose={() => setShowCreate(false)}
          onDeployed={(t) => {
            setShowCreate(false);
            setActiveTask({ id: t.id, label: '部署', appId: t.appId });
            refresh();
          }}
        />
      )}
    </div>
  );
}

function Header({ composeCap, filter, setFilter, onCreate }) {
  const filters = [
    { id: 'all', label: '全部' },
    { id: 'compose', label: 'Docker Compose' },
    { id: 'kubernetes', label: 'Kubernetes' },
  ];
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
      <div style={{ fontSize: 20, fontWeight: 700, color: T.ink1 || '#0f172a' }}>应用管理</div>
      <div style={{ fontSize: 12, color: T.ink3 || '#64748b' }}>
        {composeCap?.available ? `Compose ${composeCap.version || '可用'}` : 'Compose 未就绪'}
      </div>
      <div style={{ flex: 1 }} />
      <div style={{ display: 'flex', gap: 6 }}>
        {filters.map((f) => (
          <button key={f.id} onClick={() => setFilter(f.id)} style={filter === f.id ? chipActive : chip}>
            {f.label}
          </button>
        ))}
      </div>
      <button onClick={onCreate} style={primaryBtn}>+ 新建 Compose</button>
    </div>
  );
}

function TaskBanner({ task, label }) {
  const color = task.status === 'failed' ? '#dc2626' : task.status === 'succeeded' ? '#16a34a' : '#2563eb';
  return (
    <div style={{ marginTop: 14, padding: '10px 14px', borderRadius: 10, background: '#fff', border: `1px solid ${color}33` }}>
      <span style={{ color, fontWeight: 600 }}>{label}</span>
      <span style={{ color: T.ink3 || '#64748b', marginLeft: 8, fontSize: 13 }}>
        {task.status} · {task.phase || ''} {task.message ? `· ${task.message}` : ''}
      </span>
    </div>
  );
}

function AppCard({ app, disabled, onAction, onRemove }) {
  const phase = observedPhase(app);
  const isCompose = (app.runtime || 'kubernetes') === 'compose';
  const running = phase === 'running';
  return (
    <div style={card}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
        <span style={{ ...dot, background: PHASE_COLOR[phase] || '#94a3b8' }} />
        <strong style={{ color: T.ink1 || '#0f172a' }}>{app.name}</strong>
        <span style={runtimeBadge(isCompose)}>{isCompose ? 'Compose' : 'K8s'}</span>
        <span style={{ fontSize: 12, color: T.ink3 || '#64748b' }}>{PHASE_LABEL[phase] || phase}</span>
        <span style={{ flex: 1 }} />
        {app.observed?.services && (
          <span style={{ fontSize: 12, color: T.ink3 || '#64748b' }}>
            {app.observed.services.length} 服务 · {app.ready || 0}/{app.replicas || (app.observed.services?.length || 0)} 就绪
          </span>
        )}
      </div>
      <div style={{ fontSize: 12, color: T.ink3 || '#64748b', marginTop: 6 }}>
        {app.image || '—'}
        {app.observed?.endpoints?.length ? ` · 入口 ${app.observed.endpoints.map((e) => e.url).join(', ')}` : ''}
      </div>
      <div style={{ display: 'flex', gap: 8, marginTop: 10, flexWrap: 'wrap' }}>
        <Btn disabled={disabled || running} onClick={() => onAction('start', '启动')}>启动</Btn>
        <Btn disabled={disabled || !running} onClick={() => onAction('stop', '停止')}>停止</Btn>
        <Btn disabled={disabled} onClick={() => onAction('restart', '重启')}>重启</Btn>
        {isCompose && <Btn disabled={disabled} onClick={() => onAction('redeploy', '重部署')}>重部署</Btn>}
        <Btn danger disabled={disabled} onClick={() => onRemove(false)}>卸载</Btn>
        <Btn danger disabled={disabled} onClick={() => onRemove(true)}>卸载并删数据</Btn>
      </div>
    </div>
  );
}

function Btn({ children, onClick, disabled, danger }) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      style={{
        padding: '5px 10px', fontSize: 12, borderRadius: 7, cursor: disabled ? 'not-allowed' : 'pointer',
        border: `1px solid ${danger ? '#fecaca' : '#e2e8f0'}`, background: danger ? '#fef2f2' : '#fff',
        color: danger ? '#dc2626' : T.ink1 || '#0f172a', opacity: disabled ? 0.5 : 1,
      }}
    >{children}</button>
  );
}

function CreateDialog({ onClose, onDeployed }) {
  const [name, setName] = useState('');
  const [compose, setCompose] = useState(SAMPLE);
  const [result, setResult] = useState(null);
  const [busy, setBusy] = useState(false);
  const [secrets, setSecrets] = useState(''); // KEY=VALUE 每行（仅写，不回传）
  const toast = useToast();

  async function onValidate() {
    setResult(null);
    try {
      const r = await validateCompose({ compose, name });
      setResult(r);
      if (r.ok) toast.ok('预检通过');
      else toast.warn('预检发现阻断/错误');
    } catch (e) { toast.err(`预检失败：${e.message}`); }
  }

  async function onDeploy() {
    setBusy(true);
    try {
      const secretMap = parseEnv(secrets);
      const desired = { name: name || slugify(name), source: { kind: 'inline' }, compose, secrets: secretMap };
      const t = await applyComposeApp(desired);
      toast.ok('已提交部署任务');
      onDeployed(t);
    } catch (e) {
      toast.err(`部署失败：${e.message}`);
      setBusy(false);
    }
  }

  const blocked = result && !result.ok;
  return (
    <div style={overlay} onClick={onClose}>
      <div style={dialog} onClick={(e) => e.stopPropagation()}>
        <div style={{ display: 'flex', alignItems: 'center' }}>
          <strong style={{ fontSize: 16 }}>新建 Compose 应用</strong>
          <span style={{ flex: 1 }} />
          <button onClick={onClose} style={ghostBtn}>关闭</button>
        </div>
        <label style={fieldLabel}>应用名称（生成 ID，小写/数字/连字符）</label>
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-app" style={input} />
        <label style={fieldLabel}>Compose YAML</label>
        <textarea value={compose} onChange={(e) => setCompose(e.target.value)} style={textarea} rows={10} />
        <label style={fieldLabel}>环境变量 / Secret（KEY=VALUE 每行；仅写入 .env，不回传/不入审计）</label>
        <textarea value={secrets} onChange={(e) => setSecrets(e.target.value)} style={textarea} rows={3} placeholder="DB_PASSWORD=change-me" />

        <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
          <button onClick={onValidate} style={ghostBtn}>预检</button>
          <button onClick={onDeploy} disabled={busy || blocked} style={primaryBtn}>
            {busy ? '部署中…' : '部署'}
          </button>
          {blocked && <span style={{ color: '#dc2626', fontSize: 12, alignSelf: 'center' }}>存在阻断级风险，无法部署</span>}
        </div>

        {result && <ValidateResultView result={result} />}
      </div>
    </div>
  );
}

function ValidateResultView({ result }) {
  return (
    <div style={{ marginTop: 12, fontSize: 12.5, color: T.ink2 || '#334155' }}>
      {result.errors?.length > 0 && (
        <div style={{ color: '#dc2626' }}>错误：{result.errors.join('；')}</div>
      )}
      {result.risks?.length > 0 && (
        <div style={{ marginTop: 6 }}>
          {result.risks.map((r, i) => (
            <div key={i} style={{ color: r.level === 'blocked' ? '#dc2626' : '#d97706' }}>
              [{r.level}] {r.service}: {r.message}
            </div>
          ))}
        </div>
      )}
      {result.warnings?.length > 0 && (
        <div style={{ marginTop: 6, color: '#d97706' }}>{result.warnings.join('；')}</div>
      )}
      {result.services?.length > 0 && (
        <div style={{ marginTop: 6 }}>
          服务：{result.services.map((s) => `${s.name}(${s.image})`).join('，')}
        </div>
      )}
    </div>
  );
}

// ─── styles ───────────────────────────────────────────────────────
const card = {
  padding: '14px 16px', borderRadius: 12, background: '#fff',
  border: '1px solid #e2e8f0', boxShadow: '0 1px 2px rgba(15,23,42,0.04)',
};
const dot = { width: 9, height: 9, borderRadius: '50%', display: 'inline-block' };
const chip = {
  padding: '5px 12px', fontSize: 12.5, borderRadius: 999, border: '1px solid #e2e8f0',
  background: '#fff', color: T.ink2 || '#334155', cursor: 'pointer',
};
const chipActive = { ...chip, background: '#2563eb', color: '#fff', borderColor: '#2563eb' };
const primaryBtn = {
  padding: '7px 14px', fontSize: 13, fontWeight: 600, borderRadius: 8, cursor: 'pointer',
  border: 'none', background: '#2563eb', color: '#fff',
};
const ghostBtn = {
  padding: '6px 12px', fontSize: 12.5, borderRadius: 8, cursor: 'pointer',
  border: '1px solid #e2e8f0', background: '#fff', color: T.ink2 || '#334155',
};
const warnBox = {
  marginTop: 14, padding: '10px 14px', borderRadius: 10, background: '#fffbeb',
  border: '1px solid #fde68a', color: '#92400e', fontSize: 13,
};
const emptyBox = {
  padding: 24, textAlign: 'center', color: T.ink3 || '#64748b', fontSize: 13,
  border: '1px dashed #cbd5e1', borderRadius: 12, background: '#fff',
};
const overlay = {
  position: 'fixed', inset: 0, background: 'rgba(15,23,42,0.45)', zIndex: 100,
  display: 'flex', alignItems: 'center', justifyContent: 'center',
};
const dialog = {
  width: 'min(720px, 92vw)', maxHeight: '88vh', overflow: 'auto', background: '#fff',
  borderRadius: 14, padding: 22, boxShadow: '0 12px 40px rgba(15,23,42,0.25)',
};
const fieldLabel = { display: 'block', fontSize: 12, color: T.ink3 || '#64748b', marginTop: 10, marginBottom: 4 };
const input = { width: '100%', padding: '8px 10px', borderRadius: 8, border: '1px solid #e2e8f0', fontSize: 13, boxSizing: 'border-box' };
const textarea = { ...input, fontFamily: 'ui-monospace, monospace', resize: 'vertical' };

function runtimeBadge(isCompose) {
  return {
    fontSize: 11, padding: '2px 8px', borderRadius: 999,
    background: isCompose ? '#ecfeff' : '#eef2ff', color: isCompose ? '#0e7490' : '#3730a3',
  };
}

function slugify(s) {
  return (s || '').toLowerCase().trim().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
}
function parseEnv(text) {
  const out = {};
  for (const line of (text || '').split('\n')) {
    const t = line.trim();
    if (!t || t.startsWith('#')) continue;
    const i = t.indexOf('=');
    if (i > 0) out[t.slice(0, i).trim()] = t.slice(i + 1);
  }
  return out;
}

const SAMPLE = `services:
  web:
    image: nginx:1.27
    ports: ["8080:80"]
`;
