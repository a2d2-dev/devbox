import { useState, useEffect } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { authFetch } from '../hooks/useApi'
import { startVisiblePolling } from '../lib/visiblePolling'
import TabBar from '../components/TabBar'

const th = {
  padding: '8px 14px', fontSize: 10.5, fontWeight: 600, color: T.ink3,
  letterSpacing: '0.04em', textTransform: 'uppercase',
};

const td = { padding: '10px 14px', fontSize: 12.5, color: T.ink };

const filterInput = {
  height: 32, padding: '0 10px', borderRadius: 6,
  border: `1px solid ${T.border}`, background: T.surface,
  fontSize: 12.5, color: T.ink, outline: 'none',
};

// state 颜色（标准 Linux process state, proc(5) man page 全集）
function stateBadgeStyle(state) {
  const ch = (state || '').charAt(0);
  const tone = {
    R: { bg: '#dcfce7', fg: '#047857' }, // running
    S: { bg: '#e0f2fe', fg: '#0369a1' }, // sleeping
    D: { bg: '#fef3c7', fg: '#a16207' }, // disk sleep
    Z: { bg: '#fee2e2', fg: '#991b1b' }, // zombie
    T: { bg: '#f3e8ff', fg: '#7c2d12' }, // stopped (SIGSTOP)
    I: { bg: '#e0e7ff', fg: '#3730a3' }, // idle
    X: { bg: '#fecaca', fg: '#7f1d1d' }, // dead (kernel cleanup)
    t: { bg: '#f3e8ff', fg: '#7c2d12' }, // tracing stop
    P: { bg: '#fef3c7', fg: '#a16207' }, // parked
  }[ch] || { bg: T.surfaceAlt, fg: T.ink3 };
  return {
    display: 'inline-block', padding: '1px 6px', borderRadius: 3,
    fontSize: 10.5, fontWeight: 600, color: tone.fg, background: tone.bg,
  };
}

export function fmtBytes(n) {
  if (n == null || n < 0) return '-';
  if (n >= 1024 * 1024 * 1024) return (n / (1024 * 1024 * 1024)).toFixed(1) + ' GB';
  if (n >= 1024 * 1024) return (n / (1024 * 1024)).toFixed(1) + ' MB';
  if (n >= 1024) return (n / 1024).toFixed(0) + ' KB';
  return n + ' B';
}

function fmtDuration(seconds) {
  if (seconds == null || seconds < 0) return '无数据'
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (days) return `${days}天 ${hours}小时`
  if (hours) return `${hours}小时 ${minutes}分`
  if (minutes) return `${minutes}分`
  return `${Math.floor(seconds)}秒`
}

function fmtCPU(value) {
  return value == null ? '采样中' : `${value.toFixed(1)}%`
}

export function fmtRate(value, status = 'available') {
  if (status !== 'available') return status === 'unsupported' ? '不支持' : '无数据'
  if (value == null) return '采样中'
  return `${fmtBytes(value)}/s`
}

function sortValue(row, key) {
  if (key === 'ports') return row.ports?.[0] ?? -1
  return row[key] ?? -1
}

function SortHead({ id, label, align = 'right', sort, onSort }) {
  return <th onClick={() => onSort(id)} style={{ ...th, textAlign: align, cursor: 'pointer', whiteSpace: 'nowrap' }}>
    {label}{sort.key === id ? (sort.dir === 'asc' ? ' ↑' : ' ↓') : ''}
  </th>
}

export default function Processes({ onOpenApp }) {
  const [items, setItems] = useState([]);
  const [services, setServices] = useState([]);
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState(null); // 详情抽屉打开的 pid
  const [view, setView] = useState('processes');
  const [search, setSearch] = useState('');
  const [stateFilter, setStateFilter] = useState('');
  const [sort, setSort] = useState({ key: 'cpuPercent', dir: 'desc' });
  const [killTarget, setKillTarget] = useState(null);
  const [actionError, setActionError] = useState('');

  useEffect(() => {
    let stopped = false;
    const url = view === 'services' ? '/api/v1/supervisor/resources' : '/api/v1/processes';
    function load() {
      authFetch(url)
        .then(r => r.ok ? r.json() : null)
        .then(d => {
          if (stopped || !d) return;
          if (view === 'services') setServices(Array.isArray(d.services) ? d.services : []);
          else if (Array.isArray(d)) setItems(d);
        })
        .catch(() => {})
        .finally(() => { if (!stopped) setLoading(false); });
    }
    const stopPolling = startVisiblePolling(load, 5000);
    return () => { stopped = true; stopPolling(); };
  }, [view]);

  const source = view === 'services' ? services : items;
  const needle = search.trim().toLowerCase();
  const visible = source.filter(row => {
    if (stateFilter && !(row.state || row.statename || '').toLowerCase().includes(stateFilter.toLowerCase())) return false;
    if (!needle) return true;
    return [row.name, row.pid, row.user, row.group, ...(row.ports || [])]
      .some(value => String(value || '').toLowerCase().includes(needle));
  }).sort((a, b) => {
    const av = sortValue(a, sort.key), bv = sortValue(b, sort.key);
    const result = typeof av === 'string' ? av.localeCompare(String(bv)) : Number(av) - Number(bv);
    return sort.dir === 'asc' ? result : -result;
  });

  const changeSort = key => setSort(current => ({ key, dir: current.key === key && current.dir === 'desc' ? 'asc' : 'desc' }));

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column',
      background: T.surfaceAlt, overflow: 'hidden', position: 'relative' }}>
      {/* Header */}
      <div style={{
        padding: '14px 24px', background: T.surface,
        borderBottom: `1px solid ${T.border}`, flexShrink: 0,
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <div>
            <div style={{ fontSize: 17, fontWeight: 700, color: T.ink, letterSpacing: '-0.01em' }}>
              资源管理
            </div>
            <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3 }}>
              Supervisor 服务与宿主进程资源
            </div>
          </div>
          <div style={{ flex: 1 }}/>
          <button onClick={() => onOpenApp?.({ id: 'supervisor' })} style={navBtn}>
            <Icon name="shield" size={12}/>进程守护
          </button>
          <button onClick={() => onOpenApp?.({ id: 'network-connections' })} style={navBtn}>
            <Icon name="network" size={12}/>端口与连接
          </button>
          <div style={{ fontSize: 11.5, color: T.ink3 }}>
            共 <span className="mono tnum" style={{ color: T.ink, fontWeight: 700 }}>{visible.length}</span>
            {visible.length !== source.length && (
              <span> / {source.length}</span>
            )}
            <span> 个</span>
          </div>
        </div>

        <div style={{ display: 'flex', gap: 8, marginTop: 12, alignItems: 'center' }}>
          <div style={{ display: 'flex', gap: 2, padding: 2, borderRadius: 7, background: T.surfaceAlt }}>
            {[['services', '服务'], ['processes', '进程']].map(([id, label]) => (
              <button key={id} onClick={() => setView(id)} style={{ ...segmentBtn, background: view === id ? T.surface : 'transparent', color: view === id ? T.blueDeep : T.ink3 }}>{label}</button>
            ))}
          </div>
          <div style={{ position: 'relative' }}>
            <Icon name="search" size={13} style={{ position: 'absolute', left: 9, top: 9, color: T.ink4 }}/>
            <input style={{ ...filterInput, width: 260, paddingLeft: 28 }} placeholder="搜索名称、PID、用户或端口"
              value={search} onChange={e => setSearch(e.target.value)}/>
          </div>
          <input style={{ ...filterInput, width: 140 }} placeholder={view === 'services' ? '状态 (RUNNING)' : '状态 (R/S/D/Z/T)'}
            value={stateFilter} onChange={e => setStateFilter(e.target.value)}/>
        </div>
      </div>

      {/* 列表 */}
      <div style={{ flex: 1, overflow: 'auto', padding: 16 }}>
        <div style={{ background: T.surface, border: `1px solid ${T.border}`, borderRadius: 8 }}>
          {actionError && <div style={{ padding: '10px 14px', color: T.red, background: '#fef2f2', borderBottom: '1px solid #fecaca', fontSize: 12 }}>{actionError}</div>}
          {view === 'services' ? (
            <ServiceTable rows={visible} loading={loading} sort={sort} onSort={changeSort}/>
          ) : <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12.5 }}>
            <thead>
              <tr style={{ background: '#fafbfc' }}>
                <SortHead id="pid" label="PID" sort={sort} onSort={changeSort}/>
                <SortHead id="name" label="进程" align="left" sort={sort} onSort={changeSort}/>
                <th style={{ ...th, textAlign: 'center', width: 70 }}>状态</th>
                <SortHead id="user" label="用户" align="left" sort={sort} onSort={changeSort}/>
                <SortHead id="cpuPercent" label="CPU" sort={sort} onSort={changeSort}/>
                <SortHead id="runtimeSeconds" label="运行时间" sort={sort} onSort={changeSort}/>
                <SortHead id="memBytes" label="内存" sort={sort} onSort={changeSort}/>
                <SortHead id="ports" label="端口" sort={sort} onSort={changeSort}/>
                <th style={{ ...th, width: 54 }}></th>
              </tr>
            </thead>
            <tbody>
              {loading && (
                <tr><td colSpan={9} style={{ padding: 24, textAlign: 'center', color: T.ink3 }}>加载中...</td></tr>
              )}
              {!loading && visible.length === 0 && (
                <tr><td colSpan={9} style={{ padding: 24, textAlign: 'center', color: T.ink3 }}>无匹配进程</td></tr>
              )}
              {!loading && visible.map((p, i) => (
                <tr key={p.pid}
                  data-action="view-detail"
                  onClick={() => setSelected(p.pid)}
                  className={selected !== p.pid ? 'edge-row-hover' : undefined}
                  style={{
                    borderTop: i ? `1px solid ${T.borderSoft}` : 'none',
                    cursor: 'pointer',
                    background: selected === p.pid ? T.blueSoft : 'transparent',
                    '--edge-row-hover-bg': T.surfaceAlt,
                  }}>
                  <td style={{ ...td, textAlign: 'right', fontFamily: 'ui-monospace, monospace' }}>{p.pid}</td>
                  <td style={td}>
                    <div style={{ fontWeight: 600 }}>{p.name || '-'}</div>
                    {p.ppid > 0 && (
                      <div style={{ fontSize: 10.5, color: T.ink4, marginTop: 1 }}>PPID {p.ppid}</div>
                    )}
                  </td>
                  <td style={{ ...td, textAlign: 'center' }}>
                    <span style={stateBadgeStyle(p.state)}>{(p.state || '?').charAt(0)}</span>
                  </td>
                  <td style={{ ...td, fontFamily: 'ui-monospace, monospace', fontSize: 11.5 }}>{p.user || '-'}</td>
                  <td style={{ ...td, textAlign: 'right' }} className="mono tnum">{fmtCPU(p.cpuPercent)}</td>
                  <td style={{ ...td, textAlign: 'right', whiteSpace: 'nowrap' }}>{fmtDuration(p.runtimeSeconds)}</td>
                  <td style={{ ...td, textAlign: 'right', fontFamily: 'ui-monospace, monospace' }}>{fmtBytes(p.memBytes)}</td>
                  <td style={{ ...td, color: T.blueDeep }} className="mono">{p.ports?.length ? p.ports.map(port => `:${port}`).join(' ') : (p.portsStatus === 'available' ? '—' : '无数据')}</td>
                  <td style={{ ...td, textAlign: 'right' }}>
                    <button title="终止进程" aria-label={`终止进程 ${p.pid}`} onClick={e => { e.stopPropagation(); setActionError(''); setKillTarget(p) }} style={{ ...iconBtn, color: T.red }}>
                      <Icon name="stop" size={13} stroke={2}/>
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>}
        </div>
      </div>

      {/* 详情抽屉 */}
      {selected && <ProcessDetailDrawer pid={selected} onClose={() => setSelected(null)}/>}
      {killTarget && <TerminateDialog target={killTarget} onClose={() => setKillTarget(null)} onDone={() => {
        setItems(rows => rows.filter(row => row.pid !== killTarget.pid)); setSelected(null); setKillTarget(null);
      }} onError={setActionError}/>}
    </div>
  );
}

function ServiceTable({ rows, loading, sort, onSort }) {
  return (
    <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12.5 }}>
      <thead><tr style={{ background: '#fafbfc' }}>
        <SortHead id="name" label="服务" align="left" sort={sort} onSort={onSort}/><SortHead id="pid" label="PID" sort={sort} onSort={onSort}/>
        <th style={{ ...th, textAlign: 'center' }}>状态</th><SortHead id="cpuPercent" label="CPU" sort={sort} onSort={onSort}/>
        <SortHead id="cpuTimeSeconds" label="CPU 时间" sort={sort} onSort={onSort}/><SortHead id="memBytes" label="内存" sort={sort} onSort={onSort}/>
        <th style={{ ...th, textAlign: 'right' }}>磁盘读 / 写</th><SortHead id="ports" label="监听端口" sort={sort} onSort={onSort}/>
        <th style={{ ...th, textAlign: 'left' }}>网络</th>
      </tr></thead>
      <tbody>
        {loading && rows.length === 0 && <tr><td colSpan={9} style={emptyCell}>加载中...</td></tr>}
        {!loading && rows.length === 0 && <tr><td colSpan={9} style={emptyCell}>无匹配服务</td></tr>}
        {rows.map((service, index) => <tr key={service.name} style={{ borderTop: index ? `1px solid ${T.borderSoft}` : 'none' }}>
          <td style={td}><div style={{ fontWeight: 600 }}>{service.name}</div><div style={{ color: T.ink4, fontSize: 10.5 }}>{service.group}</div></td>
          <td style={{ ...td, textAlign: 'right' }} className="mono">{service.pid || '—'}</td>
          <td style={{ ...td, textAlign: 'center' }}><span style={stateBadgeStyle(service.statename)}>{service.statename}</span></td>
          <td style={{ ...td, textAlign: 'right' }} className="mono">{fmtCPU(service.cpuPercent)}</td>
          <td style={{ ...td, textAlign: 'right' }} className="mono">{fmtDuration(service.cpuTimeSeconds)}</td>
          <td style={{ ...td, textAlign: 'right' }} className="mono">{fmtBytes(service.memBytes)}</td>
          <td style={{ ...td, textAlign: 'right', whiteSpace: 'nowrap' }} className="mono">{fmtRate(service.readBps, service.diskIOStatus)} / {fmtRate(service.writeBps, service.diskIOStatus)}</td>
          <td style={{ ...td, textAlign: 'right', color: T.blueDeep }} className="mono">{service.ports?.length ? service.ports.map(p => `:${p}`).join(' ') : (service.portsStatus === 'available' ? '—' : '无数据')}</td>
          <td style={{ ...td, color: T.ink3 }}>{service.networkStatus === 'unsupported' ? '不支持' : '无数据'}</td>
        </tr>)}
      </tbody>
    </table>
  )
}

function TerminateDialog({ target, onClose, onDone, onError }) {
  const [busy, setBusy] = useState(false)
  const terminate = async () => {
    setBusy(true)
    try {
      const response = await authFetch(`/api/v1/processes/${target.pid}/terminate`, { method: 'POST' })
      if (!response.ok) {
        const body = await response.json().catch(() => ({}))
        throw new Error(body.error || `终止失败 (${response.status})`)
      }
      onDone()
    } catch (error) {
      onError(error.message)
      onClose()
    } finally { setBusy(false) }
  }
  return <div role="dialog" aria-modal="true" aria-label="确认终止进程" style={modalBackdrop} onClick={onClose}>
    <div style={modalPanel} onClick={e => e.stopPropagation()}>
      <div style={{ fontSize: 15, fontWeight: 700, color: T.ink }}>终止进程？</div>
      <div style={{ fontSize: 12.5, color: T.ink2, lineHeight: 1.7, marginTop: 10 }}>
        将向 <b>{target.name}</b>（PID <span className="mono">{target.pid}</span>）发送 SIGTERM。未保存的数据可能丢失。
      </div>
      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 18 }}>
        <button onClick={onClose} disabled={busy} style={dialogBtn}>取消</button>
        <button onClick={terminate} disabled={busy} style={{ ...dialogBtn, background: T.red, color: '#fff', borderColor: T.red }}>{busy ? '正在终止...' : '确认终止'}</button>
      </div>
    </div>
  </div>
}

const navBtn = { height: 30, padding: '0 9px', display: 'inline-flex', alignItems: 'center', gap: 5, border: `1px solid ${T.border}`, borderRadius: 6, background: T.surface, color: T.ink2, cursor: 'pointer', fontSize: 11.5 }
const segmentBtn = { height: 28, padding: '0 14px', border: 'none', borderRadius: 5, cursor: 'pointer', fontSize: 12, fontWeight: 600 }
const iconBtn = { width: 28, height: 28, borderRadius: 5, border: `1px solid ${T.border}`, background: T.surface, display: 'inline-flex', alignItems: 'center', justifyContent: 'center', cursor: 'pointer' }
const emptyCell = { padding: 24, textAlign: 'center', color: T.ink3 }
const modalBackdrop = { position: 'absolute', inset: 0, zIndex: 20, background: 'rgba(15,23,42,.35)', display: 'flex', alignItems: 'center', justifyContent: 'center' }
const modalPanel = { width: 390, background: T.surface, border: `1px solid ${T.border}`, borderRadius: 8, boxShadow: '0 18px 48px rgba(15,23,42,.2)', padding: 20 }
const dialogBtn = { height: 32, padding: '0 14px', border: `1px solid ${T.border}`, borderRadius: 6, background: T.surface, color: T.ink2, cursor: 'pointer', fontSize: 12.5, fontWeight: 600 }

// ===========================================================================
// ProcessDetailDrawer — 5-tab 详情抽屉
// ===========================================================================

const TAB_KEYS = [
  { id: 'basic',    label: '基础' },
  { id: 'memory',   label: '内存' },
  { id: 'fdList',   label: 'FD' },
  { id: 'env',      label: 'Env' },
  { id: 'netConns', label: '网络' },
];

function ProcessDetailDrawer({ pid, onClose }) {
  const [detail, setDetail] = useState(null);
  const [activeTab, setActiveTab] = useState('basic');
  const [error, setError] = useState(null);

  useEffect(() => {
    setDetail(null); setError(null);
    // code-review fix: AbortController 防止快速切 pid 时旧响应覆盖新 state
    const ctrl = new AbortController();

    authFetch('/api/v1/processes/' + pid, { signal: ctrl.signal })
      .then(async r => {
        if (r.ok) return r.json();
        if (r.status === 404) {
          setError('进程已退出');
          return null;
        }
        setError('获取详情失败');
        return null;
      })
      .then(d => { if (d) setDetail(d); })
      .catch(err => {
        if (err.name === 'AbortError') return; // 切换 pid 时正常取消
        setError('网络错误');
      });

    return () => ctrl.abort();
  }, [pid]);

  return (
    <>
      {/* 遮罩 */}
      <div data-action="close-drawer" onClick={onClose} style={{
        position: 'absolute', inset: 0, background: 'rgba(15,23,42,0.2)', zIndex: 5,
      }}/>
      {/* 抽屉本体 */}
      <div style={{
        position: 'absolute', top: 0, right: 0, bottom: 0,
        width: 560, background: T.surface,
        borderLeft: `1px solid ${T.border}`, boxShadow: '-8px 0 24px rgba(15,23,42,0.06)',
        zIndex: 6, display: 'flex', flexDirection: 'column', overflow: 'hidden',
      }}>
        {/* Header */}
        <div style={{
          padding: '14px 20px', borderBottom: `1px solid ${T.border}`,
          display: 'flex', alignItems: 'center', gap: 10,
        }}>
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 15, fontWeight: 700, color: T.ink }}>
              进程 #{pid}
              {detail?.basic?.name && (
                <span style={{ marginLeft: 8, fontSize: 12, fontWeight: 500, color: T.ink3 }}>
                  {detail.basic.name}
                </span>
              )}
            </div>
            <div style={{ fontSize: 10.5, color: T.ink4, marginTop: 2 }}>进程详情</div>
          </div>
          <button data-action="close-drawer" onClick={onClose} style={{
            width: 28, height: 28, borderRadius: 5, border: `1px solid ${T.border}`,
            background: T.surface, cursor: 'pointer',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
          }}>
            <Icon name="x" size={14} stroke={2}/>
          </button>
        </div>

        {/* Tabs */}
        <TabBar
          tabs={TAB_KEYS}
          active={activeTab}
          onChange={setActiveTab}
          itemAs="button"
          inactiveTextColor={T.ink2}
          style={{ borderBottom: `1px solid ${T.border}`, background: T.surfaceAlt }}
          itemStyle={{ padding: '10px 16px', fontSize: 12 }}
          getItemProps={() => ({ 'data-action': 'switch-tab' })}
        />

        {/* Content */}
        <div style={{ flex: 1, overflow: 'auto', padding: 16 }}>
          {error && <div style={{ color: T.red, fontSize: 12 }}>{error}</div>}
          {!detail && !error && <div style={{ color: T.ink3, fontSize: 12 }}>加载中...</div>}
          {detail && activeTab === 'basic' && <BasicTab basic={detail.basic}/>}
          {detail && activeTab === 'memory' && <KVTable data={detail.memory}/>}
          {detail && activeTab === 'fdList' && <FDListTab list={detail.fdList}/>}
          {detail && activeTab === 'env' && <EnvTab list={detail.env}/>}
          {detail && activeTab === 'netConns' && <NetConnsTab list={detail.netConns}/>}
        </div>
      </div>
    </>
  );
}

function BasicTab({ basic }) {
  const rows = [
    ['PID', basic.pid],
    ['进程名', basic.name],
    ['PPID', basic.ppid],
    ['线程数', basic.threads],
    ['用户', basic.user],
    ['状态', basic.state],
    ['内存 (VmRSS)', fmtBytes(basic.memBytes)],
    ['启动时间', basic.startTime || '-'],
    ['命令行', basic.cmdline || '-'],
  ];
  return <KVRows rows={rows}/>;
}

function KVRows({ rows }) {
  return (
    <table style={{ width: '100%', fontSize: 12, borderCollapse: 'collapse' }}>
      <tbody>
        {rows.map(([k, v]) => (
          <tr key={k} style={{ borderBottom: `1px solid ${T.borderSoft}` }}>
            <td style={{ padding: '8px 0', color: T.ink3, fontSize: 11.5, width: 120 }}>{k}</td>
            <td style={{ padding: '8px 0', color: T.ink, fontFamily: 'ui-monospace, monospace',
              wordBreak: 'break-all' }}>{v ?? '-'}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function KVTable({ data }) {
  const entries = Object.entries(data || {});
  if (entries.length === 0) return <div style={{ color: T.ink3, fontSize: 12 }}>无数据（权限不足 / 字段缺失）</div>;
  return (
    <table style={{ width: '100%', fontSize: 12, borderCollapse: 'collapse' }}>
      <tbody>
        {entries.map(([k, v]) => (
          <tr key={k} style={{ borderBottom: `1px solid ${T.borderSoft}` }}>
            <td style={{ padding: '6px 0', color: T.ink3, fontSize: 11.5, width: 140 }}>{k}</td>
            <td style={{ padding: '6px 0', color: T.ink, fontFamily: 'ui-monospace, monospace' }}>{v}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function FDListTab({ list }) {
  if (!list || list.length === 0) return <div style={{ color: T.ink3, fontSize: 12 }}>无 FD（权限不足或进程无打开文件）</div>;
  return (
    <table style={{ width: '100%', fontSize: 11.5, borderCollapse: 'collapse', fontFamily: 'ui-monospace, monospace' }}>
      <thead>
        <tr style={{ background: T.surfaceAlt, borderBottom: `1px solid ${T.border}` }}>
          <th style={{ ...th, textAlign: 'right', width: 50 }}>FD</th>
          <th style={{ ...th, textAlign: 'left' }}>Target</th>
        </tr>
      </thead>
      <tbody>
        {list.map(fd => (
          <tr key={fd.fd} style={{ borderBottom: `1px solid ${T.borderSoft}` }}>
            <td style={{ padding: '5px 8px', textAlign: 'right', color: T.ink2 }}>{fd.fd}</td>
            <td style={{ padding: '5px 8px', color: T.ink, wordBreak: 'break-all' }}>{fd.target}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function EnvTab({ list }) {
  if (!list || list.length === 0) return <div style={{ color: T.ink3, fontSize: 12 }}>无 Env（权限不足或进程无环境变量）</div>;
  return (
    <table style={{ width: '100%', fontSize: 11.5, borderCollapse: 'collapse' }}>
      <thead>
        <tr style={{ background: T.surfaceAlt, borderBottom: `1px solid ${T.border}` }}>
          <th style={{ ...th, textAlign: 'left', width: 160 }}>Name</th>
          <th style={{ ...th, textAlign: 'left' }}>Value</th>
        </tr>
      </thead>
      <tbody>
        {list.map((e, i) => (
          <tr key={i} style={{ borderBottom: `1px solid ${T.borderSoft}` }}>
            <td style={{ padding: '5px 8px', color: T.ink2, fontFamily: 'ui-monospace, monospace' }}>{e.name}</td>
            <td style={{ padding: '5px 8px', color: T.ink, fontFamily: 'ui-monospace, monospace',
              wordBreak: 'break-all' }}>{e.value}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function NetConnsTab({ list }) {
  if (!list || list.length === 0) return <div style={{ color: T.ink3, fontSize: 12 }}>无网络连接</div>;
  return (
    <table style={{ width: '100%', fontSize: 11.5, borderCollapse: 'collapse', fontFamily: 'ui-monospace, monospace' }}>
      <thead>
        <tr style={{ background: T.surfaceAlt, borderBottom: `1px solid ${T.border}` }}>
          <th style={{ ...th, textAlign: 'left', width: 60 }}>类型</th>
          <th style={{ ...th, textAlign: 'left' }}>本地</th>
          <th style={{ ...th, textAlign: 'left' }}>远端</th>
          <th style={{ ...th, textAlign: 'left', width: 100 }}>状态</th>
        </tr>
      </thead>
      <tbody>
        {list.map((c, i) => (
          <tr key={i} style={{ borderBottom: `1px solid ${T.borderSoft}` }}>
            <td style={{ padding: '5px 8px', color: T.ink2 }}>{c.type}</td>
            <td style={{ padding: '5px 8px', color: T.ink, wordBreak: 'break-all' }}>{c.local}</td>
            <td style={{ padding: '5px 8px', color: T.ink, wordBreak: 'break-all' }}>{c.remote}</td>
            <td style={{ padding: '5px 8px', color: T.ink2 }}>{c.state}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
