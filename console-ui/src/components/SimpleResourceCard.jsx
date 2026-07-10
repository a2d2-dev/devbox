import { T } from '../tokens'
import { Icon } from '../icons'

// Story 9.2-B: Dashboard 总览的简洁资源卡。
// 仿 edge-console「实时资源用量」grid 风格：图标 + 标签 + 大数值 + 容量小字。
// 不加 sparkline（保持总览极简，sparkline 是监控 App 范围）。
// 与 Monitoring.jsx 内的 TopResourceBar.Cell 视觉接近但不复用 —— 两 App 形态独立演化。

export function SimpleResourceCard({ icon, label, value, sub, color }) {
  const loading = value == null || value === '';
  return (
    <div style={{
      background: T.surface, border: `1px solid ${T.border}`, borderRadius: 10,
      padding: '16px 18px',
    }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 10 }}>
        <Icon name={icon} size={14} stroke={1.8} style={{ color: loading ? T.ink4 : color }}/>
        <div style={{ fontSize: 12.5, fontWeight: 600, color: T.ink, letterSpacing: '0.02em' }}>{label}</div>
      </div>
      <div className="mono tnum" style={{
        fontSize: 30, fontWeight: 700, color: loading ? T.ink4 : color, lineHeight: 1,
        letterSpacing: '-0.02em',
      }}>{loading ? '—' : value}</div>
      <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 8 }}>{sub || '—'}</div>
    </div>
  );
}

export default SimpleResourceCard;
