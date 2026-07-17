import { useState, useEffect, useRef } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, Chip, Sparkline, Card } from './ui'
import { btnSecondary, btnPrimary, btnDanger } from './AppWindow'
import TabBar from './TabBar'

function Kpi({ label, value, unit, tone }) {
  const colors = { blue: T.blue, indigo: T.indigo, green: T.green, amber: T.amber };
  return (
    <div style={{ padding: '10px 12px', borderRadius: 8,
      background: T.surfaceAlt, border: `1px solid ${T.borderSoft}` }}>
      <div style={{ fontSize: 10.5, color: T.ink3, fontWeight: 600,
        letterSpacing: '0.04em', textTransform: 'uppercase' }}>{label}</div>
      <div style={{ marginTop: 4, display: 'flex', alignItems: 'baseline', gap: 3 }}>
        <span className="mono tnum" style={{ fontSize: 20, fontWeight: 700,
          color: colors[tone], letterSpacing: '-0.02em' }}>{value}</span>
        <span style={{ fontSize: 11, color: T.ink3 }}>{unit}</span>
      </div>
    </div>
  );
}

function Legend({ color, label, value }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 5 }}>
      <span style={{ width: 10, height: 2, background: color, borderRadius: 2 }}/>
      <span style={{ color: T.ink2, fontWeight: 600 }}>{label}</span>
      <span className="mono tnum" style={{ color: T.ink3 }}>{value}</span>
    </div>
  );
}

function DefectOverview({ app }) {

  const rows = [
    ['运行状态', <Chip tone="green"><StatusDot tone="green" size={6}/>Running</Chip>, '健康'],
    ['版本', <span className="mono" style={{ fontSize: 12.5 }}>{app.version}</span>, '可升级到 v3.3.0'],
    ['模型', <span className="mono" style={{ fontSize: 12.5 }}>yolox-s-defect-2024q4.onnx</span>, '体积 28.4 MB · ONNX'],
    ['关联 GPU', <span style={{ fontSize: 12.5 }}>{app.gpu} · NVIDIA A30 · 显存预占 4.2 GB</span>, ''],
    ['部署时间', <span className="mono" style={{ fontSize: 12.5 }}>2026-05-20 09:14:33</span>, '由 admin@cloud 部署'],
    ['运行时长', <span className="mono" style={{ fontSize: 12.5 }}>6d 3h 18m</span>, ''],
    ['镜像', <span className="mono" style={{ fontSize: 12.5 }}>registry.edgex.io/defect:3.2.1-cuda11</span>, '体积 1.2 GB'],
    ['启动参数', <span className="mono" style={{ fontSize: 12.5 }}>--batch=8 --port=7821 --workers=4</span>, ''],
    ['健康检查', <span style={{ fontSize: 12.5 }}>GET /healthz · 间隔 5s</span>, '最近 24h 失败 0 次'],
    ['上次重启', <span className="mono" style={{ fontSize: 12.5 }}>2026-05-20 09:14:33</span>, '首次启动'],
  ];
  return (
    <div style={{ padding: 24, display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
      <Card title="服务信息" padding={0}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12.5 }}>
          <tbody>
            {rows.map(([k, v, hint], i) => (
              <tr key={k} style={{ borderTop: i ? `1px solid ${T.borderSoft}` : 'none' }}>
                <td style={{ width: 96, padding: '10px 16px', color: T.ink3, verticalAlign: 'top' }}>{k}</td>
                <td style={{ padding: '10px 16px', color: T.ink }}>
                  <div>{v}</div>
                  {hint && <div style={{ fontSize: 11, color: T.ink3, marginTop: 3 }}>{hint}</div>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <Card title="实时性能" action={<span style={{ fontSize: 11.5, color: T.ink3 }}>最近 5 分钟</span>}>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4,1fr)', gap: 10, marginBottom: 12 }}>
            <Kpi label="QPS"        value="120" unit="/s"  tone="blue"/>
            <Kpi label="P99 延迟"   value="45"  unit="ms" tone="indigo"/>
            <Kpi label="成功率"     value="99.97" unit="%" tone="green"/>
            <Kpi label="队列深度"   value="3"   unit="" tone="amber"/>
          </div>
          <Sparkline data={[]} color={T.blue} width={460} height={70} showAxis max={140}/>
        </Card>

        <Card title="健康检查 · 最近 24h" padding={16}>
          <div style={{ display: 'flex', gap: 2, marginTop: 4 }}>
            {Array.from({ length: 60 }).map((_, i) => {
              const fail = i === 32;
              return (
                <div key={i} style={{
                  flex: 1, height: 22, borderRadius: 2,
                  background: fail ? T.amber : T.green,
                  opacity: 0.85,
                }}/>
              );
            })}
          </div>
          <div style={{ display: 'flex', justifyContent: 'space-between',
            marginTop: 6, fontSize: 10.5, color: T.ink3 }} className="mono">
            <span>-24h</span><span>-12h</span><span>现在</span>
          </div>
          <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 8 }}>
            1 次轻微抖动（22:14，恢复时间 8s），其余 24h 全部健康。
          </div>
        </Card>
      </div>
    </div>
  );
}

function DefectMetrics() {
  return (
    <div style={{ padding: 24, display: 'flex', flexDirection: 'column', gap: 12 }}>
      <Card title="QPS · 最近 30 分钟" action={<span style={{ fontSize: 11.5, color: T.ink3 }}>平均 120 · 峰值 138</span>}>
        <Sparkline data={[]} color={T.blue} width={920} height={140} showAxis max={140}/>
      </Card>

      <Card title="推理延迟 · P50 / P95 / P99">
        <div style={{ position: 'relative', height: 180 }}>
          <Sparkline data={[]} color={T.red}    width={920} height={180} fill={false} showAxis max={70} style={{ position: 'absolute', top: 0, left: 0 }}/>
          <div style={{ position: 'absolute', top: 0, left: 0 }}>
            <Sparkline data={[]} color={T.amber} width={920} height={180} fill={false} max={70}/>
          </div>
          <div style={{ position: 'absolute', top: 0, left: 0 }}>
            <Sparkline data={[]} color={T.green} width={920} height={180} fill={false} max={70}/>
          </div>
        </div>
        <div style={{ display: 'flex', gap: 16, marginTop: 8, fontSize: 11.5 }}>
          <Legend color={T.green} label="P50" value="22ms"/>
          <Legend color={T.amber} label="P95" value="39ms"/>
          <Legend color={T.red}   label="P99" value="45ms"/>
          <div style={{ flex: 1 }}/>
          <span style={{ color: T.ink3 }}>采样窗口 · 每 60s 一次</span>
        </div>
      </Card>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
        <Card title="成功率 · 24h">
          <div style={{ display: 'flex', alignItems: 'baseline', gap: 6, marginBottom: 10 }}>
            <span className="mono tnum" style={{ fontSize: 32, fontWeight: 700, color: T.green, letterSpacing: '-0.02em' }}>99.97</span>
            <span style={{ fontSize: 14, color: T.ink3 }}>%</span>
            <span style={{ marginLeft: 'auto', fontSize: 11.5, color: T.ink3 }}>较昨日 <span style={{ color: T.green, fontWeight: 600 }}>+0.02%</span></span>
          </div>
          <div style={{ height: 6, borderRadius: 3, background: '#fee2e2', overflow: 'hidden' }}>
            <div style={{ width: '99.97%', height: '100%', background: T.green }}/>
          </div>
          <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 8, fontSize: 11.5, color: T.ink3 }}>
            <span>总请求 <span className="mono tnum" style={{ color: T.ink, fontWeight: 600 }}>10,389,224</span></span>
            <span>失败 <span className="mono tnum" style={{ color: T.red, fontWeight: 600 }}>3,116</span></span>
          </div>
        </Card>

        <Card title="资源消耗 · 24h">
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
            <Kpi label="GPU 时数"    value="8.4"   unit="h" tone="indigo"/>
            <Kpi label="显存峰值"    value="4.6"   unit="GB" tone="blue"/>
            <Kpi label="处理图像"    value="10.39" unit="M" tone="green"/>
            <Kpi label="估算成本"    value="¥38.20" unit="" tone="amber"/>
          </div>
        </Card>
      </div>
    </div>
  );
}

function DefectLogs() {
  const [q, setQ] = useState('');
  const [lines, setLines] = useState([]);

  // Stream a new line periodically
  useEffect(() => {
    const id = setInterval(() => {
      const ts = new Date();
      const hh = String(ts.getHours()).padStart(2, '0');
      const mm = String(ts.getMinutes()).padStart(2, '0');
      const ss = String(ts.getSeconds()).padStart(2, '0');
      const ms = String(ts.getMilliseconds()).padStart(3, '0');
      const pool = [
        ['INFO',  'http   ', `POST /v1/infer batch=${Math.floor(Math.random()*8+1)} latency=${Math.floor(35+Math.random()*15)}ms`],
        ['INFO',  'infer  ', `inference completed | objects=${Math.floor(Math.random()*6)} | conf>=${(0.75+Math.random()*0.2).toFixed(2)}`],
        ['DEBUG', 'engine ', `cuda mem in-use ${(3.8+Math.random()*0.6).toFixed(1)}/12.0 GB`],
      ];
      const pick = pool[Math.floor(Math.random() * pool.length)];
      setLines(L => [...L.slice(-200), [`${hh}:${mm}:${ss}.${ms}`, ...pick]]);
    }, 1100);
    return () => clearInterval(id);
  }, []);

  const filtered = q ? lines.filter(L => L.join(' ').toLowerCase().includes(q.toLowerCase())) : lines;
  const levelColor = { INFO: '#34d399', DEBUG: '#94a3b8', WARN: '#fbbf24', ERROR: '#f87171' };

  return (
    <div style={{ padding: 20 }}>
      {/* toolbar */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 10 }}>
        <div style={{
          flex: 1, maxWidth: 340, display: 'flex', alignItems: 'center', gap: 8,
          height: 32, padding: '0 10px', background: T.surface,
          border: `1px solid ${T.border}`, borderRadius: 7,
        }}>
          <Icon name="search" size={13} stroke={1.8} style={{ color: T.ink4 }}/>
          <input value={q} onChange={(e) => setQ(e.target.value)}
            placeholder="过滤日志（如 latency、ERROR、infer…）"
            style={{ flex: 1, border: 'none', outline: 'none', fontSize: 12.5, background: 'transparent' }}/>
        </div>
        <Chip tone="green"><StatusDot tone="green" size={6} pulse/>实时流</Chip>
        <Chip tone="gray">stdout · stderr</Chip>
        <div style={{ flex: 1 }}/>
        <span style={{ fontSize: 11.5, color: T.ink3 }}>共 {filtered.length} 行 · 每 1s 刷新</span>
      </div>

      {/* terminal */}
      <div style={{
        background: '#0b1020', borderRadius: 8, padding: '12px 14px',
        height: 360, overflowY: 'auto',
        fontFamily: 'ui-monospace, SFMono-Regular, "JetBrains Mono", Menlo, monospace',
        fontSize: 12, lineHeight: 1.55, color: '#cbd5e1',
        border: '1px solid #0f1729',
        boxShadow: 'inset 0 0 0 1px rgba(255,255,255,0.04)',
      }}>
        {filtered.slice(-80).map(([t, lvl, mod, msg], i) => (
          <div key={i} style={{ display: 'flex', gap: 10 }}>
            <span style={{ color: '#64748b', flexShrink: 0 }}>{t}</span>
            <span style={{ color: levelColor[lvl.trim()] || '#cbd5e1', fontWeight: 600, flexShrink: 0, width: 46 }}>{lvl.trim()}</span>
            <span style={{ color: '#7dd3fc', flexShrink: 0, width: 52 }}>{mod.trim()}</span>
            <span style={{ color: '#e2e8f0' }}>{msg}</span>
          </div>
        ))}
        <div style={{ display: 'flex', gap: 8, marginTop: 6, color: '#64748b' }}>
          <span>—</span><span className="edge-cursor" style={{ color: '#34d399' }}>▍</span>
        </div>
      </div>
    </div>
  );
}

export default function AppDetail({ appId, authed, onRequireAuth }) {
  const app = null;
  const [tab, setTab] = useState('overview');
  const tabs = [
    { id: 'overview', label: '概览', icon: 'info' },
    { id: 'metrics',  label: '指标', icon: 'dashboard' },
    { id: 'logs',     label: '日志', icon: 'terminal' },
  ];

  const requestAction = (action) => {
    if (!authed) onRequireAuth(action);
    else alert(`已执行：${action}`);
  };

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', background: T.surfaceAlt }}>
      {/* Sub-header */}
      <div style={{
        padding: '16px 24px 0', background: T.surface,
        borderBottom: `1px solid ${T.border}`,
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 14 }}>
          <div style={{
            width: 44, height: 44, borderRadius: 11, background: app.bg, color: 'white',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            boxShadow: `0 4px 12px -2px ${app.color}55, inset 0 1px 0 rgba(255,255,255,0.4)`,
          }}>
            <Icon name={app.icon} size={22} stroke={1.6}/>
          </div>
          <div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <div style={{ fontSize: 17, fontWeight: 700, color: T.ink, letterSpacing: '-0.01em' }}>{app.name}</div>
              <Chip tone="green"><StatusDot tone="green" size={6}/>运行中</Chip>
              <Chip tone="gray" style={{ fontFamily: 'inherit' }}><span className="mono">{app.version}</span></Chip>
            </div>
            <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 4, display: 'flex', alignItems: 'center', gap: 8 }}>
              <span>已运行 6 天 3 小时</span>
              <span style={{ color: '#cbd5e1' }}>·</span>
              <span>{app.gpu} · 占用 {app.gpuPct}%</span>
              <span style={{ color: '#cbd5e1' }}>·</span>
              <span className="mono">qps {app.qps} · p99 {app.p99}ms</span>
            </div>
          </div>
          <div style={{ flex: 1 }}/>
          <div style={{ display: 'flex', gap: 8 }}>
            <button className="edge-press edge-btn-secondary" style={btnSecondary}>
              <Icon name="refresh" size={13} stroke={1.8}/>
              {authed ? '重启' : '重启…'}
            </button>
            <button className="edge-press edge-btn-danger" style={btnDanger} onClick={() => requestAction('停止服务')}>
              <Icon name="stop" size={13} stroke={1.8}/>
              停止
            </button>
          </div>
        </div>

        {/* Tabs */}
        <TabBar
          tabs={tabs}
          active={tab}
          onChange={setTab}
          style={{ gap: 4 }}
          itemStyle={{ padding: '10px 14px 12px', fontSize: 13, marginBottom: -1 }}
          renderLabel={(t) => (
            <>
              <Icon name={t.icon} size={13} stroke={1.8}/>
              {t.label}
            </>
          )}
        />
      </div>

      {/* Tab content */}
      <div style={{ flex: 1, overflow: 'auto' }}>
        {tab === 'overview' && <DefectOverview app={app}/>}
        {tab === 'metrics' && <DefectMetrics/>}
        {tab === 'logs' && <DefectLogs/>}
      </div>

      {/* Action bar (footer) */}
      <div style={{
        padding: '12px 24px', borderTop: `1px solid ${T.border}`,
        background: T.surface, display: 'flex', alignItems: 'center', gap: 8,
      }}>
        {!authed ? (
          <div style={{
            display: 'flex', alignItems: 'center', gap: 8, padding: '6px 10px',
            background: '#fffbeb', border: '1px solid #fde68a', borderRadius: 7,
            color: '#b45309', fontSize: 12,
          }}>
            <Icon name="shield" size={13} stroke={1.8}/>
            危险操作（重启 / 停止 / 部署）需先验证工号身份
          </div>
        ) : (
          <div style={{
            display: 'flex', alignItems: 'center', gap: 8, padding: '6px 10px',
            background: '#ecfdf5', border: '1px solid #a7f3d0', borderRadius: 7,
            color: '#047857', fontSize: 12,
          }}>
            <Icon name="check" size={13} stroke={2}/>
            已验证：工号 20240318 · 有效至 14:30
          </div>
        )}
        <div style={{ flex: 1 }}/>
        <button className="edge-press edge-btn-secondary" style={btnSecondary}><Icon name="download" size={13} stroke={1.8}/>导出日志</button>
        <button className="edge-press edge-btn-secondary" style={btnSecondary}><Icon name="refresh" size={13} stroke={1.8}/>滚动更新</button>
        <button className={`edge-press ${authed ? 'edge-btn-primary' : 'edge-btn-secondary'}`} style={!authed ? { ...btnSecondary, color: T.ink3 } : btnPrimary} onClick={() => requestAction('重启服务')}>
          <Icon name="refresh" size={13} stroke={1.8}/>{!authed ? '需验证后重启' : '重启服务'}
        </button>
      </div>
    </div>
  );
}
