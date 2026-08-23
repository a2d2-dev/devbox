import { useEffect, useRef, useState } from 'react';
import { T } from '../tokens';
import { authFetch } from '../hooks/useApi';

// SystemStatusWidget — fnOS 桌面右侧实时状态挂件同款
//
// 对照真机 fnOS 桌面（agent-browser 提取）：右侧悬浮小挂件，行序为
//   R <磁盘读速率> / W <磁盘写速率> / ↑<上行> / ↓<下行> / CPU x% / RAM x%
// CPU/RAM 用环形进度，磁盘/网络用紧凑速率行。
//
// 数据源：GET /api/v1/metrics（2s 轮询）
//   - 磁盘速率：diskIO[].readBytesPerSec/writeBytesPerSec 求和（后端已算好速率）
//   - 网络速率：netBytesSent/netBytesRecv 是累计值 → 前端相邻两次采样差分
//   - CPU：cpuUsedPercent；RAM：memoryUsedPercent

const POLL_MS = 2000;

function fmtRate(bps) {
  if (!isFinite(bps) || bps < 0) bps = 0;
  if (bps < 1024) return { v: Math.round(bps), u: 'B/s' };
  if (bps < 1024 * 1024) return { v: (bps / 1024).toFixed(bps < 10240 ? 1 : 0), u: 'KB/s' };
  if (bps < 1024 * 1024 * 1024) return { v: (bps / 1024 / 1024).toFixed(1), u: 'MB/s' };
  return { v: (bps / 1024 / 1024 / 1024).toFixed(2), u: 'GB/s' };
}

function useSystemStats() {
  const [stats, setStats] = useState({ diskR: 0, diskW: 0, up: 0, down: 0, cpu: 0, ram: 0 });
  const prevNet = useRef(null); // { sent, recv, t }

  useEffect(() => {
    let alive = true;
    async function tick() {
      try {
        const r = await authFetch('/api/v1/metrics');
        if (!r.ok || !alive) return;
        const m = await r.json();
        const now = Date.now();

        let diskR = 0, diskW = 0;
        (m.diskIO || []).forEach(d => {
          diskR += d.readBytesPerSec || 0;
          diskW += d.writeBytesPerSec || 0;
        });

        let up = 0, down = 0;
        const p = prevNet.current;
        if (p && now > p.t) {
          const dt = (now - p.t) / 1000;
          up = Math.max(0, (m.netBytesSent - p.sent) / dt);
          down = Math.max(0, (m.netBytesRecv - p.recv) / dt);
        }
        prevNet.current = { sent: m.netBytesSent, recv: m.netBytesRecv, t: now };

        setStats({
          diskR, diskW, up, down,
          cpu: Math.round(m.cpuUsedPercent || 0),
          ram: Math.round(m.memoryUsedPercent || 0),
        });
      } catch { /* 网络抖动静默，下轮重试 */ }
    }
    tick();
    const id = setInterval(tick, POLL_MS);
    return () => { alive = false; clearInterval(id); };
  }, []);

  return stats;
}

// ─── 迷你环形进度（fnOS CPU/RAM 圆环） ─────────────────────────
function MiniRing({ pct, label, dark }) {
  const r = 17, c = 2 * Math.PI * r;
  const warn = pct >= 80;
  const color = warn ? '#f59e0b' : '#0066FF';
  return (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 3 }}>
      <div style={{ position: 'relative', width: 44, height: 44 }}>
        <svg width={44} height={44} style={{ transform: 'rotate(-90deg)' }}>
          <circle cx={22} cy={22} r={r} fill="none" strokeWidth={4}
            stroke={dark ? 'rgba(255,255,255,0.14)' : 'rgba(15,23,42,0.08)'}/>
          <circle cx={22} cy={22} r={r} fill="none" strokeWidth={4}
            stroke={color} strokeLinecap="round"
            strokeDasharray={c} strokeDashoffset={c * (1 - Math.min(pct, 100) / 100)}
            style={{ transition: 'stroke-dashoffset 0.6s ease, stroke 0.3s ease' }}/>
        </svg>
        <div className="mono tnum" style={{
          position: 'absolute', inset: 0,
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          fontSize: 10.5, fontWeight: 700,
          color: dark ? '#fff' : T.ink,
        }}>{pct}%</div>
      </div>
      <div style={{
        fontSize: 9.5, fontWeight: 600, letterSpacing: '0.05em',
        color: dark ? 'rgba(255,255,255,0.6)' : T.ink3,
      }}>{label}</div>
    </div>
  );
}

// ─── 速率行（R/W/↑/↓） ────────────────────────────────────────
function RateRow({ tag, tagColor, rate, dark }) {
  const { v, u } = fmtRate(rate);
  return (
    <div style={{ display: 'flex', alignItems: 'baseline', gap: 6 }}>
      <span className="mono" style={{
        width: 14, fontSize: 10.5, fontWeight: 700, color: tagColor, flexShrink: 0,
      }}>{tag}</span>
      <span className="mono tnum" style={{
        flex: 1, textAlign: 'right', fontSize: 11.5, fontWeight: 600,
        color: dark ? 'rgba(255,255,255,0.92)' : T.ink,
      }}>{v}</span>
      <span style={{
        width: 32, fontSize: 9.5, flexShrink: 0,
        color: dark ? 'rgba(255,255,255,0.45)' : T.ink4,
      }}>{u}</span>
    </div>
  );
}

export function SystemStatusWidget({ dark = false }) {
  const s = useSystemStats();
  return (
    <div className={dark ? 'edge-material-dark' : ''} style={{
      padding: '14px 16px',
      background: dark ? undefined : 'rgba(255,255,255,0.7)',
      backdropFilter: dark ? undefined : 'blur(12px)',
      WebkitBackdropFilter: dark ? undefined : 'blur(12px)',
      border: dark ? '1px solid rgba(255,255,255,0.12)' : '1px solid rgba(255,255,255,0.9)',
      borderRadius: 14,
      boxShadow: dark ? '0 8px 24px -8px rgba(0,0,0,0.4)' : '0 6px 20px -6px rgba(15,23,42,0.10)',
    }}>
      {/* CPU / RAM 环形 */}
      <div style={{ display: 'flex', justifyContent: 'space-around', marginBottom: 12 }}>
        <MiniRing pct={s.cpu} label="CPU" dark={dark}/>
        <MiniRing pct={s.ram} label="RAM" dark={dark}/>
      </div>

      {/* 磁盘 R/W + 网络 ↑/↓ */}
      <div style={{
        display: 'flex', flexDirection: 'column', gap: 5,
        paddingTop: 10,
        borderTop: `1px solid ${dark ? 'rgba(255,255,255,0.12)' : T.borderSoft}`,
      }}>
        <RateRow tag="R" tagColor="#34d399" rate={s.diskR} dark={dark}/>
        <RateRow tag="W" tagColor="#f87171" rate={s.diskW} dark={dark}/>
        <RateRow tag="↑" tagColor="#60a5fa" rate={s.up}   dark={dark}/>
        <RateRow tag="↓" tagColor="#a78bfa" rate={s.down} dark={dark}/>
      </div>
    </div>
  );
}

export default SystemStatusWidget;
