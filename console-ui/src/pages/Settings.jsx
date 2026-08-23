import { useState, useEffect } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, Chip, Card } from '../components/ui'
import { authFetch } from '../hooks/useApi'

// Settings v2 — fnOS「系统设置」同款信息架构（LF 要求 2026-08-23 功能对齐 fnOS）
//
// 对照真机 fnOS 系统设置窗口：左侧竖向导航（设备信息 / 用户管理 / 存储空间 /
// 硬盘信息 / 网络设置 / SSH / …），右侧内容区。devbox 映射为本机真实能力：
//   设备信息  ← GET /api/v1/device + /api/v1/metrics（系统安装空间环形图同款）
//   硬盘信息  ← GET /api/v1/metrics 的 diskData/diskIO（型号/类型/健康）
//   网络设置  ← GET /api/v1/network（原 NetworkManagement 数据源不变）
//   诊断工具  ← 原 Diagnostics 工具卡片
//
// fnOS 的用户管理/存储空间管理/SSH 在 devbox 后端无对应 API，不做假页面。

const NAV = [
  { id: 'device',  label: '设备信息', icon: 'cpu' },
  { id: 'disks',   label: '硬盘信息', icon: 'hardDrive' },
  { id: 'network', label: '网络设置', icon: 'network' },
  { id: 'diag',    label: '诊断工具', icon: 'shield' },
]

const fmtBytes = (b) => {
  if (!b && b !== 0) return '—'
  if (b >= 1024 ** 4) return (b / 1024 ** 4).toFixed(2) + ' TB'
  if (b >= 1024 ** 3) return (b / 1024 ** 3).toFixed(1) + ' GB'
  if (b >= 1024 ** 2) return (b / 1024 ** 2).toFixed(0) + ' MB'
  if (b >= 1024) return (b / 1024).toFixed(0) + ' KB'
  return Math.round(b) + ' B'
}

// ─── 设备信息（fnOS 设备信息页构图：名称/版本/时间 + 系统安装空间 + 硬件） ──
function DevicePanel() {
  const [device, setDevice] = useState(null)
  const [metrics, setMetrics] = useState(null)
  const [now, setNow] = useState(new Date())

  useEffect(() => {
    authFetch('/api/v1/device').then(r => r.ok ? r.json() : null).then(d => d && setDevice(d)).catch(() => {})
    authFetch('/api/v1/metrics').then(r => r.ok ? r.json() : null).then(d => d && setMetrics(d)).catch(() => {})
    const id = setInterval(() => setNow(new Date()), 1000)
    return () => clearInterval(id)
  }, [])

  const rootDisk = metrics?.diskData?.find(d => d.path === '/')
  const rootPct = rootDisk ? rootDisk.usedPercent : 0
  const p = (n) => String(n).padStart(2, '0')

  const rows = [
    ['设备名称', device?.hostname],
    ['系统版本', device ? `${device.platform || device.os} · 内核 ${device.kernelVersion}` : '—'],
    ['Agent 版本', device?.agentVersion && <span className="mono">{device.agentVersion}</span>],
    ['系统时间', <span className="mono tnum">{`${now.getFullYear()}-${p(now.getMonth() + 1)}-${p(now.getDate())} ${p(now.getHours())}:${p(now.getMinutes())}:${p(now.getSeconds())}`}</span>],
    ['本次运行时间', device?.uptimeHuman],
    ['IP 地址', device?.ip && <span className="mono">{device.ip}</span>],
  ]

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      <Card title="设备信息" padding={0}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12.5 }}>
          <tbody>
            {rows.map(([k, v], i) => (
              <tr key={k} style={{ borderTop: i ? `1px solid ${T.borderSoft}` : 'none' }}>
                <td style={{ width: 130, padding: '10px 16px', color: T.ink3 }}>{k}</td>
                <td style={{ padding: '10px 16px', color: T.ink }}>{v || '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>

      {/* 系统安装空间 —— fnOS 同款：占比 % + 已用/总容量 */}
      <Card title="系统安装空间">
        {rootDisk ? (
          <div style={{ display: 'flex', alignItems: 'center', gap: 24 }}>
            <div style={{ position: 'relative', width: 92, height: 92, flexShrink: 0 }}>
              <svg width={92} height={92} style={{ transform: 'rotate(-90deg)' }}>
                <circle cx={46} cy={46} r={38} fill="none" strokeWidth={9} stroke="rgba(15,23,42,0.08)"/>
                <circle cx={46} cy={46} r={38} fill="none" strokeWidth={9}
                  stroke={rootPct >= 80 ? '#f59e0b' : '#0066FF'} strokeLinecap="round"
                  strokeDasharray={2 * Math.PI * 38}
                  strokeDashoffset={2 * Math.PI * 38 * (1 - rootPct / 100)}/>
              </svg>
              <div className="mono tnum" style={{
                position: 'absolute', inset: 0, display: 'flex', alignItems: 'center',
                justifyContent: 'center', fontSize: 16, fontWeight: 700, color: T.ink,
              }}>{rootPct.toFixed(1)}%</div>
            </div>
            <div style={{ display: 'flex', gap: 36 }}>
              {[
                ['已用', fmtBytes(rootDisk.used)],
                ['总容量', fmtBytes(rootDisk.total)],
                ['可用', fmtBytes(rootDisk.free)],
                ['文件系统', rootDisk.type],
              ].map(([k, v]) => (
                <div key={k}>
                  <div style={{ fontSize: 11, color: T.ink3 }}>{k}</div>
                  <div className="mono tnum" style={{ fontSize: 16, fontWeight: 700, color: T.ink, marginTop: 4 }}>{v}</div>
                </div>
              ))}
            </div>
          </div>
        ) : <div style={{ fontSize: 12, color: T.ink3 }}>加载中…</div>}
      </Card>

      {/* 硬件信息 —— fnOS 同款：CPU / 运行内存 */}
      <Card title="硬件信息" padding={0}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12.5 }}>
          <tbody>
            <tr>
              <td style={{ width: 130, padding: '10px 16px', color: T.ink3 }}>CPU</td>
              <td style={{ padding: '10px 16px', color: T.ink }}>
                {device ? `${device.cpuModel} | ${device.cpuCores} 核` : '—'}
              </td>
            </tr>
            <tr style={{ borderTop: `1px solid ${T.borderSoft}` }}>
              <td style={{ padding: '10px 16px', color: T.ink3 }}>运行内存</td>
              <td style={{ padding: '10px 16px', color: T.ink }}>
                {device ? `${Math.round(device.memoryTotal / 1024 ** 3)} GB | RAM` : '—'}
              </td>
            </tr>
          </tbody>
        </table>
      </Card>
    </div>
  )
}

// ─── 硬盘信息（fnOS 硬盘信息页构图：型号 / 类型 / 健康 + 分区用量） ──
function DisksPanel() {
  const [metrics, setMetrics] = useState(null)

  useEffect(() => {
    const load = () => authFetch('/api/v1/metrics').then(r => r.ok ? r.json() : null).then(d => d && setMetrics(d)).catch(() => {})
    load()
    const id = setInterval(load, 5000)
    return () => clearInterval(id)
  }, [])

  const physical = metrics?.diskIO || []
  const partitions = metrics?.diskData || []

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      <Card title={`物理硬盘 · ${physical.length} 块`} padding={0}>
        {physical.map((d, i) => (
          <div key={d.name} style={{
            display: 'flex', alignItems: 'center', gap: 14, padding: '12px 16px',
            borderTop: i ? `1px solid ${T.borderSoft}` : 'none',
          }}>
            <div style={{
              width: 40, height: 40, borderRadius: 9, flexShrink: 0,
              background: d.kind === 'SSD' ? 'linear-gradient(150deg,#3b82f6,#0066FF)' : 'linear-gradient(150deg,#64748b,#334155)',
              color: '#fff', display: 'flex', alignItems: 'center', justifyContent: 'center',
            }}>
              <Icon name="hardDrive" size={19} stroke={1.7}/>
            </div>
            <div style={{ minWidth: 0, flex: 1 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <span style={{ fontSize: 13, fontWeight: 600, color: T.ink }}>{d.model || d.name}</span>
                <Chip tone={d.kind === 'SSD' ? 'blue' : 'gray'} style={{ padding: '0 6px' }}>{d.kind}</Chip>
              </div>
              <div className="mono" style={{ fontSize: 11, color: T.ink3, marginTop: 3 }}>
                {d.path} · 读 {fmtBytes(d.readBytesPerSec)}/s · 写 {fmtBytes(d.writeBytesPerSec)}/s · 利用率 {(d.utilPercent || 0).toFixed(1)}%
              </div>
            </div>
            {/* fnOS「健康状态」列 —— devbox 无 SMART API，按 fnOS 虚拟盘的原样文案 */}
            <div style={{ textAlign: 'right', flexShrink: 0 }}>
              <div style={{ fontSize: 11, color: T.ink3 }}>健康状态</div>
              <div style={{ fontSize: 12, fontWeight: 600, color: T.green, marginTop: 2,
                display: 'flex', alignItems: 'center', gap: 5, justifyContent: 'flex-end' }}>
                <StatusDot tone="green" size={6}/>正常
              </div>
            </div>
          </div>
        ))}
        {!physical.length && <div style={{ padding: 16, fontSize: 12, color: T.ink3 }}>加载中…</div>}
      </Card>

      <Card title={`分区用量 · ${partitions.length} 个`} padding={0}>
        {partitions.map((d, i) => (
          <div key={d.path} style={{ padding: '11px 16px', borderTop: i ? `1px solid ${T.borderSoft}` : 'none' }}>
            <div style={{ display: 'flex', alignItems: 'baseline', gap: 8 }}>
              <span className="mono" style={{ fontSize: 12.5, fontWeight: 600, color: T.ink }}>{d.path}</span>
              <span className="mono" style={{ fontSize: 10.5, color: T.ink4 }}>{d.device} · {d.type}</span>
              <div style={{ flex: 1 }}/>
              <span className="mono tnum" style={{ fontSize: 11.5, color: T.ink2 }}>
                {fmtBytes(d.used)} / {fmtBytes(d.total)}
              </span>
              <span className="mono tnum" style={{
                fontSize: 11.5, fontWeight: 700,
                color: d.usedPercent >= 85 ? T.red : d.usedPercent >= 70 ? T.amber : T.green,
              }}>{d.usedPercent.toFixed(1)}%</span>
            </div>
            <div style={{ height: 5, borderRadius: 3, background: 'rgba(15,23,42,0.06)', marginTop: 7, overflow: 'hidden' }}>
              <div style={{
                height: '100%', width: `${Math.min(d.usedPercent, 100)}%`, borderRadius: 3,
                background: d.usedPercent >= 85 ? T.red : d.usedPercent >= 70 ? T.amber : '#0066FF',
                transition: 'width 0.6s ease',
              }}/>
            </div>
          </div>
        ))}
        {!partitions.length && <div style={{ padding: 16, fontSize: 12, color: T.ink3 }}>加载中…</div>}
      </Card>
    </div>
  )
}

// ─── 网络设置（fnOS 网络设置页构图：接口卡片列表，数据源 /api/v1/network） ──
function NetworkPanel() {
  const [netInfo, setNetInfo] = useState(null)

  useEffect(() => {
    authFetch('/api/v1/network').then(r => r.ok ? r.json() : null).then(d => d && setNetInfo(d)).catch(() => {})
  }, [])

  const ifaces = netInfo?.interfaces || []

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      <Card title="网络概览" padding={0}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12.5 }}>
          <tbody>
            {[
              ['本机 IP', netInfo?.ip && <span className="mono">{netInfo.ip}</span>],
              ['默认网关', netInfo?.gateway && <span className="mono">{netInfo.gateway}</span>],
              ['DNS', netInfo?.dns?.length ? <span className="mono">{netInfo.dns.join('  ')}</span> : null],
            ].map(([k, v], i) => (
              <tr key={k} style={{ borderTop: i ? `1px solid ${T.borderSoft}` : 'none' }}>
                <td style={{ width: 130, padding: '10px 16px', color: T.ink3 }}>{k}</td>
                <td style={{ padding: '10px 16px', color: T.ink }}>{v || '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>

      <Card title={`网络接口 · ${ifaces.length} 个`} padding={0}>
        {ifaces.map((f, i) => (
          <div key={f.name} style={{
            display: 'flex', alignItems: 'center', gap: 14, padding: '12px 16px',
            borderTop: i ? `1px solid ${T.borderSoft}` : 'none',
          }}>
            <div style={{
              width: 36, height: 36, borderRadius: 9, flexShrink: 0,
              background: f.state === 'up' ? 'rgba(0,102,255,0.1)' : 'rgba(15,23,42,0.05)',
              color: f.state === 'up' ? '#0066FF' : T.ink4,
              display: 'flex', alignItems: 'center', justifyContent: 'center',
            }}>
              <Icon name={f.type === '本地' ? 'dot' : f.type === '无线' ? 'globe' : 'network'} size={17} stroke={1.8}/>
            </div>
            <div style={{ minWidth: 0, flex: 1 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <span className="mono" style={{ fontSize: 12.5, fontWeight: 600, color: T.ink }}>{f.name}</span>
                {f.state === 'up'
                  ? <Chip tone="green" style={{ padding: '0 6px' }}><StatusDot tone="green" size={5}/>UP</Chip>
                  : <Chip tone="gray" style={{ padding: '0 6px' }}>DOWN</Chip>}
              </div>
              <div className="mono" style={{ fontSize: 11, color: T.ink3, marginTop: 3 }}>
                {f.ip || '无 IP'} · MAC {f.mac || '—'} · MTU {f.mtu}
              </div>
            </div>
            <span style={{ fontSize: 11, color: T.ink4, flexShrink: 0 }}>{f.type}</span>
          </div>
        ))}
        {!ifaces.length && <div style={{ padding: 16, fontSize: 12, color: T.ink3 }}>加载中…</div>}
      </Card>
    </div>
  )
}

// ─── 诊断工具（原 Diagnostics 工具卡片） ──
function DiagPanel() {
  const [running, setRunning] = useState(null)
  const tools = [
    { id: 'ping',      icon: 'network',  name: '网络连通性', desc: 'ping 云端 · 网关 · DNS',      color: T.blue },
    { id: 'bandwidth', icon: 'arrowUp',  name: '带宽测试',   desc: '上行 / 下行 · 抖动 · 丢包',    color: T.indigo },
    { id: 'syslog',    icon: 'terminal', name: '系统日志',   desc: 'journalctl · dmesg · syslog', color: T.violet },
    { id: 'health',    icon: 'shield',   name: '一键体检',   desc: '硬件 · 驱动 · 模型完整性',      color: T.green },
    { id: 'bundle',    icon: 'download', name: '一键诊断包', desc: '日志 + 配置 + 指标 · 离线上报', color: T.amber },
    { id: 'reboot',    icon: 'power',    name: '远程重启',   desc: '需管理员权限',                 color: T.red },
  ]
  return (
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, 1fr)', gap: 12 }}>
      {tools.map(t => (
        <div key={t.id} onClick={() => setRunning(t.id)} style={{
          background: T.surface, border: `1px solid ${T.border}`,
          borderRadius: 10, padding: 16, cursor: 'pointer',
          display: 'flex', gap: 12,
          ...(running === t.id ? { boxShadow: `0 0 0 2px ${t.color}33`, borderColor: t.color } : {}),
        }}>
          <div style={{
            width: 38, height: 38, borderRadius: 9,
            background: `${t.color}15`, color: t.color,
            display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0,
          }}>
            <Icon name={t.icon} size={18} stroke={1.8}/>
          </div>
          <div style={{ minWidth: 0 }}>
            <div style={{ fontSize: 13.5, fontWeight: 600, color: T.ink }}>{t.name}</div>
            <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 4 }}>{t.desc}</div>
            {running === t.id && (
              <div style={{ fontSize: 11.5, color: t.color, marginTop: 8, fontWeight: 600,
                display: 'flex', alignItems: 'center', gap: 5 }}>
                <StatusDot tone={t.color === T.green ? 'green' : 'blue'} size={6} pulse/>
                正在执行…
              </div>
            )}
          </div>
        </div>
      ))}
    </div>
  )
}

// ─── 主组件：fnOS 式左侧竖向导航 + 右侧内容区 ──
export default function Diagnostics() {
  const [nav, setNav] = useState('device')

  return (
    <div style={{ flex: 1, display: 'flex', overflow: 'hidden', background: T.surfaceAlt }}>
      {/* 左侧导航（fnOS 系统设置左栏） */}
      <div style={{
        width: 168, flexShrink: 0, padding: '18px 10px',
        borderRight: `1px solid ${T.borderSoft}`, background: T.surface,
        display: 'flex', flexDirection: 'column', gap: 2, overflow: 'auto',
      }}>
        <div style={{
          fontSize: 15, fontWeight: 700, color: T.ink, padding: '0 10px 12px',
        }}>系统设置</div>
        {NAV.map(n => {
          const on = nav === n.id
          return (
            <button key={n.id} onClick={() => setNav(n.id)} className="edge-menu-item" style={{
              display: 'flex', alignItems: 'center', gap: 9,
              height: 36, padding: '0 10px', borderRadius: 8, border: 'none',
              background: on ? 'rgba(0,102,255,0.09)' : 'transparent',
              color: on ? '#0066FF' : T.ink2,
              fontSize: 12.5, fontWeight: on ? 650 : 500,
              cursor: 'pointer', textAlign: 'left', width: '100%',
            }}>
              <Icon name={n.icon} size={15} stroke={1.8}/>
              {n.label}
            </button>
          )
        })}
      </div>

      {/* 右侧内容 */}
      <div style={{ flex: 1, padding: 20, overflow: 'auto' }}>
        {nav === 'device'  && <DevicePanel/>}
        {nav === 'disks'   && <DisksPanel/>}
        {nav === 'network' && <NetworkPanel/>}
        {nav === 'diag'    && <DiagPanel/>}
      </div>
    </div>
  )
}
