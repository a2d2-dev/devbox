import { useEffect, useRef, useState } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { authFetch } from '../hooks/useApi'

const POLL_MS = 5000

function useDesktopMetric(kind) {
  const [state, setState] = useState({ data: null, loading: true, error: null })
  useEffect(() => {
    let active = true
    const load = async () => {
      try {
        const response = await authFetch(`/api/v1/desktop/status/${kind}`)
        if (!response.ok) throw new Error(`HTTP ${response.status}`)
        const data = await response.json()
        if (active) setState({ data, loading: false, error: null })
      } catch (error) {
        if (active) setState((current) => ({ ...current, loading: false, error }))
      }
    }
    load()
    const timer = setInterval(load, POLL_MS)
    return () => { active = false; clearInterval(timer) }
  }, [kind])
  return state
}

function formatRate(bytes) {
  if (!Number.isFinite(bytes) || bytes < 0) return '0 B/s'
  if (bytes < 1024) return `${Math.round(bytes)} B/s`
  if (bytes < 1024 ** 2) return `${(bytes / 1024).toFixed(1)} KB/s`
  if (bytes < 1024 ** 3) return `${(bytes / 1024 ** 2).toFixed(1)} MB/s`
  return `${(bytes / 1024 ** 3).toFixed(2)} GB/s`
}

function MetricCard({ icon, label, state, children, action }) {
  return (
    <div style={{ minHeight: 74, padding: '10px 11px', borderRadius: 7, background: 'rgba(248,250,252,0.92)', border: `1px solid ${T.borderSoft}` }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, color: T.ink3, fontSize: 10.5, fontWeight: 600 }}>
        <Icon name={icon} size={12} stroke={1.8}/>{label}
      </div>
      {state.loading && !state.data ? <div style={mutedValue}>加载中...</div> : state.error && !state.data ? <div style={{ ...mutedValue, color: T.red }}>暂不可用</div> : children}
      {state.error && state.data && <div style={{ marginTop: 5, color: T.red, fontSize: 9.5 }}>更新失败，显示上次数据</div>}
      {action}
    </div>
  )
}

export function SystemStatusWidget({ onOpenMonitoring, onOpenStorage }) {
  const cpu = useDesktopMetric('cpu')
  const memory = useDesktopMetric('memory')
  const network = useDesktopMetric('network')
  const storage = useDesktopMetric('storage')
  const uptime = useDesktopMetric('uptime')
  const previousNetwork = useRef(null)
  const [rates, setRates] = useState({ sent: 0, received: 0 })

  useEffect(() => {
    if (!network.data) return
    const now = Date.now()
    const previous = previousNetwork.current
    if (previous && now > previous.at) {
      const seconds = (now - previous.at) / 1000
      setRates({
        sent: Math.max(0, (network.data.sent - previous.sent) / seconds),
        received: Math.max(0, (network.data.received - previous.received) / seconds),
      })
    }
    previousNetwork.current = { ...network.data, at: now }
  }, [network.data])

  return (
    <section aria-label="实时系统状态" onClick={onOpenMonitoring} style={{
      padding: 14, borderRadius: 8, background: 'rgba(255,255,255,0.78)',
      border: '1px solid rgba(255,255,255,0.94)', boxShadow: '0 6px 20px -8px rgba(15,23,42,0.16)',
      backdropFilter: 'blur(12px)', cursor: 'pointer',
    }}>
      <div style={{ display: 'flex', alignItems: 'center', marginBottom: 10 }}>
        <div style={{ fontSize: 13, fontWeight: 700, color: T.ink }}>实时状态</div>
        <div style={{ flex: 1 }}/>
        <span style={{ display: 'flex', alignItems: 'center', gap: 4, fontSize: 10.5, color: T.blue }}>资源详情<Icon name="chevRight" size={12} stroke={2}/></span>
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: 7 }}>
        <MetricCard icon="cpu" label="CPU" state={cpu}><div style={valueStyle}>{Math.round(cpu.data?.percent || 0)}%</div></MetricCard>
        <MetricCard icon="memory" label="内存" state={memory}><div style={valueStyle}>{Math.round(memory.data?.percent || 0)}%</div></MetricCard>
        <MetricCard icon="network" label="网络" state={network}>
          <div style={rateStyle}>UP {formatRate(rates.sent)}</div><div style={rateStyle}>DOWN {formatRate(rates.received)}</div>
        </MetricCard>
        <MetricCard icon="hardDrive" label="存储读写" state={storage}
          action={storage.data?.configured === false ? <button type="button" onClick={(event) => { event.stopPropagation(); onOpenStorage?.() }} style={inlineAction}>配置存储</button> : null}>
          {storage.data?.configured === false ? <div style={{ ...mutedValue, color: T.amber }}>存储空间未创建</div> : <>
            <div style={rateStyle}>R {formatRate(storage.data?.readBytesPerSec || 0)}</div><div style={rateStyle}>W {formatRate(storage.data?.writeBytesPerSec || 0)}</div>
          </>}
        </MetricCard>
        <div style={{ gridColumn: '1 / -1' }}>
          <MetricCard icon="history" label="运行时长" state={uptime}><div style={{ ...valueStyle, fontSize: 15 }}>{uptime.data?.human || `${uptime.data?.seconds || 0} 秒`}</div></MetricCard>
        </div>
      </div>
    </section>
  )
}

const valueStyle = { marginTop: 8, color: T.ink, fontSize: 18, fontWeight: 700, fontVariantNumeric: 'tabular-nums' }
const mutedValue = { marginTop: 9, color: T.ink4, fontSize: 11.5 }
const rateStyle = { marginTop: 5, color: T.ink2, fontSize: 10.5, fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace', fontVariantNumeric: 'tabular-nums' }
const inlineAction = { marginTop: 6, padding: 0, border: 'none', background: 'transparent', color: T.blue, fontSize: 10.5, cursor: 'pointer' }

export default SystemStatusWidget
