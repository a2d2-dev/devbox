import { useEffect, useMemo, useRef, useState } from 'react'
import { Icon } from '../icons'
import { btnDanger, btnPrimary, btnSecondary } from '../components/AppWindow'
import { useToast } from '../components/toastContext'
import { downloadRequest, useDownloads } from '../hooks/useApi'
import './Downloads.css'

const FILTERS = [
  ['all', '全部'],
  ['waiting', '等待'],
  ['downloading', '下载中'],
  ['completed', '完成'],
  ['paused', '暂停'],
  ['error', '错误'],
]

const STATUS = {
  waiting: { label: '等待', tone: '#64748b', bg: '#f1f5f9' },
  downloading: { label: '下载中', tone: '#0066ff', bg: '#e6f4ff' },
  paused: { label: '已暂停', tone: '#b45309', bg: '#fffbeb' },
  completed: { label: '已完成', tone: '#047857', bg: '#ecfdf5' },
  error: { label: '错误', tone: '#b91c1c', bg: '#fef2f2' },
}

export default function Downloads() {
  const { data, loading, error, refresh } = useDownloads(1000)
  const [filter, setFilter] = useState('all')
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState(() => new Set())
  const [detailId, setDetailId] = useState(null)
  const [addOpen, setAddOpen] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const toast = useToast()

  const tasks = useMemo(() => data?.tasks || [], [data?.tasks])
  const visible = useMemo(() => tasks.filter(task => {
    const statusMatch = filter === 'all' || task.status === filter
    const needle = query.trim().toLowerCase()
    return statusMatch && (!needle || task.name.toLowerCase().includes(needle))
  }), [tasks, filter, query])
  const detail = tasks.find(task => task.id === detailId) || null
  const selectedTasks = tasks.filter(task => selected.has(task.id))
  const allVisibleSelected = visible.length > 0 && visible.every(task => selected.has(task.id))

  const toggleAll = () => {
    setSelected(current => {
      const next = new Set(current)
      if (allVisibleSelected) visible.forEach(task => next.delete(task.id))
      else visible.forEach(task => next.add(task.id))
      return next
    })
  }

  const runBatch = async (action) => {
    const applicable = selectedTasks.filter(task => action === 'start'
      ? ['waiting', 'paused', 'error'].includes(task.status)
      : ['waiting', 'downloading'].includes(task.status))
    const skipped = selectedTasks.length - applicable.length
    if (!applicable.length) {
      toast.err(`所选任务无可执行项：成功 0，失败 0，跳过 ${skipped}`)
      return
    }
    setBusy(true)
    const results = await Promise.allSettled(applicable.map(task => downloadRequest(`/${task.id}/${action}`, { method: 'POST' })))
    const succeeded = results.filter(result => result.status === 'fulfilled').length
    const failed = results.length - succeeded
    setBusy(false)
    refresh()
    const summary = `操作完成：成功 ${succeeded}，失败 ${failed}，跳过 ${skipped}`
    if (failed) toast.err(summary)
    else toast.ok(summary)
  }

  if (error && !data) return <UnavailableState error={error} onRetry={refresh}/>

  return (
    <div className="downloads-app">
      <header className="downloads-summary">
        <div className="downloads-summary-title">
          <div className="downloads-mark"><Icon name="download" size={20} stroke={2}/></div>
          <div><strong>下载任务</strong><span>{data?.rootDirectory || '工作区下载目录'}</span></div>
        </div>
        <RateStat icon="arrowDown" label="实时下载" value={`${formatBytes(data?.statistics?.downloadSpeedBytesPerSec || 0)}/s`} tone="#0066ff"/>
        <RateStat icon="arrowUp" label="实时上传" value={`${formatBytes(data?.statistics?.uploadSpeedBytesPerSec || 0)}/s`} tone="#0f766e"/>
        <RateStat icon="database" label="累计下载" value={formatBytes(data?.statistics?.totalDownloadedBytes || 0)} tone="#475569"/>
      </header>

      <nav className="downloads-tabs" aria-label="下载任务状态筛选">
        {FILTERS.map(([id, label]) => (
          <button key={id} onClick={() => setFilter(id)} className={filter === id ? 'active' : ''}>
            {label}<span>{data?.counts?.[id] || 0}</span>
          </button>
        ))}
      </nav>

      <div className="downloads-toolbar">
        <button className="edge-press edge-btn-primary" style={btnPrimary} onClick={() => setAddOpen(true)}>
          <Icon name="plus" size={14} stroke={2}/>添加任务
        </button>
        <div className="downloads-toolbar-rule"/>
        <button className="edge-press edge-btn-secondary" style={btnSecondary} disabled={busy || !selectedTasks.length} onClick={() => runBatch('start')} title="开始所选任务">
          <Icon name="play" size={13} stroke={2}/>开始
        </button>
        <button className="edge-press edge-btn-secondary" style={btnSecondary} disabled={busy || !selectedTasks.length} onClick={() => runBatch('pause')} title="暂停所选任务">
          <Icon name="pause" size={13} stroke={2}/>暂停
        </button>
        <button className="edge-press edge-btn-danger" style={btnDanger} disabled={busy || !selectedTasks.length} onClick={() => setDeleteOpen(true)}>
          <Icon name="trash" size={13} stroke={2}/>删除
        </button>
        <div className="downloads-selection">{selectedTasks.length ? `已选择 ${selectedTasks.length} 项` : ''}</div>
        <label className="downloads-search">
          <Icon name="search" size={14}/>
          <input value={query} onChange={event => setQuery(event.target.value)} placeholder="搜索任务名称" aria-label="搜索任务名称"/>
        </label>
        <button className="downloads-icon-button" onClick={refresh} title="刷新任务" aria-label="刷新任务"><Icon name="refresh" size={15}/></button>
      </div>

      <main className={`downloads-content ${detail ? 'with-detail' : ''}`}>
        <section className="downloads-list" aria-label="下载任务列表">
          <div className="downloads-list-head">
            <input type="checkbox" checked={allVisibleSelected} onChange={toggleAll} aria-label="选择当前列表全部任务"/>
            <span>任务</span><span>大小</span><span>状态</span><span>速度 / 剩余</span>
          </div>
          {loading && !data && <div className="downloads-empty">正在读取下载任务...</div>}
          {!loading && !error && tasks.length === 0 && <EmptyState onAdd={() => setAddOpen(true)}/>}
          {tasks.length > 0 && visible.length === 0 && <div className="downloads-empty"><Icon name="search" size={28}/><strong>没有匹配的任务</strong><span>调整筛选状态或搜索词</span></div>}
          {visible.map(task => (
            <TaskRow key={task.id} task={task} checked={selected.has(task.id)} active={detailId === task.id}
              onCheck={() => setSelected(current => toggleSet(current, task.id))} onOpen={() => setDetailId(task.id)}/>
          ))}
        </section>
        {detail && <TaskDetail task={detail} onClose={() => setDetailId(null)} onRefresh={refresh}/>}
      </main>

      {error && data && <div className="downloads-poll-warning">任务状态刷新失败，正在保留上次数据。<button onClick={refresh}>重试</button></div>}
      {addOpen && (
        <AddDialog root={data?.rootDirectory} onClose={() => setAddOpen(false)} onCreated={() => { setAddOpen(false); refresh(); toast.ok('下载任务已添加') }}/>
      )}
      {deleteOpen && (
        <DeleteDialog tasks={selectedTasks} onClose={() => setDeleteOpen(false)} onDone={(summary) => {
          setDeleteOpen(false); setSelected(new Set()); refresh()
          summary.failed ? toast.err(`删除完成：成功 ${summary.succeeded}，失败 ${summary.failed}`) : toast.ok(`删除完成：成功 ${summary.succeeded}，失败 0`)
        }}/>
      )}
    </div>
  )
}

function TaskRow({ task, checked, active, onCheck, onOpen }) {
  const status = STATUS[task.status] || STATUS.error
  const percent = task.totalBytes > 0 ? Math.min(100, Math.round(task.downloadedBytes / task.totalBytes * 100)) : 0
  return (
    <div className={`downloads-row ${active ? 'active' : ''}`} onClick={onOpen}>
      <input type="checkbox" checked={checked} onChange={onCheck} onClick={event => event.stopPropagation()} aria-label={`选择任务 ${task.name}`}/>
      <div className="downloads-task-main">
        <div className="downloads-file-icon"><Icon name="file" size={18}/></div>
        <div className="downloads-task-copy">
          <strong title={task.name}>{task.name}</strong>
          <div className="downloads-progress-track"><span style={{ width: `${percent}%`, background: status.tone }}/></div>
          <small>{formatBytes(task.downloadedBytes)} / {task.totalBytes ? formatBytes(task.totalBytes) : '未知大小'}{task.totalBytes ? ` · ${percent}%` : ''}</small>
        </div>
      </div>
      <span className="downloads-size">{task.totalBytes ? formatBytes(task.totalBytes) : '-'}</span>
      <span className="downloads-status" style={{ color: status.tone, background: status.bg }}>{status.label}</span>
      <div className="downloads-rate">
        <strong>{task.status === 'downloading' ? `${formatBytes(task.speedBytesPerSec)}/s` : '-'}</strong>
        <small>{task.status === 'downloading' ? formatETA(task.estimatedSeconds) : task.status === 'error' ? '可重试' : ''}</small>
      </div>
    </div>
  )
}

function TaskDetail({ task, onClose, onRefresh }) {
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const status = STATUS[task.status] || STATUS.error
  const act = async action => {
    setBusy(true)
    try {
      await downloadRequest(`/${task.id}/${action}`, { method: 'POST' })
      toast.ok(action === 'pause' ? '任务已暂停' : '任务已开始')
      onRefresh()
    } catch (error) { toast.err(error.message) }
    finally { setBusy(false) }
  }
  return (
    <aside className="downloads-detail">
      <div className="downloads-detail-head"><strong>任务详情</strong><button onClick={onClose} title="关闭详情" aria-label="关闭详情"><Icon name="x" size={15}/></button></div>
      <div className="downloads-detail-name"><div className="downloads-file-icon large"><Icon name="download" size={22}/></div><strong>{task.name}</strong><span className="downloads-status" style={{ color: status.tone, background: status.bg }}>{status.label}</span></div>
      {task.error && <div className="downloads-error-box"><Icon name="alertTri" size={15}/><div><strong>下载失败</strong><span>{task.error}</span></div></div>}
      <dl>
        <Detail label="进度" value={`${formatBytes(task.downloadedBytes)} / ${task.totalBytes ? formatBytes(task.totalBytes) : '未知'}`}/>
        <Detail label="实时速度" value={task.status === 'downloading' ? `${formatBytes(task.speedBytesPerSec)}/s` : '-'}/>
        <Detail label="剩余时间" value={task.status === 'downloading' ? formatETA(task.estimatedSeconds) : '-'}/>
        <Detail label="断点续传" value={task.resumeSupported ? '服务器支持' : task.downloadedBytes ? '未确认支持' : '等待检测'}/>
        <Detail label="目标文件" value={task.destination} mono/>
        <Detail label="来源 URL" value={task.url} mono/>
        <Detail label="创建时间" value={formatTime(task.createdAt)}/>
      </dl>
      <div className="downloads-detail-actions">
        {['waiting', 'paused', 'error'].includes(task.status) && <button disabled={busy} className="edge-press edge-btn-primary" style={btnPrimary} onClick={() => act('start')}><Icon name="play" size={13}/> {task.status === 'error' ? '重试' : '开始'}</button>}
        {['waiting', 'downloading'].includes(task.status) && <button disabled={busy} className="edge-press edge-btn-secondary" style={btnSecondary} onClick={() => act('pause')}><Icon name="pause" size={13}/>暂停</button>}
      </div>
    </aside>
  )
}

function Detail({ label, value, mono }) {
  return <div><dt>{label}</dt><dd className={mono ? 'mono' : ''} title={value}>{value}</dd></div>
}

function AddDialog({ root, onClose, onCreated }) {
  const [url, setURL] = useState('')
  const [directory, setDirectory] = useState('downloads')
  const [autoStart, setAutoStart] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const inputRef = useRef(null)
  useDialog(onClose, inputRef)
  const submit = async event => {
    event.preventDefault(); setBusy(true); setError('')
    try {
      await downloadRequest('', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ url, targetDirectory: directory, start: autoStart }) })
      onCreated()
    } catch (requestError) { setError(requestError.message); setBusy(false) }
  }
  return (
    <div className="downloads-dialog-backdrop" onMouseDown={event => event.target === event.currentTarget && !busy && onClose()}>
      <form className="downloads-dialog" onSubmit={submit} role="dialog" aria-modal="true" aria-labelledby="add-download-title">
        <div className="downloads-dialog-head"><div><strong id="add-download-title">添加下载任务</strong><span>仅支持 HTTP(S) 直链</span></div><button type="button" onClick={onClose} disabled={busy} aria-label="关闭"><Icon name="x"/></button></div>
        <label>下载地址<input ref={inputRef} type="url" required value={url} onChange={event => setURL(event.target.value)} placeholder="https://example.com/archive.zip"/></label>
        <label>目标目录<input required value={directory} onChange={event => setDirectory(event.target.value)} placeholder="downloads"/><small>下载根目录：{root || '工作区'}。不允许使用 .. 或访问根目录之外的位置。</small></label>
        <label className="downloads-check"><input type="checkbox" checked={autoStart} onChange={event => setAutoStart(event.target.checked)}/><span>添加后立即开始</span></label>
        {error && <div className="downloads-dialog-error">{error}</div>}
        <div className="downloads-dialog-actions"><button type="button" style={btnSecondary} onClick={onClose} disabled={busy}>取消</button><button type="submit" style={btnPrimary} disabled={busy || !url.trim()}>{busy ? '正在添加...' : '添加任务'}</button></div>
      </form>
    </div>
  )
}

function DeleteDialog({ tasks, onClose, onDone }) {
  const [deleteFile, setDeleteFile] = useState(false)
  const [busy, setBusy] = useState(false)
  const confirmRef = useRef(null)
  useDialog(onClose, confirmRef, busy)
  const remove = async () => {
    setBusy(true)
    const results = await Promise.allSettled(tasks.map(task => downloadRequest(`/${task.id}?deleteFile=${deleteFile}`, { method: 'DELETE' })))
    onDone({ succeeded: results.filter(result => result.status === 'fulfilled').length, failed: results.filter(result => result.status === 'rejected').length })
  }
  return (
    <div className="downloads-dialog-backdrop" onMouseDown={event => event.target === event.currentTarget && !busy && onClose()}>
      <div className="downloads-dialog compact" role="alertdialog" aria-modal="true" aria-labelledby="delete-download-title">
        <div className="downloads-dialog-head"><div><strong id="delete-download-title">删除 {tasks.length} 个下载任务？</strong><span>此操作会从任务中心移除所选记录</span></div></div>
        <div className="downloads-delete-options">
          <label className={!deleteFile ? 'active' : ''}><input type="radio" name="delete-mode" checked={!deleteFile} onChange={() => setDeleteFile(false)}/><div><strong>仅删除任务</strong><span>保留已下载文件和未完成的临时文件</span></div></label>
          <label className={deleteFile ? 'active danger' : ''}><input type="radio" name="delete-mode" checked={deleteFile} onChange={() => setDeleteFile(true)}/><div><strong>同时删除文件</strong><span>任务记录、目标文件和临时文件都会删除</span></div></label>
        </div>
        <div className="downloads-dialog-actions"><button style={btnSecondary} onClick={onClose} disabled={busy}>取消</button><button ref={confirmRef} style={btnDanger} onClick={remove} disabled={busy}>{busy ? '正在删除...' : '确认删除'}</button></div>
      </div>
    </div>
  )
}

function EmptyState({ onAdd }) {
  return <div className="downloads-empty"><div className="downloads-empty-icon"><Icon name="download" size={30}/></div><strong>还没有下载任务</strong><span>添加 HTTP(S) 直链后，任务进度会显示在这里</span><button style={btnPrimary} onClick={onAdd}><Icon name="plus" size={13}/>添加任务</button></div>
}

function UnavailableState({ error, onRetry }) {
  const forbidden = error.status === 403
  return <div className="downloads-unavailable"><div><Icon name={forbidden ? 'lock' : 'alertTri'} size={30}/></div><strong>{forbidden ? '没有下载目录权限' : error.status === 503 ? '下载引擎不可用' : '无法读取下载任务'}</strong><span>{error.message}</span><button style={btnSecondary} onClick={onRetry}><Icon name="refresh" size={13}/>重试</button></div>
}

function RateStat({ icon, label, value, tone }) {
  return <div className="downloads-rate-stat"><Icon name={icon} size={15} style={{ color: tone }}/><div><span>{label}</span><strong className="tnum">{value}</strong></div></div>
}

function useDialog(onClose, focusRef, busy = false) {
  useEffect(() => {
    focusRef.current?.focus()
    const onKey = event => { if (event.key === 'Escape' && !busy) onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose, focusRef, busy])
}

function toggleSet(current, id) { const next = new Set(current); if (next.has(id)) next.delete(id); else next.add(id); return next }
function formatBytes(bytes) { if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'; const units = ['B', 'KB', 'MB', 'GB', 'TB']; const i = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024))); const value = bytes / (1024 ** i); return `${value >= 100 || i === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[i]}` }
function formatETA(seconds) { if (!seconds) return '计算中'; if (seconds < 60) return `${seconds} 秒`; if (seconds < 3600) return `${Math.ceil(seconds / 60)} 分钟`; return `${Math.floor(seconds / 3600)} 小时 ${Math.ceil((seconds % 3600) / 60)} 分` }
function formatTime(value) { return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '-' }
