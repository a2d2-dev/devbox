import { useEffect, useMemo, useState } from 'react';
import { T } from '../tokens';
import { Icon } from '../icons';
import { btnPrimary, btnSecondary, btnDanger } from '../components/AppWindow';
import { StatusDot } from '../components/ui';
import { useToast } from '../components/toastContext';
import {
  dockerServiceAction, executeDockerMigration, planDockerMigration, setDockerAutostart,
  useDockerOverview, useDockerStats,
} from '../hooks/useApi';

const EMPTY_HISTORY = { cpu: [], memory: [], rx: [], tx: [], times: [] };

export default function DockerOverview({ authed, onRequireAuth, onOpenCompose }) {
  const [foreground, setForeground] = useState(() => document.visibilityState !== 'hidden');
  const { data: overview, loading, refresh } = useDockerOverview(foreground ? 5000 : 30000);
  const { data: statsData } = useDockerStats(foreground ? 3000 : 30000);
  const [busy, setBusy] = useState('');
  const [operationError, setOperationError] = useState(null);
  const [storageOpen, setStorageOpen] = useState(false);
  const toast = useToast();

  useEffect(() => {
    const update = () => setForeground(document.visibilityState !== 'hidden');
    document.addEventListener('visibilitychange', update);
    return () => document.removeEventListener('visibilitychange', update);
  }, []);

  const stats = statsData?.current;
  const history = statsData?.history || EMPTY_HISTORY;

  const rates = useMemo(() => ({
    rx: rateSeries(history.rx, history.times),
    tx: rateSeries(history.tx, history.times),
  }), [history]);

  function requireAuth(action) {
    if (authed) return true;
    onRequireAuth?.(action);
    return false;
  }

  async function runServiceAction(action) {
    if (!requireAuth(`${serviceActionLabel(action)} Docker`)) return;
    setBusy(action);
    setOperationError(null);
    try {
      await dockerServiceAction(action);
      await refresh();
      toast.ok(`Docker 已${serviceActionLabel(action)}`);
    } catch (error) {
      setOperationError(error);
      toast.err(error.message || 'Docker 服务操作失败');
    } finally {
      setBusy('');
    }
  }

  async function toggleAutostart(enabled) {
    if (!requireAuth('修改 Docker 开机自启')) return;
    setBusy('autostart');
    setOperationError(null);
    try {
      await setDockerAutostart(enabled);
      await refresh();
      toast.ok(enabled ? '已开启 Docker 开机自启' : '已关闭 Docker 开机自启');
    } catch (error) {
      setOperationError(error);
      toast.err(error.message || '开机自启设置失败');
    } finally {
      setBusy('');
    }
  }

  const service = overview?.service;
  const running = service?.state === 'running';
  const unavailable = service && !running;
  const status = serviceStatus(service?.state, loading);
  const storage = overview?.storage;
  const diskUsed = Math.max(0, (storage?.disk?.totalBytes || 0) - (storage?.disk?.availableBytes || 0));
  const diskPct = storage?.disk?.totalBytes > 0 ? diskUsed / storage.disk.totalBytes * 100 : 0;

  return (
    <div style={{ width: '100%', minWidth: 0, height: '100%', overflow: 'auto', background: T.bg }}>
      <div style={{ padding: '20px clamp(14px, 3vw, 28px) 32px', maxWidth: 1180, margin: '0 auto' }}>
        <header style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap', marginBottom: 18 }}>
          <div style={{ width: 36, height: 36, borderRadius: 8, background: '#2563eb', color: 'white', display: 'grid', placeItems: 'center' }}>
            <Icon name="server" size={19} stroke={1.8}/>
          </div>
          <div>
            <h1 style={{ margin: 0, fontSize: 20, lineHeight: 1.2, color: T.ink, letterSpacing: 0 }}>Docker</h1>
            <div style={{ marginTop: 3, display: 'flex', alignItems: 'center', gap: 6, fontSize: 11.5, color: T.ink3 }}>
              <StatusDot tone={status.tone} size={8}/><span>{status.label}</span>
              {overview?.version && <span className="mono">v{overview.version}</span>}
            </div>
          </div>
          <div style={{ flex: 1 }}/>
          <button style={btnSecondary} onClick={() => refresh()} title="刷新概览" aria-label="刷新概览">
            <Icon name="refresh" size={14}/>
          </button>
          <button style={btnPrimary} onClick={onOpenCompose}>
            <Icon name="apps" size={14}/>Compose 管理
          </button>
        </header>

        <section aria-label="Docker 概览" style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 10 }}>
          <SummaryCard label="服务状态" value={status.label} detail={overview?.idleSummary || '检测中'} icon="power" tone={status.color}/>
          <SummaryCard label="Compose 项目" value={formatCount(overview?.composeProjects?.running)} detail={`共 ${formatCount(overview?.composeProjects?.total)} 个项目`} icon="apps" tone={T.teal}/>
          <SummaryCard label="容器" value={formatCount(overview?.containers?.running)} detail={`共 ${formatCount(overview?.containers?.total)} 个容器`} icon="server" tone={T.blue}/>
          <SummaryCard label="运行负载" value={stats?.available ? `${formatPercent(stats.cpuPercent)} CPU` : '无数据'} detail={stats?.available ? formatBytes(stats.memoryUsageBytes) + ' 内存' : 'Docker daemon 不可用'} icon="cpu" tone={T.amber}/>
        </section>

        {unavailable && (
          <div role="status" style={{ ...noticeStyle, marginTop: 12, background: service.state === 'not_installed' ? T.redSoft : T.amberSoft, borderColor: service.state === 'not_installed' ? '#fecaca' : '#fde68a' }}>
            <Icon name={service.state === 'not_installed' ? 'alertTri' : 'info'} size={16}/>
            <div style={{ minWidth: 0 }}>
              <div style={{ fontWeight: 700 }}>{service.state === 'not_installed' ? 'Docker 未安装' : 'Docker 服务已停止'}</div>
              <div style={{ marginTop: 3, color: T.ink3, overflowWrap: 'anywhere' }}>{service.diagnostic || '实时监控与容器统计暂不可用'}</div>
            </div>
          </div>
        )}

        {operationError && <OperationError error={operationError} onClose={() => setOperationError(null)}/>}

        <div style={{ display: 'grid', gridTemplateColumns: 'minmax(260px, 0.8fr) minmax(320px, 1.2fr)', gap: 12, marginTop: 12 }} className="docker-settings-grid">
          <section style={panelStyle}>
            <SectionTitle icon="power" title="服务控制"/>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginTop: 14, flexWrap: 'wrap' }}>
              {running ? (
                <>
                  <button disabled={!!busy || !service?.controlSupported} style={btnDanger} onClick={() => runServiceAction('stop')}>
                    <Icon name="stop" size={13}/>{busy === 'stop' ? '正在停止' : '停止'}
                  </button>
                  <button disabled={!!busy || !service?.controlSupported} style={btnSecondary} onClick={() => runServiceAction('restart')}>
                    <Icon name="refresh" size={13}/>{busy === 'restart' ? '正在重启' : '重启'}
                  </button>
                </>
              ) : (
                <button disabled={!!busy || !service?.controlSupported || service?.state === 'not_installed' || storage?.valid === false} style={btnPrimary} onClick={() => runServiceAction('start')}>
                  <Icon name="play" size={13}/>{busy === 'start' ? '正在启动' : '启动'}
                </button>
              )}
              {!service?.controlSupported && <span style={{ fontSize: 11.5, color: T.ink3 }}>当前环境不支持服务控制</span>}
            </div>
            <div style={{ height: 1, background: T.borderSoft, margin: '16px 0' }}/>
            <label style={{ display: 'flex', alignItems: 'center', gap: 10, cursor: service?.autostartSupported ? 'pointer' : 'not-allowed' }}>
              <input type="checkbox" checked={!!service?.autostartEnabled} disabled={!!busy || !service?.autostartSupported}
                onChange={(e) => toggleAutostart(e.target.checked)} style={{ width: 16, height: 16, accentColor: T.blue }}/>
              <span style={{ fontSize: 13, color: T.ink2, fontWeight: 600 }}>开机自动启动</span>
            </label>
          </section>

          <section style={panelStyle}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <SectionTitle icon="hardDrive" title="数据存储"/>
              <div style={{ flex: 1 }}/>
              <button style={btnSecondary} onClick={() => requireAuth('配置 Docker 存储位置') && setStorageOpen(true)}>
                <Icon name="gear" size={13}/><span>{storage?.valid ? '迁移' : '设置'}</span>
              </button>
            </div>
            <div className="mono" style={{ marginTop: 14, fontSize: 12.5, color: storage?.valid ? T.ink : T.red, overflowWrap: 'anywhere' }}>
              {storage?.path || '未配置'}
            </div>
            {storage?.valid ? (
              <>
                <div style={{ height: 7, borderRadius: 4, background: T.borderSoft, marginTop: 13, overflow: 'hidden' }}>
                  <div style={{ width: `${Math.min(100, diskPct)}%`, height: '100%', background: diskPct >= 85 ? T.red : diskPct >= 70 ? T.amber : T.teal }}/>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, marginTop: 7, fontSize: 11.5, color: T.ink3 }}>
                  <span>已用 {formatBytes(diskUsed)}</span><span>可用 {formatBytes(storage.disk.availableBytes)}</span>
                </div>
              </>
            ) : (
              <div style={{ ...noticeStyle, marginTop: 12, background: T.redSoft, borderColor: '#fecaca', color: '#991b1b' }}>
                <Icon name="alertTri" size={14}/><span>{storage?.error || '存储位置异常，Docker 启动已被阻止'}</span>
              </div>
            )}
          </section>
        </div>

        <section style={{ marginTop: 18 }}>
          <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, marginBottom: 10 }}>
            <h2 style={{ margin: 0, fontSize: 15, color: T.ink, letterSpacing: 0 }}>实时监控</h2>
            <span style={{ fontSize: 11, color: T.ink4 }}>{foreground ? '3 秒刷新' : '后台 30 秒刷新'}</span>
          </div>
          {stats?.available ? (
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))', gap: 10 }}>
              <MetricChart title="CPU" value={formatPercent(stats.cpuPercent)} series={history.cpu} color={T.blue} unit="%"/>
              <MetricChart title="内存" value={`${formatBytes(stats.memoryUsageBytes)} / ${formatBytes(stats.memoryLimitBytes)}`} series={history.memory} color={T.teal} unit="%"/>
              <MetricChart title="网络接收" value={formatRate(last(rates.rx))} series={rates.rx} color={T.green} unit="B/s"/>
              <MetricChart title="网络发送" value={formatRate(last(rates.tx))} series={rates.tx} color={T.amber} unit="B/s"/>
            </div>
          ) : (
            <div style={{ minHeight: 128, border: `1px dashed ${T.border}`, borderRadius: 8, display: 'grid', placeItems: 'center', background: T.surface }}>
              <div style={{ textAlign: 'center', color: T.ink3 }}>
                <Icon name="chart" size={24}/><div style={{ marginTop: 8, fontSize: 12 }}>{stats?.diagnostic || 'Docker daemon 不可用，暂无监控数据'}</div>
              </div>
            </div>
          )}
        </section>
      </div>

      {storageOpen && (
        <StorageMigrationDialog
          storage={storage}
          onClose={() => setStorageOpen(false)}
          onDone={() => { setStorageOpen(false); refresh(); }}
        />
      )}
      <style>{`@media (max-width: 760px) { .docker-settings-grid { grid-template-columns: 1fr !important; } }`}</style>
    </div>
  );
}

function SummaryCard({ label, value, detail, icon, tone }) {
  return (
    <div style={{ minHeight: 94, padding: 14, background: T.surface, border: `1px solid ${T.border}`, borderRadius: 8 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 7, color: T.ink3, fontSize: 11.5 }}><Icon name={icon} size={13}/>{label}</div>
      <div className="mono tnum" style={{ marginTop: 9, fontSize: 22, lineHeight: 1, fontWeight: 750, color: tone }}>{value}</div>
      <div style={{ marginTop: 7, fontSize: 11, color: T.ink4 }}>{detail}</div>
    </div>
  );
}

function SectionTitle({ icon, title }) {
  return <div style={{ display: 'flex', alignItems: 'center', gap: 7, fontSize: 13.5, fontWeight: 700, color: T.ink }}><Icon name={icon} size={15}/>{title}</div>;
}

function OperationError({ error, onClose }) {
  return (
    <div role="alert" style={{ ...noticeStyle, marginTop: 12, background: T.redSoft, borderColor: '#fecaca', color: '#991b1b', alignItems: 'flex-start' }}>
      <Icon name="alertTri" size={16}/>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ fontWeight: 700 }}>{error.message || '操作失败'}</div>
        {error.detail && <pre style={{ margin: '6px 0 0', whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', font: `11px/1.45 ${T.mono}`, color: T.ink3 }}>{error.detail}</pre>}
      </div>
      <button onClick={onClose} aria-label="关闭诊断" style={iconButton}><Icon name="x" size={13}/></button>
    </div>
  );
}

function MetricChart({ title, value, series, color, unit }) {
  const width = 240, height = 56;
  const values = series.length > 1 ? series : [];
  const max = Math.max(...values, unit === '%' ? 100 : 1);
  const points = values.map((v, index) => `${index / (values.length - 1) * width},${height - Math.min(1, v / max) * (height - 4) - 2}`).join(' ');
  return (
    <div style={{ padding: 14, background: T.surface, border: `1px solid ${T.border}`, borderRadius: 8, minWidth: 0 }}>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 8 }}>
        <span style={{ fontSize: 11.5, color: T.ink3 }}>{title}</span><span className="mono tnum" style={{ marginLeft: 'auto', fontSize: 13, fontWeight: 700, color: T.ink }}>{value}</span>
      </div>
      <div style={{ height, marginTop: 10, borderBottom: `1px solid ${T.borderSoft}` }}>
        {values.length > 1 ? <svg width="100%" height={height} viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" aria-hidden="true">
          <polyline points={points} fill="none" stroke={color} strokeWidth="2" vectorEffect="non-scaling-stroke"/>
        </svg> : <div style={{ height: '100%', display: 'grid', placeItems: 'center', fontSize: 11, color: T.ink4 }}>采样中</div>}
      </div>
    </div>
  );
}

function StorageMigrationDialog({ storage, onClose, onDone }) {
  const [target, setTarget] = useState('');
  const [plan, setPlan] = useState(null);
  const [typed, setTyped] = useState('');
  const [accepted, setAccepted] = useState(false);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState(null);
  const toast = useToast();

  async function generatePlan() {
    setBusy('plan'); setError(null); setPlan(null);
    try { setPlan(await planDockerMigration(target)); }
    catch (e) { setError(e); }
    finally { setBusy(''); }
  }

  async function executePlan() {
    setBusy('execute'); setError(null);
    try {
      const result = await executeDockerMigration(plan.targetPath, plan.id);
      toast.ok(result.message || 'Docker 数据迁移完成');
      onDone();
    } catch (e) { setError(e); setBusy(''); }
  }

  return (
    <div role="presentation" onClick={busy ? undefined : onClose} style={{ position: 'fixed', inset: 0, zIndex: 300, background: 'rgba(15,23,42,0.5)', display: 'grid', placeItems: 'center', padding: 18 }}>
      <div role="dialog" aria-modal="true" aria-labelledby="docker-storage-title" onClick={(e) => e.stopPropagation()} style={{ width: 620, maxWidth: '96vw', maxHeight: '90vh', overflow: 'auto', background: T.surface, borderRadius: 8, boxShadow: T.shadow.xl }}>
        <div style={{ padding: '15px 18px', borderBottom: `1px solid ${T.border}`, display: 'flex', alignItems: 'center', gap: 9 }}>
          <Icon name="hardDrive" size={17}/><h2 id="docker-storage-title" style={{ margin: 0, fontSize: 15, color: T.ink, letterSpacing: 0 }}>迁移 Docker 数据存储</h2>
          <div style={{ flex: 1 }}/><button onClick={onClose} disabled={!!busy} aria-label="关闭" style={iconButton}><Icon name="x" size={14}/></button>
        </div>
        <div style={{ padding: 18 }}>
          <div style={{ padding: 10, borderRadius: 8, background: T.amberSoft, border: '1px solid #fde68a', color: '#92400e', fontSize: 12, lineHeight: 1.5 }}>
            迁移期间 Docker 与全部容器会停止。{plan ? `目标磁盘至少需要 ${formatBytes(plan.requiredBytes)} 可用空间；` : '生成计划后会核对目标磁盘容量；'}旧目录在成功后仍会保留。
          </div>
          <label style={{ display: 'block', marginTop: 15, fontSize: 11.5, color: T.ink3 }}>当前位置</label>
          <div className="mono" style={{ marginTop: 5, fontSize: 12, color: T.ink }}>{storage?.path || '未配置'}</div>
          <label htmlFor="docker-target-path" style={{ display: 'block', marginTop: 13, fontSize: 11.5, color: T.ink3 }}>新位置</label>
          <div style={{ display: 'flex', gap: 8, marginTop: 6 }}>
            <input id="docker-target-path" value={target} disabled={!!busy || !!plan} onChange={(e) => setTarget(e.target.value)} placeholder="/data/docker" className="mono"
              style={{ flex: 1, minWidth: 0, height: 34, padding: '0 10px', borderRadius: 6, border: `1px solid ${T.border}`, fontSize: 12, color: T.ink }}/>
            {!plan ? <button onClick={generatePlan} disabled={!target.trim() || !!busy} style={btnPrimary}><Icon name="chart" size={13}/>{busy === 'plan' ? '检查中' : '生成计划'}</button>
              : <button onClick={() => { setPlan(null); setAccepted(false); setTyped(''); }} disabled={!!busy} style={btnSecondary}><Icon name="edit" size={13}/>修改</button>}
          </div>

          {plan && <div style={{ marginTop: 16 }}>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
              <PlanFact label="需迁移" value={formatBytes(plan.requiredBytes)}/><PlanFact label="目标可用" value={formatBytes(plan.availableBytes)}/>
            </div>
            <ol style={{ margin: '14px 0 0', paddingLeft: 20, color: T.ink2, fontSize: 12, lineHeight: 1.55 }}>
              {plan.steps?.map((step) => <li key={step.order} style={{ marginBottom: 7 }}><strong>{step.title}</strong><div style={{ color: T.ink3 }}>{step.description}</div></li>)}
            </ol>
            <label style={{ display: 'flex', alignItems: 'flex-start', gap: 9, marginTop: 13, fontSize: 12, color: T.ink2 }}>
              <input type="checkbox" checked={accepted} onChange={(e) => setAccepted(e.target.checked)} style={{ marginTop: 2, accentColor: T.red }}/>
              <span>确认迁移会停止 Docker 服务，且已核对目标路径与容量</span>
            </label>
            <label htmlFor="docker-confirm" style={{ display: 'block', marginTop: 11, fontSize: 11.5, color: T.ink3 }}>输入“迁移”二次确认</label>
            <input id="docker-confirm" value={typed} onChange={(e) => setTyped(e.target.value)} disabled={!!busy}
              style={{ width: '100%', boxSizing: 'border-box', height: 34, marginTop: 6, padding: '0 10px', borderRadius: 6, border: `1px solid ${T.border}`, fontSize: 12 }}/>
          </div>}

          {error && <div role="alert" style={{ ...noticeStyle, marginTop: 13, background: T.redSoft, borderColor: '#fecaca', color: '#991b1b' }}>
            <Icon name="alertTri" size={14}/><div><strong>{error.message}</strong>{error.detail && <div className="mono" style={{ marginTop: 4, fontSize: 10.5, overflowWrap: 'anywhere' }}>{error.detail}</div>}</div>
          </div>}
        </div>
        <div style={{ padding: '12px 18px', borderTop: `1px solid ${T.border}`, display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
          <button onClick={onClose} disabled={!!busy} style={btnSecondary}>取消</button>
          {plan && <button onClick={executePlan} disabled={busy === 'execute' || !accepted || typed.trim() !== '迁移'} style={btnDanger}>
            <Icon name="swap" size={13}/>{busy === 'execute' ? '正在迁移' : '停止服务并迁移'}
          </button>}
        </div>
      </div>
    </div>
  );
}

function PlanFact({ label, value }) {
  return <div style={{ padding: 10, borderRadius: 8, background: T.surfaceAlt, border: `1px solid ${T.borderSoft}` }}><div style={{ fontSize: 10.5, color: T.ink3 }}>{label}</div><div className="mono" style={{ marginTop: 4, fontSize: 13, fontWeight: 700, color: T.ink }}>{value}</div></div>;
}

function rateSeries(values, times) {
  if (values.length < 2) return [];
  return values.slice(1).map((value, index) => {
    const seconds = (new Date(times[index + 1]).getTime() - new Date(times[index]).getTime()) / 1000;
    return seconds > 0 ? Math.max(0, (value - values[index]) / seconds) : 0;
  });
}

function last(values) { return values.length ? values[values.length - 1] : 0; }
function formatCount(value) { return Number.isFinite(value) ? String(value) : '—'; }
function formatPercent(value) { return `${(Number(value) || 0).toFixed(1)}%`; }
function formatBytes(value) {
  const n = Number(value) || 0;
  if (n >= 1024 ** 4) return `${(n / 1024 ** 4).toFixed(1)} TiB`;
  if (n >= 1024 ** 3) return `${(n / 1024 ** 3).toFixed(1)} GiB`;
  if (n >= 1024 ** 2) return `${(n / 1024 ** 2).toFixed(1)} MiB`;
  if (n >= 1024) return `${(n / 1024).toFixed(1)} KiB`;
  return `${Math.round(n)} B`;
}
function formatRate(value) { return `${formatBytes(value)}/s`; }
function serviceActionLabel(action) { return ({ start: '启动', stop: '停止', restart: '重启' })[action] || action; }
function serviceStatus(state, loading) {
  if (loading && !state) return { label: '检测中', tone: 'gray', color: T.ink3 };
  if (state === 'running') return { label: '运行中', tone: 'green', color: T.green };
  if (state === 'stopped') return { label: '已停止', tone: 'amber', color: T.amber };
  return { label: '未安装', tone: 'red', color: T.red };
}

const panelStyle = { padding: 16, background: T.surface, border: `1px solid ${T.border}`, borderRadius: 8, minWidth: 0 };
const noticeStyle = { display: 'flex', alignItems: 'center', gap: 9, padding: '10px 12px', border: '1px solid', borderRadius: 8, fontSize: 11.5, color: T.ink2 };
const iconButton = { width: 28, height: 28, display: 'grid', placeItems: 'center', padding: 0, border: 'none', borderRadius: 6, background: 'transparent', color: T.ink3, cursor: 'pointer' };
