import { useEffect, useMemo, useRef, useState } from 'react';
import { T } from '../tokens';
import { Chip, StatusDot } from './ui';
import { btnPrimary, btnSecondary } from './AppWindow';
import { useToast } from './toastContext';
import {
  useAppLogs, useAppOperations, useAppRevisions, useCompose, useEnv, useStorage,
  restoreAppRevision, updateComposeApp, useTask, validateCompose,
} from '../hooks/useApi';
import { formatDateTime, groupRisks, PHASE_LABEL, PHASE_TONE, volumeMeta, envDisplay } from '../lib/compose';
import { parseEnv } from '../lib/compose';

const pane = { padding: 14 };
const box = { background: T.surfaceAlt, border: `1px solid ${T.borderSoft}`, borderRadius: 8, padding: 12 };

export function ComposeOverview({ app }) {
  const phase = app.observed?.phase || 'unknown';
  const conditions = app.observed?.conditions || [];
  const endpoints = app.observed?.endpoints || [];
  return <div style={pane}>
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit,minmax(150px,1fr))', gap: 8 }}>
      <Info label="状态" value={PHASE_LABEL[phase] || phase} tone={PHASE_TONE[phase]}/><Info label="运行时" value="Docker Compose"/>
      <Info label="配置 revision" value={`r${app.revision || 0}`}/><Info label="服务数" value={(app.observed?.services || []).length}/>
    </div>
    {app.observed?.message && <Notice tone="amber">{app.observed.message}</Notice>}
    <Section title="入口">{endpoints.length ? endpoints.map((e, i) => <a key={i} href={e.url} target="_blank" rel="noreferrer" style={{ display: 'block', color: T.blueDeep, marginBottom: 5 }}>{e.name || e.url} · {e.url}</a>) : <Empty text="该应用没有公开入口"/>}</Section>
    <Section title="运行条件">{conditions.length ? conditions.map((c, i) => <div key={i} style={{ ...box, marginBottom: 6 }}><strong>{c.type || 'Condition'}</strong><span style={{ marginLeft: 8, color: T.ink3 }}>{c.status}</span>{c.message && <div style={{ marginTop: 4, color: T.ink3 }}>{c.message}</div>}</div>) : <Empty text="暂无异常条件"/>}</Section>
  </div>;
}

export function ComposeServices({ app }) {
  const services = app.observed?.services || [];
  return <div style={pane}>{services.length ? services.map((s) => <div key={s.name} style={{ ...box, marginBottom: 8 }}>
    <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
      <StatusDot tone={s.health === 'unhealthy' ? 'red' : s.state === 'running' ? 'green' : 'gray'} size={7}/><strong>{s.name}</strong>
      <Chip tone={s.health === 'healthy' ? 'green' : s.health === 'unhealthy' ? 'red' : 'gray'}>{s.health || '无健康检查'}</Chip>
      <span style={{ marginLeft: 'auto', color: T.ink3 }}>{s.state || 'unknown'}</span>
    </div>
    <div className="mono" style={{ marginTop: 7, color: T.ink3, wordBreak: 'break-all' }}>{s.image || '—'}</div>
    {!!s.ports?.length && <div style={{ marginTop: 7 }}>{s.ports.map((p, i) => <Chip key={i} tone="blue">{portLabel(p)}</Chip>)}</div>}
    {s.containerId && <div className="mono" style={{ marginTop: 7, fontSize: 11, color: T.ink4 }}>container {s.containerId.slice(0, 12)}</div>}
  </div>) : <Empty text="尚未观察到 service/container；部署任务可能仍在执行"/>}</div>;
}

export function ComposeLogs({ app }) {
  const services = app.observed?.services || [];
  const [service, setService] = useState(() => services[0]?.name || '');
  const { lines, loading } = useAppLogs(app.id, 3000, 300, service);
  const ref = useRef(null);
  useEffect(() => { if (ref.current) ref.current.scrollTop = ref.current.scrollHeight; }, [lines]);
  return <div style={pane}><div style={{ display: 'flex', gap: 8, marginBottom: 8, alignItems: 'center' }}><Chip tone="green"><StatusDot tone="green" size={6} pulse/>Service 日志</Chip>
    {services.length > 0 && <select aria-label="选择 service" value={service} onChange={(e) => setService(e.target.value)} style={{ height: 30, borderRadius: 7, border: `1px solid ${T.border}`, padding: '0 8px', background: T.surface }}><option value="">第一个 service</option>{services.map((s) => <option key={s.name} value={s.name}>{s.name}</option>)}</select>}
    <span style={{ color: T.ink4, fontSize: 11 }}>3 秒刷新 · 最近 300 行</span></div>
    <div ref={ref} style={{ background: '#0b1020', color: '#e2e8f0', borderRadius: 8, padding: 12, minHeight: 320, maxHeight: 'calc(100vh - 290px)', overflow: 'auto', font: '11px/1.6 ui-monospace,monospace' }}>
      {!lines.length && <div style={{ color: '#64748b' }}>{loading ? '加载日志中…' : '暂无日志'}</div>}
      {lines.map((line, i) => <div key={i} style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>{line}</div>)}
    </div>
  </div>;
}

export function ComposeEditor({ app }) {
  const { data, loading, error, refresh } = useCompose(app.id);
  const [draft, setDraft] = useState(null);
  const [validation, setValidation] = useState(null);
  const [confirm, setConfirm] = useState(false);
  const [saving, setSaving] = useState(false);
  const [conflict, setConflict] = useState(false);
  const [taskId, setTaskId] = useState(null);
  const [secrets, setSecrets] = useState('');
  const { task } = useTask(taskId);
  const toast = useToast();
  const content = draft ?? data?.compose ?? '';
  const risks = groupRisks(validation?.risks);
  const canSave = validation?.ok && risks.blocked.length === 0 && (!risks.confirmation.length || confirm) && !saving;

  async function check() {
    setValidation(null); setConfirm(false); setConflict(false);
    try { setValidation(await validateCompose({ name: app.name, compose: content, appId: app.id, retainEnvironment: true, secrets: parseEnv(secrets) })); }
    catch (e) { toast.err(`预检失败：${e.message}`); }
  }
  async function save() {
    setSaving(true); setConflict(false);
    try {
      const t = await updateComposeApp({ id: app.id, name: app.name, compose: content, expectedRevision: data.revision, source: app.source, confirmRisky: confirm, retainEnvironment: true, secrets: parseEnv(secrets) });
      setTaskId(t.id); toast.ok('配置更新任务已提交');
    } catch (e) {
      if (e.status === 409 || e.reason === 'revision_mismatch') setConflict(true);
      else toast.err(`保存失败：${e.message}`);
    } finally { setSaving(false); }
  }
  return <div style={pane}>
    <Notice>保存前必须通过后端 `docker compose config` 与风险预检；使用 expectedRevision，绝不静默覆盖并发修改。</Notice>
    {loading && <Empty text="加载 Compose 事实源…"/>}{error && <Notice tone="red">无法读取 Compose：{String(error.message || error)}</Notice>}
    <textarea aria-label="Compose YAML" spellCheck={false} value={content} onChange={(e) => { setDraft(e.target.value); setValidation(null); }} style={{ width: '100%', minHeight: 360, resize: 'vertical', boxSizing: 'border-box', border: `1px solid ${T.border}`, borderRadius: 8, padding: 12, background: '#0b1020', color: '#e2e8f0', font: '12px/1.55 ui-monospace,monospace' }}/>
    <label style={{ display: 'block', marginTop: 10, marginBottom: 5, fontWeight: 700 }}>轮换 Secret（可选）</label>
    <textarea aria-label="轮换 Secret" value={secrets} onChange={(e) => { setSecrets(e.target.value); setValidation(null); }} rows={3} spellCheck={false} placeholder="DB_PASSWORD=new-value" style={{ width: '100%', resize: 'vertical', boxSizing: 'border-box', border: `1px solid ${T.border}`, borderRadius: 8, padding: 10, font: '12px/1.5 ui-monospace,monospace' }}/>
    <div style={{ color: T.ink4, fontSize: 11, marginTop: 4 }}>只提交要新增或轮换的键；未填写的现有 Secret 由后端保留，原值不会回传浏览器。</div>
    <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginTop: 10, flexWrap: 'wrap' }}>
      <button onClick={check} style={btnSecondary}>预检</button><button onClick={save} disabled={!canSave} style={{ ...btnPrimary, opacity: canSave ? 1 : 0.55 }}>保存并重新部署</button>
      <span style={{ color: T.ink4 }}>当前 r{data?.revision || app.revision || 0}</span>
    </div>
    {validation && <RiskSummary risks={risks} confirm={confirm} setConfirm={setConfirm}/>}
    {conflict && <Notice tone="red">配置已被其他操作更新。请重新加载最新 revision 后再修改。 <button onClick={() => { setDraft(null); setValidation(null); setConflict(false); refresh(); }} style={linkBtn}>重新加载</button></Notice>}
    {task && <Notice tone={task.status === 'failed' ? 'red' : 'blue'}>任务 {task.status} · {task.phase || 'queued'}{task.message ? ` · ${task.message}` : ''}</Notice>}
  </div>;
}

export function ComposeEnv({ app }) {
  const { data, loading, error } = useEnv(app.id);
  const vars = data?.vars || [];
  return <div style={pane}><Notice>环境变量接口只返回 key、类型和 configured 状态。Secret 原值不会回显；要轮换 Secret，请编辑 Compose 引用并在重新部署时提交新值。</Notice>
    {loading && <Empty text="加载环境变量元信息…"/>}{error && <Notice tone="red">无法读取环境变量元信息</Notice>}
    {vars.length ? vars.map((v) => { const meta = envDisplay(v); return <div key={v.key} style={{ ...box, display: 'flex', gap: 8, alignItems: 'center', marginBottom: 6 }}><code>{v.key}</code><Chip tone={meta.secret ? 'amber' : 'gray'}>{meta.typeLabel}</Chip>{v.required && <Chip tone="red">必填</Chip>}<span style={{ marginLeft: 'auto', color: v.configured ? '#166534' : T.ink4 }}>{v.configured ? '已配置' : '未配置'}</span></div>; }) : !loading && <Empty text="Compose 未声明可展示的环境变量"/>}
  </div>;
}

export function ComposeStorage({ app }) {
  const { data, loading, error } = useStorage(app.id);
  const volumes = data?.volumes || [];
  return <div style={pane}><Notice>只有 managed 卷在 purge 时可删除；external、bind 与 socket 生命周期不属于应用，devbox 永不自动删除。</Notice>
    {loading && <Empty text="分析存储资源…"/>}{error && <Notice tone="red">无法读取存储清单</Notice>}
    {volumes.length ? volumes.map((v, i) => { const meta = volumeMeta(v); return <div key={`${v.source}-${v.target}-${i}`} style={{ ...box, marginBottom: 7 }}><div style={{ display: 'flex', gap: 8, alignItems: 'center' }}><Chip tone={meta.tone}>{meta.label}</Chip><code style={{ wordBreak: 'break-all' }}>{v.source || 'anonymous'}</code><span style={{ marginLeft: 'auto', color: v.deletable ? '#b91c1c' : T.ink4 }}>{v.deletable ? 'purge 可删' : '永不自动删除'}</span></div><div style={{ marginTop: 6, color: T.ink3 }}>挂载到 <code>{v.target || '—'}</code> · {meta.hint}</div></div>; }) : !loading && <Empty text="没有声明卷或宿主挂载"/>}
    {data?.managedDataDir && <div style={{ ...box, marginTop: 8 }}><strong>受管数据目录</strong><div className="mono" style={{ marginTop: 5, wordBreak: 'break-all', color: T.ink3 }}>{data.managedDataDir}</div></div>}
  </div>;
}

export function ComposeRevisions({ app }) {
  const { data, loading, error, refresh } = useAppRevisions(app.id);
  const revisions = Array.isArray(data) ? data : data?.revisions || [];
  const [restoring, setRestoring] = useState(null);
  const [taskId, setTaskId] = useState(null);
  const { task } = useTask(taskId);
  const toast = useToast();
  async function restore(rev) {
    if (!window.confirm(`恢复 revision ${rev.number}？\n\n回退配置不会自动恢复应用数据；数据库 schema 也不保证可逆。`)) return;
    setRestoring(rev.number);
    try { const submitted = await restoreAppRevision(app.id, rev.number); setTaskId(submitted.id); toast.ok(`已提交 revision ${rev.number} 配置回退任务`); refresh(); }
    catch (e) { toast.err(`回退失败：${e.message}`); } finally { setRestoring(null); }
  }
  return <div style={pane}><Notice tone="amber"><strong>定义回退，不是数据恢复。</strong> 回退配置不会自动恢复应用数据，数据库 schema 可能不可逆。</Notice>
    {task && <Notice tone={task.status === 'failed' ? 'red' : 'blue'}>回退任务 {task.status} · {task.phase || 'queued'}{task.message ? ` · ${task.message}` : ''}</Notice>}
    {loading && <Empty text="加载 revision…"/>}{error && <Notice tone="red">无法读取 revision</Notice>}
    {revisions.map((r) => <div key={r.number} style={{ ...box, marginBottom: 7, display: 'flex', alignItems: 'center', gap: 10 }}><strong>r{r.number}</strong><span style={{ color: T.ink3 }}>{formatDateTime(r.createdAt)}</span><Chip tone="gray">{r.source?.kind || 'inline'}</Chip><span className="mono" style={{ color: T.ink4 }}>{r.composeHash?.slice(0, 10)}</span><button onClick={() => restore(r)} disabled={restoring != null || r.number === app.revision} style={{ ...btnSecondary, marginLeft: 'auto', opacity: r.number === app.revision ? 0.55 : 1 }}>{r.number === app.revision ? '当前' : restoring === r.number ? '提交中…' : '回退配置'}</button></div>)}
    {!loading && !revisions.length && <Empty text="暂无 revision 记录"/>}
  </div>;
}

export function ComposeOperations({ app }) {
  const { data, loading, error } = useAppOperations(app.id, 4000);
  const ops = Array.isArray(data) ? data : data?.operations || [];
  return <div style={pane}>{loading && <Empty text="加载操作记录…"/>}{error && <Notice tone="red">无法读取操作记录</Notice>}
    {ops.map((t) => <div key={t.id} style={{ ...box, marginBottom: 7 }}><div style={{ display: 'flex', gap: 8, alignItems: 'center' }}><StatusDot tone={t.status === 'succeeded' ? 'green' : t.status === 'failed' ? 'red' : 'blue'} size={7}/><strong>{t.type}{t.action ? ` · ${t.action}` : ''}</strong><Chip tone={t.status === 'failed' ? 'red' : t.status === 'succeeded' ? 'green' : 'blue'}>{t.status}</Chip><span style={{ marginLeft: 'auto', color: T.ink4 }}>{formatDateTime(t.createdAt)}</span></div><div style={{ marginTop: 5, color: T.ink3 }}>{t.phase || 'queued'}{t.message ? ` · ${t.message}` : ''}{t.revision ? ` · r${t.revision}` : ''}</div></div>)}
    {!loading && !ops.length && <Empty text="暂无操作记录"/>}
  </div>;
}

function Info({ label, value, tone = 'gray' }) { return <div style={box}><div style={{ color: T.ink4, fontSize: 11 }}>{label}</div><div style={{ marginTop: 5, fontWeight: 700, color: tone === 'red' ? '#b91c1c' : tone === 'green' ? '#166534' : T.ink }}>{value}</div></div>; }
function Section({ title, children }) { return <div style={{ marginTop: 14 }}><div style={{ fontSize: 12, fontWeight: 700, marginBottom: 7 }}>{title}</div>{children}</div>; }
function Empty({ text }) { return <div style={{ ...box, color: T.ink4, textAlign: 'center', padding: 20 }}>{text}</div>; }
function Notice({ tone = 'blue', children }) { const colors = tone === 'red' ? ['#fef2f2','#fecaca','#b91c1c'] : tone === 'amber' ? ['#fffbeb','#fde68a','#92400e'] : ['#e6f4ff','#99c7ff','#0043b8']; return <div style={{ marginBottom: 10, padding: 10, borderRadius: 8, background: colors[0], border: `1px solid ${colors[1]}`, color: colors[2], lineHeight: 1.55 }}>{children}</div>; }
function portLabel(p) { if (typeof p === 'string') return p; return `${p.hostPort || p.containerPort || ''}${p.hostPort ? `→${p.containerPort || ''}` : ''}/${p.protocol || 'tcp'}`; }
function RiskSummary({ risks, confirm, setConfirm }) {
  const rows = useMemo(() => [['blocked','阻断',risks.blocked,'#b91c1c'],['confirmation','需确认',risks.confirmation,'#92400e'],['warning','警告',risks.warning,'#92400e']], [risks]);
  return <div style={{ marginTop: 10 }}>{rows.map(([key,label,items,color]) => items.length ? <div key={key} style={{ ...box, marginBottom: 6, color }}><strong>{label}</strong>{items.map((r, i) => <div key={i} style={{ marginTop: 4 }}>{r.service ? `${r.service}: ` : ''}{r.message}</div>)}</div> : null)}{risks.confirmation.length > 0 && risks.blocked.length === 0 && <label style={{ display: 'flex', gap: 8, alignItems: 'flex-start', ...box }}><input type="checkbox" checked={confirm} onChange={(e) => setConfirm(e.target.checked)}/><span>我已了解 confirmation 级风险并显式确认。blocked 风险不可绕过。</span></label>}{!risks.blocked.length && !risks.confirmation.length && !risks.warning.length && <Notice>预检通过，没有风险项。</Notice>}</div>;
}
const linkBtn = { border: 0, background: 'transparent', color: 'inherit', textDecoration: 'underline', cursor: 'pointer', fontWeight: 700 };
