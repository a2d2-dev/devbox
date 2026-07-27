// [UI Merged 2026-06-20] Ports（仅 LISTEN 视图）入口已并入
// Story 6.2 NetworkConnections 全连接表（LISTEN + ESTABLISHED + ...）。
// 桌面图标与 AppShell 路由已移除，本文件保留供历史追溯。
// 后端 GET /api/v1/ports 仍可用，可被运维 CLI 或脚本调用。
// 详见 ADR 2026-06-20-agent-console-controlled-desktop.md。

import { useState, useEffect, useRef } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, Chip, Sparkline } from '../components/ui'
import { useFiles, usePorts, useModels } from '../hooks/useApi'

const btnSecondary = {
  display: 'inline-flex', alignItems: 'center', gap: 5,
  borderRadius: 7, border: `1px solid ${T.border}`,
  background: 'white', color: T.ink2, cursor: 'pointer',
  fontSize: 12.5, fontWeight: 500,
};

const btnPrimary = {
  display: 'inline-flex', alignItems: 'center', gap: 5,
  borderRadius: 7, border: 'none',
  background: T.blue, color: 'white', cursor: 'pointer',
  fontSize: 12.5, fontWeight: 600,
  height: 32, padding: '0 14px',
};

const th = { padding: '8px 14px', fontSize: 10.5, fontWeight: 600, color: T.ink3,
  letterSpacing: '0.04em', textTransform: 'uppercase' };

const iconBtnLight = {
  width: 26, height: 26, borderRadius: 5, border: `1px solid ${T.border}`,
  background: T.surface, color: T.ink3, cursor: 'pointer',
  display: 'flex', alignItems: 'center', justifyContent: 'center',
};

const Card = ({ title, action, padding = 16, children }) => (
  <div style={{ background: T.surface, border: `1px solid ${T.border}`, borderRadius: 10 }}>
    {title && (
      <div style={{ display: 'flex', alignItems: 'center', padding: '10px 16px',
        borderBottom: `1px solid ${T.borderSoft}` }}>
        <div style={{ fontSize: 12.5, fontWeight: 600, color: T.ink }}>{title}</div>
        <div style={{ flex: 1 }}/>
        {action}
      </div>
    )}
    <div style={{ padding }}>{children}</div>
  </div>
);

export default function PortsFace({ authed, onRequireAuth }) {
  const [livePorts, setLivePorts] = useState([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');
  useEffect(() => {
    fetch('/api/v1/ports').then(r => r.ok ? r.json() : null).then(d => {
      if (d && Array.isArray(d)) {
        setLivePorts(d.map(x => ({
          id: x.process + ':' + x.port,
          app: x.process || 'unknown',
          port: x.port,
          proto: x.protocol || 'TCP',
          state: x.state === 'local-only' ? 'lan' : 'public',
          url: (x.local === '0.0.0.0' || x.local === '*' || x.local === '::' ? 'http://' + (window.DEVICE?.ip || 'localhost') : 'http://' + x.local) + ':' + x.port,
          traffic: 0,
          since: '',
          auth: '',
          pid: x.pid,
          local: x.local,
        })));
      }
      setLoading(false);
    }).catch(() => setLoading(false));
  }, []);
  return (
    <div className="edge-page edge-ports-page" style={{ flex: 1, display: 'flex', flexDirection: 'column',
      background: T.surfaceAlt, overflow: 'hidden' }}>
      <div className="edge-ports-header" style={{
        padding: '14px 24px', background: T.surface,
        borderBottom: `1px solid ${T.border}`, flexShrink: 0,
      }}>
        <div className="edge-ports-header-row" style={{ display: 'flex', alignItems: 'center' }}>
          <div>
            <div style={{ fontSize: 17, fontWeight: 700, color: T.ink, letterSpacing: '-0.01em' }}>端口与公网访问</div>
            <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3 }}>
              通过 Cloudflare Tunnel 把本机服务安全暴露到公网 ·
              <span className="mono" style={{ marginLeft: 4 }}>share.edgex.cloud</span>
            </div>
          </div>
          <div style={{ flex: 1 }}/>
          <button className="edge-press edge-btn-primary" style={btnPrimary}>
            <Icon name="plus" size={13} stroke={2}/>新增端口转发
          </button>
        </div>
      </div>

      <div className="edge-ports-content" style={{ flex: 1, overflow: 'auto', padding: 24 }}>
        {/* Stats */}
        <div className="edge-ports-stats" style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 10, marginBottom: 16 }}>
          {[
            { label: '已暴露端口', val: '7',  unit: '个' },
            { label: '公网访问', val: '4',  unit: '个' },
            { label: '今日访问', val: '10.4', unit: 'k' },
            { label: '上行流量', val: '2.3', unit: 'GB' },
          ].map((s, i) => (
            <div key={i} style={{ background: T.surface, border: `1px solid ${T.border}`,
              borderRadius: 8, padding: 14 }}>
              <div style={{ fontSize: 10.5, color: T.ink3, fontWeight: 600,
                letterSpacing: '0.04em', textTransform: 'uppercase' }}>{s.label}</div>
              <div style={{ display: 'flex', alignItems: 'baseline', gap: 4, marginTop: 4 }}>
                <span className="mono tnum" style={{ fontSize: 22, fontWeight: 700, color: T.ink,
                  letterSpacing: '-0.02em' }}>{s.val}</span>
                <span style={{ fontSize: 12, color: T.ink3 }}>{s.unit}</span>
              </div>
            </div>
          ))}
        </div>

        <div className="edge-ports-toolbar" style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 12 }}>
          <div style={{ fontSize: 14, fontWeight: 700, color: T.ink }}>端口列表</div>
          <div style={{ fontSize: 11.5, padding: '2px 8px', borderRadius: 999,
            background: T.surface, border: `1px solid ${T.border}`, color: T.ink3,
          }}>共 {livePorts.length} 个</div>
          <div style={{ flex: 1 }}/>
          <div className="edge-ports-search" style={{
            display: 'flex', alignItems: 'center', gap: 8,
            height: 32, padding: '0 10px', borderRadius: 7,
            background: T.surface, border: `1px solid ${T.border}`, width: 260,
          }}>
            <Icon name="search" size={13} stroke={1.8} style={{ color: T.ink4 }}/>
            <input value={search} onChange={(e) => setSearch(e.target.value)}
              placeholder="搜索服务名或端口..."
              style={{ flex: 1, border: 'none', outline: 'none', fontSize: 12.5, background: 'transparent' }}/>
          </div>
        </div>

        <div className="edge-ports-table edge-table-scroll">
          <Card padding={0}>
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12.5 }}>
            <thead>
              <tr style={{ background: '#fafbfc' }}>
                <th style={{ ...th, textAlign: 'left' }}>服务</th>
                <th style={{ ...th, textAlign: 'left' }}>端口 / 协议</th>
                <th style={{ ...th, textAlign: 'left' }}>访问范围</th>
                <th style={{ ...th, textAlign: 'left' }}>访问 URL</th>
                <th style={{ ...th, textAlign: 'right' }}>今日访问</th>
                <th style={{ ...th, textAlign: 'left' }}>认证</th>
                <th style={{ ...th, textAlign: 'right' }}></th>
              </tr>
            </thead>
            <tbody>
              {livePorts.filter(p => !search || (p.app||'').toLowerCase().includes(search.toLowerCase()) || String(p.port).includes(search)).map((p, i) => {
                const state = {
                  public: { tone: 'green', label: '公网', icon: 'globe', color: '#047857' },
                  lan:    { tone: 'blue',  label: '内网', icon: 'network', color: T.blueDeep },
                  offline:{ tone: 'red',   label: '离线', icon: 'cloudOff', color: T.red },
                }[p.state];
                return (
                  <tr key={p.id} style={{ borderTop: i ? `1px solid ${T.borderSoft}` : 'none' }}>
                    <td style={{ padding: '10px 14px' }}>
                      <span style={{ fontSize: 12.5, color: T.ink, fontWeight: 600 }}>{p.app}</span>
                    </td>
                    <td style={{ padding: '10px 14px' }} className="mono">
                      <span style={{ color: T.ink, fontWeight: 600 }}>:{p.port}</span>
                      <span style={{ color: T.ink3, marginLeft: 6 }}>{p.proto}</span>
                    </td>
                    <td style={{ padding: '10px 14px' }}>
                      <span style={{
                        display: 'inline-flex', alignItems: 'center', gap: 4,
                        fontSize: 11.5, color: state.color, fontWeight: 600,
                      }}>
                        <Icon name={state.icon} size={11} stroke={1.8}/>{state.label}
                      </span>
                    </td>
                    <td style={{ padding: '10px 14px' }} className="mono">
                      <span style={{ fontSize: 11.5, color: p.state === 'offline' ? T.ink4 : T.ink2,
                        background: T.surfaceAlt, padding: '2px 6px', borderRadius: 3,
                        border: `1px solid ${T.borderSoft}` }}>{p.url}</span>
                    </td>
                    <td style={{ padding: '10px 14px', textAlign: 'right', color: T.ink2 }} className="mono tnum">{p.traffic.toLocaleString()}</td>
                    <td style={{ padding: '10px 14px', color: T.ink3, fontSize: 11.5 }}>
                      {p.auth === 'token' && <Chip tone="blue">Token</Chip>}
                      {p.auth === 'cookie' && <Chip tone="gray">Cookie</Chip>}
                      {p.auth === 'none' && <Chip tone="amber">无</Chip>}
                      {p.auth === '—' && <span>—</span>}
                    </td>
                    <td style={{ padding: '10px 14px', textAlign: 'right' }}>
                      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 4 }}>
                        {p.state !== 'offline' && (
                          <button type="button" style={iconBtnLight} title="复制 URL" aria-label={`复制 ${p.app} 的 URL`}>
                            <Icon name="copy" size={12} stroke={1.8}/>
                          </button>
                        )}
                        {p.state === 'public' && (
                          <button type="button" style={iconBtnLight} title="二维码" aria-label={`显示 ${p.app} 的二维码`}>
                            <Icon name="qrcode" size={12} stroke={1.8}/>
                          </button>
                        )}
                        <button type="button" style={iconBtnLight} title="编辑" aria-label={`编辑 ${p.app}`}>
                          <Icon name="gear" size={12} stroke={1.8}/>
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
            </table>
          </Card>
        </div>
      </div>
    </div>
  );
}
