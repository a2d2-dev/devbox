import { useState, useEffect, useRef } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, Chip, Ring, Sparkline, Card, useTicker } from '../components/ui'
import { useDevice, useMetrics, useMetricsHistory, useApps, useAlerts, useNetwork } from '../hooks/useApi'
import { ALERTS as MOCK_ALERTS } from '../data/mock'
import { btnSecondary, btnPrimary, btnDanger } from '../components/AppWindow'

export default function AlertCenter({ authed, onRequireAuth }) {
  const [tab, setTab] = useState('active');
  const [expanded, setExpanded] = useState(null);
  const [liveAlerts, setLiveAlerts] = useState(MOCK_ALERTS);
  useEffect(() => {
    const poll = () => fetch('/api/v1/alerts').then(r => r.ok ? r.json() : null).then(d => { if (d && Array.isArray(d) && d.length > 0) setLiveAlerts(d); }).catch(() => {});
    poll();
    const id = setInterval(poll, 15000);
    return () => clearInterval(id);
  }, []);
  const filtered = liveAlerts.filter(a => tab === 'active' ? a.state === 'active' : tab === 'history' ? a.state === 'recovered' : true);

  const sevMap = {
    critical:  { tone: 'red',   icon: 'alertTri', label: '紧急', color: T.red },
    warning:   { tone: 'amber', icon: 'alertTri', label: '警告', color: T.amber },
    info:      { tone: 'blue',  icon: 'info',     label: '信息', color: T.blue },
    recovered: { tone: 'green', icon: 'check',    label: '已恢复', color: T.green },
  };

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', background: T.surfaceAlt }}>
      {/* header */}
      <div style={{ padding: '18px 24px 0', background: T.surface,
        borderBottom: `1px solid ${T.border}` }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 14 }}>
          <div>
            <div style={{ fontSize: 17, fontWeight: 700, color: T.ink, letterSpacing: '-0.01em' }}>告警中心</div>
            <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 4 }}>
              共 {liveAlerts.length} 条 · <span style={{ color: T.red, fontWeight: 600 }}>{liveAlerts.filter(a=>a.state==='active').length} 活跃</span> · {liveAlerts.filter(a=>a.state!=='active').length} 已恢复
            </div>
          </div>
          <div style={{ flex: 1 }}/>
          <div style={{ display: 'flex', gap: 8 }}>
            <button style={btnSecondary}><Icon name="download" size={13} stroke={1.8}/>导出</button>
            <button style={authed ? btnPrimary : { ...btnSecondary, color: T.ink3 }} onClick={() => !authed && onRequireAuth('全部确认告警')}>
              <Icon name="check" size={13} stroke={2}/>{authed ? '全部确认' : '需验证后确认'}
            </button>
          </div>
        </div>
        <div style={{ display: 'flex', gap: 4 }}>
          {[['active','活跃',2],['history','已恢复',2],['all','全部',4]].map(([id, lbl, n]) => (
            <div key={id} onClick={() => setTab(id)} style={{
              padding: '8px 14px 10px', fontSize: 13, cursor: 'pointer',
              color: tab === id ? T.blueDeep : T.ink3,
              fontWeight: tab === id ? 600 : 500,
              borderBottom: `2px solid ${tab === id ? T.blue : 'transparent'}`,
              marginBottom: -1,
            }}>{lbl} <span style={{ color: T.ink4, fontWeight: 500 }}>· {n}</span></div>
          ))}
        </div>
      </div>

      {/* list */}
      <div style={{ flex: 1, overflow: 'auto', padding: 24 }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          {filtered.map(a => {
            const s = sevMap[a.sev];
            const open = expanded === a.id;
            return (
              <div key={a.id} style={{
                background: T.surface, borderRadius: 10,
                border: `1px solid ${T.border}`,
                borderLeft: `3px solid ${s.color}`,
                overflow: 'hidden',
              }}>
                <div onClick={() => setExpanded(open ? null : a.id)} style={{
                  padding: '14px 16px', display: 'flex', alignItems: 'center', gap: 12,
                  cursor: 'pointer',
                }}>
                  <div style={{
                    width: 30, height: 30, borderRadius: 8, flexShrink: 0,
                    background: `${s.color}15`, color: s.color,
                    display: 'flex', alignItems: 'center', justifyContent: 'center',
                  }}>
                    <Icon name={s.icon} size={15} stroke={1.8}/>
                  </div>
                  <div style={{ minWidth: 0, flex: 1 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                      <span style={{ fontSize: 13.5, fontWeight: 600, color: T.ink }}>{a.title}</span>
                      <Chip tone={s.tone}>{s.label}</Chip>
                      {a.state === 'active' && <Chip tone="red"><StatusDot tone="red" size={6} pulse/>活跃</Chip>}
                      {a.state === 'recovered' && <Chip tone="gray">已恢复</Chip>}
                    </div>
                    <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 4, display: 'flex', gap: 8 }}>
                      <span className="mono">{a.time}</span>
                      <span style={{ color: '#cbd5e1' }}>·</span>
                      <span>{a.target}</span>
                    </div>
                  </div>
                  {a.state === 'active' && (
                    <button onClick={(e) => { e.stopPropagation(); !authed && onRequireAuth('确认告警 ' + a.id); }}
                      style={authed ? btnPrimary : btnSecondary}>
                      <Icon name="check" size={13} stroke={2}/>{authed ? '确认' : '需验证'}
                    </button>
                  )}
                  <Icon name="chevDown" size={14} stroke={2}
                    style={{ color: T.ink4, transform: `rotate(${open ? 180 : 0}deg)`, transition: 'transform 0.2s' }}/>
                </div>
                {open && (
                  <div className="edge-fade-in" style={{
                    padding: '12px 16px 16px', background: T.surfaceAlt,
                    borderTop: `1px solid ${T.borderSoft}`,
                  }}>
                    <div style={{ fontSize: 12.5, color: T.ink2, lineHeight: 1.7 }}>{a.detail}</div>
                    {a.suggest && (
                      <div style={{ marginTop: 12 }}>
                        <div style={{ fontSize: 10.5, color: T.ink3, fontWeight: 600,
                          letterSpacing: '0.04em', textTransform: 'uppercase', marginBottom: 6 }}>建议操作</div>
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                          {a.suggest.map((sg, i) => (
                            <div key={i} style={{
                              display: 'flex', alignItems: 'center', gap: 8,
                              padding: '8px 10px', background: T.surface,
                              border: `1px solid ${T.borderSoft}`, borderRadius: 6,
                              fontSize: 12, color: T.ink2,
                            }}>
                              <span style={{
                                width: 18, height: 18, borderRadius: '50%',
                                background: T.blueSoft, color: T.blueDeep,
                                display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
                                fontSize: 10.5, fontWeight: 700, flexShrink: 0,
                              }} className="mono">{i + 1}</span>
                              <span style={{ flex: 1 }}>{sg}</span>
                              <Icon name="chevRight" size={12} stroke={2} style={{ color: T.ink4 }}/>
                            </div>
                          ))}
                        </div>
                      </div>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
