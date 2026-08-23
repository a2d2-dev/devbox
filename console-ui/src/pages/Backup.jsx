import { useMemo, useState } from 'react'
import { Icon } from '../icons'
import { Chip } from '../components/ui'
import {
  createBackup, fetchBackupLog, pauseBackup, preflightBackup, previewBackupRestore,
  restoreBackup, runBackup, useBackupHistory, useBackups, useBackupVersions,
} from '../hooks/useApi'
import './Backup.css'

const STEPS = ['源', '目标', '计划', '规则与保留', '预检确认']

const emptyForm = {
  name: '',
  source: { type: 'local', path: '', host: '', port: 22, identityFile: '' },
  target: { type: 'local', path: '', host: '', port: 22, identityFile: '' },
  scheduleKind: 'daily', cron: '0 2 * * *', weekday: 1, time: '02:00',
  mode: 'versioned', incremental: true, delete: false, excludes: '', keepLast: 7,
}

function taskPayload(form) {
  const [hour, minute] = form.time.split(':').map(Number)
  const endpoint = value => {
    const result = { type: value.type, path: value.path.trim() }
    if (value.type === 'ssh') {
      result.host = value.host.trim()
      result.port = Number(value.port) || 22
      if (value.identityFile.trim()) result.identityFile = value.identityFile.trim()
    }
    return result
  }
  return {
    name: form.name.trim(), source: endpoint(form.source), target: endpoint(form.target),
    schedule: {
      kind: form.scheduleKind,
      ...(form.scheduleKind === 'cron' ? { cron: form.cron.trim() } : { hour, minute }),
      ...(form.scheduleKind === 'weekly' ? { weekday: Number(form.weekday) } : {}),
    },
    excludes: form.excludes.split('\n').map(value => value.trim()).filter(Boolean),
    retention: { keepLast: Number(form.keepLast) }, mode: form.mode,
    incremental: form.mode === 'versioned' && form.incremental, delete: form.delete,
  }
}

function formatDate(value) {
  if (!value) return '—'
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}

function formatBytes(value) {
  if (!value) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let size = value, index = 0
  while (size >= 1024 && index < units.length - 1) { size /= 1024; index += 1 }
  return `${size.toFixed(index ? 1 : 0)} ${units[index]}`
}

function statusMeta(status) {
  return {
    idle: ['gray', '待运行'], queued: ['amber', '等待中'], running: ['blue', '运行中'],
    success: ['green', '成功'], failed: ['red', '失败'],
  }[status] || ['gray', status || '未知']
}

function IconButton({ icon, title, onClick, disabled, tone = 'normal' }) {
  return <button className={`backup-icon-button ${tone}`} title={title} aria-label={title} onClick={onClick} disabled={disabled}>
    <Icon name={icon} size={15}/>
  </button>
}

export default function Backup() {
  const { data: tasks = [], loading, error, refresh } = useBackups()
  const [view, setView] = useState('list')
  const [selectedId, setSelectedId] = useState(null)
  const [notice, setNotice] = useState('')
  const selected = tasks.find(task => task.id === selectedId) || null

  async function action(fn, success) {
    setNotice('')
    try { await fn(); setNotice(success); refresh() }
    catch (err) { setNotice(err.message || String(err)) }
  }

  if (view === 'create') return <CreateWizard onClose={() => setView('list')} onCreated={() => { setView('list'); refresh() }}/>
  if (view === 'history' && selectedId) return <HistoryView task={selected} taskId={selectedId} onBack={() => setView('list')}/>

  return <div className="backup-app">
    <header className="backup-toolbar">
      <div>
        <h1>备份</h1>
        <span>{tasks.length} 个任务</span>
      </div>
      <div className="backup-toolbar-actions">
        <IconButton icon="refresh" title="刷新" onClick={refresh}/>
        <button className="backup-primary" onClick={() => setView('create')}><Icon name="plus" size={15}/>新建任务</button>
      </div>
    </header>
    {notice && <div className="backup-notice">{notice}</div>}
    <main className="backup-main">
      {loading && tasks.length === 0 && <Empty icon="refresh" text="正在读取任务"/>}
      {error && tasks.length === 0 && <Empty icon="alertTri" text="备份服务不可用" detail={error.message}/>}
      {!loading && !error && tasks.length === 0 && <Empty icon="layers" text="暂无备份任务"/>}
      {tasks.length > 0 && <div className="backup-table-wrap">
        <table className="backup-table">
          <thead><tr><th>任务</th><th>路径</th><th>状态</th><th>上次运行</th><th>下次运行</th><th aria-label="操作"/></tr></thead>
          <tbody>{tasks.map(task => {
            const [tone, label] = statusMeta(task.status)
            return <tr key={task.id}>
              <td><strong>{task.name}</strong><small>{task.mode === 'versioned' ? (task.incremental ? '版本 · 增量' : '版本 · 全量') : '单目录镜像'}</small></td>
              <td><code>{task.source.host ? `${task.source.host}:` : ''}{task.source.path}</code><span className="backup-arrow">→</span><code>{task.target.host ? `${task.target.host}:` : ''}{task.target.path}</code></td>
              <td><Chip tone={task.paused ? 'gray' : tone}>{task.paused ? '已暂停' : label}</Chip>{task.status === 'failed' && <small className="backup-error-line" title={task.lastResult}>{task.lastResult}</small>}</td>
              <td>{formatDate(task.lastRunAt)}</td><td>{task.paused ? '—' : formatDate(task.nextRunAt)}</td>
              <td><div className="backup-row-actions">
                <IconButton icon="play" title="立即运行" disabled={task.status === 'running' || task.status === 'queued'} onClick={() => action(() => runBackup(task.id), '任务已进入执行队列')}/>
                <IconButton icon={task.paused ? 'play' : 'stop'} title={task.paused ? '恢复计划' : '暂停计划'} onClick={() => action(() => pauseBackup(task.id, !task.paused), task.paused ? '计划已恢复' : '计划已暂停')}/>
                <IconButton icon="history" title="历史与恢复" onClick={() => { setSelectedId(task.id); setView('history') }}/>
              </div></td>
            </tr>
          })}</tbody>
        </table>
      </div>}
    </main>
  </div>
}

function Empty({ icon, text, detail }) {
  return <div className="backup-empty"><Icon name={icon} size={28}/><strong>{text}</strong>{detail && <span>{detail}</span>}</div>
}

function CreateWizard({ onClose, onCreated }) {
  const [step, setStep] = useState(0)
  const [form, setForm] = useState(emptyForm)
  const [preflight, setPreflight] = useState(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const payload = useMemo(() => taskPayload(form), [form])
  const setEndpoint = (key, patch) => setForm(value => ({ ...value, [key]: { ...value[key], ...patch } }))

  async function check() {
    setBusy(true); setError(''); setPreflight(null)
    try { setPreflight(await preflightBackup(payload)) }
    catch (err) { setError(err.message || String(err)); if (err.preflight) setPreflight(err.preflight) }
    finally { setBusy(false) }
  }
  async function create() {
    setBusy(true); setError('')
    try { await createBackup(payload); onCreated() }
    catch (err) { setError(err.message || String(err)) }
    finally { setBusy(false) }
  }

  return <div className="backup-app">
    <header className="backup-toolbar"><div><h1>新建备份任务</h1><span>{STEPS[step]}</span></div><IconButton icon="x" title="关闭" onClick={onClose}/></header>
    <div className="backup-steps">{STEPS.map((label, index) => <div key={label} className={index === step ? 'active' : index < step ? 'done' : ''}><span>{index < step ? <Icon name="check" size={12}/> : index + 1}</span>{label}</div>)}</div>
    <main className="backup-wizard-main">
      {step === 0 && <section className="backup-form-section"><h2>备份源</h2><Field label="任务名称"><input value={form.name} onChange={e => setForm(v => ({ ...v, name: e.target.value }))} placeholder="例如：项目数据每日备份"/></Field><EndpointEditor value={form.source} onChange={patch => setEndpoint('source', patch)} allowMount={false}/></section>}
      {step === 1 && <section className="backup-form-section"><h2>备份目标</h2><EndpointEditor value={form.target} onChange={patch => setEndpoint('target', patch)} allowMount/></section>}
      {step === 2 && <section className="backup-form-section"><h2>运行计划</h2><Segment values={[['daily','每天'],['weekly','每周'],['cron','自定义 cron']]} value={form.scheduleKind} onChange={scheduleKind => setForm(v => ({ ...v, scheduleKind }))}/>{form.scheduleKind !== 'cron' ? <div className="backup-form-grid">{form.scheduleKind === 'weekly' && <Field label="星期"><select value={form.weekday} onChange={e => setForm(v => ({ ...v, weekday: e.target.value }))}>{['周日','周一','周二','周三','周四','周五','周六'].map((day, i) => <option key={day} value={i}>{day}</option>)}</select></Field>}<Field label="时间"><input type="time" value={form.time} onChange={e => setForm(v => ({ ...v, time: e.target.value }))}/></Field></div> : <Field label="Cron 表达式"><input className="mono" value={form.cron} onChange={e => setForm(v => ({ ...v, cron: e.target.value }))}/></Field>}</section>}
      {step === 3 && <section className="backup-form-section"><h2>规则与保留</h2><Field label="存储模式"><Segment values={[['versioned','时间版本'],['mirror','单目录镜像']]} value={form.mode} onChange={mode => setForm(v => ({ ...v, mode }))}/></Field><div className="backup-form-grid"><Field label="保留最近版本"><input type="number" min="1" max="999" value={form.keepLast} disabled={form.mode === 'mirror'} onChange={e => setForm(v => ({ ...v, keepLast: e.target.value }))}/></Field><Field label="同步策略"><label className="backup-check"><input type="checkbox" checked={form.delete} onChange={e => setForm(v => ({ ...v, delete: e.target.checked }))}/>删除目标端多余文件</label>{form.mode === 'versioned' && <label className="backup-check"><input type="checkbox" checked={form.incremental} onChange={e => setForm(v => ({ ...v, incremental: e.target.checked }))}/>硬链接增量</label>}</Field></div><Field label="排除规则"><textarea rows="5" value={form.excludes} onChange={e => setForm(v => ({ ...v, excludes: e.target.value }))} placeholder={'node_modules/\n*.tmp\n.cache/'}/></Field></section>}
      {step === 4 && <section className="backup-form-section"><h2>预检确认</h2><Summary payload={payload}/><button className="backup-secondary backup-check-button" onClick={check} disabled={busy}><Icon name="shield" size={15}/>{busy ? '检查中' : '执行预检'}</button>{preflight && <Preflight result={preflight}/>}</section>}
      {error && <div className="backup-form-error"><Icon name="alertTri" size={15}/>{error}</div>}
    </main>
    <footer className="backup-wizard-footer"><button className="backup-secondary" onClick={step === 0 ? onClose : () => { setStep(v => v - 1); setPreflight(null) }}>{step === 0 ? '取消' : '上一步'}</button><div/>{step < 4 ? <button className="backup-primary" disabled={(step === 0 && (!form.name.trim() || !form.source.path.trim())) || (step === 1 && !form.target.path.trim())} onClick={() => setStep(v => v + 1)}>下一步<Icon name="chevRight" size={14}/></button> : <button className="backup-primary" disabled={!preflight?.ok || busy} onClick={create}><Icon name="check" size={14}/>创建任务</button>}</footer>
  </div>
}

function EndpointEditor({ value, onChange, allowMount }) {
  const types = allowMount ? [['local','本机目录'],['mount','外接设备'],['ssh','远程服务器']] : [['local','本机目录'],['ssh','远程服务器']]
  return <><Segment values={types} value={value.type} onChange={type => onChange({ type })}/><div className="backup-form-grid"><Field label="目录路径"><input className="mono" value={value.path} onChange={e => onChange({ path: e.target.value })} placeholder={value.type === 'mount' ? '/mnt/backup' : '/data/projects'}/></Field>{value.type === 'ssh' && <><Field label="SSH 用户与主机"><input className="mono" value={value.host} onChange={e => onChange({ host: e.target.value })} placeholder="backup@example.com"/></Field><Field label="SSH 端口"><input type="number" min="1" max="65535" value={value.port} onChange={e => onChange({ port: e.target.value })}/></Field><Field label="Identity file"><input className="mono" value={value.identityFile} onChange={e => onChange({ identityFile: e.target.value })} placeholder="/root/.ssh/id_ed25519"/></Field></>}</div></>
}

function Segment({ values, value, onChange }) { return <div className="backup-segment">{values.map(([id,label]) => <button key={id} className={value === id ? 'active' : ''} onClick={() => onChange(id)}>{label}</button>)}</div> }
function Field({ label, children }) { return <label className="backup-field"><span>{label}</span>{children}</label> }

function Summary({ payload }) { return <div className="backup-summary"><div><span>任务</span><strong>{payload.name}</strong></div><div><span>源</span><code>{payload.source.host ? `${payload.source.host}:` : ''}{payload.source.path}</code></div><div><span>目标</span><code>{payload.target.host ? `${payload.target.host}:` : ''}{payload.target.path}</code></div><div><span>模式</span><strong>{payload.mode === 'versioned' ? `${payload.incremental ? '增量' : '全量'}版本 · 保留 ${payload.retention.keepLast} 份` : '单目录镜像'}</strong></div></div> }

function Preflight({ result }) { return <div className={`backup-preflight ${result.ok ? 'ok' : 'failed'}`}><div className="backup-preflight-heading"><Icon name={result.ok ? 'check' : 'alertTri'} size={17}/><strong>{result.ok ? '预检通过' : '预检未通过'}</strong><span>预计 {formatBytes(result.estimatedBytes)} · 可用 {formatBytes(result.availableBytes)}</span></div>{result.checks.map(check => <div key={check.name} className="backup-check-row"><Icon name={check.ok ? 'check' : 'x'} size={14}/><strong>{check.name}</strong><span>{check.message}</span></div>)}</div> }

function HistoryView({ task, taskId, onBack }) {
  const { data: histories = [], refresh } = useBackupHistory(taskId)
  const { data: versions = [], refresh: refreshVersions } = useBackupVersions(taskId)
  const [log, setLog] = useState(null)
  const [restoreVersion, setRestoreVersion] = useState('')
  const [destination, setDestination] = useState('')
  const [preview, setPreview] = useState(null)
  const [confirmed, setConfirmed] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const selectedVersion = restoreVersion || versions[versions.length - 1] || ''
  async function showLog(history) { setError(''); try { setLog(await fetchBackupLog(taskId, history.id)) } catch (err) { setError(err.message) } }
  async function doPreview() { setBusy(true); setError(''); setPreview(null); setConfirmed(false); try { setPreview(await previewBackupRestore(taskId, { version: selectedVersion, destination })) } catch (err) { setError(err.message) } finally { setBusy(false) } }
  async function doRestore() { setBusy(true); setError(''); try { await restoreBackup(taskId, { version: selectedVersion, destination, confirm: true, previewToken: preview.token }); setPreview(null); setConfirmed(false); refresh() } catch (err) { setError(err.message) } finally { setBusy(false) } }

  return <div className="backup-app"><header className="backup-toolbar"><div className="backup-back-title"><IconButton icon="chevLeft" title="返回任务列表" onClick={onBack}/><div><h1>{task?.name || '任务历史'}</h1><span>历史记录与恢复</span></div></div><IconButton icon="refresh" title="刷新" onClick={() => { refresh(); refreshVersions() }}/></header><main className="backup-history-layout"><section className="backup-history-list"><h2>运行历史</h2>{histories.length === 0 ? <Empty icon="history" text="暂无运行记录"/> : histories.map(history => { const [tone,label] = statusMeta(history.status); return <button key={history.id} className="backup-history-row" onClick={() => showLog(history)}><span className={`backup-kind ${history.kind}`}>{history.kind === 'restore' ? <Icon name="restore" size={14}/> : <Icon name="layers" size={14}/>}</span><span><strong>{history.kind === 'restore' ? '恢复' : history.version || '备份'}</strong><small>{formatDate(history.startedAt)} · {formatBytes(history.transferredBytes)}</small></span><Chip tone={tone}>{label}</Chip><span className="backup-phase">{history.phase}</span><Icon name="log" size={14}/></button> })}</section><aside className="backup-restore-panel"><h2>恢复</h2><Field label="备份版本"><select value={selectedVersion} onChange={e => { setRestoreVersion(e.target.value); setPreview(null) }}>{versions.slice().reverse().map(version => <option key={version}>{version}</option>)}</select></Field><Field label="恢复位置"><input className="mono" value={destination} onChange={e => { setDestination(e.target.value); setPreview(null) }} placeholder={task?.source?.path || '/data/restore'}/></Field><button className="backup-secondary backup-full" disabled={!selectedVersion || busy} onClick={doPreview}><Icon name="search" size={14}/>{busy ? '检查中' : '预览冲突'}</button>{preview && <div className="backup-restore-preview"><div><strong>{preview.conflicts.length} 个冲突</strong><span>{preview.changes.length} 项变更</span></div><div className="backup-conflicts">{preview.conflicts.length ? preview.conflicts.map(path => <code key={path}>{path}</code>) : <span>没有文件冲突</span>}</div><label className="backup-check"><input type="checkbox" checked={confirmed} onChange={e => setConfirmed(e.target.checked)}/>确认覆盖以上冲突文件</label><button className="backup-primary backup-full" disabled={!confirmed || busy} onClick={doRestore}><Icon name="restore" size={14}/>确认恢复</button></div>}{error && <div className="backup-form-error"><Icon name="alertTri" size={14}/>{error}</div>}</aside></main>{log && <div className="backup-log-overlay" onClick={() => setLog(null)}><section onClick={e => e.stopPropagation()}><header><div><strong>运行日志</strong><span>{log.phase}{log.error ? ` · ${log.error}` : ''}</span></div><IconButton icon="x" title="关闭日志" onClick={() => setLog(null)}/></header><pre>{log.log || '此运行没有输出。'}</pre></section></div>}</div>
}
