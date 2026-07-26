// Processes.jsx — 进程管理（PRD FR-13 / Story 6.1）
//
// 列表 + 顶部筛选器 + 行点击弹 5-tab 详情抽屉（basic/memory/fdList/env/netConns）。
//
// 设计约束（AC-NO-KILL-BUTTON / AC-NO-DESTRUCTIVE）：
//   - **不出现**「结束」/ kill / SIGTERM / SIGKILL / 重启 / 任何 destructive 按钮
//   - 所有 data-action 属性受白名单约束：仅 view-detail / close-drawer 等只读动作

import { useState, useEffect } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { authFetch } from '../hooks/useApi'
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

function fmtBytes(n) {
  if (!n || n < 0) return '-';
  if (n >= 1024 * 1024 * 1024) return (n / (1024 * 1024 * 1024)).toFixed(1) + ' GB';
  if (n >= 1024 * 1024) return (n / (1024 * 1024)).toFixed(1) + ' MB';
  if (n >= 1024) return (n / 1024).toFixed(0) + ' KB';
  return n + ' B';
}

export default function Processes() {
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState(null); // 详情抽屉打开的 pid
  const [filters, setFilters] = useState({ state: '', pid: '', name: '', user: '' });

  // 轮询 /api/v1/processes 每 10 秒
  useEffect(() => {
    let stopped = false;
    function load() {
      authFetch('/api/v1/processes')
        .then(r => r.ok ? r.json() : null)
        .then(d => { if (!stopped && Array.isArray(d)) setItems(d); })
        .catch(() => {})
        .finally(() => { if (!stopped) setLoading(false); });
    }
    load();
    const id = setInterval(load, 10000);
    return () => { stopped = true; clearInterval(id); };
  }, []);

  // 筛选
  const visible = items.filter(p => {
    if (filters.state && !p.state?.toLowerCase().includes(filters.state.toLowerCase())) return false;
    if (filters.pid && !String(p.pid).includes(filters.pid)) return false;
    if (filters.name && !(p.name || '').toLowerCase().includes(filters.name.toLowerCase())) return false;
    if (filters.user && !(p.user || '').toLowerCase().includes(filters.user.toLowerCase())) return false;
    return true;
  });

  return (
    <div className="edge-page edge-processes-page" style={{ flex: 1, display: 'flex', flexDirection: 'column',
      background: T.surfaceAlt, overflow: 'hidden', position: 'relative' }}>
      {/* Header */}
      <div className="edge-page-header" style={{
        padding: '14px 24px', background: T.surface,
        borderBottom: `1px solid ${T.border}`, flexShrink: 0,
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <div>
            <div style={{ fontSize: 17, fontWeight: 700, color: T.ink, letterSpacing: '-0.01em' }}>
              进程管理
            </div>
            <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3 }}>
              宿主进程列表 · 只读 · 离线可用 · 不可结束进程
            </div>
          </div>
          <div style={{ flex: 1 }}/>
          <div style={{ fontSize: 11.5, color: T.ink3 }}>
            共 <span className="mono tnum" style={{ color: T.ink, fontWeight: 700 }}>{visible.length}</span>
            {visible.length !== items.length && (
              <span> / {items.length}</span>
            )}
            <span> 个</span>
          </div>
        </div>

        {/* 筛选器 */}
        <div className="edge-filter-row" style={{ display: 'flex', gap: 8, marginTop: 12 }}>
          <input style={{ ...filterInput, width: 110 }} placeholder="状态 (R/S/D/Z/T)"
            value={filters.state} onChange={e => setFilters({...filters, state: e.target.value})}/>
          <input style={{ ...filterInput, width: 100 }} placeholder="PID"
            value={filters.pid} onChange={e => setFilters({...filters, pid: e.target.value})}/>
          <input style={{ ...filterInput, width: 200 }} placeholder="进程名"
            value={filters.name} onChange={e => setFilters({...filters, name: e.target.value})}/>
          <input style={{ ...filterInput, width: 130 }} placeholder="用户"
            value={filters.user} onChange={e => setFilters({...filters, user: e.target.value})}/>
        </div>
      </div>

      {/* 列表 */}
      <div className="edge-page-content" style={{ flex: 1, overflow: 'auto', padding: 16 }}>
        <div className="edge-table-scroll" style={{ background: T.surface, border: `1px solid ${T.border}`, borderRadius: 8 }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12.5 }}>
            <thead>
              <tr style={{ background: '#fafbfc' }}>
                <th style={{ ...th, textAlign: 'right', width: 80 }}>PID</th>
                <th style={{ ...th, textAlign: 'left' }}>进程</th>
                <th style={{ ...th, textAlign: 'center', width: 70 }}>状态</th>
                <th style={{ ...th, textAlign: 'left', width: 100 }}>用户</th>
                <th style={{ ...th, textAlign: 'right', width: 100 }}>内存</th>
                <th style={{ ...th, textAlign: 'left' }}>命令行</th>
              </tr>
            </thead>
            <tbody>
              {loading && (
                <tr><td colSpan={6} style={{ padding: 24, textAlign: 'center', color: T.ink3 }}>加载中...</td></tr>
              )}
              {!loading && visible.length === 0 && (
                <tr><td colSpan={6} style={{ padding: 24, textAlign: 'center', color: T.ink3 }}>无匹配进程</td></tr>
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
                  <td style={{ ...td, textAlign: 'right', fontFamily: 'ui-monospace, monospace' }}>{fmtBytes(p.memBytes)}</td>
                  <td style={{ ...td, fontFamily: 'ui-monospace, monospace', fontSize: 11.5,
                    color: T.ink2, maxWidth: 400, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {p.cmdline || '-'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {/* 详情抽屉 */}
      {selected && <ProcessDetailDrawer pid={selected} onClose={() => setSelected(null)}/>}
    </div>
  );
}

// ===========================================================================
// ProcessDetailDrawer — 5-tab 详情抽屉
// AC-NO-KILL-BUTTON: 任何 tab 都不出现「结束」/ kill / destructive 按钮
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
      <div className="edge-detail-drawer" style={{
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
            <div style={{ fontSize: 10.5, color: T.ink4, marginTop: 2 }}>只读视图 · 不可结束</div>
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
