import React, { useState, useEffect, useRef, useCallback } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { useSupervisor, supervisorControl, supervisorLogs } from '../hooks/useApi'

const STATUS_COLORS = {
  RUNNING:  '#22c55e',
  STOPPED:  '#94a3b8',
  FATAL:    '#ef4444',
  STARTING: '#f59e0b',
  BACKOFF:  '#f59e0b',
  EXITED:   '#94a3b8',
  UNKNOWN:  '#94a3b8',
};

const FILTERS = ['ALL', 'RUNNING', 'STOPPED', 'FATAL'];

function formatUptime(start, now) {
  if (!start || !now || start <= 0) return '';
  const sec = now - start;
  if (sec < 60) return `${sec}s`;
  if (sec < 3600) return `${Math.floor(sec / 60)}m`;
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ${Math.floor((sec % 3600) / 60)}m`;
  return `${Math.floor(sec / 86400)}d ${Math.floor((sec % 86400) / 3600)}h`;
}

function StatusPill({ label, count, color, active, onClick }) {
  return (
    <button onClick={onClick} style={{
      display: 'inline-flex', alignItems: 'center', gap: 5,
      padding: '4px 10px', borderRadius: 12,
      background: active ? (color + '18') : 'white',
      border: `1px solid ${active ? color : T.border}`,
      fontSize: 12, fontWeight: active ? 600 : 400,
      color: active ? color : T.ink2,
      cursor: 'pointer',
    }}>
      {label}
      <span style={{
        fontSize: 11, fontWeight: 700, color: 'white',
        background: color, borderRadius: 8, padding: '1px 6px',
        minWidth: 18, textAlign: 'center',
      }}>{count}</span>
    </button>
  );
}

function ServiceCard({ proc, onViewLog, onControl }) {
  const color = STATUS_COLORS[proc.statename] || '#94a3b8';
  const isRunning = proc.statename === 'RUNNING';

  return (
    <div style={{
      background: 'white', borderRadius: 8,
      border: `1px solid ${T.border}`,
      padding: '10px 14px',
      display: 'flex', alignItems: 'center', gap: 10,
    }}>
      {/* Status dot */}
      <div style={{
        width: 8, height: 8, borderRadius: '50%',
        background: color, flexShrink: 0,
        boxShadow: isRunning ? `0 0 6px ${color}60` : 'none',
      }}/>

      {/* Name + group badge */}
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
          <span style={{ fontSize: 13, fontWeight: 600, color: T.ink }}>{proc.name}</span>
          <span style={{
            fontSize: 10, padding: '1px 6px', borderRadius: 4,
            background: T.blueSoft, color: T.blueDeep,
          }}>{proc.group}</span>
        </div>
        <div style={{ fontSize: 11, color: T.ink3, marginTop: 2 }}>
          {isRunning && proc.pid > 0 && <span>PID {proc.pid}</span>}
          {isRunning && proc.port && <span> · :{proc.port}</span>}
          {isRunning && <span> · {formatUptime(proc.start, proc.now)}</span>}
          {!isRunning && <span>{proc.statename}</span>}
        </div>
      </div>

      {/* Actions */}
      <div style={{ display: 'flex', gap: 4, flexShrink: 0 }}>
        <button onClick={() => onViewLog(proc.name)} title="查看日志" style={actionBtn}>
          <Icon name="terminal" size={13} stroke={1.8}/>
        </button>
        {isRunning ? (
          <button onClick={() => onControl(proc.name, 'restart')} title="重启" style={actionBtn}>
            <Icon name="refresh" size={13} stroke={1.8}/>
          </button>
        ) : (
          <button onClick={() => onControl(proc.name, 'start')} title="启动"
            style={{ ...actionBtn, color: '#22c55e' }}>
            <Icon name="play" size={13} stroke={2}/>
          </button>
        )}
        {isRunning && (
          <button onClick={() => onControl(proc.name, 'stop')} title="停止"
            style={{ ...actionBtn, color: '#ef4444' }}>
            <Icon name="stop" size={13} stroke={2}/>
          </button>
        )}
      </div>
    </div>
  );
}

const actionBtn = {
  display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
  width: 28, height: 28, borderRadius: 6,
  background: '#f8fafc', border: `1px solid ${T.border}`,
  color: T.ink2, cursor: 'pointer',
};

function LogModal({ name, onClose }) {
  const [log, setLog] = useState('');
  const [loading, setLoading] = useState(true);
  const ref = useRef(null);

  useEffect(() => {
    setLoading(true);
    supervisorLogs(name)
      .then(d => { setLog(d.log || ''); setLoading(false); })
      .catch(() => { setLog('Failed to fetch logs'); setLoading(false); });
  }, [name]);

  useEffect(() => {
    if (ref.current) ref.current.scrollTop = ref.current.scrollHeight;
  }, [log]);

  return (
    <div style={{
      position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.4)',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      zIndex: 1000,
    }} onClick={onClose}>
      <div onClick={e => e.stopPropagation()} style={{
        width: '80%', maxWidth: 900, height: '70vh',
        background: '#1e293b', borderRadius: 12,
        display: 'flex', flexDirection: 'column',
        boxShadow: '0 20px 60px rgba(0,0,0,0.3)',
      }}>
        <div style={{
          padding: '10px 16px', borderBottom: '1px solid #334155',
          display: 'flex', alignItems: 'center', justifyContent: 'space-between',
        }}>
          <span style={{ fontSize: 13, fontWeight: 600, color: '#e2e8f0' }}>
            {name} — stdout
          </span>
          <button onClick={onClose} style={{
            background: 'none', border: 'none', color: '#94a3b8',
            cursor: 'pointer', fontSize: 18, lineHeight: 1,
          }}>×</button>
        </div>
        <pre ref={ref} style={{
          flex: 1, margin: 0, padding: 14, overflow: 'auto',
          fontSize: 12, lineHeight: 1.6, color: '#cbd5e1',
          fontFamily: 'ui-monospace, "Cascadia Code", Menlo, monospace',
          whiteSpace: 'pre-wrap', wordBreak: 'break-all',
        }}>
          {loading ? 'Loading...' : (log || '(empty)')}
        </pre>
      </div>
    </div>
  );
}

export default function SupervisorFace() {
  const { data, loading } = useSupervisor(5000);
  const [filter, setFilter] = useState('ALL');
  const [logTarget, setLogTarget] = useState(null);
  const [controlling, setControlling] = useState(null);

  const processes = data?.processes || [];

  const counts = {
    ALL: processes.length,
    RUNNING: processes.filter(p => p.statename === 'RUNNING').length,
    STOPPED: processes.filter(p => p.statename === 'STOPPED' || p.statename === 'EXITED').length,
    FATAL: processes.filter(p => p.statename === 'FATAL').length,
  };

  const filtered = filter === 'ALL'
    ? processes
    : processes.filter(p => {
        if (filter === 'STOPPED') return p.statename === 'STOPPED' || p.statename === 'EXITED';
        return p.statename === filter;
      });

  // Group by group name, sorted by groupOrder
  const groups = {};
  filtered.forEach(p => {
    if (!groups[p.group]) groups[p.group] = { order: p.groupOrder, procs: [] };
    groups[p.group].procs.push(p);
  });
  const sortedGroups = Object.entries(groups).sort((a, b) => a[1].order - b[1].order);

  const handleControl = useCallback(async (name, action) => {
    setControlling(name);
    try {
      await supervisorControl(name, action);
    } catch (e) {
      console.error('Control failed:', e);
    }
    setControlling(null);
  }, []);

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column', background: '#f8fafc' }}>
      {/* Header */}
      <div style={{
        padding: '10px 16px', borderBottom: `1px solid ${T.borderSoft}`,
        background: T.surface, display: 'flex', alignItems: 'center', gap: 12,
        flexShrink: 0,
      }}>
        <Icon name="gear" size={16} stroke={1.8} style={{ color: T.ink2 }}/>
        <span style={{ fontSize: 13, fontWeight: 600, color: T.ink }}>Supervisor</span>
        {data?.hostname && (
          <span style={{ fontSize: 11.5, color: T.ink3 }}>
            {data.hostname} · {data.ip}
          </span>
        )}
        <div style={{ flex: 1 }}/>
        <div style={{ display: 'flex', gap: 6 }}>
          {FILTERS.map(f => (
            <StatusPill
              key={f}
              label={f}
              count={counts[f]}
              color={f === 'RUNNING' ? '#22c55e' : f === 'STOPPED' ? '#94a3b8' : f === 'FATAL' ? '#ef4444' : T.blueDeep}
              active={filter === f}
              onClick={() => setFilter(f)}
            />
          ))}
        </div>
      </div>

      {/* Content */}
      <div style={{ flex: 1, overflow: 'auto', padding: 16 }}>
        {loading && processes.length === 0 ? (
          <div style={{ textAlign: 'center', padding: 40, color: T.ink3, fontSize: 13 }}>
            Loading...
          </div>
        ) : sortedGroups.length === 0 ? (
          <div style={{ textAlign: 'center', padding: 40, color: T.ink3, fontSize: 13 }}>
            {filter === 'ALL' ? '没有找到 Supervisor 进程' : `没有 ${filter} 状态的进程`}
          </div>
        ) : (
          sortedGroups.map(([groupName, { procs }]) => (
            <div key={groupName} style={{ marginBottom: 20 }}>
              <div style={{
                fontSize: 12, fontWeight: 600, color: T.ink2,
                marginBottom: 8, paddingLeft: 2,
              }}>
                {groupName}
                <span style={{ fontWeight: 400, color: T.ink3, marginLeft: 6 }}>
                  ({procs.length})
                </span>
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                {procs.map(p => (
                  <ServiceCard
                    key={p.name}
                    proc={p}
                    onViewLog={setLogTarget}
                    onControl={handleControl}
                  />
                ))}
              </div>
            </div>
          ))
        )}
      </div>

      {/* Log modal */}
      {logTarget && <LogModal name={logTarget} onClose={() => setLogTarget(null)}/>}
    </div>
  );
}
