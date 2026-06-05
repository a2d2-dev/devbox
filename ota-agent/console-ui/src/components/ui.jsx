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
export const Sparkline = ({ data, color = T.blue, width = 160, height = 40, fill = true, showAxis = false, max }) => {
  if (!data || data.length === 0) return null;
  const m = max ?? Math.max(...data, 1);
  const stepX = width / (data.length - 1);
  const pts = data.map((v, i) => [i * stepX, height - (v / m) * (height - 4) - 2]);
  const path = pts.map((p, i) => `${i ? 'L' : 'M'}${p[0].toFixed(1)} ${p[1].toFixed(1)}`).join(' ');
  const fillPath = `${path} L ${width} ${height} L 0 ${height} Z`;
  return (
    <svg width={width} height={height} style={{ display: 'block' }}>
      {showAxis && [0.25, 0.5, 0.75].map(f => (
        <line key={f} x1="0" x2={width} y1={height * f} y2={height * f}
              stroke="#f1f5f9" strokeDasharray="2 3"/>
      ))}
      {fill && (
        <path d={fillPath} fill={color} opacity="0.10"/>
      )}
      <path d={path} fill="none" stroke={color} strokeWidth="1.6" strokeLinejoin="round" strokeLinecap="round"/>
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
  const [now, setNow] = useState(new Date(2026, 4, 26, 14, 8, 32));
  useEffect(() => {
    const id = setInterval(() => setNow(n => new Date(n.getTime() + 1000)), 1000);
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
