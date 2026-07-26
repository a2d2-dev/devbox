import { useState, useEffect, useMemo, useRef } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { btnSecondary } from '../components/AppWindow'
import { getAuthToken } from '../hooks/useApi'
import { AnimatePresence, PopScale } from '../motion'
import { useOverlayLayer } from '../overlays/OverlayProvider'

// Story 7.1 audit 需要 Bearer token。所有 fetch 调用走 authFetch 自动带 token。
function authFetch(url, opts = {}) {
  const token = getAuthToken()
  const headers = { ...(opts.headers || {}) }
  if (token) headers['Authorization'] = 'Bearer ' + token
  return fetch(url, { ...opts, headers })
}

// Story 7.1: 操作日志审计页
// GET /api/v1/audit/events?limit=N&offset=M&type=...&user=&since=ISO8601&until=ISO8601&outcome=
// 双 sink 后端：sqlite (本地) + MQTT (云端聚合)；本页查询本地 sqlite

// CR E4: NOT_IMPL 灰显（agent 当前无对应触发点）
const EVENT_TYPES = [
  { value: 'LOGIN_SUCCESS',  label: '登录成功',   tone: 'green' },
  { value: 'LOGIN_FAILED',   label: '登录失败',   tone: 'red' },
  { value: 'APP_INSTALL',    label: '应用安装',   tone: 'blue',   notImpl: true },
  { value: 'APP_UNINSTALL',  label: '应用卸载',   tone: 'amber' },
  { value: 'APP_START',      label: '应用启动',   tone: 'blue' },
  { value: 'APP_STOP',       label: '应用停止',   tone: 'slate' },
  { value: 'APP_RESTART',    label: '应用重启',   tone: 'blue' },
  { value: 'SHELL_OPEN',     label: '打开 Shell', tone: 'violet' },
  { value: 'SHELL_CLOSE',    label: '关闭 Shell', tone: 'slate' },
  { value: 'FILE_LIST',      label: '浏览文件',   tone: 'slate' },
  { value: 'FILE_DOWNLOAD',  label: '下载文件',   tone: 'amber',  notImpl: true },
  { value: 'MEMO_EDIT',      label: '备忘录编辑', tone: 'slate',  notImpl: true },
  { value: 'SETTING_CHANGE', label: '系统设置变更', tone: 'slate', notImpl: true },
]

const TONE_COLOR = { green: '#059669', red: '#dc2626', blue: '#2563eb', amber: '#d97706', violet: '#7c3aed', slate: '#475569' }

const PAGE_SIZE = 50

export default function AuditLog() {
  // filter state
  const [timeRange, setTimeRange] = useState('24h') // 1h / 6h / 24h / 7d / 30d
  const [typeFilter, setTypeFilter] = useState([]) // multi
  const [userFilter, setUserFilter] = useState('')
  const [outcomeFilter, setOutcomeFilter] = useState('all') // all / success / failure
  const [page, setPage] = useState(0)

  // data state
  const [events, setEvents] = useState([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [cloudOnline, setCloudOnline] = useState(null)
  const [expanded, setExpanded] = useState(null)

  // cloud status (一次性 + 偶尔轮询)
  useEffect(() => {
    let cancelled = false
    function poll() {
      fetch('/api/v1/cloud/status').then(r => r.ok ? r.json() : { cloudOnline: false }).then(d => {
        if (!cancelled) setCloudOnline(!!d.cloudOnline)
      }).catch(() => { if (!cancelled) setCloudOnline(false) })
    }
    poll()
    const id = setInterval(poll, 30000)
    return () => { cancelled = true; clearInterval(id) }
  }, [])

  // build query url
  const since = useMemo(() => {
    const now = new Date()
    const map = { '1h': 1, '6h': 6, '24h': 24, '7d': 168, '30d': 720 }
    const hours = map[timeRange] ?? 24
    return new Date(now.getTime() - hours * 3600 * 1000).toISOString()
  }, [timeRange])

  const queryUrl = useMemo(() => {
    const params = new URLSearchParams()
    params.set('limit', String(PAGE_SIZE))
    params.set('offset', String(page * PAGE_SIZE))
    params.set('since', since)
    if (userFilter) params.set('user', userFilter)
    if (outcomeFilter !== 'all') params.append('outcome', outcomeFilter)
    typeFilter.forEach(t => params.append('type', t))
    return '/api/v1/audit/events?' + params.toString()
  }, [page, since, userFilter, outcomeFilter, typeFilter])

  // fetch (poll 每 30s)
  // CR C8: 后台 tab 暂停轮询；CR E5: 每次 load 开头清 error
  useEffect(() => {
    let cancelled = false
    let id = null
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
    function startPolling() {
      if (id != null) return
      load()
      id = setInterval(load, 30000)
    }
    function stopPolling() {
      if (id != null) { clearInterval(id); id = null }
    }
    function onVisibility() {
      if (document.hidden) stopPolling()
      else startPolling()
    }
    if (!document.hidden) startPolling()
    document.addEventListener('visibilitychange', onVisibility)
    return () => {
      cancelled = true
      stopPolling()
      document.removeEventListener('visibilitychange', onVisibility)
    }
  }, [queryUrl])

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const eventTypeMeta = (t) => EVENT_TYPES.find(e => e.value === t) || { label: t, tone: 'slate' }

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', background: T.surfaceAlt }}>
      {/* header + filters */}
      <div style={{ padding: '18px 24px 14px', background: T.surface, borderBottom: `1px solid ${T.border}` }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12 }}>
          <div>
            <div style={{ fontSize: 17, fontWeight: 700, color: T.ink, letterSpacing: '-0.01em' }}>操作日志</div>
            <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 4 }}>
              共 {total} 条 · 当前页 {page + 1}/{totalPages}
              {cloudOnline === false && <span style={{ color: '#a16207', marginLeft: 8 }}>· 云端聚合不可用（仅本节点）</span>}
            </div>
          </div>
          <div style={{ flex: 1 }}/>
          {loading && <span style={{ fontSize: 11, color: T.ink4 }}>加载中…</span>}
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

          {/* outcome */}
          <div style={{ display: 'flex', gap: 2, background: T.surfaceAlt, padding: 2, borderRadius: 8 }}>
            {[['all', '全部'], ['success', '成功'], ['failure', '失败']].map(([v, lbl]) => (
              <button key={v} onClick={() => { setOutcomeFilter(v); setPage(0) }} style={{
                padding: '5px 10px', fontSize: 12, fontWeight: 500, border: 'none',
                background: outcomeFilter === v ? T.surface : 'transparent',
                color: outcomeFilter === v ? T.blueDeep : T.ink3,
                borderRadius: 6, cursor: 'pointer',
              }}>{lbl}</button>
            ))}
          </div>

          {/* event type multi */}
          <EventTypeMultiSelect value={typeFilter} onChange={v => { setTypeFilter(v); setPage(0) }}/>

          {(typeFilter.length > 0 || userFilter || outcomeFilter !== 'all' || timeRange !== '24h') && (
            <button className="edge-press edge-btn-secondary" style={{ ...btnSecondary, height: 30, padding: '0 10px' }}
              onClick={() => { setTypeFilter([]); setUserFilter(''); setOutcomeFilter('all'); setTimeRange('24h'); setPage(0) }}>
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
                <th style={th}>时间</th>
                <th style={th}>用户</th>
                <th style={th}>事件</th>
                <th style={th}>资源</th>
                <th style={th}>源 IP</th>
                <th style={th}>结果</th>
              </tr>
            </thead>
            <tbody>
              {events.length === 0 && !loading && (
                <tr><td colSpan={6} style={{ ...td, textAlign: 'center', padding: 32, color: T.ink4 }}>没有匹配的事件</td></tr>
              )}
              {events.map(ev => {
                const meta = eventTypeMeta(ev.event_type)
                return <EventRow key={ev.id} ev={ev} meta={meta} onClick={() => setExpanded(ev)}/>
              })}
            </tbody>
          </table>
        </div>

        {/* pagination */}
        {totalPages > 1 && (
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 8, marginTop: 16 }}>
            <button onClick={() => setPage(p => Math.max(0, p - 1))} disabled={page === 0} style={pageBtn(page === 0)}>← 上一页</button>
            <span style={{ fontSize: 12.5, color: T.ink2 }}>第 {page + 1} / {totalPages} 页</span>
            <button onClick={() => setPage(p => Math.min(totalPages - 1, p + 1))} disabled={page >= totalPages - 1} style={pageBtn(page >= totalPages - 1)}>下一页 →</button>
          </div>
        )}
      </div>

      {/* CR C7: 详情右侧抽屉（替代行内展开） */}
      {expanded && <DetailDrawer ev={expanded} meta={eventTypeMeta(expanded.event_type)} onClose={() => setExpanded(null)}/>}
    </div>
  )
}

// CR C7: 简化为单行（详情移到右侧 DetailDrawer）
function EventRow({ ev, meta, onClick }) {
  const tone = TONE_COLOR[meta.tone] || TONE_COLOR.slate
  return (
    <tr onClick={onClick} style={{ cursor: 'pointer', borderTop: `1px solid ${T.border}` }}>
      <td style={td}>
        <div style={{ fontWeight: 500, color: T.ink }}>{relativeTime(ev.ts)}</div>
        <div style={{ fontSize: 10.5, color: T.ink4 }} title={ev.ts}>{shortTime(ev.ts)}</div>
      </td>
      <td style={td}>{ev.username || '-'}</td>
      <td style={td}>
        <span style={{
          padding: '2px 8px', borderRadius: 6, fontSize: 11.5, fontWeight: 600,
          background: tone + '18', color: tone,
        }}>{meta.label}</span>
      </td>
      <td style={td}>
        {ev.resource_id ? (
          <span style={{ color: T.ink2, fontFamily: 'ui-monospace,SFMono-Regular,Menlo,monospace', fontSize: 11.5 }}>
            {ev.resource_kind && <span style={{ color: T.ink4 }}>{ev.resource_kind}:</span>}{ev.resource_id}
          </span>
        ) : <span style={{ color: T.ink4 }}>-</span>}
      </td>
      <td style={td}>{ev.source_ip || '-'}</td>
      <td style={td}>
        {ev.outcome === 'success' ? (
          <span style={{ color: '#059669', fontWeight: 600 }}>● 成功</span>
        ) : (
          <span style={{ color: '#dc2626', fontWeight: 600 }}>● 失败</span>
        )}
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
          <DetailField label="用户" value={ev.username || '-'}/>
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

function EventTypeMultiSelect({ value, onChange }) {
  const [open, setOpen] = useState(false)
  const toggle = (v) => {
    if (value.includes(v)) onChange(value.filter(x => x !== v))
    else onChange([...value, v])
  }
  return (
    <div style={{ position: 'relative' }}>
      <button onClick={() => setOpen(o => !o)} style={{
        height: 30, padding: '0 10px', border: `1px solid ${T.border}`, borderRadius: 6,
        background: T.surface, fontSize: 12.5, color: T.ink, cursor: 'pointer',
        display: 'flex', alignItems: 'center', gap: 6,
      }}>
        事件类型{value.length > 0 ? ` (${value.length})` : ''}
        <Icon name="chevDown" size={12} stroke={2}/>
      </button>
      <AnimatePresence>
      {open && (
        <PopScale origin="top left" className="edge-material-surface" style={{
          position: 'absolute', top: 34, left: 0, zIndex: 100,
          border: `1px solid ${T.border}`, borderRadius: 8,
          boxShadow: '0 8px 32px -4px rgba(0,0,0,0.12)',
          minWidth: 200, padding: 6, maxHeight: 320, overflow: 'auto',
        }}>
          {EVENT_TYPES.map(et => (
            <label key={et.value} className="edge-menu-item" title={et.notImpl ? 'NOT_IMPL：当前 sprint 未触发，保留枚举常量' : ''} style={{
              display: 'flex', alignItems: 'center', gap: 8, padding: '6px 8px',
              cursor: et.notImpl ? 'not-allowed' : 'pointer', borderRadius: 4, fontSize: 12.5,
              opacity: et.notImpl ? 0.42 : 1,
            }} onMouseDown={e => e.preventDefault()}>
              <input type="checkbox" disabled={et.notImpl}
                checked={value.includes(et.value)} onChange={() => !et.notImpl && toggle(et.value)}/>
              <span style={{ color: T.ink2 }}>{et.label}</span>
              <span style={{ marginLeft: 'auto', fontSize: 10, color: T.ink4, fontFamily: 'ui-monospace' }}>{et.value}</span>
            </label>
          ))}
          <div style={{ borderTop: `1px solid ${T.border}`, marginTop: 4, padding: 6, textAlign: 'right' }}>
            <button onClick={() => { setOpen(false) }} className="edge-press edge-btn-secondary" style={{ ...btnSecondary, height: 26, padding: '0 10px', fontSize: 11.5 }}>完成</button>
          </div>
        </PopScale>
      )}
      </AnimatePresence>
    </div>
  )
}

const th = { padding: '10px 14px', textAlign: 'left', borderBottom: `1px solid ${T.border}` }
const td = { padding: '10px 14px', verticalAlign: 'top' }
const pageBtn = (disabled) => ({
  padding: '6px 14px', border: `1px solid ${T.border}`, borderRadius: 6,
  background: disabled ? T.surfaceAlt : T.surface, fontSize: 12.5,
  color: disabled ? T.ink4 : T.ink2, cursor: disabled ? 'default' : 'pointer',
})

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
