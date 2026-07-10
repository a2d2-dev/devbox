// TrendChart — 通用时间序列趋势图组件。
//
// 设计目标：
//   - 一个组件搞定所有 Monitoring 趋势卡的曲线展示
//   - 自包含：header (title + 平均/峰值) + chart + X 时间轴 一气呵成，
//     调用方不用拼 TrendCard + Sparkline + format helper 三块
//   - 不拉伸文字：SVG 用真实 px 宽（ResizeObserver 测容器），无 viewBox
//     拉伸（这是 Sparkline 之前的 root cause bug）
//   - 单位 formatter 内置（percent/bytesPerSec/celsius/watts），调用方只
//     传 unit 字符串不用自己写 format 逻辑
//   - 平均 / 峰值 自动算（调用方传 series 即可）
//
// 这是新写的组件，不动 Sparkline（drawer MetricPanel / GpuRow MiniSpark
// 仍用 Sparkline，不在本 commit 范围）。

import { useState, useEffect, useRef } from 'react'
import { T } from '../tokens'

// 内置单位 formatter —— Y 轴 tick / 平均 / 峰值统一走它，单位一致不漏掉
const FORMATTERS = {
  percent: (v) => `${Math.round(v)}%`,
  bytesPerSec: (v) => {
    if (v == null || !isFinite(v)) return '—'
    if (v >= 1024 * 1024) return (v / (1024 * 1024)).toFixed(1) + ' MB/s'
    if (v >= 1024) return (v / 1024).toFixed(1) + ' KB/s'
    return Math.round(v) + ' B/s'
  },
  celsius: (v) => `${Math.round(v)}°C`,
  watts: (v) => `${Math.round(v)} W`,
}

// padL 按单位调宽 —— bytesPerSec / celsius 这种长字符 tick 需要更多空间
// 否则 "1.0 MB/s" 会被截成 ".0 MB/s"
const PAD_LEFT = {
  percent: 32,
  celsius: 36,
  watts: 36,
  bytesPerSec: 52,
}

// X 轴 4 段时间标签：1h/6h/24h 时间窗 → ["-1h", "-40m", "-20m", "现在"]
function xAxisLabels(window) {
  const m = { '1h': 60, '6h': 360, '24h': 1440 }[window] || 60
  const fmt = (mins) => {
    if (mins === 0) return '现在'
    if (mins < 60) return `-${mins}m`
    return `-${Math.round(mins / 60)}h`
  }
  return [fmt(m), fmt(Math.round(m * 2 / 3)), fmt(Math.round(m / 3)), '现在']
}

// monotonePath — Catmull-Rom → cubic Bezier 平滑（同 Sparkline 风格）
function monotonePath(pts) {
  if (pts.length < 2) return ''
  if (pts.length === 2) return `M${pts[0][0]} ${pts[0][1]} L${pts[1][0]} ${pts[1][1]}`
  const out = [`M${pts[0][0]} ${pts[0][1]}`]
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[i - 1] || pts[i]
    const p1 = pts[i]
    const p2 = pts[i + 1]
    const p3 = pts[i + 2] || p2
    const cp1x = p1[0] + (p2[0] - p0[0]) / 6
    const cp1y = p1[1] + (p2[1] - p0[1]) / 6
    const cp2x = p2[0] - (p3[0] - p1[0]) / 6
    const cp2y = p2[1] - (p3[1] - p1[1]) / 6
    out.push(`C${cp1x.toFixed(1)} ${cp1y.toFixed(1)}, ${cp2x.toFixed(1)} ${cp2y.toFixed(1)}, ${p2[0]} ${p2[1]}`)
  }
  return out.join(' ')
}

/**
 * 通用时间序列图。
 *
 * @param {string}   title  卡片标题（"CPU 使用率 · 最近 1 小时"）
 * @param {number[]} series 数据序列
 * @param {string}   color  线条 / fill 色（fill 自动 20% opacity）
 * @param {string}   unit   'percent' | 'bytesPerSec' | 'celsius' | 'watts'
 * @param {string}   window '1h' | '6h' | '24h'，决定 X 轴 4 段时间 label
 * @param {number=}  max    可选，锁定 Y 上限（如 percent 锁 100）；不传则用 dataMax × 1.1
 */
export function TrendChart({ title, series = [], color = T.blue, unit = 'percent', window = '1h', max }) {
  const containerRef = useRef(null)
  const [chartW, setChartW] = useState(520)

  useEffect(() => {
    if (!containerRef.current) return
    const el = containerRef.current
    const update = () => setChartW(Math.max(60, Math.floor(el.clientWidth)))
    update()
    const ro = new ResizeObserver(update)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const fmt = FORMATTERS[unit] || ((v) => String(Math.round(v)))
  const hasData = series.length > 0
  const avg = hasData ? series.reduce((a, b) => a + b, 0) / series.length : 0
  const peak = hasData ? Math.max(...series) : 0

  const chartH = 84
  const padL = PAD_LEFT[unit] || 32
  const padR = 4, padT = 4, padB = 4
  const innerW = Math.max(1, chartW - padL - padR)
  const innerH = chartH - padT - padB

  const dataMax = hasData ? Math.max(...series, 1) : 1
  const hi = max != null ? max : Math.ceil(dataMax * 1.1) || 1

  const stepX = series.length > 1 ? innerW / (series.length - 1) : 0
  const pts = series.map((v, i) => [
    padL + i * stepX,
    padT + innerH - (v / hi) * innerH,
  ])
  const path = pts.length > 0 ? monotonePath(pts) : ''
  const fillPath = pts.length > 0
    ? `${path} L ${padL + innerW} ${padT + innerH} L ${padL} ${padT + innerH} Z`
    : ''
  const ticks = [0, hi * 0.33, hi * 0.66, hi].map(v => ({
    v,
    y: padT + innerH - (v / hi) * innerH,
  }))

  const labels = xAxisLabels(window)

  return (
    <div style={{
      background: T.surface, border: `1px solid ${T.border}`, borderRadius: 10,
      padding: 16,
    }}>
      {/* Header: title + 平均/峰值 */}
      <div style={{ display: 'flex', alignItems: 'center', marginBottom: 10 }}>
        <div style={{ fontSize: 13, fontWeight: 600, color: T.ink }}>{title}</div>
        <div style={{ flex: 1 }}/>
        <div style={{ display: 'flex', gap: 14, fontSize: 11.5, color: T.ink3 }}>
          <span>平均 <span className="mono tnum" style={{ color: T.ink, fontWeight: 600 }}>
            {hasData ? fmt(avg) : '—'}
          </span></span>
          <span>峰值 <span className="mono tnum" style={{ color: T.ink, fontWeight: 600 }}>
            {hasData ? fmt(peak) : '—'}
          </span></span>
        </div>
      </div>

      {/* Chart：ResizeObserver host div 撑满，svg 用真实 chartW px 不拉伸 */}
      <div ref={containerRef} style={{ width: '100%', height: chartH }}>
        {hasData ? (
          <svg width={chartW} height={chartH} style={{ display: 'block' }}>
            {/* Y 轴 4 个 tick：网格虚线 + 文字 label（用 fmt 走单位 formatter） */}
            {ticks.map((t, i) => (
              <line key={`g-${i}`} x1={padL} x2={padL + innerW} y1={t.y} y2={t.y}
                    stroke="#e5e7eb" strokeDasharray="3 3"/>
            ))}
            {ticks.map((t, i) => (
              <text key={`l-${i}`} x={padL - 4} y={t.y + 3}
                    textAnchor="end" fontSize="9" fill="#9ca3af">{fmt(t.v)}</text>
            ))}
            {/* 半透明 area fill + 描边 */}
            <path d={fillPath} fill={color} opacity="0.20"/>
            <path d={path} fill="none" stroke={color} strokeWidth="1.6"
                  strokeLinejoin="round" strokeLinecap="round"/>
          </svg>
        ) : (
          <div style={{
            height: chartH, display: 'flex', alignItems: 'center', justifyContent: 'center',
            fontSize: 11.5, color: T.ink4,
          }}>暂无数据</div>
        )}
      </div>

      {/* X 轴 4 段时间 label */}
      <div style={{
        display: 'flex', justifyContent: 'space-between',
        fontSize: 10.5, color: T.ink4, marginTop: 4, paddingLeft: padL,
      }} className="mono">
        {labels.map((l, i) => <span key={i}>{l}</span>)}
      </div>
    </div>
  )
}

// makeRateSeries 把累计 bytes 时序差分成速率序列 (B/s)。
// 给 TrendChart unit='bytesPerSec' 喂数据用；网络这种 counter 类型必须差分。
// 用相邻 timestamp 差作 dt；没 timestamps 退化为 5s 步长（agent 采样间隔）。
// agent 重启 / counter reset 导致 diff 为负 → clamp 0 防图表向下尖刺。
// 输出长度 = 输入长度 - 1。
export function makeRateSeries(byteSeries, timestamps) {
  if (!byteSeries || byteSeries.length < 2) return []
  const out = []
  for (let i = 1; i < byteSeries.length; i++) {
    const diff = byteSeries[i] - byteSeries[i - 1]
    let dt = 5
    if (timestamps && timestamps[i] && timestamps[i - 1]) {
      const ms = new Date(timestamps[i]).getTime() - new Date(timestamps[i - 1]).getTime()
      if (ms > 0) dt = ms / 1000
    }
    out.push(Math.max(0, diff / dt))
  }
  return out
}
