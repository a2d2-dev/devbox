import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { authFetch } from '../hooks/useApi'
import { FileIcon } from '../components/AppShell'
import { useToast } from '../components/toastContext'

const IMAGE_EXTS = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'ico', 'avif'])
const button = {
  display: 'inline-flex', alignItems: 'center', justifyContent: 'center', gap: 6,
  height: 30, padding: '0 10px', borderRadius: 6, border: `1px solid ${T.border}`,
  background: '#fff', color: T.ink2, fontSize: 12, fontWeight: 550, cursor: 'pointer',
}
const iconButton = { ...button, width: 30, padding: 0 }

function formatSize(value) {
  if (value == null || value === '') return ''
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = Number(value)
  let index = 0
  while (size >= 1000 && index < units.length - 1) { size /= 1000; index += 1 }
  return `${index === 0 ? size : size.toFixed(1)} ${units[index]}`
}

function formatDate(value) {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '' : date.toLocaleString('zh-CN', { hour12: false })
}

function parentPath(path) { const parts = path.split('/').filter(Boolean); parts.pop(); return parts.join('/') }
function entryKey(entry) { return entry.id || `${entry.source}:${entry.path}` }

async function apiJSON(url, options) {
  const response = await authFetch(url, options)
  const body = await response.json().catch(() => ({}))
  if (!response.ok) throw new Error(body.message || body.code || `请求失败 (${response.status})`)
  return body
}

function SidebarRow({ icon, label, active, disabled, detail, onClick }) {
  return (
    <button
      type="button" disabled={disabled && !onClick} onClick={onClick}
      className={!active ? 'edge-row-hover' : undefined}
      style={{
        width: '100%', minHeight: 32, display: 'flex', alignItems: 'center', gap: 8,
        padding: '6px 10px', border: 0, borderRadius: 5, textAlign: 'left',
        background: active ? T.blueSoft : 'transparent', color: active ? T.blueDeep : (disabled ? T.ink4 : T.ink2),
        fontSize: 12.5, fontWeight: active ? 650 : 500, cursor: onClick ? 'pointer' : 'default',
        '--edge-row-hover-bg': T.surface,
      }}>
      <Icon name={icon} size={14} stroke={1.8}/>
      <span style={{ minWidth: 0, flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{label}</span>
      {detail && <span style={{ fontSize: 9.5, color: T.ink4 }}>{detail}</span>}
    </button>
  )
}

function SectionLabel({ children }) {
  return <div style={{ padding: '12px 10px 5px', fontSize: 10, color: T.ink4, fontWeight: 700, textTransform: 'uppercase' }}>{children}</div>
}

function EmptyState({ icon = 'folder', title, message }) {
  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', padding: 32, textAlign: 'center' }}>
      <Icon name={icon} size={36} stroke={1.3} style={{ color: T.ink4 }}/>
      <div style={{ marginTop: 12, fontSize: 14, color: T.ink, fontWeight: 650 }}>{title}</div>
      <div style={{ marginTop: 6, maxWidth: 360, fontSize: 12, color: T.ink3, lineHeight: 1.6 }}>{message}</div>
    </div>
  )
}

export default function FilesFace() {
  const toast = useToast()
  const rootRef = useRef(null)
  const fileInputRef = useRef(null)
  const [history, setHistory] = useState([{ source: 'my', path: '' }])
  const [historyIndex, setHistoryIndex] = useState(0)
  const [sources, setSources] = useState([])
  const [sourceID, setSourceID] = useState('my')
  const [path, setPath] = useState('')
  const [view, setView] = useState('source')
  const [items, setItems] = useState([])
  const [selected, setSelected] = useState(new Set())
  const [query, setQuery] = useState('')
  const [sortBy, setSortBy] = useState('name')
  const [sortOrder, setSortOrder] = useState('asc')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [refreshKey, setRefreshKey] = useState(0)
  const [uploading, setUploading] = useState(false)
  const [moreOpen, setMoreOpen] = useState(false)
  const [preview, setPreview] = useState(null)

  const source = sources.find(item => item.id === sourceID)
  const capabilities = source?.capabilities || {}
  const selectedItems = useMemo(() => items.filter(item => selected.has(entryKey(item))), [items, selected])
  const one = selectedItems.length === 1 ? selectedItems[0] : null

  const reload = useCallback(() => setRefreshKey(value => value + 1), [])

  useEffect(() => {
    let cancelled = false
    apiJSON('/api/v1/files/sources').then(data => {
      if (!cancelled) setSources(Array.isArray(data) ? data : [])
    }).catch(err => { if (!cancelled) setError(err.message) })
    return () => { cancelled = true }
  }, [refreshKey])

  useEffect(() => {
    let cancelled = false
    const timer = setTimeout(async () => {
      setLoading(true); setError('')
      try {
        let url
        if (view === 'source') {
          if (source && !source.available) { setItems([]); setLoading(false); return }
          const params = new URLSearchParams({ source: sourceID, path, sort: sortBy, order: sortOrder })
          if (query.trim()) {
            params.set('q', query.trim())
            url = `/api/v1/files/search?${params}`
          } else url = `/api/v1/files?${params}`
        } else if (view === 'trash') {
          url = `/api/v1/files/trash?q=${encodeURIComponent(query.trim())}`
        } else if (view === 'favorites') url = '/api/v1/files/favorites'
        else if (view === 'recent') url = '/api/v1/files/recent'
        else url = '/api/v1/files/shares'
        const data = await apiJSON(url)
        if (!cancelled) {
          const next = Array.isArray(data) ? data : []
          const filtered = view !== 'source' && view !== 'trash' && query.trim()
            ? next.filter(item => `${item.name || ''} ${item.path || item.originalPath || ''}`.toLowerCase().includes(query.trim().toLowerCase()))
            : next
          setItems(filtered)
          setSelected(new Set())
        }
      } catch (err) { if (!cancelled) { setItems([]); setError(err.message) } }
      finally { if (!cancelled) setLoading(false) }
    }, query ? 220 : 0)
    return () => { cancelled = true; clearTimeout(timer) }
  }, [view, sourceID, path, query, sortBy, sortOrder, refreshKey, source])

  const navigate = useCallback((nextSource, nextPath, push = true) => {
    setView('source'); setSourceID(nextSource); setPath(nextPath); setQuery(''); setSelected(new Set())
    if (push) {
      const nextHistory = history.slice(0, historyIndex + 1)
      nextHistory.push({ source: nextSource, path: nextPath })
      setHistory(nextHistory)
      setHistoryIndex(nextHistory.length - 1)
    }
  }, [history, historyIndex])

  const goHistory = direction => {
    const index = historyIndex + direction
    const target = history[index]
    if (!target) return
    setHistoryIndex(index)
    navigate(target.source, target.path, false)
  }

  const openCollection = nextView => {
    setView(nextView); setPath(''); setQuery(''); setSelected(new Set()); setMoreOpen(false)
  }

  const post = async (url, body) => apiJSON(url, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
  })

  const uploadFiles = useCallback(async fileList => {
    const files = Array.from(fileList || [])
    if (!files.length || uploading || view !== 'source' || !capabilities.upload) return
    setUploading(true)
    try {
      for (const file of files) {
        const form = new FormData()
        form.append('source', sourceID); form.append('path', path); form.append('name', file.name); form.append('file', file, file.name)
        await apiJSON('/api/v1/files/upload', { method: 'POST', body: form })
      }
      toast.ok(files.length === 1 ? `已上传 ${files[0].name}` : `已上传 ${files.length} 个文件`)
      reload()
    } catch (err) { toast.err(err.message) }
    finally { setUploading(false); if (fileInputRef.current) fileInputRef.current.value = '' }
  }, [uploading, view, capabilities.upload, sourceID, path, toast, reload])

  useEffect(() => {
    const onPaste = event => {
      if (event.target?.matches?.('input, textarea, [contenteditable="true"]')) return
      const image = Array.from(event.clipboardData?.items || []).find(item => item.type?.startsWith('image/'))?.getAsFile()
      if (!image || view !== 'source' || !capabilities.upload) return
      event.preventDefault()
      const ext = (image.type.split('/')[1] || 'png').split(';')[0]
      const stamp = new Date().toISOString().replace(/[-:T]/g, '').slice(0, 14)
      uploadFiles([new File([image], `screenshot-${stamp}.${ext}`, { type: image.type })])
    }
    window.addEventListener('paste', onPaste)
    return () => window.removeEventListener('paste', onPaste)
  }, [view, capabilities.upload, uploadFiles])

  useEffect(() => {
    if (!one || one.isDir || !IMAGE_EXTS.has((one.type || one.name?.split('.').pop() || '').toLowerCase()) || !one.source || !one.path) return undefined
    let cancelled = false
    let objectURL = ''
    const key = entryKey(one)
    authFetch(`/api/v1/files/content?source=${encodeURIComponent(one.source)}&path=${encodeURIComponent(one.path)}`)
      .then(response => response.ok ? response.blob() : null)
      .then(blob => { if (blob && !cancelled) { objectURL = URL.createObjectURL(blob); setPreview({ key, url: objectURL }) } })
      .catch(() => {})
    return () => { cancelled = true; if (objectURL) URL.revokeObjectURL(objectURL) }
    // previewURL is intentionally managed by this effect's cleanup.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [one?.source, one?.path, one?.isDir, one?.type, one?.name])

  const createFolder = async () => {
    const name = window.prompt('新文件夹名称')?.trim()
    if (!name) return
    try { await post('/api/v1/files/mkdir', { source: sourceID, path, name }); toast.ok(`已创建 ${name}`); reload() }
    catch (err) { toast.err(err.message) }
  }

  const rename = async () => {
    const name = window.prompt('新名称', one?.name)?.trim()
    if (!name || !one) return
    try { await post('/api/v1/files/rename', { source: one.source, path: one.path, name }); toast.ok('已重命名'); reload() }
    catch (err) { toast.err(err.message) }
  }

  const transfer = async copy => {
    if (!one) return
    const destination = window.prompt(copy ? '复制到目录（相对来源根）' : '移动到目录（相对来源根）', '')
    if (destination == null) return
    try { await post('/api/v1/files/transfer', { source: one.source, path: one.path, destination, copy }); toast.ok(copy ? '已复制' : '已移动'); reload() }
    catch (err) { toast.err(err.message) }
  }

  const download = async () => {
    if (!one || one.isDir) return
    try {
      const response = await authFetch(`/api/v1/files/download?source=${encodeURIComponent(one.source)}&path=${encodeURIComponent(one.path)}`)
      if (!response.ok) throw new Error(`下载失败 (${response.status})`)
      const blob = await response.blob()
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a'); link.href = url; link.download = one.name; link.click()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch (err) { toast.err(err.message) }
  }

  const toggleFavorite = async enabled => {
    if (!one) return
    try { await post('/api/v1/files/favorites', { source: one.source, path: one.path, enabled }); toast.ok(enabled ? '已收藏' : '已取消收藏'); reload() }
    catch (err) { toast.err(err.message) }
  }

  const createShare = async () => {
    if (!one || one.isDir) return
    const choice = window.prompt('有效期：1h、24h、7d，留空表示永久', '24h')
    if (choice == null) return
    const values = { '': 0, '1h': 3600, '24h': 86400, '7d': 604800 }
    if (!(choice in values)) { toast.err('有效期仅支持 1h、24h、7d 或留空'); return }
    try {
      const share = await post('/api/v1/files/shares', { source: one.source, path: one.path, expiresIn: values[choice] })
      const url = `${window.location.origin}${share.url}`
      const copied = await copyText(url)
      toast.ok(copied ? '外链已创建并复制' : `外链已创建：${url}`)
    } catch (err) { toast.err(err.message) }
  }

  const deleteSelected = async () => {
    if (!selectedItems.length) return
    const permanent = !capabilities.trash
    const message = permanent
      ? `此来源不支持回收站，将永久删除 ${selectedItems.length} 项。此操作不可撤销，确认继续？`
      : `将 ${selectedItems.length} 项移入回收站？`
    if (!window.confirm(message)) return
    try {
      for (const item of selectedItems) await post('/api/v1/files/delete', { source: item.source, path: item.path, permanent, confirm: permanent })
      toast.ok(permanent ? '已永久删除' : '已移入回收站'); reload()
    } catch (err) { toast.err(err.message) }
  }

  const restoreTrash = async () => {
    try { for (const item of selectedItems) await post('/api/v1/files/trash/restore', { id: item.id }); toast.ok('已恢复到原路径'); reload() }
    catch (err) { toast.err(err.message) }
  }

  const purgeTrash = async () => {
    if (!window.confirm(`永久删除选中的 ${selectedItems.length} 项？此操作不可撤销。`)) return
    try { for (const item of selectedItems) await post('/api/v1/files/trash/purge', { id: item.id, confirm: true }); toast.ok('已永久删除'); reload() }
    catch (err) { toast.err(err.message) }
  }

  const emptyTrash = async () => {
    if (!window.confirm('清空回收站？所有内容将永久删除且无法恢复。')) return
    try { await post('/api/v1/files/trash/empty', { confirm: true }); toast.ok('回收站已清空'); reload() }
    catch (err) { toast.err(err.message) }
  }

  const revokeShare = async () => {
    if (!one || !window.confirm(`撤销「${one.name}」的外链？`)) return
    try { await apiJSON(`/api/v1/files/shares/${encodeURIComponent(one.id)}`, { method: 'DELETE' }); toast.ok('外链已撤销'); reload() }
    catch (err) { toast.err(err.message) }
  }

  const openEntry = entry => {
    if (view === 'source' && entry.isDir) navigate(entry.source, entry.path)
    else if ((view === 'favorites' || view === 'recent') && entry.isDir) navigate(entry.source, entry.path)
    else if ((view === 'favorites' || view === 'recent') && !entry.isDir) {
      setSourceID(entry.source); setSelected(new Set([entryKey(entry)]))
    }
  }

  const external = sources.filter(item => item.kind === 'external')
  const network = sources.filter(item => item.kind === 'network')
  const configured = sources.filter(item => ['personal', 'configured'].includes(item.kind))
  const apps = sources.find(item => item.kind === 'applications')
  const breadcrumbs = path.split('/').filter(Boolean)
  const previewURL = one && preview?.key === entryKey(one) ? preview.url : ''
  const viewTitle = { trash: '回收站', favorites: '我的收藏', recent: '最近访问', shares: '外链管理' }[view]
  const empty = !loading && !error && items.length === 0

  return (
    <div ref={rootRef} tabIndex={0} style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', background: T.surface, overflow: 'hidden', outline: 'none' }}>
      <div style={{ minHeight: 50, padding: '9px 14px', borderBottom: `1px solid ${T.border}`, display: 'flex', alignItems: 'center', gap: 7, flexWrap: 'wrap' }}>
        <button title="后退" aria-label="后退" disabled={historyIndex === 0} onClick={() => goHistory(-1)} style={{ ...iconButton, opacity: historyIndex === 0 ? .4 : 1 }}><Icon name="chevLeft" size={13}/></button>
        <button title="前进" aria-label="前进" disabled={historyIndex >= history.length - 1} onClick={() => goHistory(1)} style={{ ...iconButton, opacity: historyIndex >= history.length - 1 ? .4 : 1 }}><Icon name="chevRight" size={13}/></button>
        <button title="刷新" aria-label="刷新" onClick={reload} style={iconButton}><Icon name="refresh" size={13}/></button>
        <div style={{ minWidth: 180, flex: 1, height: 30, display: 'flex', alignItems: 'center', gap: 5, padding: '0 10px', border: `1px solid ${T.border}`, borderRadius: 6, background: T.surfaceAlt, fontSize: 12 }}>
          {view === 'source' ? <>
            <span onClick={() => navigate(sourceID, '')} style={{ cursor: 'pointer', fontWeight: 650, color: T.blueDeep }}>{source?.name || '文件'}</span>
            {breadcrumbs.map((part, index) => <span key={`${part}-${index}`} style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}><Icon name="chevRight" size={10}/><span onClick={() => navigate(sourceID, breadcrumbs.slice(0, index + 1).join('/'))} style={{ cursor: 'pointer' }}>{part}</span></span>)}
          </> : <span style={{ fontWeight: 650 }}>{viewTitle}</span>}
        </div>
        <div style={{ width: 210, height: 30, display: 'flex', alignItems: 'center', gap: 6, padding: '0 9px', border: `1px solid ${T.border}`, borderRadius: 6, background: '#fff' }}>
          <Icon name="search" size={13} style={{ color: T.ink4 }}/><input value={query} onChange={event => setQuery(event.target.value)} placeholder={view === 'trash' ? '搜索回收站' : '搜索当前范围'} style={{ minWidth: 0, flex: 1, border: 0, outline: 0, background: 'transparent', fontSize: 12 }}/>
        </div>
        {view === 'source' && <>
          <button disabled={!capabilities.mkdir} onClick={createFolder} style={{ ...button, opacity: capabilities.mkdir ? 1 : .45 }}><Icon name="plus" size={12}/>新建文件夹</button>
          <input ref={fileInputRef} type="file" multiple style={{ display: 'none' }} onChange={event => uploadFiles(event.target.files)}/>
          <button disabled={!capabilities.upload || uploading} onClick={() => fileInputRef.current?.click()} style={{ ...button, background: T.blue, color: '#fff', borderColor: T.blue, opacity: capabilities.upload ? 1 : .45 }}><Icon name="upload" size={12}/>{uploading ? '上传中' : '上传'}</button>
        </>}
        {view === 'trash' && <button disabled={!items.length} onClick={emptyTrash} style={{ ...button, color: '#b42318', opacity: items.length ? 1 : .45 }}><Icon name="trash" size={12}/>清空</button>}
      </div>

      <div style={{ flex: 1, minHeight: 0, display: 'flex' }}>
        <aside style={{ width: 210, flexShrink: 0, overflowY: 'auto', padding: '5px 8px 14px', borderRight: `1px solid ${T.border}`, background: T.surfaceAlt }}>
          <SectionLabel>位置</SectionLabel>
          {configured.map(item => <SidebarRow key={item.id} icon="folder" label={item.name} active={view === 'source' && sourceID === item.id} disabled={!item.available} detail={!item.available ? '不可用' : ''} onClick={() => navigate(item.id, '')}/>)}
          <SidebarRow icon="layers" label={apps?.name || '应用文件'} active={view === 'source' && sourceID === 'apps'} disabled={!apps?.available} detail={!apps?.available ? '未配置' : ''} onClick={() => navigate('apps', '')}/>
          <SectionLabel>挂载</SectionLabel>
          {external.map(item => <SidebarRow key={item.id} icon="hardDrive" label={item.name} active={view === 'source' && sourceID === item.id} onClick={() => navigate(item.id, '')}/>)}
          {!external.length && <SidebarRow icon="hardDrive" label="外接存储" disabled detail="未检测到"/>}
          {network.map(item => <SidebarRow key={item.id} icon="network" label={item.name} active={view === 'source' && sourceID === item.id} onClick={() => navigate(item.id, '')}/>)}
          {!network.length && <SidebarRow icon="network" label="远程挂载" disabled detail="未挂载"/>}
          <SectionLabel>集合</SectionLabel>
          <SidebarRow icon="star" label="我的收藏" active={view === 'favorites'} onClick={() => openCollection('favorites')}/>
          <SidebarRow icon="history" label="最近访问" active={view === 'recent'} onClick={() => openCollection('recent')}/>
          <SidebarRow icon="trash" label="回收站" active={view === 'trash'} onClick={() => openCollection('trash')}/>
          <SidebarRow icon="link" label="外链管理" active={view === 'shares'} onClick={() => openCollection('shares')}/>
        </aside>

        <main style={{ minWidth: 0, flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          <div style={{ minHeight: 41, padding: '5px 12px', display: 'flex', alignItems: 'center', gap: 7, borderBottom: `1px solid ${T.borderSoft}`, background: '#fff' }}>
            {view === 'source' && <>
              <button disabled={!one || one.isDir} onClick={download} style={{ ...button, opacity: one && !one.isDir ? 1 : .45 }}><Icon name="download" size={12}/>下载</button>
              <button disabled={!one || !capabilities.favorite} onClick={() => toggleFavorite(true)} style={{ ...button, opacity: one && capabilities.favorite ? 1 : .45 }}><Icon name="star" size={12}/>收藏</button>
              <button disabled={!one || one.isDir || !capabilities.share} onClick={createShare} style={{ ...button, opacity: one && !one.isDir && capabilities.share ? 1 : .45 }}><Icon name="link" size={12}/>分享</button>
              <button disabled={!selectedItems.length || !capabilities.delete} onClick={deleteSelected} style={{ ...button, color: '#b42318', opacity: selectedItems.length && capabilities.delete ? 1 : .45 }}><Icon name="trash" size={12}/>删除</button>
              <div style={{ position: 'relative' }}>
                <button disabled={!one} onClick={() => setMoreOpen(value => !value)} style={{ ...button, opacity: one ? 1 : .45 }}>更多<Icon name="chevDown" size={11}/></button>
                {moreOpen && one && <div style={{ position: 'absolute', zIndex: 20, top: 34, left: 0, width: 150, padding: 4, border: `1px solid ${T.border}`, borderRadius: 6, background: '#fff', boxShadow: '0 8px 24px rgba(15,23,42,.15)' }}>
                  <MenuButton label="重命名" disabled={!capabilities.rename} onClick={() => { setMoreOpen(false); rename() }}/>
                  <MenuButton label="移动到…" disabled={!capabilities.move} onClick={() => { setMoreOpen(false); transfer(false) }}/>
                  <MenuButton label="复制到…" disabled={!capabilities.copy} onClick={() => { setMoreOpen(false); transfer(true) }}/>
                  {one.absPath && (
                    <MenuButton label="复制完整路径" onClick={async () => { setMoreOpen(false); await copyText(one.absPath); toast.ok('路径已复制') }}/>
                  )}
                </div>}
              </div>
              <span style={{ marginLeft: 'auto', fontSize: 11, color: T.ink4 }}>{source?.available ? `${items.length} 项` : ''}</span>
              <select value={sortBy} onChange={event => setSortBy(event.target.value)} aria-label="排序字段" style={{ ...button, outline: 0 }}><option value="name">名称</option><option value="size">大小</option><option value="time">时间</option></select>
              <button title="切换排序方向" aria-label="切换排序方向" onClick={() => setSortOrder(value => value === 'asc' ? 'desc' : 'asc')} style={iconButton}><Icon name={sortOrder === 'asc' ? 'arrowUp' : 'arrowDown'} size={12}/></button>
            </>}
            {view === 'trash' && <><button disabled={!selectedItems.length} onClick={restoreTrash} style={{ ...button, opacity: selectedItems.length ? 1 : .45 }}><Icon name="refresh" size={12}/>恢复</button><button disabled={!selectedItems.length} onClick={purgeTrash} style={{ ...button, color: '#b42318', opacity: selectedItems.length ? 1 : .45 }}><Icon name="trash" size={12}/>永久删除</button></>}
            {view === 'favorites' && <><button disabled={!one} onClick={() => toggleFavorite(false)} style={{ ...button, opacity: one ? 1 : .45 }}><Icon name="star" size={12}/>取消收藏</button><button disabled={!one} onClick={() => one && navigate(one.source, one.isDir ? one.path : parentPath(one.path))} style={{ ...button, opacity: one ? 1 : .45 }}><Icon name="folder" size={12}/>打开所在位置</button></>}
            {view === 'recent' && <button disabled={!one} onClick={() => one && navigate(one.source, one.isDir ? one.path : parentPath(one.path))} style={{ ...button, opacity: one ? 1 : .45 }}><Icon name="folder" size={12}/>打开所在位置</button>}
            {view === 'shares' && <button disabled={!one} onClick={revokeShare} style={{ ...button, color: '#b42318', opacity: one ? 1 : .45 }}><Icon name="x" size={12}/>撤销外链</button>}
          </div>

          <div style={{ flex: 1, minHeight: 0, display: 'flex', overflow: 'hidden' }}>
            <div style={{ minWidth: 0, flex: 1, overflow: 'auto' }}>
              {source && view === 'source' && !source.available ? <EmptyState icon="folder" title={`${source.name}不可用`} message={source.reason || '请先配置并挂载对应目录。'}/>
                : loading ? <EmptyState icon="refresh" title="正在加载" message=""/>
                : error ? <EmptyState icon="alertTri" title="无法加载文件" message={error}/>
                : empty ? <EmptyState icon={view === 'trash' ? 'trash' : view === 'shares' ? 'link' : 'folder'} title={view === 'trash' ? '回收站为空' : view === 'shares' ? '暂无外链' : query ? '没有匹配结果' : '这里还没有文件'} message={view === 'source' && source?.kind === 'applications' ? 'Compose 应用数据目录中暂无可管理文件。' : ''}/>
                : <FileTable view={view} items={items} selected={selected} setSelected={setSelected} onOpen={openEntry}/>}
            </div>
            {one && view !== 'trash' && view !== 'shares' && <aside style={{ width: 260, flexShrink: 0, borderLeft: `1px solid ${T.borderSoft}`, background: T.surfaceAlt, overflow: 'auto' }}>
              <div style={{ height: 170, display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 14, borderBottom: `1px solid ${T.borderSoft}`, background: '#fff' }}>
                {previewURL
                  ? <img src={previewURL} alt={one.name} style={{ maxWidth: '100%', maxHeight: '100%', objectFit: 'contain' }}/>
                  : <FileIcon type={one.isDir ? 'dir' : one.type} size={64}/>
                }
              </div>
              <div style={{ padding: 14 }}>
                <div style={{ fontSize: 13, fontWeight: 650, color: T.ink, overflowWrap: 'anywhere' }}>{one.name}</div>
                <Detail label="位置" value={one.path || one.originalPath}/><Detail label="大小" value={formatSize(one.size) || '—'}/><Detail label="时间" value={formatDate(one.modified || one.openedAt || one.addedAt) || '—'}/>
              </div>
            </aside>}
          </div>
        </main>
      </div>
    </div>
  )
}

function MenuButton({ label, disabled, onClick }) {
  return <button type="button" disabled={disabled} onClick={onClick} style={{ width: '100%', height: 30, padding: '0 9px', border: 0, borderRadius: 4, background: 'transparent', color: disabled ? T.ink4 : T.ink2, textAlign: 'left', fontSize: 12, cursor: disabled ? 'not-allowed' : 'pointer' }}>{label}</button>
}

function Detail({ label, value }) {
  return <div style={{ display: 'flex', gap: 8, marginTop: 10, fontSize: 11.5 }}><span style={{ width: 42, flexShrink: 0, color: T.ink4 }}>{label}</span><span style={{ minWidth: 0, color: T.ink2, overflowWrap: 'anywhere' }}>{value}</span></div>
}

function FileTable({ view, items, selected, setSelected, onOpen }) {
  const allSelected = items.length > 0 && items.every(item => selected.has(entryKey(item)))
  const toggle = (key, checked) => setSelected(current => { const next = new Set(current); if (checked) next.add(key); else next.delete(key); return next })
  return (
    <table style={{ width: '100%', borderCollapse: 'collapse', tableLayout: 'fixed', fontSize: 12.5 }}>
      <thead style={{ position: 'sticky', top: 0, zIndex: 2, background: T.surfaceAlt }}><tr style={{ borderBottom: `1px solid ${T.border}` }}>
        <th style={{ width: 42, padding: '8px 12px' }}><input aria-label="全选" type="checkbox" checked={allSelected} onChange={event => setSelected(event.target.checked ? new Set(items.map(entryKey)) : new Set())}/></th>
        <Header width="auto">名称</Header><Header width="28%">位置</Header><Header width="100px">大小</Header><Header width="155px">{view === 'trash' ? '删除时间' : view === 'shares' ? '有效期' : '时间'}</Header>
      </tr></thead>
      <tbody>{items.map(item => {
        const key = entryKey(item); const active = selected.has(key)
        const type = item.isDir ? 'dir' : (item.type || 'file')
        const location = item.originalPath || item.path || ''
        const time = item.deletedAt || item.expiresAt || item.openedAt || item.addedAt || item.modified
        return <tr key={key} onClick={() => toggle(key, !active)} onDoubleClick={() => onOpen(item)} style={{ height: 39, borderBottom: `1px solid ${T.borderSoft}`, background: active ? T.blueSoft : '#fff', cursor: 'pointer' }}>
          <td style={{ padding: '8px 12px', textAlign: 'center' }}><input aria-label={`选择 ${item.name}`} type="checkbox" checked={active} onClick={event => event.stopPropagation()} onChange={event => toggle(key, event.target.checked)}/></td>
          <td style={{ padding: '8px 10px', overflow: 'hidden' }}><div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}><FileIcon type={type} size={15}/><span title={item.name} style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: T.ink, fontWeight: item.isDir ? 650 : 500 }}>{item.name}</span></div></td>
          <td title={location} style={{ padding: '8px 10px', color: T.ink3, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{location}</td>
          <td className="mono" style={{ padding: '8px 10px', color: T.ink3, textAlign: 'right' }}>{item.isDir ? '' : formatSize(item.size)}</td>
          <td className="mono" style={{ padding: '8px 10px', color: T.ink3, textAlign: 'right', whiteSpace: 'nowrap' }}>{item.expiresAt === null ? '永久' : formatDate(time)}</td>
        </tr>
      })}</tbody>
    </table>
  )
}

function Header({ children, width }) { return <th style={{ width, padding: '8px 10px', textAlign: 'left', color: T.ink4, fontSize: 10.5, fontWeight: 700 }}>{children}</th> }

async function copyText(text) {
  try { await navigator.clipboard.writeText(text); return true } catch {
    try {
      const area = document.createElement('textarea'); area.value = text; area.style.position = 'fixed'; area.style.opacity = '0'; document.body.appendChild(area); area.select()
      const copied = document.execCommand('copy'); area.remove(); return copied
    } catch { return false }
  }
}
