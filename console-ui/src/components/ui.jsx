import React, { useState, useEffect, useRef } from 'react';
import { T } from '../tokens';
import { Icon } from '../icons';

export const StatusDot = ({ tone = 'green', size = 8, pulse = false }) => {
  const colors = { green: T.green, amber: T.amber, red: T.red, gray: T.ink4, blue: T.blue };
  return (
    <span style={{
      display: 'inline-block', width: size, height: size, borderRadius: '50%',
      background: colors[tone], flexShrink: 0,
      boxShadow: pulse ? `0 0 0 0 ${colors[tone]}55` : 'none',
    }} className={pulse ? 'edge-live-dot' : ''}/>
  );
};

export const Chip = ({ tone = 'gray', children, style }) => {
  const map = {
    green: { bg: '#ecfdf5', fg: '#047857', bd: '#a7f3d0' },
    amber: { bg: '#fffbeb', fg: '#b45309', bd: '#fde68a' },
    red:   { bg: '#fef2f2', fg: '#b91c1c', bd: '#fecaca' },
    blue:  { bg: '#eff6ff', fg: '#1d4ed8', bd: '#bfdbfe' },
    gray:  { bg: '#f1f5f9', fg: '#475569', bd: '#e2e8f0' },
    violet:{ bg: '#f5f3ff', fg: '#6d28d9', bd: '#ddd6fe' },
  };
  const c = map[tone] || map.gray;
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: 4,
      padding: '2px 7px', borderRadius: 4,
      background: c.bg, color: c.fg, border: `1px solid ${c.bd}`,
      fontSize: 11, fontWeight: 600, letterSpacing: 0.2,
      ...style,
    }}>{children}</span>
  );
};

// Ring progress (CSS conic)
export const Ring = ({ value = 0, size = 96, thickness = 10, color = T.blue, track = '#e2e8f0', label, sublabel }) => {
  const pct = Math.max(0, Math.min(100, value));
  return (
    <div style={{ position: 'relative', width: size, height: size }}>
      <div style={{
        width: size, height: size, borderRadius: '50%',
        background: `conic-gradient(${color} ${pct * 3.6}deg, ${track} 0)`,
      }}/>
      <div style={{
        position: 'absolute', inset: thickness, borderRadius: '50%', background: 'white',
        display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center',
      }}>
        <div className="mono tnum" style={{ fontSize: size * 0.24, fontWeight: 700, color: T.ink, lineHeight: 1 }}>
          {Math.round(pct)}<span style={{ fontSize: size * 0.13, color: T.ink3, fontWeight: 600 }}>%</span>
        </div>
        {sublabel && <div style={{ fontSize: 10.5, color: T.ink3, marginTop: 3 }}>{sublabel}</div>}
      </div>
    </div>
  );
};

// Tiny SVG sparkline
// monotonePath — Catmull-Rom → cubic Bezier 平滑（参考 recharts <Area type="monotone">）
// pts: [[x, y], ...]，返回 SVG path "M ... C ... C ..."
function monotonePath(pts) {
  if (pts.length < 2) return '';
  if (pts.length === 2) return `M${pts[0][0]} ${pts[0][1]} L${pts[1][0]} ${pts[1][1]}`;
  const out = [`M${pts[0][0]} ${pts[0][1]}`];
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[i - 1] || pts[i];
    const p1 = pts[i];
    const p2 = pts[i + 1];
    const p3 = pts[i + 2] || p2;
    // tension 0.5
    const cp1x = p1[0] + (p2[0] - p0[0]) / 6;
    const cp1y = p1[1] + (p2[1] - p0[1]) / 6;
    const cp2x = p2[0] - (p3[0] - p1[0]) / 6;
    const cp2y = p2[1] - (p3[1] - p1[1]) / 6;
    out.push(`C${cp1x.toFixed(1)} ${cp1y.toFixed(1)}, ${cp2x.toFixed(1)} ${cp2y.toFixed(1)}, ${p2[0]} ${p2[1]}`);
  }
  return out.join(' ');
}

// Sparkline — 复刻云端 recharts <Area type="monotone">：
//  - Y 轴自适应 [0, dataMax*1.1] (不写死 100，让小波动可见)
//  - cubic Bezier 平滑曲线
//  - fillOpacity 0.2 area fill
//  - 横向虚线网格 (CartesianGrid horizontal-only)
//  - 左侧 Y 轴 4 个 tick label (showAxis 时)
// max prop 仍保留作上限 cap（如果需要锁 0-100 可显式传 100）。
export const Sparkline = ({ data, color = T.blue, width = 520, height = 84, fill = true, showAxis = false, max, unit = '%' }) => {
  if (!data || data.length === 0) return null;
  const lo = 0;
  const dataMax = Math.max(...data, 1);
  const hi = max != null ? max : Math.ceil(dataMax * 1.1);
  const padL = showAxis ? 32 : 0; // Y 轴 tick 留宽
  const padR = 4, padT = 4, padB = 4;
  const innerW = width - padL - padR;
  const innerH = height - padT - padB;
  const stepX = innerW / (data.length - 1);
  const pts = data.map((v, i) => {
    const x = padL + i * stepX;
    const y = padT + innerH - ((v - lo) / (hi - lo)) * innerH;
    return [x, y];
  });
  const path = monotonePath(pts);
  const fillPath = `${path} L ${padL + innerW} ${padT + innerH} L ${padL} ${padT + innerH} Z`;
  // Y 轴 4 个 tick (含 0 与 hi)
  const ticks = [0, hi * 0.33, hi * 0.66, hi].map(v => ({
    v,
    y: padT + innerH - ((v - lo) / (hi - lo)) * innerH,
  }));
  return (
    <svg viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none"
         style={{ display: 'block', width: '100%', height }}>
      {showAxis && ticks.map((t, i) => (
        <line key={i} x1={padL} x2={padL + innerW} y1={t.y} y2={t.y}
              stroke="#e5e7eb" strokeDasharray="3 3" vectorEffect="non-scaling-stroke"/>
      ))}
      {showAxis && ticks.map((t, i) => (
        <text key={`l-${i}`} x={padL - 4} y={t.y + 3}
              textAnchor="end" fontSize="9" fill="#9ca3af">{Math.round(t.v)}{unit}</text>
      ))}
      {fill && <path d={fillPath} fill={color} opacity="0.20"/>}
      <path d={path} fill="none" stroke={color} strokeWidth="1.6" strokeLinejoin="round" strokeLinecap="round" vectorEffect="non-scaling-stroke"/>
    </svg>
  );
};

// Card
export const Card = ({ children, style, padding = 16, title, action }) => (
  <div style={{
    background: T.surface, border: `1px solid ${T.border}`, borderRadius: 10,
    boxShadow: '0 1px 2px rgba(15,23,42,0.04)', ...style,
  }}>
    {title && (
      <div style={{ display: 'flex', alignItems: 'center', padding: '12px 16px',
        borderBottom: `1px solid ${T.borderSoft}` }}>
        <div style={{ fontSize: 13, fontWeight: 600, color: T.ink }}>{title}</div>
        <div style={{ flex: 1 }}/>
        {action}
      </div>
    )}
    <div style={{ padding }}>{children}</div>
  </div>
);

// ─── Hooks ──────────────────────────────────────────────────────
export function useClock() {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 1000);
    return () => clearInterval(id);
  }, []);
  return now;
}

export function useTicker(initial, makeNext, intervalMs = 2500) {
  const [v, setV] = useState(initial);
  useEffect(() => {
    const id = setInterval(() => setV(prev => makeNext(prev)), intervalMs);
    return () => clearInterval(id);
  }, []);
  return v;
}
