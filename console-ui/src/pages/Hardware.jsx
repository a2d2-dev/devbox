import { useState } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { useHardware, useSensors } from '../hooks/useApi'

// ─── Sensor UI helpers ────────────────────────────────────────

function tempColor(t, max, crit) {
  if (!t) return T.ink3
  if (crit && t >= crit - 5) return '#dc2626'
  if (max && t >= max - 5) return '#b45309'
  if (t >= 70) return '#f59e0b'
  return '#059669'
}

function TempBar({ tempC, maxC, critC, w = 220 }) {
  const cap = critC || maxC || 100
  const pct = Math.max(0, Math.min(100, (tempC / cap) * 100))
  const color = tempColor(tempC, maxC, critC)
  const maxPct = maxC ? (maxC / cap) * 100 : null
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
      <div style={{
        width: w, height: 8, borderRadius: 4,
        background: '#e2e8f0', position: 'relative', overflow: 'hidden',
      }}>
        <div style={{
          position: 'absolute', left: 0, top: 0, bottom: 0,
          width: `${pct}%`, background: color,
          transition: 'width 400ms ease',
        }} />
        {maxPct != null && maxPct < 100 && (
          <div style={{
            position: 'absolute', top: 0, bottom: 0,
            left: `${maxPct}%`, width: 2, background: '#94a3b8',
          }} />
        )}
      </div>
      <div style={{
        minWidth: 60, fontSize: 12, fontWeight: 700,
        color, fontFamily: '"JetBrains Mono", ui-monospace, monospace',
      }}>{tempC?.toFixed(0)} °C</div>
    </div>
  )
}

function LiveMetric({ label, value, unit, color = T.ink }) {
  return (
    <div>
      <div style={{ fontSize: 10.5, color: T.ink3, marginBottom: 2 }}>{label}</div>
      <div style={{
        fontSize: 22, fontWeight: 700, color,
        fontFamily: '"JetBrains Mono", ui-monospace, monospace',
        lineHeight: 1.1,
      }}>
        {value}
        {unit && <span style={{ fontSize: 11, marginLeft: 4, color: T.ink3, fontWeight: 500 }}>{unit}</span>}
      </div>
    </div>
  )
}

// ─── Formatters / shared bits ────────────────────────────────

const MONO = { fontFamily: '"JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, monospace' }

function fmtBytes(n) {
  if (!n) return '—'
  if (n >= 1024 ** 4) return (n / 1024 ** 4).toFixed(2) + ' TiB'
  if (n >= 1024 ** 3) return (n / 1024 ** 3).toFixed(2) + ' GiB'
  if (n >= 1024 ** 2) return (n / 1024 ** 2).toFixed(2) + ' MiB'
  if (n >= 1024) return (n / 1024).toFixed(2) + ' KiB'
  return n + ' B'
}
function fmtUptime(sec) {
  if (!sec) return '—'
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const bits = []
  if (d) bits.push(`${d} 天`)
  if (h) bits.push(`${h} 小时`)
  bits.push(`${m} 分`)
  return bits.join(' ')
}
function humanGen(g) {
  if (!g) return ''
  const map = { '2.5GT/s': 'Gen1', '5GT/s': 'Gen2', '8GT/s': 'Gen3', '16GT/s': 'Gen4', '32GT/s': 'Gen5', '64GT/s': 'Gen6' }
  return map[g] || g
}
function classifyPCIe(dev) {
  const c = dev.classCode
  if (c === '0300' || c === '0302' || c === '0301' || c === '0380') return { kind: 'gpu', label: '显卡', color: '#7c3aed' }
  if (c === '0106' || c === '0101' || c === '0108') return { kind: 'storage', label: '存储', color: '#0891b2' }
  if (c === '0200' || c === '0280') return { kind: 'net', label: '网络', color: '#0e7490' }
  if (c === '0403' || c === '0401') return { kind: 'audio', label: '音频', color: '#0d9488' }
  if (c === '0c03' || c === '0c05') return { kind: 'usb', label: 'USB', color: '#64748b' }
  if (c === '0604' || c === '0600') return { kind: 'bridge', label: '桥/根', color: '#94a3b8' }
  return { kind: 'other', label: dev.class || '其它', color: '#94a3b8' }
}
function copyText(t) {
  try { navigator.clipboard.writeText(t) } catch {}
}

// LinkStatus 视觉映射：
//   ok       → 无标记
//   empty    → 空槽 (灰淡)
//   idle     → ASPM 空闲省电 (灰⚡黄底提示)
//   downgrade→ 真降级 (红⚠橙底)
const LINK_UI = {
  ok:        { label: '',      color: T.ink,     bg: 'transparent', marker: ''  },
  empty:     { label: '空槽',  color: '#94a3b8', bg: 'transparent', marker: '·' },
  idle:      { label: '省电',  color: '#94a3b8', bg: '#f8fafc',     marker: '⚡' },
  downgrade: { label: '降级',  color: '#b45309', bg: '#fef3c7',     marker: '⚠' },
}
function linkUI(status) { return LINK_UI[status] || LINK_UI.ok }

// ─── Page root ───────────────────────────────────────────────

export default function Hardware() {
  const { data, loading } = useHardware()

  if (loading && !data) {
    return <div style={{ padding: 40, color: T.ink3 }}>正在采集硬件清单…</div>
  }
  if (!data) {
    return <div style={{ padding: 40, color: T.ink3 }}>硬件采集失败。</div>
  }

  return (
    // flex:1 + width:100% 确保占满 AppWindow 内容区，避免被父 flex 容器缩到内容宽度
    <div style={{
      flex: 1, width: '100%',
      display: 'flex', flexDirection: 'column', height: '100%',
      background: T.canvas || '#f8fafc',
    }}>
      <Toolbar data={data} />
      <div style={{ flex: 1, overflow: 'hidden' }}>
        <ReportView data={data} />
      </div>
    </div>
  )
}

function Toolbar({ data }) {
  return (
    <div style={{
      display: 'flex', alignItems: 'center', gap: 10,
      padding: '10px 20px', borderBottom: `1px solid ${T.border}`,
      background: 'white', flexShrink: 0,
    }}>
      <div style={{ fontSize: 15, fontWeight: 700, color: T.ink }}>硬件中心</div>
      <div style={{ color: T.ink3, fontSize: 12 }}>
        · {data.os?.hostname} · 采集于 {new Date(data.collectedAt).toLocaleTimeString()}
      </div>
    </div>
  )
}

// ─── Report view: left nav + right detail table ──────────────

function ReportView({ data }) {
  const sections = [
    { id: 'overview', name: '概览',      icon: 'dashboard' },
    { id: 'sensors',  name: '传感器',    icon: 'bell' },
    { id: 'gpu',      name: '显卡',      icon: 'sparkle' },
    { id: 'cpu',      name: 'CPU',      icon: 'cpu' },
    { id: 'memory',   name: '内存',      icon: 'cpu' },
    { id: 'storage',  name: '存储',      icon: 'hardDrive' },
    { id: 'network',  name: '网络',      icon: 'network' },
    { id: 'pcie',     name: 'PCIe',     icon: 'cpu' },
    { id: 'system',   name: 'BIOS/主板', icon: 'gear' },
  ]
  const [cur, setCur] = useState('overview')

  return (
    <div style={{ display: 'flex', height: '100%', width: '100%' }}>
      <aside style={{
        width: 180, flexShrink: 0,
        borderRight: `1px solid ${T.border}`, background: 'white',
        overflow: 'auto', padding: '10px 0',
      }}>
        {sections.map(s => (
          <button key={s.id} onClick={() => setCur(s.id)} style={{
            display: 'flex', alignItems: 'center', gap: 8,
            width: '100%', padding: '9px 16px', border: 'none',
            background: cur === s.id ? T.blueSoft : 'transparent',
            color: cur === s.id ? T.blueDeep : T.ink2,
            fontSize: 13, fontWeight: cur === s.id ? 600 : 500,
            cursor: 'pointer', textAlign: 'left',
            borderLeft: `3px solid ${cur === s.id ? T.blueDeep : 'transparent'}`,
          }}>
            <Icon name={s.icon} size={15} />
            {s.name}
          </button>
        ))}
      </aside>
      <main style={{ flex: 1, minWidth: 0, overflow: 'auto', padding: 20 }}>
        {cur === 'overview' && <Overview data={data} />}
        {cur === 'sensors'  && <SensorsPane data={data} />}
        {cur === 'gpu'      && <GPUPane data={data} />}
        {cur === 'cpu'      && <CPUPane data={data} />}
        {cur === 'memory'   && <MemoryPane data={data} />}
        {cur === 'storage'  && <StoragePane data={data} />}
        {cur === 'network'  && <NetworkPane data={data} />}
        {cur === 'pcie'     && <PCIePane data={data} />}
        {cur === 'system'   && <SystemPane data={data} />}
      </main>
    </div>
  )
}

function Section({ title, cmd, children, action }) {
  return (
    <div style={{
      background: 'white', border: `1px solid ${T.border}`, borderRadius: 10,
      marginBottom: 16,
    }}>
      <div style={{
        display: 'flex', alignItems: 'center', gap: 10,
        padding: '10px 14px', borderBottom: `1px solid ${T.borderSoft || '#f1f5f9'}`,
      }}>
        <div style={{ fontSize: 13.5, fontWeight: 700, color: T.ink }}>{title}</div>
        {cmd && (
          <button onClick={() => copyText(cmd)} style={{
            padding: '2px 8px', fontSize: 10.5, borderRadius: 4,
            background: '#f1f5f9', color: T.ink3, border: `1px solid ${T.border}`,
            cursor: 'pointer', ...MONO,
          }}>复制 {cmd}</button>
        )}
        <div style={{ flex: 1 }} />
        {action}
      </div>
      <div style={{ padding: 14 }}>{children}</div>
    </div>
  )
}
function KV({ k, v, warn, mono = true }) {
  return (
    <div style={{
      display: 'flex', gap: 16, padding: '5px 0',
      borderBottom: `1px dashed ${T.borderSoft || '#f1f5f9'}`,
      alignItems: 'baseline',
    }}>
      <div style={{ width: 150, color: T.ink3, fontSize: 12.5, flexShrink: 0 }}>{k}</div>
      <div style={{
        flex: 1, fontSize: 12.5,
        color: warn ? '#b45309' : T.ink,
        fontWeight: warn ? 600 : 400,
        wordBreak: 'break-word',
        ...(mono ? MONO : {}),
      }}>
        {warn && <span style={{ marginRight: 6 }}>⚠</span>}
        {v !== undefined && v !== null && v !== '' ? v : <span style={{ color: T.ink3 }}>—</span>}
      </div>
    </div>
  )
}

// ─── Individual panes ────────────────────────────────────────

function OverviewLiveStrip() {
  const { data: s } = useSensors()
  if (!s) return null
  const cpu = s.cpu || {}
  const gpu = (s.gpus || [])[0]
  return (
    <Section title="当前状态"
             action={<span style={{ fontSize: 11, color: T.ink3 }}>每 3 秒刷新</span>}>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 20 }}>
        <div>
          <div style={{ fontSize: 11, color: T.ink3, marginBottom: 6 }}>CPU 包温度</div>
          <TempBar tempC={cpu.packageTempC} maxC={cpu.maxTempC} critC={cpu.critTempC} w={180} />
        </div>
        <LiveMetric label="CPU 包功耗 (RAPL)"
                    value={cpu.powerAvailable ? cpu.packagePowerW.toFixed(1) : '—'}
                    unit={cpu.powerAvailable ? 'W' : ''}
                    color={T.blueDeep} />
        {gpu && (
          <div>
            <div style={{ fontSize: 11, color: T.ink3, marginBottom: 6 }}>GPU 温度 · {gpu.hwmon}</div>
            <TempBar tempC={gpu.tempC} maxC={gpu.maxTempC} critC={gpu.critTempC} w={180} />
          </div>
        )}
      </div>
    </Section>
  )
}

function Overview({ data }) {
  const populated = (data.memory.dimms || []).filter(d => d.populated)
  const physNICs = (data.network || []).filter(n => !n.isVirtual)
  const disks = data.storage || []
  const gpus = data.gpus || []
  const pcie = data.pcie || []
  const downgraded = pcie.filter(d => d.linkStatus === 'downgrade')
  const idle       = pcie.filter(d => d.linkStatus === 'idle')
  const empty      = pcie.filter(d => d.linkStatus === 'empty')

  const nouveauGpus = gpus.filter(g => g.driver === 'nouveau')
  const hasIssues = downgraded.length > 0 || nouveauGpus.length > 0

  return (
    <div>
      <Section title="系统身份">
        <KV k="主机名"      v={data.os.hostname} />
        <KV k="发行版"      v={data.os.distro} />
        <KV k="内核 / 架构" v={`${data.os.kernel} · ${data.os.arch}`} />
        <KV k="运行时长"    v={fmtUptime(data.os.uptimeSeconds)} mono={false} />
      </Section>

      <Section title="硬件摘要">
        <KV k="主板"   v={data.board.available ? `${data.board.manufacturer} ${data.board.product}` : '—'} />
        <KV k="BIOS"  v={data.bios.available ? `${data.bios.vendor} · ${data.bios.version} · ${data.bios.date}` : '—'} />
        <KV k="CPU"    v={`${data.cpu.model || '—'}  ·  ${data.cpu.cores || '?'} 核 / ${data.cpu.threads || '?'} 线程`} />
        <KV k="内存"   v={`${fmtBytes(data.memory.totalBytes)} · ${populated.length} / ${data.memory.dimms?.length || '?'} DIMM 已插`} />
        <KV k="GPU"    v={gpus.length ? gpus.map(g => g.model).join(' · ') : '未检测到独立显卡'} />
        <KV k="存储"   v={`${disks.length} 块磁盘 · 合计 ${fmtBytes(disks.reduce((s, d) => s + (d.sizeBytes || 0), 0))}`} />
        <KV k="物理网卡" v={`${physNICs.length} 个 · 虚拟接口 ${(data.network || []).length - physNICs.length} 个`} />
        <KV k="PCIe"    v={`${pcie.length} 个设备 · 空槽 ${empty.length} · 省电空闲 ${idle.length} · 真降级 ${downgraded.length}`} mono={false} />
      </Section>

      <OverviewLiveStrip />

      {hasIssues && (
        <Section title="⚠ 检查异常">
          {nouveauGpus.map(g => (
            <KV key={g.pciAddress} k="GPU 驱动" warn mono={false}
                v={`${g.pciAddress} 使用 nouveau 开源驱动 (无 CUDA)，AI 场景应改用 NVIDIA 官方驱动`} />
          ))}
          {downgraded.map(d => (
            <KV key={d.address} k="PCIe 真降级" warn mono={false}
                v={`${d.address} ${d.device} · 能力 ${humanGen(d.linkGenCap)} ${d.linkWidCap}，当前 ${humanGen(d.linkGenCur)} ${d.linkWidCur}`} />
          ))}
        </Section>
      )}
      {!hasIssues && (
        <Section title="检查结果">
          <div style={{ color: '#059669', fontWeight: 600, fontSize: 13 }}>无异常。</div>
          {idle.length > 0 && (
            <div style={{ color: T.ink3, fontSize: 12, marginTop: 6 }}>
              有 {idle.length} 个设备处于 PCIe ASPM 省电空闲，实际使用时会自动升速，非故障。
            </div>
          )}
        </Section>
      )}
    </div>
  )
}

// 风扇显示：nvidia-smi 给百分比，hwmon 给 RPM
function fmtFan(g) {
  if (!g.fanKnown) return { value: '—', unit: '' }
  if (g.source === 'nvidia-smi') return { value: g.fanPct ?? 0, unit: '%' }
  return { value: g.fanRpm ?? 0, unit: 'RPM' }
}

function GPULiveStrip() {
  const { data: s } = useSensors()
  const gpus = s?.gpus || []
  if (!gpus.length) return null
  const isNvidia = gpus[0].source === 'nvidia-smi'
  return (
    <Section title="当前状态"
             cmd={isNvidia ? "nvidia-smi -q -d TEMPERATURE,POWER,UTILIZATION" : undefined}
             action={<span style={{ fontSize: 11, color: T.ink3 }}>
               每 3 秒刷新 · 来源 {gpus[0].source}
             </span>}>
      {gpus.map((g, i) => {
        const fan = fmtFan(g)
        return (
          <div key={i} style={{ marginBottom: i < gpus.length - 1 ? 14 : 0 }}>
            <div style={{
              display: 'grid',
              gridTemplateColumns: g.powerKnown ? '2fr 1fr 1fr 1fr' : '2fr 1fr 1fr',
              gap: 20, alignItems: 'center',
            }}>
              <div>
                <div style={{ fontSize: 11, color: T.ink3, marginBottom: 6 }}>
                  温度 · {g.hwmon}
                </div>
                <TempBar tempC={g.tempC} maxC={g.maxTempC} critC={g.critTempC} />
                {(g.maxTempC || g.critTempC) && (
                  <div style={{ fontSize: 10.5, color: T.ink3, marginTop: 4, ...MONO }}>
                    阈值: max {g.maxTempC}°C · crit {g.critTempC}°C
                  </div>
                )}
              </div>
              <LiveMetric label="风扇" value={fan.value} unit={fan.unit} />
              {g.powerKnown && (
                <LiveMetric label={`功耗 / ${g.powerLimitW?.toFixed(0)}W`}
                            value={g.powerDrawW?.toFixed(1)} unit="W"
                            color={g.powerDrawW > g.powerLimitW * 0.9 ? '#dc2626' : T.blueDeep} />
              )}
              {g.powerKnown && (
                <LiveMetric label="Util" value={g.utilPct} unit="%"
                            color={g.utilPct >= 80 ? '#059669' : T.ink} />
              )}
              {!g.powerKnown && (
                <LiveMetric label="功耗" value="—" unit="" color={T.ink3} />
              )}
            </div>
            {g.powerKnown && (
              <div style={{
                display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 20, marginTop: 12,
              }}>
                <div>
                  <div style={{ fontSize: 11, color: T.ink3, marginBottom: 4 }}>显存</div>
                  <div style={{
                    fontSize: 13, fontWeight: 700, ...MONO,
                    color: g.memUsedMiB / g.memTotalMiB > 0.9 ? '#dc2626' : T.ink,
                  }}>
                    {g.memUsedMiB} / {g.memTotalMiB} <span style={{ fontSize: 11, color: T.ink3 }}>MiB</span>
                  </div>
                  <div style={{
                    width: '100%', height: 5, background: '#e2e8f0', borderRadius: 3, marginTop: 4,
                    overflow: 'hidden',
                  }}>
                    <div style={{
                      width: `${(g.memUsedMiB / g.memTotalMiB) * 100}%`,
                      height: '100%', background: T.blueDeep, transition: 'width 400ms ease',
                    }} />
                  </div>
                </div>
                <div>
                  <div style={{ fontSize: 11, color: T.ink3, marginBottom: 4 }}>P-State</div>
                  <div style={{ fontSize: 13, fontWeight: 700, ...MONO, color: T.ink }}>{g.pState || '—'}</div>
                  <div style={{ fontSize: 10.5, color: T.ink3, marginTop: 2 }}>
                    P0 = 满速, P8/12 = 深度省电
                  </div>
                </div>
                <div>
                  <div style={{ fontSize: 11, color: T.ink3, marginBottom: 4 }}>PCIe 地址</div>
                  <div style={{ fontSize: 13, fontWeight: 700, ...MONO, color: T.ink }}>{g.pciAddress}</div>
                </div>
              </div>
            )}
            {!g.powerKnown && (
              <div style={{ marginTop: 10, fontSize: 10.5, color: T.ink3 }}>
                当前从 sysfs hwmon 读取。装了 NVIDIA 官方驱动 (nvidia-smi) 后可拿到功耗 / util / 显存 / P-State。
              </div>
            )}
          </div>
        )
      })}
    </Section>
  )
}

const GPU_DIAG_COMMANDS = [
  { group: 'PCIe / 硬件',   desc: '完整 PCIe 配置和链路信息',  cmd: (a) => `lspci -vvv -s ${a}` },
  { group: 'PCIe / 硬件',   desc: '拓扑树，看 GPU 挂在哪根总线', cmd: () => 'lspci -tvnnmm' },
  { group: 'PCIe / 硬件',   desc: '当前 PCIe 链路速率/宽度',    cmd: (a) => `lspci -vv -s ${a} | grep -E 'LnkCap|LnkSta'` },
  { group: '内核模块',      desc: '看当前用的是哪个驱动',       cmd: () => 'lsmod | grep -iE "nvidia|nouveau|amdgpu"' },
  { group: '内核模块',      desc: 'nouveau/nvidia 冲突排查',    cmd: () => 'dmesg | grep -iE "nvidia|nouveau|drm" | tail -30' },
  { group: 'NVIDIA (需官方驱动)', desc: '完整状态 (温度/功耗/进程)', cmd: () => 'nvidia-smi -q' },
  { group: 'NVIDIA (需官方驱动)', desc: '紧凑指标 CSV，方便脚本采集', cmd: () => 'nvidia-smi --query-gpu=name,driver_version,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw --format=csv' },
  { group: 'NVIDIA (需官方驱动)', desc: '多卡拓扑 (NVLink / PCIe / SYS)', cmd: () => 'nvidia-smi topo -m' },
  { group: 'NVIDIA (需官方驱动)', desc: '看 CUDA / driver 版本',  cmd: () => 'nvidia-smi --query-gpu=driver_version --format=csv,noheader && nvcc --version' },
  { group: 'nouveau (当前驱动)',  desc: '看当前功率/频率状态',    cmd: () => 'cat /sys/kernel/debug/dri/*/pstate 2>/dev/null || echo "需 root 且加载 debugfs"' },
  { group: 'OpenGL / 图形栈',    desc: 'OpenGL 后端和 renderer',  cmd: () => 'glxinfo -B | head -20' },
  { group: 'OpenGL / 图形栈',    desc: 'VA-API 硬解能力',        cmd: () => 'vainfo' },
]

function GPUCommandsSection({ pciAddress }) {
  const groups = {}
  for (const c of GPU_DIAG_COMMANDS) {
    if (!groups[c.group]) groups[c.group] = []
    groups[c.group].push(c)
  }
  return (
    <Section title="常用诊断命令"
             action={<span style={{ fontSize: 11, color: T.ink3 }}>点复制到剪贴板</span>}>
      {Object.entries(groups).map(([g, items]) => (
        <div key={g} style={{ marginBottom: 12 }}>
          <div style={{ fontSize: 11.5, color: T.ink3, fontWeight: 600, marginBottom: 4 }}>{g}</div>
          {items.map((c, i) => {
            const cmd = c.cmd(pciAddress)
            return (
              <button key={i} onClick={() => copyText(cmd)} className="edge-press edge-row-hover" style={{
                display: 'flex', gap: 10, alignItems: 'baseline',
                width: '100%', textAlign: 'left', border: 'none',
                background: 'transparent', cursor: 'pointer',
                padding: '5px 6px', borderRadius: 6,
                borderBottom: `1px dashed ${T.borderSoft || '#f1f5f9'}`,
              }}>
                <span style={{ fontSize: 11.5, color: T.ink3, minWidth: 200, flexShrink: 0 }}>
                  {c.desc}
                </span>
                <span style={{
                  ...MONO, fontSize: 12, color: T.ink,
                  flex: 1, wordBreak: 'break-all',
                }}>{cmd}</span>
                <span style={{
                  fontSize: 10, color: T.blueDeep, fontWeight: 600,
                  padding: '1px 6px', borderRadius: 4,
                  background: T.blueSoft, flexShrink: 0,
                }}>复制</span>
              </button>
            )
          })}
        </div>
      ))}
    </Section>
  )
}

function GPUPane({ data }) {
  const gpus = data.gpus || []
  if (!gpus.length) return <div style={{ color: T.ink3 }}>未检测到独立显卡。</div>
  return <><GPULiveStrip />{gpus.map((g, i) => {
    const ui = linkUI(g.linkStatus)
    return (
      <Section key={g.pciAddress} title={`显卡 #${i + 1} · ${g.pciAddress}`}
               cmd={`lspci -vvv -s ${g.pciAddress}`}>
        <KV k="设备"     v={g.model} />
        <KV k="设备类"   v={g.class} />
        <KV k="驱动"     v={g.driver} warn={g.driver === 'nouveau'} />
        <KV k="PCIe 能力" v={`${humanGen(g.linkGenCap)} ${g.linkWidCap}`} />
        <KV k="PCIe 协商"
            v={
              <span>
                {humanGen(g.linkGenCur)} {g.linkWidCur}
                {g.linkStatus && g.linkStatus !== 'ok' && (
                  <span style={{
                    marginLeft: 8, fontSize: 11, padding: '1px 6px', borderRadius: 4,
                    color: ui.color, background: ui.bg, border: `1px solid ${ui.color}22`,
                  }}>
                    {ui.marker} {ui.label}
                  </span>
                )}
              </span>
            }
            warn={g.linkStatus === 'downgrade'} />
        {g.linkStatus === 'idle' && (
          <KV k="说明" mono={false}
              v="宽度打满、代降低 —— 典型 ASPM 省电空闲。实际有 CUDA/图形负载时会自动升到 Gen 上限。" />
        )}
        {g.driver === 'nouveau' && (
          <KV k="建议" warn mono={false}
              v="当前使用 nouveau 开源驱动，缺失 CUDA/NVENC 支持。生产用途请安装 NVIDIA 官方驱动 + CUDA 工具链。" />
        )}
      </Section>
    )
  })}
  <GPUCommandsSection pciAddress={gpus[0].pciAddress} />
  </>
}

function CPULiveStrip() {
  const { data: s } = useSensors()
  const cpu = s?.cpu
  if (!cpu) return null
  return (
    <Section title="当前状态"
             action={<span style={{ fontSize: 11, color: T.ink3 }}>每 3 秒刷新</span>}>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 20 }}>
        <div>
          <div style={{ fontSize: 11, color: T.ink3, marginBottom: 6 }}>包温度</div>
          <TempBar tempC={cpu.packageTempC} maxC={cpu.maxTempC} critC={cpu.critTempC} />
          <div style={{ fontSize: 10.5, color: T.ink3, marginTop: 4, ...MONO }}>
            阈值: max {cpu.maxTempC}°C · crit {cpu.critTempC}°C
          </div>
        </div>
        <div>
          <LiveMetric label="包功耗 (Intel RAPL)"
                      value={cpu.powerAvailable ? cpu.packagePowerW.toFixed(1) : '—'}
                      unit={cpu.powerAvailable ? 'W' : ''}
                      color={cpu.packagePowerW > 100 ? '#dc2626' : T.blueDeep} />
          {!cpu.powerAvailable && (
            <div style={{ fontSize: 10.5, color: T.ink3, marginTop: 4 }}>{cpu.powerReason}</div>
          )}
        </div>
      </div>
      {cpu.coreTemps?.length > 0 && (
        <div style={{ marginTop: 14 }}>
          <div style={{ fontSize: 11, color: T.ink3, marginBottom: 6 }}>每核温度</div>
          <div style={{
            display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(180px, 1fr))', gap: 8,
          }}>
            {cpu.coreTemps.map(ct => (
              <div key={ct.label} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <div style={{ ...MONO, fontSize: 11, color: T.ink3, minWidth: 55 }}>{ct.label}</div>
                <div style={{ flex: 1 }}>
                  <TempBar tempC={ct.tempC} maxC={ct.maxC} critC={ct.critC} w={90} />
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </Section>
  )
}

function CPUPane({ data }) {
  const c = data.cpu
  return (
    <>
    <CPULiveStrip />
    <Section title="CPU" cmd="lscpu">
      <KV k="型号"     v={c.model} />
      <KV k="厂商"     v={c.vendor} />
      <KV k="架构"     v={c.arch} />
      <KV k="槽位数"   v={c.sockets} />
      <KV k="物理核心" v={c.cores} />
      <KV k="逻辑线程" v={c.threads} />
      <KV k="频率"     v={c.maxMHz ? `${c.minMHz?.toFixed(0) || '?'} - ${c.maxMHz.toFixed(0)} MHz` : '—'} />
      <KV k="虚拟化"   v={c.virt} />
      <KV k="指令集"   v={(c.flagsHint || []).join('  ')} />
      {(c.caches || []).map(x => <KV key={x} k={x.split(':')[0]} v={x.split(':').slice(1).join(':').trim()} />)}
    </Section>
    </>
  )
}

function MemoryPane({ data }) {
  const m = data.memory
  const populated = (m.dimms || []).filter(d => d.populated)
  return (
    <>
      <Section title="内存总览">
        <KV k="总容量"    v={fmtBytes(m.totalBytes)} />
        <KV k="可用"      v={fmtBytes(m.availableBytes)} />
        <KV k="DIMM 插槽" v={m.dimms ? `${populated.length} / ${m.dimms.length} 已插` : m.dimmsReason} />
      </Section>
      {m.dimmsAvailable && (
        <Section title="DIMM 详情" cmd="dmidecode -t memory"
                 action={<span style={{ fontSize: 11, color: T.ink3 }}>仅显示已插入槽位</span>}>
          {populated.length === 0 && <div style={{ color: T.ink3 }}>无已插入的 DIMM。</div>}
          {populated.length > 0 && (
            <table style={{ width: '100%', fontSize: 12.5, ...MONO, borderCollapse: 'collapse' }}>
              <thead>
                <tr style={{ color: T.ink3, textAlign: 'left', borderBottom: `1px solid ${T.border}` }}>
                  <th style={{ padding: '6px 4px' }}>槽位</th>
                  <th>容量</th>
                  <th>类型</th>
                  <th>频率</th>
                  <th>厂商</th>
                  <th>Part No.</th>
                </tr>
              </thead>
              <tbody>
                {populated.map((d, i) => (
                  <tr key={i} style={{ borderBottom: `1px dashed ${T.borderSoft || '#f1f5f9'}` }}>
                    <td style={{ padding: '6px 4px', color: T.ink2 }}>{d.slot}</td>
                    <td>{fmtBytes(d.sizeBytes)}</td>
                    <td>{d.type || '—'}</td>
                    <td>{d.speedMTs ? d.speedMTs + ' MT/s' : '—'}</td>
                    <td>{d.manufacturer || '—'}</td>
                    <td>{d.partNumber || '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Section>
      )}
    </>
  )
}

function StoragePane({ data }) {
  return (
    <Section title="块设备" cmd="lsblk -O">
      <table style={{ width: '100%', fontSize: 12.5, ...MONO, borderCollapse: 'collapse' }}>
        <thead>
          <tr style={{ color: T.ink3, textAlign: 'left', borderBottom: `1px solid ${T.border}` }}>
            <th style={{ padding: '6px 4px' }}>路径</th>
            <th>型号</th>
            <th>容量</th>
            <th>介质</th>
            <th>总线</th>
            <th>挂载</th>
          </tr>
        </thead>
        <tbody>
          {(data.storage || []).map(d => (
            <tr key={d.path} style={{ borderBottom: `1px dashed ${T.borderSoft || '#f1f5f9'}` }}>
              <td style={{ padding: '6px 4px', color: T.ink2 }}>{d.path}</td>
              <td>{d.model || d.vendor || '—'}</td>
              <td>{fmtBytes(d.sizeBytes)}</td>
              <td>{d.rotational ? 'HDD' : 'SSD'}</td>
              <td>{(d.transport || '').toUpperCase()}</td>
              <td>{d.mountpoint || '—'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </Section>
  )
}

function NetworkPane({ data }) {
  const phys = (data.network || []).filter(n => !n.isVirtual)
  const virt = (data.network || []).filter(n => n.isVirtual)
  return (
    <>
      <Section title={`物理网卡 · ${phys.length} 个`} cmd="ethtool <iface>">
        {phys.length === 0 && <div style={{ color: T.ink3 }}>未识别到物理网卡。</div>}
        {phys.map(n => (
          <div key={n.name} style={{ marginBottom: 12, paddingBottom: 12, borderBottom: `1px dashed ${T.borderSoft || '#f1f5f9'}` }}>
            <div style={{ fontWeight: 600, marginBottom: 4, ...MONO }}>
              {n.name}
              <span style={{ marginLeft: 8, fontSize: 11, color: n.state === 'UP' ? '#059669' : T.ink3 }}>{n.state}</span>
            </div>
            <KV k="驱动 / 固件" v={`${n.driver || '—'}  ·  ${n.fwVersion || '—'}`} />
            <KV k="链路"        v={`${n.linkSpeed || '—'}${n.linkDuplex ? ' · ' + n.linkDuplex : ''}`} />
            <KV k="MAC"         v={n.mac} />
            <KV k="MTU"         v={n.mtu} />
            <KV k="IPv4"        v={(n.ipv4 || []).join(', ') || '—'} />
          </div>
        ))}
      </Section>
      <Section title={`虚拟接口 · ${virt.length} 个`}
               action={<span style={{ fontSize: 11, color: T.ink3 }}>docker / bridge / veth / tun 等</span>}>
        <div style={{ ...MONO, fontSize: 12, color: T.ink2, columnCount: 3, columnGap: 24 }}>
          {virt.map(n => (
            <div key={n.name} style={{ padding: '2px 0' }}>
              {n.name} <span style={{ color: T.ink3 }}>({n.virtualKind})</span>
            </div>
          ))}
        </div>
      </Section>
    </>
  )
}

function PCIePane({ data }) {
  const all = data.pcie || []
  const counts = { downgrade: 0, idle: 0, empty: 0, ok: 0 }
  for (const d of all) counts[d.linkStatus || 'ok']++

  const [filter, setFilter] = useState('all')
  const filtered = filter === 'all' ? all : all.filter(d => (d.linkStatus || 'ok') === filter)

  const chips = [
    { id: 'all',       label: '全部',    n: all.length,        color: T.ink3 },
    { id: 'downgrade', label: '真降级',  n: counts.downgrade, color: '#b45309' },
    { id: 'idle',      label: '省电空闲', n: counts.idle,      color: '#94a3b8' },
    { id: 'empty',     label: '空槽',    n: counts.empty,     color: '#94a3b8' },
    { id: 'ok',        label: '正常',    n: counts.ok,        color: '#059669' },
  ]

  return (
    <Section title={`PCIe 设备总览 · ${all.length} 个`}
             cmd="lspci -vvv -nn"
             action={
               <div style={{ display: 'flex', gap: 4 }}>
                 {chips.map(c => (
                   <button key={c.id} onClick={() => setFilter(c.id)} style={{
                     padding: '2px 8px', fontSize: 11, borderRadius: 10,
                     background: filter === c.id ? c.color + '18' : 'white',
                     border: `1px solid ${filter === c.id ? c.color : T.border}`,
                     color: filter === c.id ? c.color : T.ink3,
                     fontWeight: filter === c.id ? 700 : 500,
                     cursor: 'pointer',
                   }}>
                     {c.label} <span style={{ opacity: 0.7 }}>{c.n}</span>
                   </button>
                 ))}
               </div>
             }>
      <table style={{ width: '100%', fontSize: 11.5, ...MONO, borderCollapse: 'collapse', tableLayout: 'fixed' }}>
        <colgroup>
          <col style={{ width: 72 }} />
          <col style={{ width: 72 }} />
          <col />
          <col style={{ width: 110 }} />
          <col style={{ width: 90 }} />
          <col style={{ width: 130 }} />
        </colgroup>
        <thead>
          <tr style={{ color: T.ink3, textAlign: 'left', borderBottom: `1px solid ${T.border}` }}>
            <th style={{ padding: '4px 4px' }}>地址</th>
            <th>类别</th>
            <th>厂商 / 设备</th>
            <th>驱动</th>
            <th>PCIe 能力</th>
            <th>PCIe 协商</th>
          </tr>
        </thead>
        <tbody>
          {filtered.map(d => {
            const cat = classifyPCIe(d)
            const ui = linkUI(d.linkStatus)
            return (
              <tr key={d.address} style={{
                borderBottom: `1px dashed ${T.borderSoft || '#f1f5f9'}`,
                background: ui.bg,
                opacity: d.linkStatus === 'empty' ? 0.55 : 1,
              }}>
                <td style={{ padding: '4px 4px', color: T.ink2 }}>{d.address}</td>
                <td><span style={{ color: cat.color, fontWeight: 600 }}>{cat.label}</span></td>
                <td style={{ whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{d.device}</td>
                <td>{d.driver || <span style={{ color: T.ink3 }}>—</span>}</td>
                <td>{humanGen(d.linkGenCap)} {d.linkWidCap}</td>
                <td style={{ color: ui.color, fontWeight: d.linkStatus === 'downgrade' ? 700 : 400 }}>
                  {ui.marker && <span style={{ marginRight: 4 }}>{ui.marker}</span>}
                  {humanGen(d.linkGenCur)} {d.linkWidCur}
                  {ui.label && <span style={{ marginLeft: 6, fontSize: 10, opacity: 0.85 }}>({ui.label})</span>}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
      <div style={{ marginTop: 12, fontSize: 11, color: T.ink3, lineHeight: 1.7 }}>
        <div><b>空槽</b> = 桥/根端口的 downlink 报 x0，表示这个 PCIe 插槽没插东西。</div>
        <div><b>省电空闲</b> = 宽度打满、速率降级，是 ASPM 电源管理机制，有负载时自动升速。</div>
        <div><b>真降级</b> = 宽度实际低于设备能力，通常是主板槽位规格限制或物理接触问题。</div>
      </div>
    </Section>
  )
}

function SensorsPane({ data }) {
  const { data: s } = useSensors()
  if (!s) return <div style={{ color: T.ink3 }}>正在读取传感器…</div>
  const cpu = s.cpu || {}
  const gpus = s.gpus || []
  const fans = s.fans || []
  return (
    <>
      <Section title="CPU 温度 / 功耗"
               action={<span style={{ fontSize: 11, color: T.ink3 }}>每 3 秒刷新 · 采样 200ms</span>}>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 20, marginBottom: 14 }}>
          <div>
            <div style={{ fontSize: 11, color: T.ink3, marginBottom: 6 }}>包温度</div>
            <TempBar tempC={cpu.packageTempC} maxC={cpu.maxTempC} critC={cpu.critTempC} />
          </div>
          <LiveMetric label="包功耗 (Intel RAPL)"
                      value={cpu.powerAvailable ? cpu.packagePowerW.toFixed(1) : '—'}
                      unit={cpu.powerAvailable ? 'W' : ''}
                      color={T.blueDeep} />
          <LiveMetric label="最高核温"
                      value={cpu.coreTemps?.length
                        ? Math.max(...cpu.coreTemps.map(x => x.tempC)).toFixed(0)
                        : '—'}
                      unit="°C" />
        </div>
        {cpu.coreTemps?.length > 0 && (
          <div style={{ marginTop: 8 }}>
            <div style={{ fontSize: 11, color: T.ink3, marginBottom: 6 }}>每核温度</div>
            <div style={{
              display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))', gap: 8,
            }}>
              {cpu.coreTemps.map(ct => (
                <div key={ct.label} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <div style={{ ...MONO, fontSize: 11, color: T.ink3, minWidth: 55 }}>{ct.label}</div>
                  <div style={{ flex: 1 }}>
                    <TempBar tempC={ct.tempC} maxC={ct.maxC} critC={ct.critC} w={100} />
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}
      </Section>

      <Section title={`GPU · ${gpus.length} 个`}
               action={gpus[0]?.source && (
                 <span style={{ fontSize: 11, color: T.ink3 }}>数据来源 {gpus[0].source}</span>
               )}>
        {gpus.length === 0 && <div style={{ color: T.ink3 }}>未检测到独立显卡传感器。</div>}
        {gpus.map((g, i) => {
          const fan = fmtFan(g)
          return (
            <div key={i} style={{
              padding: '10px 0',
              borderBottom: i < gpus.length - 1 ? `1px dashed ${T.borderSoft || '#f1f5f9'}` : 'none',
            }}>
              <div style={{
                display: 'grid',
                gridTemplateColumns: g.powerKnown ? 'repeat(5, 1fr)' : '2fr 1fr 1fr',
                gap: 20, alignItems: 'center', marginBottom: g.powerKnown ? 10 : 0,
              }}>
                <div style={{ gridColumn: g.powerKnown ? 'span 2' : 'span 1' }}>
                  <div style={{ fontSize: 11, color: T.ink3, marginBottom: 6 }}>
                    温度 · {g.hwmon}
                  </div>
                  <TempBar tempC={g.tempC} maxC={g.maxTempC} critC={g.critTempC} />
                </div>
                <LiveMetric label="风扇" value={fan.value} unit={fan.unit} />
                {g.powerKnown && (
                  <LiveMetric label={`功耗 / ${g.powerLimitW?.toFixed(0)}W`}
                              value={g.powerDrawW?.toFixed(1)} unit="W"
                              color={g.powerDrawW > g.powerLimitW * 0.9 ? '#dc2626' : T.blueDeep} />
                )}
                {g.powerKnown && (
                  <LiveMetric label="Util" value={g.utilPct} unit="%"
                              color={g.utilPct >= 80 ? '#059669' : T.ink} />
                )}
                {!g.powerKnown && (
                  <LiveMetric label="功耗" value="—" unit="" color={T.ink3} />
                )}
              </div>
              {g.powerKnown && (
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 20 }}>
                  <div>
                    <div style={{ fontSize: 11, color: T.ink3, marginBottom: 4 }}>显存</div>
                    <div style={{ ...MONO, fontSize: 13, fontWeight: 700 }}>
                      {g.memUsedMiB} / {g.memTotalMiB} <span style={{ fontSize: 11, color: T.ink3 }}>MiB</span>
                    </div>
                    <div style={{
                      width: '100%', height: 5, background: '#e2e8f0', borderRadius: 3,
                      marginTop: 4, overflow: 'hidden',
                    }}>
                      <div style={{
                        width: `${(g.memUsedMiB / g.memTotalMiB) * 100}%`,
                        height: '100%', background: T.blueDeep, transition: 'width 400ms ease',
                      }} />
                    </div>
                  </div>
                  <div>
                    <div style={{ fontSize: 11, color: T.ink3, marginBottom: 4 }}>P-State</div>
                    <div style={{ ...MONO, fontSize: 13, fontWeight: 700 }}>{g.pState || '—'}</div>
                  </div>
                  <div>
                    <div style={{ fontSize: 11, color: T.ink3, marginBottom: 4 }}>PCIe 地址</div>
                    <div style={{ ...MONO, fontSize: 13, fontWeight: 700 }}>{g.pciAddress}</div>
                  </div>
                </div>
              )}
            </div>
          )
        })}
        {gpus[0]?.source === 'hwmon' && (
          <div style={{ marginTop: 10, fontSize: 10.5, color: T.ink3 }}>
            当前是 sysfs hwmon 数据源 (nouveau/amdgpu)。装 NVIDIA 官方驱动后自动切到 nvidia-smi，多出 功耗 / util / 显存 / P-State 四项。
          </div>
        )}
      </Section>

      {fans.length > 0 && (
        <Section title={`机箱风扇 · ${fans.length} 个`}>
          <table style={{ width: '100%', fontSize: 12, ...MONO }}>
            <thead>
              <tr style={{ color: T.ink3, textAlign: 'left' }}>
                <th style={{ padding: '4px 0' }}>来源</th>
                <th style={{ textAlign: 'right' }}>转速</th>
              </tr>
            </thead>
            <tbody>
              {fans.map((f, i) => (
                <tr key={i}>
                  <td>{f.source}</td>
                  <td style={{ textAlign: 'right', color: f.rpm > 0 ? T.ink : T.ink3 }}>
                    {f.rpm} RPM
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Section>
      )}

      <Section title="能读到什么 / 读不到什么">
        <div style={{ fontSize: 12, color: T.ink2, lineHeight: 1.75 }}>
          <div>✓ CPU 温度：<code>/sys/class/hwmon/coretemp</code> · 每核独立</div>
          <div>✓ CPU 功耗：<code>/sys/class/powercap/intel-rapl</code> · 采样 200ms 差分 (仅 Intel/较新 AMD)</div>
          <div>✓ GPU 温度/风扇/功耗/util/显存/P-State：<code>nvidia-smi</code> 优先，回退 <code>/sys/class/hwmon/nouveau|amdgpu</code></div>
          <div style={{ marginTop: 6, color: T.ink3 }}>
            ✗ 磁盘温度需 <code>smartctl</code>（未安装），装了就能出现在存储页<br/>
            ✗ 机箱主板温度/电压需 <code>lm-sensors</code>（未配置）
          </div>
        </div>
      </Section>
    </>
  )
}

function SystemPane({ data }) {
  return (
    <>
      <Section title="BIOS" cmd="dmidecode -t bios">
        <KV k="厂商"     v={data.bios.vendor} />
        <KV k="版本"     v={data.bios.version} />
        <KV k="发布日期" v={data.bios.date} />
      </Section>
      <Section title="主板" cmd="dmidecode -t baseboard">
        <KV k="厂商"    v={data.board.manufacturer} />
        <KV k="产品"    v={data.board.product} />
        <KV k="系列"    v={data.board.systemName} />
        <KV k="序列号"  v={data.board.serial} />
      </Section>
    </>
  )
}
