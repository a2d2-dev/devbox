// NetworkConnections.jsx — 网络连接管理（PRD FR-14 / Story 6.2）
//
// 承接 Story 3.3 移除的 Ports UI 入口，提供全连接表（LISTEN + ESTABLISHED + ...）。
// 只读 GET，无任何 destructive 操作（kill / close / block）。

import { useState, useEffect } from 'react'
import { T } from '../tokens'
import { authFetch } from '../hooks/useApi'

const th = {
  padding: '8px 14px', fontSize: 10.5, fontWeight: 600, color: T.ink3,
  letterSpacing: '0.04em', textTransform: 'uppercase',
};
const td = { padding: '8px 14px', fontSize: 12, color: T.ink, fontFamily: 'ui-monospace, monospace' };

const filterInput = {
  height: 30, padding: '0 10px', borderRadius: 6,
  border: `1px solid ${T.border}`, background: T.surface,
  fontSize: 12, color: T.ink, outline: 'none',
};

function stateBadgeColor(state) {
  if (state === 'LISTEN') return { bg: '#dcfce7', fg: '#047857' };
  if (state === 'ESTABLISHED') return { bg: '#e0f2fe', fg: '#0369a1' };
  if (state === 'TIME_WAIT' || state === 'CLOSE_WAIT' || state === 'FIN_WAIT1' || state === 'FIN_WAIT2') return { bg: '#fef3c7', fg: '#a16207' };
  return { bg: T.surfaceAlt, fg: T.ink3 };
}

const ALL_STATES = ['LISTEN', 'ESTABLISHED', 'TIME_WAIT', 'CLOSE_WAIT',
  'FIN_WAIT1', 'FIN_WAIT2', 'SYN_SENT', 'SYN_RECV', 'CLOSE', 'CLOSING', 'LAST_ACK'];

export default function NetworkConnections() {
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [filters, setFilters] = useState({ stateFilter: 'ALL', pid: '', name: '', port: '' });

  useEffect(() => {
    let stopped = false;
    function load() {
      authFetch('/api/v1/network/connections?limit=2000')
        .then(r => r.ok ? r.json() : null)
        .then(d => {
          if (stopped) return;
          if (Array.isArray(d)) { setItems(d); setError(null); }
        })
        .catch(() => { if (!stopped) setError('网络信息读取失败'); })
        .finally(() => { if (!stopped) setLoading(false); });
    }
    load();
    const id = setInterval(load, 10000);
    return () => { stopped = true; clearInterval(id); };
  }, []);

  const visible = items.filter(c => {
    if (filters.stateFilter !== 'ALL' && c.state !== filters.stateFilter) return false;
    if (filters.pid && !String(c.pid).includes(filters.pid)) return false;
    if (filters.name && !(c.processName || '').toLowerCase().includes(filters.name.toLowerCase())) return false;
    if (filters.port && !(c.local || '').includes(':' + filters.port) && !(c.remote || '').includes(':' + filters.port)) return false;
    return true;
  });

  return (
    <div className="edge-page edge-network-page" style={{ flex: 1, display: 'flex', flexDirection: 'column',
      background: T.surfaceAlt, overflow: 'hidden' }}>
      {/* Header */}
      <div className="edge-page-header" style={{
        padding: '14px 24px', background: T.surface,
        borderBottom: `1px solid ${T.border}`, flexShrink: 0,
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <div>
            <div style={{ fontSize: 17, fontWeight: 700, color: T.ink, letterSpacing: '-0.01em' }}>
              网络连接
            </div>
            <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3 }}>
              全节点 socket 视图 · 只读 · 离线可用 · 不可关闭连接
            </div>
          </div>
          <div style={{ flex: 1 }}/>
          <div style={{ fontSize: 11.5, color: T.ink3 }}>
            共 <span className="mono tnum" style={{ color: T.ink, fontWeight: 700 }}>{visible.length}</span>
            {visible.length !== items.length && <span> / {items.length}</span>}
            <span> 条</span>
          </div>
        </div>
        <div className="edge-filter-row" style={{ display: 'flex', gap: 8, marginTop: 12, alignItems: 'center' }}>
          <select
            value={filters.stateFilter}
            onChange={e => setFilters({...filters, stateFilter: e.target.value})}
            style={{ ...filterInput, width: 130 }}>
            <option value="ALL">全部状态</option>
            {ALL_STATES.map(s => <option key={s} value={s}>{s}</option>)}
          </select>
          <input style={{ ...filterInput, width: 100 }} placeholder="PID"
            value={filters.pid} onChange={e => setFilters({...filters, pid: e.target.value})}/>
          <input style={{ ...filterInput, width: 160 }} placeholder="进程名"
            value={filters.name} onChange={e => setFilters({...filters, name: e.target.value})}/>
          <input style={{ ...filterInput, width: 100 }} placeholder="端口"
            value={filters.port} onChange={e => setFilters({...filters, port: e.target.value})}/>
        </div>
      </div>

      {/* 表格 */}
      <div style={{ flex: 1, overflow: 'auto', padding: 16 }}>
        {error && (
          <div style={{ padding: '10px 14px', borderRadius: 6,
            background: '#fef2f2', border: '1px solid #fecaca',
            color: '#991b1b', fontSize: 12, marginBottom: 14 }}>{error}</div>
        )}
        <div className="edge-table-scroll" style={{ background: T.surface, border: `1px solid ${T.border}`, borderRadius: 8 }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
            <thead>
              <tr style={{ background: '#fafbfc' }}>
                <th style={{ ...th, textAlign: 'left', width: 70 }}>类型</th>
                <th style={{ ...th, textAlign: 'left', width: 110 }}>状态</th>
                <th style={{ ...th, textAlign: 'left' }}>本地</th>
                <th style={{ ...th, textAlign: 'left' }}>远端</th>
                <th style={{ ...th, textAlign: 'right', width: 80 }}>PID</th>
                <th style={{ ...th, textAlign: 'left', width: 140 }}>进程</th>
              </tr>
            </thead>
            <tbody>
              {loading && <tr><td colSpan={6} style={{ padding: 24, textAlign: 'center', color: T.ink3 }}>加载中...</td></tr>}
              {!loading && visible.length === 0 && <tr><td colSpan={6} style={{ padding: 24, textAlign: 'center', color: T.ink3 }}>无匹配连接</td></tr>}
              {!loading && visible.map((c, i) => {
                const color = stateBadgeColor(c.state);
                return (
                  <tr key={i} style={{ borderTop: i ? `1px solid ${T.borderSoft}` : 'none' }}>
                    <td style={td}>{c.type}</td>
                    <td style={td}>
                      <span style={{
                        display: 'inline-block', padding: '1px 6px', borderRadius: 3,
                        fontSize: 10.5, fontWeight: 600,
                        background: color.bg, color: color.fg,
                      }}>{c.state}</span>
                    </td>
                    <td style={{ ...td, wordBreak: 'break-all' }}>{c.local}</td>
                    <td style={{ ...td, wordBreak: 'break-all' }}>{c.remote}</td>
                    <td style={{ ...td, textAlign: 'right' }}>{c.pid || '-'}</td>
                    <td style={td}>{c.processName || '-'}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
