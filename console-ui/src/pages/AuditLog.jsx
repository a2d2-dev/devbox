import { useState, useEffect, useMemo, useRef } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { btnSecondary } from '../components/AppWindow'
import { getAuthToken } from '../hooks/useApi'
import { useOverlayLayer } from '../overlays/OverlayProvider'
import { startVisiblePolling } from '../lib/visiblePolling'

// 所有日志 API 调用统一携带控制台 Bearer token。
function authFetch(url, opts = {}) {
  const token = getAuthToken()
  const headers = { ...(opts.headers || {}) }
  if (token) headers['Authorization'] = 'Bearer ' + token
  return fetch(url, { ...opts, headers })
}

const EVENT_TYPES = [
  { value: 'LOGIN_SUCCESS',  label: '登录成功',   tone: 'green' },
  { value: 'LOGIN_FAILED',   label: '登录失败',   tone: 'red' },
  { value: 'APP_INSTALL',    label: '应用安装',   tone: 'blue' },
  { value: 'APP_UNINSTALL',  label: '应用卸载',   tone: 'amber' },
  { value: 'APP_START',      label: '应用启动',   tone: 'blue' },
  { value: 'APP_STOP',       label: '应用停止',   tone: 'slate' },
  { value: 'APP_RESTART',    label: '应用重启',   tone: 'blue' },
  { value: 'SERVICE_START',  label: '服务启动',   tone: 'green' },
  { value: 'SERVICE_STOP',   label: '服务停止',   tone: 'amber' },
  { value: 'SERVICE_RESTART', label: '服务重启',  tone: 'blue' },
  { value: 'PROCESS_TERMINATE', label: '终止进程', tone: 'red' },
  { value: 'LOG_CLEAR',      label: '清空日志',   tone: 'red' },
  { value: 'SHELL_OPEN',     label: '打开 Shell', tone: 'violet' },
  { value: 'SHELL_CLOSE',    label: '关闭 Shell', tone: 'slate' },
  { value: 'FILE_LIST',      label: '浏览文件',   tone: 'slate' },
  { value: 'FILE_DOWNLOAD',  label: '下载文件',   tone: 'amber',  notImpl: true },
  { value: 'MEMO_EDIT',      label: '备忘录编辑', tone: 'slate',  notImpl: true },
  { value: 'SETTING_CHANGE', label: '系统设置变更', tone: 'slate', notImpl: true },
]

const TONE_COLOR = { green: '#059669', red: '#dc2626', blue: '#0066ff', amber: '#d97706', violet: '#7c3aed', slate: '#475569' }

const LEVEL_META = {
  info: { label: '信息', color: '#0066ff' },
  warning: { label: '警告', color: '#d97706' },
  error: { label: '错误', color: '#dc2626' },
}

const MODULES = [
  ['auth', '认证'], ['supervisor', '服务'], ['apps', '应用'], ['process', '进程'], ['audit', '审计'], ['system', '系统'],
]

export default function AuditLog() {
  const [timeRange, setTimeRange] = useState('24h') // 1h / 6h / 24h / 7d / 30d
  const [levelFilter, setLevelFilter] = useState('')
  const [moduleFilter, setModuleFilter] = useState('')
  const [userFilter, setUserFilter] = useState('')
  const [page, setPage] = useState(0)
  const [pageSize, setPageSize] = useState(25)
  const [jumpPage, setJumpPage] = useState('')
  const [refreshKey, setRefreshKey] = useState(0)
  const [confirmClear, setConfirmClear] = useState(false)

  // data state
  const [events, setEvents] = useState([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [expanded, setExpanded] = useState(null)

  // build query url
  const since = useMemo(() => {
    const now = new Date()
    const map = { '1h': 1, '6h': 6, '24h': 24, '7d': 168, '30d': 720 }
    const hours = map[timeRange] ?? 24
    return new Date(now.getTime() - hours * 3600 * 1000).toISOString()
  }, [timeRange])

  const queryUrl = useMemo(() => {
    const params = new URLSearchParams()
    params.set('limit', String(pageSize))
    params.set('offset', String(page * pageSize))
    params.set('since', since)
    params.set('_refresh', String(refreshKey))
    if (userFilter) params.set('user', userFilter)
    if (levelFilter) params.set('level', levelFilter)
    if (moduleFilter) params.set('module', moduleFilter)
    return '/api/v1/audit/events?' + params.toString()
  }, [page, pageSize, since, userFilter, levelFilter, moduleFilter, refreshKey])

  // fetch (poll 每 30s)
  // CR C8: 后台 tab 暂停轮询；CR E5: 每次 load 开头清 error
  useEffect(() => {
    let cancelled = false
    function load() {
      setLoading(true)
      setError('')
      authFetch(queryUrl).then(async r => {
        if (!r.ok) {
          if (!cancelled) setError(`查询失败 (${r.status})`)
          return
        }
        const d = await r.json()
        if (!cancelled) {
          setEvents(Array.isArray(d.events) ? d.events : [])
          setTotal(d.total || 0)
        }
      }).catch(e => {
        if (!cancelled) setError('网络错误：' + String(e))
      }).finally(() => {
        if (!cancelled) setLoading(false)
      })
    }
    const stopPolling = startVisiblePolling(load, 30000)
    return () => {
      cancelled = true
      stopPolling()
    }
  }, [queryUrl])

  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const eventTypeMeta = (t) => EVENT_TYPES.find(e => e.value === t) || { label: t, tone: 'slate' }

  const clearLogs = async () => {
    setError('')
    const response = await authFetch('/api/v1/audit/events', { method: 'DELETE' })
    if (!response.ok) {
      const body = await response.json().catch(() => ({}))
      throw new Error(body.error || `清空失败 (${response.status})`)
    }
    setPage(0)
    setConfirmClear(false)
    setRefreshKey(key => key + 1)
  }

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', background: T.surfaceAlt }}>
      {/* header + filters */}
      <div style={{ padding: '18px 24px 14px', background: T.surface, borderBottom: `1px solid ${T.border}` }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12 }}>
          <div>
            <div style={{ fontSize: 17, fontWeight: 700, color: T.ink }}>系统日志</div>
            <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 4 }}>
              共 {total} 条 · 当前页 {page + 1}/{totalPages}
            </div>
          </div>
          <div style={{ flex: 1 }}/>
          {loading && <span style={{ fontSize: 11, color: T.ink4 }}>加载中…</span>}
          <button onClick={() => setConfirmClear(true)} style={{ ...btnSecondary, height: 30, padding: '0 10px', color: T.red }}>
            <Icon name="trash" size={12}/>清空
          </button>
        </div>

        {/* filter row */}
        <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap', alignItems: 'center' }}>
          {/* time range */}
          <div style={{ display: 'flex', gap: 2, background: T.surfaceAlt, padding: 2, borderRadius: 8 }}>
            {['1h', '6h', '24h', '7d', '30d'].map(r => (
              <button key={r} onClick={() => { setTimeRange(r); setPage(0) }} style={{
                padding: '5px 10px', fontSize: 12, fontWeight: 500, border: 'none',
                background: timeRange === r ? T.surface : 'transparent',
                color: timeRange === r ? T.blueDeep : T.ink3,
                borderRadius: 6, cursor: 'pointer',
                boxShadow: timeRange === r ? '0 1px 2px rgba(0,0,0,0.04)' : 'none',
              }}>{r}</button>
            ))}
          </div>

          {/* user */}
          <input
            placeholder="用户名（模糊）"
            value={userFilter}
            onChange={e => { setUserFilter(e.target.value); setPage(0) }}
            style={{
              height: 30, border: `1px solid ${T.border}`, borderRadius: 6,
              padding: '0 10px', fontSize: 12.5, color: T.ink, outline: 'none', width: 160,
            }}
          />

          <select value={levelFilter} onChange={e => { setLevelFilter(e.target.value); setPage(0) }} style={selectStyle}>
            <option value="">全部等级</option><option value="info">信息</option><option value="warning">警告</option><option value="error">错误</option>
          </select>
          <select value={moduleFilter} onChange={e => { setModuleFilter(e.target.value); setPage(0) }} style={selectStyle}>
            <option value="">全部模块</option>{MODULES.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
          </select>

          {(levelFilter || moduleFilter || userFilter || timeRange !== '24h') && (
            <button className="edge-press edge-btn-secondary" style={{ ...btnSecondary, height: 30, padding: '0 10px' }}
              onClick={() => { setLevelFilter(''); setModuleFilter(''); setUserFilter(''); setTimeRange('24h'); setPage(0) }}>
              <Icon name="x" size={12} stroke={2}/>重置
            </button>
          )}
        </div>
      </div>

      {/* table */}
      <div style={{ flex: 1, overflow: 'auto', padding: 16 }}>
        {error && (
          <div style={{ padding: 12, background: '#fef2f2', border: '1px solid #fecaca', borderRadius: 8, color: '#dc2626', fontSize: 13, marginBottom: 12 }}>
            {error}
          </div>
        )}
        <div style={{ background: T.surface, borderRadius: 10, border: `1px solid ${T.border}`, overflow: 'hidden' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12.5 }}>
            <thead>
              <tr style={{ background: T.surfaceAlt, color: T.ink3, fontWeight: 600, fontSize: 11.5 }}>
                <th style={th}>等级</th><th style={th}>模块</th><th style={th}>时间</th><th style={th}>用户</th><th style={th}>事件</th>
              </tr>
            </thead>
            <tbody>
              {events.length === 0 && !loading && (
                <tr><td colSpan={5} style={{ ...td, textAlign: 'center', padding: 32, color: T.ink4 }}>没有匹配的日志</td></tr>
              )}
              {events.map(ev => {
                const meta = eventTypeMeta(ev.event_type)
                return <EventRow key={ev.id} ev={ev} meta={meta} onClick={() => setExpanded(ev)}/>
              })}
            </tbody>
          </table>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 8, marginTop: 16 }}>
            <button onClick={() => setPage(p => Math.max(0, p - 1))} disabled={page === 0} style={pageBtn(page === 0)}>← 上一页</button>
            <span style={{ fontSize: 12.5, color: T.ink2 }}>第 {page + 1} / {totalPages} 页</span>
            <button onClick={() => setPage(p => Math.min(totalPages - 1, p + 1))} disabled={page >= totalPages - 1} style={pageBtn(page >= totalPages - 1)}>下一页 →</button>
            <select value={pageSize} onChange={e => { setPageSize(Number(e.target.value)); setPage(0) }} style={selectStyle}>
              {[10, 25, 50, 100].map(size => <option key={size} value={size}>{size} 条/页</option>)}
            </select>
            <input value={jumpPage} onChange={e => setJumpPage(e.target.value.replace(/\D/g, ''))} placeholder="页码" style={{ ...selectStyle, width: 58 }}/>
            <button style={pageBtn(false)} onClick={() => { const target = Math.min(totalPages, Math.max(1, Number(jumpPage) || 1)); setPage(target - 1); setJumpPage('') }}>跳页</button>
        </div>
      </div>

      {/* CR C7: 详情右侧抽屉（替代行内展开） */}
      {expanded && <DetailDrawer ev={expanded} meta={eventTypeMeta(expanded.event_type)} onClose={() => setExpanded(null)}/>}
      {confirmClear && <ClearDialog onClose={() => setConfirmClear(false)} onConfirm={clearLogs} onError={message => setError(message)}/>}
    </div>
  )
}

// CR C7: 简化为单行（详情移到右侧 DetailDrawer）
function EventRow({ ev, meta, onClick }) {
  const level = LEVEL_META[ev.level] || { label: ev.level || '信息', color: T.ink3 }
  const moduleLabel = MODULES.find(([value]) => value === ev.module)?.[1] || ev.module || '系统'
  return (
    <tr onClick={onClick} style={{ cursor: 'pointer', borderTop: `1px solid ${T.border}` }}>
      <td style={td}><span style={{ padding: '2px 7px', borderRadius: 4, fontWeight: 600, color: level.color, background: level.color + '14' }}>{level.label}</span></td>
      <td style={td}><span className="mono" style={{ color: T.ink2 }}>{moduleLabel}</span></td>
      <td style={td}>
        <div style={{ fontWeight: 500, color: T.ink }}>{relativeTime(ev.ts)}</div>
        <div style={{ fontSize: 10.5, color: T.ink4 }} title={ev.ts}>{shortTime(ev.ts)}</div>
      </td>
      <td style={td}>{ev.username || '-'}</td>
      <td style={td}>
        <div style={{ fontWeight: 600, color: T.ink }}>{ev.event || meta.label}</div>
        <div style={{ fontSize: 10.5, color: T.ink4, marginTop: 2 }}>{ev.event_type}{ev.resource_id ? ` · ${ev.resource_kind}:${ev.resource_id}` : ''}</div>
      </td>
    </tr>
  )
}

// CR C7: 右侧详情抽屉（spec AC-AUDIT-PAGE-TABLE 要求）
function DetailDrawer({ ev, meta, onClose }) {
  const tone = TONE_COLOR[meta.tone] || TONE_COLOR.slate
  const drawerRef = useRef(null)
  const closeRef = useRef(null)
  const { backdropProps, layerProps } = useOverlayLayer({
    id: 'audit-detail', onDismiss: onClose, layerRef: drawerRef, initialFocusRef: closeRef,
  })
  return (
    <>
      {/* 半透明蒙层（点击关闭） */}
      <div {...backdropProps} style={{
        position: 'absolute', inset: 0, background: 'rgba(15,23,42,0.32)', zIndex: 50,
      }}/>
      {/* 右侧抽屉 */}
      <div ref={drawerRef} role="dialog" aria-modal="true" aria-labelledby="audit-detail-title" tabIndex={-1}
        {...layerProps} style={{
        position: 'absolute', top: 0, right: 0, bottom: 0, width: 440, zIndex: 51,
        background: T.surface, boxShadow: '-12px 0 32px -4px rgba(0,0,0,0.12)',
        display: 'flex', flexDirection: 'column',
      }}>
        {/* header */}
        <div style={{ padding: '16px 20px', borderBottom: `1px solid ${T.border}`, display: 'flex', alignItems: 'center', gap: 12 }}>
          <span id="audit-detail-title" style={{
            padding: '3px 10px', borderRadius: 6, fontSize: 12, fontWeight: 600,
            background: tone + '18', color: tone,
          }}>{meta.label}</span>
          <div style={{ flex: 1, fontSize: 11.5, color: T.ink3 }}>事件 #{ev.id}</div>
          <button ref={closeRef} onClick={onClose} aria-label="关闭审计详情" style={{
            border: 'none', background: 'transparent', cursor: 'pointer', color: T.ink3,
            padding: 4, display: 'flex',
          }}><Icon name="x" size={16} stroke={2}/></button>
        </div>
        {/* body */}
        <div style={{ flex: 1, overflow: 'auto', padding: 20, fontSize: 12.5, color: T.ink2 }}>
          <DetailField label="时间" value={shortTime(ev.ts)} mono/>
          <DetailField label="UTC" value={ev.ts} mono/>
          <DetailField label="等级" value={ev.level || '-'}/>
          <DetailField label="模块" value={ev.module || '-'}/>
          <DetailField label="用户" value={ev.username || '-'}/>
          <DetailField label="事件" value={ev.event || meta.label}/>
          <DetailField label="资源" value={ev.resource_id ? `${ev.resource_kind || '?'}:${ev.resource_id}` : '-'} mono/>
          <DetailField label="源 IP" value={ev.source_ip || '-'} mono/>
          <DetailField label="结果" value={ev.outcome}
            valueStyle={{ color: ev.outcome === 'success' ? '#059669' : '#dc2626', fontWeight: 600 }}/>
          <DetailField label="User Agent" value={ev.user_agent || '-'} small/>
          <div style={{ marginTop: 16, fontSize: 11, fontWeight: 600, color: T.ink4 }}>PAYLOAD</div>
          <pre style={{
            margin: '6px 0 0', padding: 12, background: T.surfaceAlt,
            border: `1px solid ${T.border}`, borderRadius: 6,
            fontSize: 11.5, color: T.ink2, overflow: 'auto', maxHeight: 280,
          }}>{ev.payload ? JSON.stringify(ev.payload, null, 2) : '(无)'}</pre>
        </div>
      </div>
    </>
  )
}

function DetailField({ label, value, mono, small, valueStyle }) {
  return (
    <div style={{ display: 'grid', gridTemplateColumns: '90px 1fr', gap: 10, padding: '8px 0', borderBottom: `1px solid ${T.borderSoft}` }}>
      <div style={{ color: T.ink4, fontSize: 11.5 }}>{label}</div>
      <div style={{ ...(mono && { fontFamily: 'ui-monospace,SFMono-Regular,Menlo,monospace' }), ...(small && { fontSize: 11 }), wordBreak: 'break-all', ...(valueStyle || {}) }}>{value}</div>
    </div>
  )
}

function ClearDialog({ onClose, onConfirm, onError }) {
  const [busy, setBusy] = useState(false)
  const confirm = async () => {
    setBusy(true)
    try { await onConfirm() } catch (error) { onError(error.message); onClose() } finally { setBusy(false) }
  }
  return <div role="dialog" aria-modal="true" aria-label="确认清空系统日志" onClick={onClose} style={{ position: 'absolute', inset: 0, zIndex: 60, background: 'rgba(15,23,42,.35)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
    <div onClick={e => e.stopPropagation()} style={{ width: 390, padding: 20, background: T.surface, border: `1px solid ${T.border}`, borderRadius: 8, boxShadow: '0 18px 48px rgba(15,23,42,.2)' }}>
      <div style={{ fontSize: 15, fontWeight: 700, color: T.ink }}>清空系统日志？</div>
      <div style={{ marginTop: 10, fontSize: 12.5, color: T.ink2, lineHeight: 1.7 }}>现有日志将被永久删除。此次清空操作本身会作为新的审计日志保留。</div>
      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 18 }}>
        <button disabled={busy} onClick={onClose} style={dialogBtn}>取消</button>
        <button disabled={busy} onClick={confirm} style={{ ...dialogBtn, color: '#fff', background: T.red, borderColor: T.red }}>{busy ? '正在清空...' : '确认清空'}</button>
      </div>
    </div>
  </div>
}

const th = { padding: '10px 14px', textAlign: 'left', borderBottom: `1px solid ${T.border}` }
const td = { padding: '10px 14px', verticalAlign: 'top' }
const pageBtn = (disabled) => ({
  padding: '6px 14px', border: `1px solid ${T.border}`, borderRadius: 6,
  background: disabled ? T.surfaceAlt : T.surface, fontSize: 12.5,
  color: disabled ? T.ink4 : T.ink2, cursor: disabled ? 'default' : 'pointer',
})
const selectStyle = { height: 30, padding: '0 8px', border: `1px solid ${T.border}`, borderRadius: 6, background: T.surface, color: T.ink2, fontSize: 12 }
const dialogBtn = { height: 32, padding: '0 14px', border: `1px solid ${T.border}`, borderRadius: 6, background: T.surface, color: T.ink2, cursor: 'pointer', fontSize: 12.5, fontWeight: 600 }

// time helpers
function relativeTime(iso) {
  if (!iso) return '-'
  const t = new Date(iso).getTime()
  if (isNaN(t)) return iso
  const diff = Date.now() - t
  if (diff < 60_000) return '刚刚'
  if (diff < 3600_000) return `${Math.floor(diff / 60_000)} 分钟前`
  if (diff < 86400_000) return `${Math.floor(diff / 3600_000)} 小时前`
  return `${Math.floor(diff / 86400_000)} 天前`
}

function shortTime(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  return d.toLocaleString('zh-CN', { hour12: false })
}
