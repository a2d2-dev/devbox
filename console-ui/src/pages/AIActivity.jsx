import { useMemo, useState, useEffect, useRef } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { cleanupStaleCodex, useAIActivity, useAITranscript } from '../hooks/useApi'
import TabBar from '../components/TabBar'
import { useViewportEnvironment } from '../hooks/useViewportEnvironment'

const aiTabItemStyle = {
  height: 39,
  padding: '0 14px',
  borderRadius: '6px 6px 0 0',
  fontSize: 13,
};

const th = {
  padding: '8px 12px',
  fontSize: 10.5,
  color: T.ink3,
  textTransform: 'uppercase',
  letterSpacing: '0.04em',
  fontWeight: 700,
  background: '#f8fafc',
  borderBottom: `1px solid ${T.borderSoft}`,
};

const td = {
  padding: '9px 12px',
  fontSize: 12,
  color: T.ink,
  borderBottom: `1px solid ${T.borderSoft}`,
  verticalAlign: 'top',
};

function fmtAge(sec) {
  if (!sec || sec < 0) return '-';
  if (sec < 60) return `${sec}s`;
  if (sec < 3600) return `${Math.floor(sec / 60)}m`;
  if (sec < 86400) return `${Math.floor(sec / 3600)}h`;
  return `${Math.floor(sec / 86400)}d ${Math.floor((sec % 86400) / 3600)}h`;
}

function fmtTime(v) {
  if (!v) return '-';
  const d = new Date(v);
  if (Number.isNaN(d.getTime())) return '-';
  return d.toLocaleString();
}

function fmtBytes(n) {
  if (!n) return '-';
  if (n >= 1024 * 1024) return (n / 1024 / 1024).toFixed(1) + ' MB';
  if (n >= 1024) return (n / 1024).toFixed(0) + ' KB';
  return n + ' B';
}

function Pill({ tone = 'slate', children }) {
  const map = {
    red: { bg: T.redSoft, fg: '#b91c1c', bd: '#fecaca' },
    amber: { bg: T.amberSoft, fg: '#a16207', bd: '#fde68a' },
    blue: { bg: T.blueSoft, fg: T.blueDeep, bd: '#bfdbfe' },
    green: { bg: T.greenSoft, fg: '#047857', bd: '#bbf7d0' },
    slate: { bg: T.surfaceAlt, fg: T.ink3, bd: T.border },
  }[tone];
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: 4,
      height: 20, padding: '0 7px', borderRadius: 5,
      fontSize: 11, fontWeight: 700,
      color: map.fg, background: map.bg, border: `1px solid ${map.bd}`,
    }}>{children}</span>
  );
}

function SoftButton({ tone = 'slate', icon, children, onClick, disabled, title }) {
  const map = {
    red: { bg: '#fff1f2', fg: '#dc2626', bd: '#fecdd3' },
    amber: { bg: '#fffbeb', fg: '#b45309', bd: '#fde68a' },
    blue: { bg: '#eff6ff', fg: T.blue, bd: '#bfdbfe' },
    green: { bg: '#ecfdf5', fg: '#047857', bd: '#bbf7d0' },
    slate: { bg: '#f8fafc', fg: T.ink2, bd: T.border },
    dark: { bg: '#1e293b', fg: '#fff', bd: '#1e293b' },
  }[tone];
  return (
    <button
      title={title}
      disabled={disabled}
      onClick={onClick}
      style={{
        height: 32,
        padding: icon && children ? '0 11px' : '0 10px',
        borderRadius: 6,
        border: `1px solid ${disabled ? T.border : map.bd}`,
        background: disabled ? T.surfaceAlt : map.bg,
        color: disabled ? T.ink4 : map.fg,
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 7,
        fontSize: 12,
        fontWeight: 800,
        cursor: disabled ? 'default' : 'pointer',
        whiteSpace: 'nowrap',
      }}>
      {icon && <Icon name={icon} size={13} stroke={2}/>}
      {children}
    </button>
  );
}

function AgentIcon({ id, issue, active }) {
  const icon = id === 'codex' ? 'code' : id === 'hermes' ? 'zap' : id === 'openclaw' ? 'openclaw' : id === 'agent-browser' ? 'globe' : 'brain';
  const color = issue ? T.red : id === 'codex' ? T.blue : id === 'openclaw' ? T.violet : id === 'hermes' ? T.teal : T.amber;
  return (
    <span style={{
      width: 30,
      height: 30,
      borderRadius: 8,
      flexShrink: 0,
      display: 'inline-flex',
      alignItems: 'center',
      justifyContent: 'center',
      color: '#fff',
      background: color,
      boxShadow: active ? `0 6px 14px ${color}35` : 'none',
    }}>
      <Icon name={icon} size={15} stroke={2}/>
    </span>
  );
}

function Stat({ icon, label, value, tone = T.blue }) {
  return (
    <div style={{
      background: T.surface,
      border: `1px solid ${T.border}`,
      borderRadius: 8,
      padding: '12px 14px',
      minWidth: 132,
    }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, color: T.ink3, fontSize: 11.5, fontWeight: 600 }}>
        <Icon name={icon} size={13} stroke={1.8} style={{ color: tone }}/>
        {label}
      </div>
      <div className="mono tnum" style={{ marginTop: 8, ...T.type.title, lineHeight: 1, color: T.ink, fontWeight: 800 }}>
        {value ?? '-'}
      </div>
    </div>
  );
}

function Section({ title, right, children }) {
  return (
    <div style={{
      background: T.surface,
      border: `1px solid ${T.border}`,
      borderRadius: 8,
      overflow: 'hidden',
      flexShrink: 0,
    }}>
      <div style={{
        height: 38,
        padding: '0 14px',
        display: 'flex',
        alignItems: 'center',
        borderBottom: `1px solid ${T.borderSoft}`,
      }}>
        <div style={{ fontSize: 13, fontWeight: 800, color: T.ink }}>{title}</div>
        <div style={{ flex: 1 }}/>
        {right}
      </div>
      {children}
    </div>
  );
}

function Empty({ text = '暂无数据' }) {
  return <div style={{ padding: 18, color: T.ink3, fontSize: 12 }}>{text}</div>;
}

const boardStatusOrder = [
  ['working', 'Working'],
  ['blocked', 'Blocked'],
  ['idle', 'Idle'],
  ['done', 'Done'],
  ['unknown', 'Unknown'],
];

function boardStatusMeta(status) {
  return {
    working: { label: 'Working', tone: 'green', color: T.green, soft: '#ecfdf5', border: '#bbf7d0' },
    blocked: { label: 'Blocked', tone: 'red', color: T.red, soft: '#fff1f2', border: '#fecdd3' },
    idle: { label: 'Idle', tone: 'amber', color: T.amber, soft: '#fffbeb', border: '#fde68a' },
    done: { label: 'Done', tone: 'slate', color: T.ink4, soft: '#f8fafc', border: T.borderSoft },
    unknown: { label: 'Unknown', tone: 'slate', color: T.ink4, soft: '#f8fafc', border: T.borderSoft },
  }[status] || { label: 'Unknown', tone: 'slate', color: T.ink4, soft: '#f8fafc', border: T.borderSoft };
}

function shortPath(path) {
  if (!path) return '-';
  const parts = String(path).split('/').filter(Boolean);
  if (parts.length <= 2) return path;
  return parts.slice(-2).join('/');
}

function BoardAgentRow({ card, active, onClick }) {
  const meta = boardStatusMeta(card.status);
  return (
    <button onClick={onClick} style={{
      width: '100%',
      minHeight: 58,
      textAlign: 'left',
      display: 'grid',
      gridTemplateColumns: '12px 1fr auto',
      gap: 10,
      alignItems: 'center',
      padding: '10px 11px',
      border: `1px solid ${active ? meta.color : T.borderSoft}`,
      borderRadius: 7,
      background: active ? meta.soft : T.surface,
      cursor: 'pointer',
      boxShadow: active ? '0 1px 3px rgba(15,23,42,.08)' : 'none',
    }}>
      <span style={{ width: 9, height: 9, borderRadius: 5, background: meta.color }}/>
      <span style={{ minWidth: 0 }}>
        <span style={{ display: 'block', fontSize: 12.5, color: T.ink, fontWeight: 850, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
          {card.name || card.id}
        </span>
        <span className="mono" style={{ display: 'block', marginTop: 4, fontSize: 10.5, color: T.ink4, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
          {shortPath(card.cwd)}{card.gitBranch ? ` · ${card.gitBranch}` : ''}
        </span>
      </span>
      <span style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: 4 }}>
        <Pill tone={meta.tone}>{card.kind}</Pill>
        {card.pid ? <span className="mono" style={{ fontSize: 10, color: T.ink4 }}>PID {card.pid}</span> : null}
      </span>
    </button>
  );
}

function TranscriptTail({ card }) {
  const transcriptPath = card?.transcriptPath || '';
  const { data, loading, error } = useAITranscript(transcriptPath, 200, 3000);
  const boxRef = useRef(null);

  useEffect(() => {
    if (boxRef.current) {
      boxRef.current.scrollTop = boxRef.current.scrollHeight;
    }
  }, [data?.text, transcriptPath]);

  if (!card) {
    return <Empty text="请选择一个 agent 查看详情" />;
  }
  if (!transcriptPath) {
    return <Empty text="该 agent 没有可读取的 transcript 路径" />;
  }
  if (error) {
    return (
      <div style={{ padding: 14, color: T.red, fontSize: 12.5 }}>
        transcript 读取失败：{error.message || String(error)}
      </div>
    );
  }

  return (
    <pre ref={boxRef} style={{
      margin: 0,
      padding: 14,
      minHeight: 0,
      height: '100%',
      overflow: 'auto',
      background: '#0f172a',
      color: '#dbeafe',
      fontFamily: T.mono,
      fontSize: 11,
      lineHeight: 1.55,
      whiteSpace: 'pre-wrap',
      wordBreak: 'break-word',
    }}>{loading && !data ? '加载 transcript...' : (data?.text || 'transcript 暂无内容')}</pre>
  );
}

function AgentBoard({ board }) {
  const cards = board?.agents || [];
  const [selectedId, setSelectedId] = useState('');
  const [mobileDetailOpen, setMobileDetailOpen] = useState(false);

  useEffect(() => {
    if (!cards.length) {
      setSelectedId('');
      return;
    }
    if (!cards.some(c => c.id === selectedId)) {
      setSelectedId(cards[0].id);
    }
  }, [cards, selectedId]);

  const selected = cards.find(c => c.id === selectedId) || cards[0] || null;
  const counts = board?.counts || {};
  const grouped = boardStatusOrder.map(([status, label]) => ({
    status,
    label,
    count: counts[status] || 0,
    cards: cards.filter(c => (c.status || 'unknown') === status),
  })).filter(g => g.status !== 'unknown' || g.count > 0 || g.cards.length > 0);

  if (!cards.length) {
    return <Empty text="没有发现 agent，Board 会在检测到进程、worker 或会话后显示。" />;
  }

  const meta = boardStatusMeta(selected?.status);
  return (
    <div className={`edge-ai-board${mobileDetailOpen ? ' is-detail-open' : ''}`} style={{ padding: 12, display: 'grid', gridTemplateColumns: '360px 1fr', gap: 12, minHeight: 560 }}>
      <div className="edge-ai-board-list" style={{ border: `1px solid ${T.border}`, borderRadius: 8, overflow: 'hidden', background: T.surface, minHeight: 0 }}>
        <div style={{ padding: '11px 14px', display: 'flex', alignItems: 'center', borderBottom: `1px solid ${T.borderSoft}` }}>
          <div style={{ fontSize: 13, fontWeight: 900, color: T.ink }}>Agent Board</div>
          <div style={{ flex: 1 }}/>
          <Pill tone="slate">{cards.length}</Pill>
        </div>
        <div style={{ padding: 10, display: 'grid', gap: 12, maxHeight: 'calc(100vh - 330px)', overflow: 'auto' }}>
          {grouped.map(group => {
            const groupMeta = boardStatusMeta(group.status);
            const isBlocked = group.status === 'blocked' && group.count > 0;
            return (
              <div key={group.status} style={{
                border: `1px solid ${isBlocked ? groupMeta.border : T.borderSoft}`,
                borderRadius: 8,
                background: isBlocked ? groupMeta.soft : T.surface,
                overflow: 'hidden',
              }}>
                <div style={{
                  padding: '9px 10px',
                  display: 'flex',
                  alignItems: 'center',
                  gap: 8,
                  borderBottom: group.cards.length ? `1px solid ${isBlocked ? groupMeta.border : T.borderSoft}` : 'none',
                }}>
                  <span style={{ width: 8, height: 8, borderRadius: 4, background: groupMeta.color }}/>
                  <div style={{ fontSize: 12.5, fontWeight: 900, color: isBlocked ? T.red : T.ink }}>{group.label}</div>
                  <div style={{ flex: 1 }}/>
                  <Pill tone={groupMeta.tone}>{group.count}</Pill>
                </div>
                {group.cards.length ? (
                  <div style={{ display: 'grid', gap: 7, padding: 8 }}>
                    {group.cards.map(card => (
                      <BoardAgentRow key={card.id} card={card} active={selected?.id === card.id} onClick={() => { setSelectedId(card.id); setMobileDetailOpen(true); }}/>
                    ))}
                  </div>
                ) : (
                  <div style={{ padding: '9px 10px', color: T.ink4, fontSize: 11.5 }}>暂无</div>
                )}
              </div>
            );
          })}
        </div>
      </div>
      <div className="edge-ai-board-detail" style={{ minWidth: 0, border: `1px solid ${T.border}`, borderRadius: 8, overflow: 'hidden', background: T.surface, display: 'grid', gridTemplateRows: 'auto 1fr', minHeight: 0 }}>
        <div style={{ padding: '13px 15px', borderBottom: `1px solid ${T.borderSoft}`, display: 'flex', alignItems: 'center', gap: 10 }}>
          <button type="button" className="edge-ai-back" onClick={() => setMobileDetailOpen(false)} aria-label="返回 Agent 列表">
            <Icon name="chevLeft" size={16} stroke={2}/><span>列表</span>
          </button>
          <span style={{ width: 10, height: 10, borderRadius: 5, background: meta.color, flexShrink: 0 }}/>
          <div style={{ minWidth: 0, flex: 1 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
              <div style={{ ...T.type.heading, fontWeight: 900, color: T.ink, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                {selected?.name || 'Agent'}
              </div>
              <Pill tone={meta.tone}>{meta.label}</Pill>
              {selected?.model && <Pill tone={String(selected.model).includes('fable') ? 'red' : 'blue'}>{selected.model}</Pill>}
            </div>
            <div className="mono" style={{ marginTop: 5, color: T.ink4, fontSize: 10.5, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
              {selected?.cwd || '-'}{selected?.gitBranch ? ` · ${selected.gitBranch}` : ''}
            </div>
          </div>
          <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
            {selected?.lastAt && <Pill tone="slate">{fmtTime(selected.lastAt)}</Pill>}
            {selected?.transcriptPath && <Pill tone="amber">只读 transcript</Pill>}
          </div>
        </div>
        <div style={{ display: 'grid', gridTemplateRows: 'auto 1fr', minHeight: 0 }}>
          <div style={{ padding: '10px 14px', borderBottom: `1px solid ${T.borderSoft}`, background: '#f8fafc' }}>
            <div style={{ color: T.ink3, fontSize: 11.5, fontWeight: 800, marginBottom: 5 }}>最近事件 / Prompt</div>
            <div style={{ color: T.ink2, fontSize: 12.5, lineHeight: 1.45, wordBreak: 'break-word' }}>
              {selected?.lastText || '暂无最近事件'}
            </div>
            {selected?.transcriptPath && (
              <div className="mono" style={{ marginTop: 6, color: T.ink4, fontSize: 10.5, wordBreak: 'break-all' }}>{selected.transcriptPath}</div>
            )}
          </div>
          <TranscriptTail card={selected}/>
        </div>
      </div>
    </div>
  );
}

function AgentCard({ agent, active, onClick }) {
  const issue = agent.issues?.some(i => i.severity === 'critical');
  const count = (agent.processes?.length || 0) + (agent.sessions?.length || 0) + (agent.workers?.length || 0);
  return (
    <button onClick={onClick} style={{
      textAlign: 'left',
      background: active ? '#fffaf2' : T.surface,
      border: `1px solid ${active ? T.amber : T.borderSoft}`,
      borderRadius: 9,
      padding: 12,
      cursor: 'pointer',
      boxShadow: active ? '0 1px 3px rgba(15,23,42,.08)' : 'none',
    }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <AgentIcon id={agent.id} issue={issue} active={active}/>
        <div style={{ fontSize: 13, fontWeight: 800, color: T.ink }}>{agent.name}</div>
        <div style={{ flex: 1 }}/>
        {issue ? <Pill tone="red">风险</Pill> : count > 0 ? <Pill tone="slate">{count}</Pill> : null}
      </div>
      <div style={{ marginTop: 8, color: T.ink3, fontSize: 11.5, lineHeight: 1.45 }}>
        {agent.description}
      </div>
      <div style={{ display: 'flex', gap: 6, marginTop: 9, flexWrap: 'wrap' }}>
        <Pill tone="blue">进程 {agent.processes?.length || 0}</Pill>
        <Pill tone="green">会话 {agent.sessions?.length || 0}</Pill>
        <Pill tone="slate">配置 {agent.configs?.filter(c => c.exists).length || 0}</Pill>
      </div>
    </button>
  );
}

function Issues({ issues }) {
  if (!issues?.length) return <Empty text="暂无限流、风险模型或配置异常" />;
  return (
    <div style={{ display: 'flex', flexDirection: 'column' }}>
      {issues.slice(0, 8).map((it, i) => (
        <div key={i} style={{
          padding: '10px 14px',
          borderBottom: i === issues.length - 1 ? 'none' : `1px solid ${T.borderSoft}`,
          display: 'flex',
          gap: 10,
        }}>
          <Icon name={it.severity === 'critical' ? 'alertTri' : 'info'} size={15} stroke={1.9}
            style={{ color: it.severity === 'critical' ? T.red : T.amber, flexShrink: 0, marginTop: 1 }}/>
          <div style={{ minWidth: 0 }}>
            <div style={{ fontSize: 12.5, fontWeight: 800, color: T.ink }}>{it.title}</div>
            <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3, lineHeight: 1.45, wordBreak: 'break-word' }}>
              {it.detail}
            </div>
            {it.ref && <div className="mono" style={{ fontSize: 10.5, color: T.ink4, marginTop: 4 }}>{it.ref}</div>}
          </div>
        </div>
      ))}
    </div>
  );
}

function ProcessTable({ rows }) {
  if (!rows?.length) return <Empty />;
  return (
    <div className="edge-ai-data-scroll">
      <table style={{ width: '100%', minWidth: 760, borderCollapse: 'collapse' }}>
      <thead><tr>
        <th style={{ ...th, textAlign: 'left' }}>PID</th>
        <th style={{ ...th, textAlign: 'left' }}>进程</th>
        <th style={{ ...th, textAlign: 'left' }}>模型/会话</th>
        <th style={{ ...th, textAlign: 'left' }}>工作目录</th>
        <th style={{ ...th, textAlign: 'left' }}>运行</th>
      </tr></thead>
      <tbody>
        {rows.map(p => (
          <tr key={p.pid}>
            <td style={td} className="mono tnum">{p.pid}</td>
            <td style={td}>
              <div style={{ fontWeight: 700 }}>{p.name}</div>
              <div className="mono" style={{ color: T.ink3, fontSize: 10.5, marginTop: 3, maxWidth: 420, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                {p.cmdline}
              </div>
            </td>
            <td style={td}>
              {p.model && <Pill tone={String(p.model).includes('fable') ? 'red' : 'blue'}>{p.model}</Pill>}
              {p.sessionId && <div className="mono" style={{ marginTop: 5, fontSize: 10.5, color: T.ink3 }}>{p.sessionId}</div>}
            </td>
            <td style={td}><span className="mono" style={{ fontSize: 10.5, color: T.ink3 }}>{p.cwd || '-'}</span></td>
            <td style={td}>{fmtAge(p.ageSec)}</td>
          </tr>
        ))}
        </tbody>
      </table>
    </div>
  );
}

function statusTone(row) {
  const text = `${row.state || ''} ${row.model || ''} ${row.cmdline || ''} ${row.cwd || ''}`.toLowerCase();
  if (text.includes('deleted') || text.includes('fable')) return { label: '异常', tone: 'red', dot: T.red };
  return { label: '运行', tone: 'green', dot: T.green };
}

function ProcessConsole({ rows }) {
  if (!rows?.length) return <Empty />;
  const errorCount = rows.filter(p => {
    const text = `${p.state || ''} ${p.cwd || ''} ${p.model || ''} ${p.cmdline || ''}`.toLowerCase();
    return text.includes('deleted') || text.includes('fable');
  }).length;
  const runningCount = rows.length - errorCount;
  return (
    <div style={{ padding: 18 }}>
      <div style={{ marginBottom: 12, color: T.ink3, fontSize: 12.5 }}>
        共 <b style={{ color: T.ink }}>{rows.length}</b> 个进程 · 运行 <b style={{ color: T.green }}>{runningCount}</b> · 异常 <b style={{ color: T.red }}>{errorCount}</b>
      </div>
      <div className="edge-ai-data-scroll" style={{ border: `1px solid ${T.borderSoft}`, borderRadius: 8, background: T.surface }}>
        <div style={{ minWidth: 720 }}>
          <div style={{
          display: 'grid',
          gridTemplateColumns: '130px 150px 120px 1fr 86px',
          padding: '10px 16px',
          background: T.surfaceAlt,
          color: T.ink4,
          fontSize: 10.5,
          fontWeight: 800,
          textTransform: 'uppercase',
          borderBottom: `1px solid ${T.borderSoft}`,
        }}>
          <div>PID</div><div>Worker</div><div>状态</div><div>模型 / CPU / 内存</div><div style={{ textAlign: 'right' }}>操作</div>
        </div>
        {rows.map((p, i) => {
          const meta = statusTone(p);
          return (
            <div key={`${p.pid}-${p.sessionId || p.name}-${i}`} style={{
              display: 'grid',
              gridTemplateColumns: '130px 150px 120px 1fr 86px',
              padding: '14px 16px',
              alignItems: 'center',
              borderBottom: i === rows.length - 1 ? 'none' : `1px solid ${T.borderSoft}`,
            }}>
              <div className="mono tnum" style={{ fontSize: 12.5, fontWeight: 800, color: T.ink }}>{p.pid}</div>
              <div>
                <div className="mono" style={{ fontSize: 12, color: T.ink2, fontWeight: 700 }}>{p.sessionId || p.name}</div>
                <div className="mono" style={{ marginTop: 4, fontSize: 10.5, color: T.ink4, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{p.name}</div>
              </div>
              <div><Pill tone={meta.tone}><span style={{ width: 6, height: 6, borderRadius: 3, background: meta.dot }}/>{meta.label}</Pill></div>
              <div style={{ minWidth: 0 }}>
                <div className="mono" style={{ fontSize: 11.5, color: String(p.model).includes('fable') ? T.red : T.ink2, fontWeight: 700 }}>{p.model || '-'}</div>
                <div className="mono" style={{ marginTop: 4, fontSize: 10.5, color: T.ink4, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                  {p.cwd || p.cmdline || '-'} · {fmtAge(p.ageSec)}
                </div>
              </div>
              <div style={{ textAlign: 'right' }}>
                <SoftButton tone="red" disabled title="暂未开放进程终止接口">终止</SoftButton>
              </div>
            </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

function SessionTable({ rows }) {
  if (!rows?.length) return <Empty />;
  return (
    <div className="edge-ai-data-scroll">
      <table style={{ width: '100%', minWidth: 820, borderCollapse: 'collapse' }}>
      <thead><tr>
        <th style={{ ...th, textAlign: 'left' }}>会话</th>
        <th style={{ ...th, textAlign: 'left' }}>模型/状态</th>
        <th style={{ ...th, textAlign: 'left' }}>Chat 摘要</th>
        <th style={{ ...th, textAlign: 'left' }}>路径</th>
        <th style={{ ...th, textAlign: 'left' }}>更新时间</th>
      </tr></thead>
      <tbody>
        {rows.map(s => (
          <tr key={`${s.kind}-${s.id}-${s.path}`}>
            <td style={td}>
              <div className="mono" style={{ fontSize: 11.5, fontWeight: 700 }}>{s.id}</div>
              <div style={{ marginTop: 5, display: 'flex', gap: 5, flexWrap: 'wrap' }}>
                {s.linkedPid && <Pill tone="blue">PID {s.linkedPid}</Pill>}
                {s.linkedWorker && <Pill tone="slate">worker {s.linkedWorker}</Pill>}
                {s.sessionKind && <Pill tone="slate">{s.sessionKind}</Pill>}
              </div>
            </td>
            <td style={td}>
              {s.model ? <Pill tone={String(s.model).includes('fable') ? 'red' : 'blue'}>{s.model}</Pill> : '-'}
              {s.rateLimited && <div style={{ marginTop: 6 }}><Pill tone="red">限流/账号</Pill></div>}
            </td>
            <td style={td}>
              <div style={{ maxWidth: 420, color: T.ink2, lineHeight: 1.45, wordBreak: 'break-word' }}>
                {s.lastError || s.lastPrompt || '-'}
              </div>
              {s.cwd && <div className="mono" style={{ fontSize: 10.5, color: T.ink4, marginTop: 5 }}>{s.cwd}</div>}
            </td>
            <td style={td}><span className="mono" style={{ fontSize: 10.5, color: T.ink3 }}>{s.path}</span></td>
            <td style={td}>{fmtTime(s.updatedAt)}</td>
          </tr>
        ))}
        </tbody>
      </table>
    </div>
  );
}

function sessionTitle(s) {
  if (s.lastPrompt) return String(s.lastPrompt).slice(0, 42);
  if (s.lastError) return String(s.lastError).slice(0, 42);
  return s.id;
}

function SessionsConsole({ rows }) {
  const [selectedId, setSelectedId] = useState('');
  useEffect(() => {
    if (!rows?.length) {
      setSelectedId('');
      return;
    }
    if (!rows.some(s => `${s.kind}-${s.id}-${s.path}` === selectedId)) {
      const s = rows[0];
      setSelectedId(`${s.kind}-${s.id}-${s.path}`);
    }
  }, [rows, selectedId]);

  if (!rows?.length) return <Empty />;
  const selected = rows.find(s => `${s.kind}-${s.id}-${s.path}` === selectedId) || rows[0];
  const selectedKey = `${selected.kind}-${selected.id}-${selected.path}`;
  return (
    <div style={{ padding: 18, display: 'grid', gridTemplateColumns: '340px 1fr', gap: 16, minHeight: 430 }}>
      <div style={{ border: `1px solid ${T.borderSoft}`, borderRadius: 8, overflow: 'hidden', background: T.surface }}>
        <div style={{ padding: '11px 14px', color: T.ink2, fontSize: 13, fontWeight: 800, borderBottom: `1px solid ${T.borderSoft}` }}>
          会话 · {rows.length}
        </div>
        <div style={{ maxHeight: 390, overflow: 'auto' }}>
          {rows.map((s, i) => {
            const key = `${s.kind}-${s.id}-${s.path}`;
            const on = key === selectedKey;
            return (
              <button key={key} onClick={() => setSelectedId(key)} style={{
                width: '100%',
                textAlign: 'left',
                padding: '13px 15px',
                border: 'none',
                borderBottom: i === rows.length - 1 ? 'none' : `1px solid ${T.borderSoft}`,
                borderLeft: `2px solid ${on ? T.blue : 'transparent'}`,
                background: on ? '#eff6ff' : T.surface,
                cursor: 'pointer',
              }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <span style={{ width: 7, height: 7, borderRadius: 4, background: s.rateLimited ? T.red : T.teal, flexShrink: 0 }}/>
                  <div style={{ flex: 1, minWidth: 0, fontSize: 12.5, fontWeight: 800, color: T.ink, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                    {sessionTitle(s)}
                  </div>
                  <span className="mono" style={{ fontSize: 10.5, color: T.ink4 }}>{s.sessionKind || s.kind}</span>
                </div>
                <div style={{ marginTop: 7, display: 'flex', gap: 8, alignItems: 'center' }}>
                  {s.model && <Pill tone={String(s.model).includes('fable') ? 'red' : 'blue'}>{s.model}</Pill>}
                  <span style={{ fontSize: 10.5, color: T.ink4 }}>{fmtTime(s.updatedAt)}</span>
                </div>
              </button>
            );
          })}
        </div>
      </div>
      <div style={{ border: `1px solid ${T.borderSoft}`, borderRadius: 8, overflow: 'hidden', background: '#f8fafc', minWidth: 0 }}>
        <div style={{ padding: '14px 16px', borderBottom: `1px solid ${T.borderSoft}`, background: T.surface }}>
          <div style={{ ...T.type.heading, fontWeight: 900, color: T.ink }}>{sessionTitle(selected)}</div>
          <div className="mono" style={{ marginTop: 5, color: T.ink4, fontSize: 10.5, wordBreak: 'break-all' }}>{selected.path}</div>
        </div>
        <div style={{ padding: 18, display: 'flex', flexDirection: 'column', gap: 14 }}>
          {selected.lastPrompt && (
            <div style={{ alignSelf: 'flex-end', maxWidth: '78%' }}>
              <div style={{ fontSize: 10.5, fontWeight: 800, color: T.blue, textAlign: 'right', marginBottom: 5 }}>用户</div>
              <div style={{ padding: '10px 13px', borderRadius: '10px 10px 3px 10px', background: T.blue, color: '#fff', fontSize: 13, lineHeight: 1.55 }}>
                {selected.lastPrompt}
              </div>
            </div>
          )}
          <div style={{ alignSelf: 'flex-start', maxWidth: '78%' }}>
            <div style={{ fontSize: 10.5, fontWeight: 800, color: T.teal, marginBottom: 5 }}>Assistant</div>
            <div style={{ padding: '10px 13px', borderRadius: '10px 10px 10px 3px', background: T.surface, border: `1px solid ${T.borderSoft}`, color: selected.rateLimited ? T.red : T.ink2, fontSize: 13, lineHeight: 1.55 }}>
              {selected.lastError || selected.lastPrompt || '该会话没有可展示的消息摘要。'}
            </div>
          </div>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', paddingTop: 4 }}>
            {selected.linkedPid && <Pill tone="blue">PID {selected.linkedPid}</Pill>}
            {selected.linkedWorker && <Pill tone="slate">worker {selected.linkedWorker}</Pill>}
            {selected.cwd && <Pill tone="slate">{selected.cwd}</Pill>}
          </div>
        </div>
      </div>
    </div>
  );
}

function WorkerTable({ rows }) {
  if (!rows?.length) return <Empty />;
  return (
    <div className="edge-ai-data-scroll">
      <table style={{ width: '100%', minWidth: 760, borderCollapse: 'collapse' }}>
      <thead><tr>
        <th style={{ ...th, textAlign: 'left' }}>Worker</th>
        <th style={{ ...th, textAlign: 'left' }}>模型/权限</th>
        <th style={{ ...th, textAlign: 'left' }}>最近事件</th>
        <th style={{ ...th, textAlign: 'left' }}>目录</th>
      </tr></thead>
      <tbody>
        {rows.map(w => (
          <tr key={w.short}>
            <td style={td}>
              <div className="mono" style={{ fontWeight: 800 }}>{w.short}</div>
              <div className="mono" style={{ fontSize: 10.5, color: T.ink3, marginTop: 3 }}>PID {w.pid || '-'}</div>
            </td>
            <td style={td}>
              <Pill tone={w.risky ? 'red' : 'blue'}>{w.model || '-'}</Pill>
              <div style={{ marginTop: 6, display: 'flex', gap: 5, flexWrap: 'wrap' }}>
                {w.permission && <Pill tone="slate">{w.permission}</Pill>}
                {w.effort && <Pill tone="slate">effort {w.effort}</Pill>}
                {w.source && <Pill tone="slate">{w.source}</Pill>}
              </div>
            </td>
            <td style={td}>
              <div style={{ color: w.risky ? T.red : T.ink2, lineHeight: 1.45, maxWidth: 460, wordBreak: 'break-word' }}>
                {w.lastTimeline || '-'}
              </div>
              {w.lastAt && <div style={{ fontSize: 10.5, color: T.ink4, marginTop: 4 }}>{fmtTime(w.lastAt)}</div>}
            </td>
            <td style={td}><span className="mono" style={{ fontSize: 10.5, color: T.ink3 }}>{w.cwd || '-'}</span></td>
          </tr>
        ))}
        </tbody>
      </table>
    </div>
  );
}

function ConfigList({ rows }) {
  if (!rows?.length) return <Empty />;
  return (
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(360px, 1fr))', gap: 10, padding: 12 }}>
      {rows.map(c => (
        <div key={`${c.kind}-${c.path}`} style={{ border: `1px solid ${T.borderSoft}`, borderRadius: 7, overflow: 'hidden', background: T.surface }}>
          <div style={{ padding: '9px 10px', borderBottom: `1px solid ${T.borderSoft}`, display: 'flex', gap: 8, alignItems: 'center' }}>
            <Icon name={c.exists ? 'file' : 'alertTri'} size={13} stroke={1.8} style={{ color: c.exists ? T.blue : T.amber }}/>
            <div className="mono" title={c.path} style={{ fontSize: 11, color: T.ink, fontWeight: 700, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
              {c.path}
            </div>
          </div>
          <div style={{ padding: '8px 10px', display: 'flex', gap: 6, flexWrap: 'wrap' }}>
            <Pill tone={c.exists ? 'green' : 'amber'}>{c.exists ? '存在' : '未找到'}</Pill>
            {c.exists && <Pill tone="slate">{fmtBytes(c.sizeBytes)}</Pill>}
            {c.exists && <Pill tone="slate">{fmtTime(c.updatedAt)}</Pill>}
          </div>
          {c.preview && (
            <pre style={{
              margin: 0,
              padding: '9px 10px',
              maxHeight: 220,
              overflow: 'auto',
              background: '#0f172a',
              color: '#dbeafe',
              fontSize: 10.5,
              lineHeight: 1.45,
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-word',
              fontFamily: T.mono,
            }}>{c.preview}</pre>
          )}
        </div>
      ))}
    </div>
  );
}

function ConfigConsole({ rows }) {
  const existing = rows?.filter(c => c.exists) || [];
  const list = rows?.length ? rows : [];
  const [selectedPath, setSelectedPath] = useState('');
  const [draft, setDraft] = useState('');

  useEffect(() => {
    if (!list.length) {
      setSelectedPath('');
      setDraft('');
      return;
    }
    const selectedExists = list.some(c => c.path === selectedPath);
    const next = selectedExists ? list.find(c => c.path === selectedPath) : (existing[0] || list[0]);
    if (next && next.path !== selectedPath) {
      setSelectedPath(next.path);
      setDraft(next.preview || '');
    }
  }, [list, existing, selectedPath]);

  if (!list.length) return <Empty />;
  const selected = list.find(c => c.path === selectedPath) || existing[0] || list[0];
  const ext = (selected.path || '').split('.').pop() || 'conf';
  const reset = () => setDraft(selected.preview || '');

  return (
    <div style={{ padding: 18, display: 'grid', gridTemplateColumns: '300px 1fr', gap: 16, minHeight: 0 }}>
      <div style={{ border: `1px solid ${T.borderSoft}`, borderRadius: 8, overflow: 'hidden', background: T.surface }}>
        <div style={{ padding: '11px 14px', color: T.ink2, fontSize: 13, fontWeight: 800, borderBottom: `1px solid ${T.borderSoft}` }}>
          配置文件 · {existing.length}
        </div>
        <div style={{ maxHeight: 360, overflow: 'auto' }}>
          {list.map((c, i) => {
            const on = c.path === selected.path;
            const name = c.path.split('/').pop();
            const kind = (c.path.split('.').pop() || 'conf').slice(0, 6);
            return (
              <button key={`${c.kind}-${c.path}`} onClick={() => { setSelectedPath(c.path); setDraft(c.preview || ''); }} style={{
                width: '100%',
                display: 'flex',
                alignItems: 'center',
                gap: 10,
                padding: '12px 14px',
                border: 'none',
                borderBottom: i === list.length - 1 ? 'none' : `1px solid ${T.borderSoft}`,
                borderLeft: `2px solid ${on ? T.amber : 'transparent'}`,
                background: on ? '#fffaf2' : T.surface,
                cursor: 'pointer',
                textAlign: 'left',
              }}>
                <span style={{
                  width: 28,
                  height: 28,
                  borderRadius: 7,
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  background: c.exists ? T.amberSoft : T.surfaceAlt,
                  color: c.exists ? T.amber : T.ink4,
                  flexShrink: 0,
                }}>
                  <Icon name={c.exists ? 'file' : 'alertTri'} size={13} stroke={2}/>
                </span>
                <span style={{ flex: 1, minWidth: 0 }}>
                  <span style={{ display: 'block', fontSize: 12.5, color: T.ink, fontWeight: 800, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{name}</span>
                  <span className="mono" style={{ display: 'block', marginTop: 3, color: T.ink4, fontSize: 10.5, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{c.path}</span>
                </span>
                <Pill tone={c.exists ? 'slate' : 'amber'}>{kind}</Pill>
              </button>
            );
          })}
        </div>
      </div>
      <div style={{ border: `1px solid ${T.borderSoft}`, borderRadius: 8, overflow: 'hidden', background: T.surface, minWidth: 0 }}>
        <div style={{ padding: '12px 16px', display: 'flex', alignItems: 'center', gap: 10, borderBottom: `1px solid ${T.borderSoft}` }}>
          <div className="mono" style={{ flex: 1, minWidth: 0, color: T.ink, fontSize: 12.5, fontWeight: 800, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
            {selected.path}
          </div>
          <SoftButton tone="slate" onClick={reset}>重置</SoftButton>
          <SoftButton tone="blue" disabled title="当前只读展示真实配置，写入接口尚未开放">保存</SoftButton>
        </div>
        <div style={{ padding: '18px 20px', display: 'grid', gap: 18, maxHeight: 390, overflow: 'auto' }}>
          <div>
            <div style={{ color: T.ink3, fontSize: 12.5, fontWeight: 800, marginBottom: 12 }}>结构化字段</div>
            <div style={{ display: 'grid', gridTemplateColumns: '180px 1fr', gap: '12px 22px', alignItems: 'center' }}>
              <div>
                <div style={{ fontSize: 12.5, color: T.ink2, fontWeight: 700 }}>文件状态</div>
                <div className="mono" style={{ fontSize: 10.5, color: T.ink4, marginTop: 2 }}>exists</div>
              </div>
              <div><Pill tone={selected.exists ? 'green' : 'amber'}>{selected.exists ? '存在' : '未找到'}</Pill></div>
              <div>
                <div style={{ fontSize: 12.5, color: T.ink2, fontWeight: 700 }}>文件大小</div>
                <div className="mono" style={{ fontSize: 10.5, color: T.ink4, marginTop: 2 }}>size</div>
              </div>
              <div><Pill tone="slate">{selected.exists ? fmtBytes(selected.sizeBytes) : '-'}</Pill></div>
              <div>
                <div style={{ fontSize: 12.5, color: T.ink2, fontWeight: 700 }}>更新时间</div>
                <div className="mono" style={{ fontSize: 10.5, color: T.ink4, marginTop: 2 }}>mtime</div>
              </div>
              <div style={{ fontSize: 12.5, color: T.ink2 }}>{selected.exists ? fmtTime(selected.updatedAt) : '-'}</div>
            </div>
          </div>
          <div>
            <div style={{ color: T.ink3, fontSize: 12.5, fontWeight: 800, marginBottom: 10 }}>原始配置</div>
            <textarea
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              spellCheck={false}
              style={{
                width: '100%',
                minHeight: 190,
                resize: 'vertical',
                border: `1px solid ${T.border}`,
                borderRadius: 8,
                padding: 12,
                outline: 'none',
                background: '#0f172a',
                color: '#dbeafe',
                fontFamily: T.mono,
                fontSize: 11,
                lineHeight: 1.55,
              }}
            />
          </div>
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            <Pill tone="blue">{ext}</Pill>
            <Pill tone="slate">真实文件预览</Pill>
            <Pill tone="slate">截断预览</Pill>
            <Pill tone="amber">只读</Pill>
          </div>
        </div>
      </div>
    </div>
  );
}

function CodexCleanupPanel({ onDone }) {
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState(null);
  const [error, setError] = useState('');

  async function runCleanup() {
    const ok = window.confirm('只会清理工作目录已 deleted 的 Codex app-server / broker 进程。确认执行？');
    if (!ok) return;
    setBusy(true);
    setError('');
    try {
      const r = await cleanupStaleCodex();
      setResult(r);
      onDone?.();
    } catch (err) {
      setError(err?.message || String(err));
    } finally {
      setBusy(false);
    }
  }

  const matched = result?.matched?.length || 0;
  const termed = result?.termed?.length || 0;
  const killed = result?.killed?.length || 0;
  const failed = result?.failed?.length || 0;

  return (
    <div style={{
      display: 'flex',
      alignItems: 'center',
      gap: 12,
      padding: '11px 13px',
      background: '#fffbeb',
      border: '1px solid #fde68a',
      borderRadius: 8,
    }}>
      <span style={{
        width: 30,
        height: 30,
        borderRadius: 8,
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        color: '#92400e',
        background: '#fef3c7',
        flexShrink: 0,
      }}>
        <Icon name="trash" size={15} stroke={2}/>
      </span>
      <div style={{ minWidth: 0, flex: 1 }}>
        <div style={{ fontSize: 12.5, fontWeight: 800, color: T.ink }}>清理 stale Codex app-server</div>
        <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 2 }}>
          仅匹配 cwd 含 deleted 且命令为 Codex app-server / app-server-broker 的进程。
        </div>
        {result && (
          <div style={{ display: 'flex', gap: 6, marginTop: 7, flexWrap: 'wrap' }}>
            <Pill tone="blue">匹配 {matched}</Pill>
            <Pill tone="green">TERM {termed}</Pill>
            <Pill tone={killed ? 'amber' : 'slate'}>KILL {killed}</Pill>
            <Pill tone={failed ? 'red' : 'slate'}>失败 {failed}</Pill>
          </div>
        )}
        {error && <div style={{ color: T.red, fontSize: 11.5, marginTop: 6 }}>{error}</div>}
      </div>
      <SoftButton tone="amber" icon="trash" onClick={runCleanup} disabled={busy}>
        {busy ? '清理中...' : '清理'}
      </SoftButton>
    </div>
  );
}

export default function AIActivity() {
  const { data, loading, error, refresh } = useAIActivity(5000);
  const { isPhone } = useViewportEnvironment();
  const agents = data?.agentTypes || [];
  const [agentId, setAgentId] = useState('');
  const [tab, setTab] = useState('board');
  const [mobileDetailOpen, setMobileDetailOpen] = useState(false);
  const active = useMemo(() => {
    if (!agents.length) return null;
    return agents.find(a => a.id === agentId) || agents[0];
  }, [agents, agentId]);

  const issues = data?.issues || [];
  const summary = data?.summary || {};
  const board = data?.board || { counts: {}, agents: [] };
  const activeIssue = active?.issues?.some(i => i.severity === 'critical');

  return (
    <div className="edge-page edge-ai-page" style={{ flex: 1, display: 'flex', flexDirection: 'column', background: T.surfaceAlt, overflow: 'hidden' }}>
      <div className="edge-ai-header" style={{ padding: '14px 22px', background: T.surface, borderBottom: `1px solid ${T.border}`, flexShrink: 0 }}>
        <div className="edge-ai-header-row" style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <span style={{
            width: 40,
            height: 40,
            borderRadius: 10,
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: '#fff',
            background: T.violet,
            boxShadow: '0 8px 18px rgba(139,92,246,.28)',
          }}>
            <Icon name="brain" size={20} stroke={2}/>
          </span>
          <div>
            <div style={{ ...T.type.title, fontWeight: 900, color: T.ink }}>Agent 活动中心</div>
            <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3 }}>
              Claude Code / Codex / OpenClaw / Hermes · 进程、会话、配置与限流风险
            </div>
          </div>
          <div style={{ flex: 1 }}/>
          {loading && <Pill tone="slate">刷新中</Pill>}
          {error && <Pill tone="red">接口异常</Pill>}
          {data?.generatedAt && <div style={{ fontSize: 11, color: T.ink3 }}>更新 {fmtTime(data.generatedAt)}</div>}
          <SoftButton tone="slate" icon="refresh" onClick={refresh}>刷新</SoftButton>
        </div>
        <div className="edge-ai-summary" style={{ display: 'flex', gap: 10, marginTop: 13, flexWrap: 'wrap' }}>
          <Stat icon="brain" label="Agent 类型" value={summary.agentTypes || agents.length} tone={T.blue}/>
          <Stat icon="dashboard" label="Board 卡片" value={board.agents?.length || 0} tone={T.teal}/>
          <Stat icon="cpu" label="Claude 进程" value={summary.claudeProcesses || 0} tone={T.indigo}/>
          <Stat icon="code" label="Codex 进程" value={summary.codexProcesses || 0} tone={T.cyan}/>
          <Stat icon="alertTri" label="风险 Worker" value={summary.riskyWorkers || 0} tone={summary.riskyWorkers ? T.red : T.green}/>
          <Stat icon="file" label="配置文件" value={summary.configFiles || 0} tone={T.slate}/>
        </div>
      </div>

      <div className={`edge-ai-layout${mobileDetailOpen ? ' is-detail-open' : ''}`} style={{ flex: 1, display: 'grid', gridTemplateColumns: '300px 1fr', gap: 12, padding: 12, minHeight: 0 }}>
        <div className="edge-ai-master" style={{ display: 'flex', flexDirection: 'column', gap: 10, minHeight: 0, overflow: 'hidden' }}>
          <Section title="Agent 类型">
            <div style={{
              display: 'flex',
              flexDirection: 'column',
              gap: 8,
              padding: 10,
              overflowY: 'auto',
              maxHeight: 310,
              minHeight: 250,
            }}>
              {agents.length ? agents.map(a => (
                <AgentCard key={a.id} agent={a} active={active?.id === a.id}
                  onClick={() => { setAgentId(a.id); setTab('overview'); if (isPhone) setMobileDetailOpen(true); }}/>
              )) : <Empty text={loading ? '正在加载...' : '没有发现 agent 活动'} />}
            </div>
          </Section>
          <div style={{ minHeight: 0, overflow: 'auto' }}>
            <Section title="全局风险">
              <Issues issues={issues}/>
            </Section>
          </div>
        </div>

        <div className="edge-ai-detail" style={{ minWidth: 0, minHeight: 0, display: 'flex', flexDirection: 'column', gap: 10 }}>
          <div style={{
            background: T.surface,
            border: `1px solid ${T.border}`,
            borderRadius: 8,
            overflow: 'hidden',
            minHeight: 0,
          }}>
            <div style={{
              padding: '14px 18px 12px',
              display: 'flex',
              alignItems: 'center',
              gap: 12,
              borderBottom: `1px solid ${T.borderSoft}`,
            }}>
              <button type="button" className="edge-ai-back" onClick={() => setMobileDetailOpen(false)} aria-label="返回 Agent 类型列表">
                <Icon name="chevLeft" size={16} stroke={2}/><span>列表</span>
              </button>
              {active && <AgentIcon id={active.id} issue={activeIssue} active/>}
              <div style={{ minWidth: 0 }}>
                <div style={{ ...T.type.heading, fontWeight: 900, color: T.ink }}>{active ? active.name : '详情'}</div>
                {active && <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 2 }}>{active.description}</div>}
              </div>
              <div style={{ flex: 1 }}/>
              {active && <Pill tone="blue">{active.id}</Pill>}
            </div>
            <div style={{
              padding: '0 18px',
              display: 'flex',
              gap: 4,
              borderBottom: `1px solid ${T.borderSoft}`,
              background: T.surface,
            }}>
              <TabBar
                tabs={[
                  { id: 'board', label: 'Board', icon: 'dashboard' },
                  { id: 'overview', label: '概览', icon: 'dashboard' },
                  { id: 'processes', label: '进程', icon: 'cpu' },
                  { id: 'sessions', label: '会话 / Chat', icon: 'message' },
                  { id: 'configs', label: '配置', icon: 'file' },
                  { id: 'events', label: '事件', icon: 'clock' },
                ]}
                active={tab}
                onChange={setTab}
                itemAs="button"
                activeTextColor={T.blue}
                activeWeight={800}
                inactiveWeight={600}
                style={{ gap: 4 }}
                itemStyle={aiTabItemStyle}
                activeItemStyle={{ background: '#eff6ff' }}
                renderLabel={(t2) => (
                  <>
                    <Icon name={t2.icon} size={13} stroke={2}/>{t2.label}
                  </>
                )}
              />
            </div>
            <div style={{ overflow: 'auto', maxHeight: 'calc(100vh - 260px)' }}>
              {tab !== 'board' && !active && <Empty />}
              {tab === 'board' && <AgentBoard board={board}/>}
              {active && tab === 'overview' && (
                <div style={{ padding: 12, display: 'grid', gap: 10 }}>
                  {active.id === 'codex' && <CodexCleanupPanel/>}
                  <div className="edge-ai-stat-grid" style={{ display: 'grid', gridTemplateColumns: 'repeat(4, minmax(120px, 1fr))', gap: 10 }}>
                    <Stat icon="cpu" label="进程" value={active.processes?.length || 0}/>
                    <Stat icon="message" label="会话" value={active.sessions?.length || 0}/>
                    <Stat icon="file" label="配置" value={active.configs?.filter(c => c.exists).length || 0}/>
                    <Stat icon="alertTri" label="风险" value={active.issues?.length || 0} tone={active.issues?.length ? T.red : T.green}/>
                  </div>
                  <Section title="风险与提示"><Issues issues={active.issues}/></Section>
                  {active.workers?.length > 0 && <Section title="Claude Worker"><WorkerTable rows={active.workers}/></Section>}
                </div>
              )}
              {active && tab === 'processes' && (
                <div style={{ display: 'grid', gap: 10, padding: active.id === 'codex' ? 12 : 0 }}>
                  {active.id === 'codex' && <CodexCleanupPanel/>}
                  <div style={{ overflow: 'hidden', borderRadius: active.id === 'codex' ? 8 : 0 }}>
                    <ProcessConsole rows={active.workers?.length ? active.workers.map(w => ({
                      pid: w.pid || '-',
                      name: w.short,
                      sessionId: w.short,
                      model: w.model,
                      cmdline: w.lastTimeline,
                      cwd: w.cwd,
                      ageSec: 0,
                      state: w.risky ? 'error' : 'active',
                    })) : active.processes}/>
                  </div>
                </div>
              )}
              {active && tab === 'sessions' && <SessionsConsole rows={active.sessions}/>}
              {active && tab === 'configs' && <ConfigConsole rows={active.configs}/>}
              {active && tab === 'events' && (
                <div style={{ display: 'grid', gap: 10, padding: 12 }}>
                  <Section title="风险事件"><Issues issues={active.issues}/></Section>
                  {active.workers?.length > 0 && <Section title="Worker Timeline 摘要"><WorkerTable rows={active.workers}/></Section>}
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
