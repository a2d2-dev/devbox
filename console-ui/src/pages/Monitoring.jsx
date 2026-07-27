import { useState, useEffect } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, Chip, Sparkline } from '../components/ui'
import { TrendChart, makeRateSeries } from '../components/TrendChart'
import { getAuthToken } from '../hooks/useApi'

// Story 9.2-A: 独立「监控」App。
// 从 Dashboard.jsx 迁移 TrendCard / GpuRow / GpuProcessTable / 数据拉取 + 新增顶部 4 大资源卡。
// 后端 0 改动，复用 Story 9.1 已建 3 接口（/metrics/history /metrics/history/gpu /gpu/processes）。

function authFetchJSON(url) {
  const token = getAuthToken()
  const headers = token ? { Authorization: 'Bearer ' + token } : {}
  return fetch(url, { headers }).then(r => r.ok ? r.json() : null).catch(() => null)
}

// ─── Metric (GpuRow 子组件，从 Dashboard.jsx 迁移) ──
function Metric({ label, value, max, pct, color, mono }) {
  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between',
        fontSize: 11, color: T.ink3, marginBottom: 4 }}>
        <span>{label}</span>
        <span className="mono">{max}</span>
      </div>
      <div className={mono ? 'mono tnum' : 'tnum'} style={{
        ...T.type.heading, color: T.ink, lineHeight: 1, marginBottom: 5,
      }}>{value}</div>
      <div style={{ height: 4, borderRadius: 2, background: '#f1f5f9', overflow: 'hidden' }}>
        <div style={{
          width: `${Math.min(100, pct)}%`, height: '100%', background: color,
          borderRadius: 2, transition: 'width 0.4s ease',
        }}/>
      </div>
    </div>
  );
}

// ─── MiniSpark (从 Dashboard.jsx 迁移) ──────────────
function MiniSpark({ series, color }) {
  const w = 140, h = 18;
  if (!series || series.length < 2) {
    return <div style={{ height: h, fontSize: 9, color: T.ink4, lineHeight: `${h}px` }}>—</div>;
  }
  const lo = Math.min(...series), hi = Math.max(...series);
  const range = hi - lo || 1;
  const pts = series.map((v, i) => {
    const x = (i / (series.length - 1)) * w;
    const y = h - 1 - ((v - lo) / range) * (h - 2);
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  }).join(' ');
  return (
    <svg width="100%" height={h} viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none" style={{ display: 'block' }}>
      <polyline points={pts} fill="none" stroke={color} strokeWidth="1.4" strokeLinejoin="round"/>
    </svg>
  );
}

function formatBytesPerSec(v) {
  if (v == null || !isFinite(v)) return '—';
  if (v >= 1024 * 1024) return (v / (1024 * 1024)).toFixed(1) + ' MB/s';
  if (v >= 1024) return (v / 1024).toFixed(1) + ' KB/s';
  return Math.round(v) + ' B/s';
}

function formatNumber(v, digits = 1) {
  if (v == null || !isFinite(v)) return '—';
  return v.toFixed(digits);
}

function DiskIOPanel({ disks }) {
  const items = Array.isArray(disks) ? [...disks] : [];
  if (!items.length) return null;
  items.sort((a, b) => Number(b.rotational) - Number(a.rotational) || a.name.localeCompare(b.name));

  const MetricCell = ({ label, value, tone }) => (
    <div style={{
      minHeight: 52, padding: '8px 10px', borderRadius: 8,
      background: T.surfaceAlt, border: `1px solid ${T.borderSoft}`,
    }}>
      <div style={{ fontSize: 10.5, color: T.ink3, marginBottom: 5 }}>{label}</div>
      <div className="mono tnum" style={{ fontSize: 14, fontWeight: 700, color: tone || T.ink }}>
        {value}
      </div>
    </div>
  );

  return (
    <div style={{
      background: T.surface, border: `1px solid ${T.border}`, borderRadius: 10,
      padding: 16, marginBottom: 12,
    }}>
      <div style={{ display: 'flex', alignItems: 'center', marginBottom: 12 }}>
        <div style={{ fontSize: 13, fontWeight: 600, color: T.ink }}>物理磁盘 I/O</div>
        <div style={{ fontSize: 11.5, color: T.ink3, marginLeft: 10 }}>
          按块设备 · 吞吐 / IOPS / 忙碌度 / 延迟
        </div>
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(360px, 1fr))', gap: 12 }}>
        {items.map(d => {
          const util = Math.max(0, Math.min(100, d.utilPercent || 0));
          const utilColor = util >= 85 ? T.red : util >= 65 ? T.amber : T.green;
          return (
            <div key={d.name} style={{
              border: `1px solid ${T.borderSoft}`, borderRadius: 8,
              background: '#fff', padding: 12,
            }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
                <div style={{
                  width: 30, height: 30, borderRadius: 7,
                  background: d.rotational ? '#fffbeb' : '#eff4ff',
                  color: d.rotational ? T.amber : T.blue,
                  border: `1px solid ${d.rotational ? '#fde68a' : '#bfdbfe'}`,
                  display: 'flex', alignItems: 'center', justifyContent: 'center',
                }}>
                  <Icon name="hardDrive" size={15} stroke={1.8}/>
                </div>
                <div style={{ minWidth: 0 }}>
                  <div className="mono" style={{ fontSize: 13, fontWeight: 700, color: T.ink }}>
                    {d.path || `/dev/${d.name}`}
                    <span style={{
                      marginLeft: 7, padding: '1px 6px', borderRadius: 3,
                      background: d.rotational ? '#fffbeb' : '#eff4ff',
                      color: d.rotational ? '#b45309' : T.blueDeep,
                      border: `1px solid ${d.rotational ? '#fde68a' : '#bfdbfe'}`,
                      fontSize: 10.5, fontFamily: 'inherit',
                    }}>{d.kind || (d.rotational ? 'HDD' : 'SSD')}</span>
                  </div>
                  <div style={{ fontSize: 11, color: T.ink3, marginTop: 2, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                    {d.model || '未知型号'}
                  </div>
                </div>
                <div style={{ flex: 1 }}/>
                <div className="mono tnum" style={{ fontSize: 12, fontWeight: 700, color: utilColor }}>
                  {formatNumber(util, 0)}%
                </div>
              </div>
              <div style={{ height: 5, borderRadius: 3, background: '#e5e7eb', overflow: 'hidden', marginBottom: 10 }}>
                <div style={{ width: `${util}%`, height: '100%', background: utilColor, borderRadius: 3 }}/>
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 8 }}>
                <MetricCell label="读取" value={formatBytesPerSec(d.readBytesPerSec)} tone={T.teal}/>
                <MetricCell label="写入" value={formatBytesPerSec(d.writeBytesPerSec)} tone={T.amber}/>
                <MetricCell label="读 IOPS" value={formatNumber(d.readIops, 1)} />
                <MetricCell label="写 IOPS" value={formatNumber(d.writeIops, 1)} />
                <MetricCell label="平均等待" value={`${formatNumber(d.avgAwaitMs, 1)} ms`} tone={d.avgAwaitMs >= 50 ? T.red : T.ink}/>
                <MetricCell label="读等待" value={`${formatNumber(d.readAwaitMs, 1)} ms`} />
                <MetricCell label="写等待" value={`${formatNumber(d.writeAwaitMs, 1)} ms`} />
                <MetricCell label="队列深度" value={formatNumber(d.avgQueueSize, 2)} />
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function PerCoreCPUGrid({ cores }) {
  const items = Array.isArray(cores) ? cores : [];
  if (!items.length) {
    return null;
  }
  return (
    <div style={{
      background: T.surface, border: `1px solid ${T.border}`, borderRadius: 10,
      padding: 16, marginBottom: 12,
    }}>
      <div style={{ display: 'flex', alignItems: 'center', marginBottom: 12 }}>
        <div style={{ fontSize: 13, fontWeight: 600, color: T.ink }}>CPU 每核使用率</div>
        <div style={{ fontSize: 11.5, color: T.ink3, marginLeft: 10 }}>{items.length} 核 · 实时</div>
      </div>
      <div style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(auto-fill, minmax(92px, 1fr))',
        gap: 8,
      }}>
        {items.map((v, i) => {
          const pct = Math.max(0, Math.min(100, Number.isFinite(v) ? v : 0));
          const color = pct >= 85 ? T.red : pct >= 65 ? T.amber : T.blue;
          return (
            <div key={i} style={{
              minHeight: 58, border: `1px solid ${T.borderSoft}`, borderRadius: 8,
              background: T.surfaceAlt, padding: '9px 10px',
            }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
                <span className="mono" style={{ fontSize: 11, color: T.ink3 }}>CPU {i}</span>
                <span className="mono tnum" style={{ fontSize: 12, fontWeight: 700, color }}>{Math.round(pct)}%</span>
              </div>
              <div style={{ height: 5, borderRadius: 3, background: '#e5e7eb', overflow: 'hidden' }}>
                <div style={{
                  width: `${pct}%`, height: '100%', background: color, borderRadius: 3,
                  transition: 'width 0.4s ease',
                }}/>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

// ─── GpuRow (从 Dashboard.jsx 迁移) ─────────────────
function GpuRow({ g, live, history }) {
  const memPct = (g.memUsed / g.memTotal) * 100;
  const h = history || {};
  return (
    <div style={{
      border: `1px solid ${T.borderSoft}`, borderRadius: 8, padding: 14,
      background: T.surfaceAlt,
    }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
        <div style={{
          width: 26, height: 26, borderRadius: 6,
          background: `linear-gradient(140deg, ${T.indigo}, #4338ca)`,
          color: 'white', display: 'flex', alignItems: 'center', justifyContent: 'center',
        }}>
          <Icon name="bolt" size={13} stroke={2}/>
        </div>
        <div style={{ fontSize: 13, fontWeight: 600, color: T.ink }}>GPU {g.id} · {g.model}</div>
        <div style={{ flex: 1 }}/>
        <Chip tone="green"><StatusDot tone="green" size={6}/>正常</Chip>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10, marginBottom: 6 }}>
        <div>
          <Metric label="利用率" value={`${live.toFixed(0)}%`} max="100%" pct={live} color={T.indigo}/>
          <MiniSpark series={h.util} color={T.indigo}/>
        </div>
        <div>
          <Metric label="显存" value={`${g.memUsed} GB`} max={`${g.memTotal} GB`} pct={memPct} color={T.cyan}/>
          <MiniSpark series={h.memPercent} color={T.cyan}/>
        </div>
        <div>
          <Metric label="温度" value={`${g.temp}°`} max="85°" pct={(g.temp/85)*100} color={T.amber} mono/>
          <MiniSpark series={h.temp} color={T.amber}/>
        </div>
        <div>
          <Metric label="功耗" value={`${g.power} W`} max={`${g.max} W`} pct={(g.power/g.max)*100} color={T.green} mono/>
          <MiniSpark series={h.power} color={T.green}/>
        </div>
      </div>
    </div>
  );
}

// ─── GpuProcessTable (从 Dashboard.jsx 迁移) ─────────
function GpuProcessTable({ data, status }) {
  if (status === 'no-nvidia-smi') {
    return null;
  }
  const head = { fontSize: 10.5, fontWeight: 600, color: T.ink3,
    letterSpacing: '0.06em', textTransform: 'uppercase' };
  return (
    <div style={{
      background: T.surface, border: `1px solid ${T.border}`, borderRadius: 10,
      padding: 0, overflow: 'hidden', marginBottom: 12,
    }}>
      <div style={{ display: 'flex', alignItems: 'center', padding: '12px 16px',
        borderBottom: `1px solid ${T.borderSoft}` }}>
        <div style={{ fontSize: 13, fontWeight: 600, color: T.ink }}>GPU 进程占用</div>
        <div style={{ flex: 1 }}/>
        <span style={{ fontSize: 11.5, color: T.ink3 }}>{
          status === 'ok' ? `${data.length} 个活跃进程` :
          status === 'error' ? '进程数据暂不可用' : '加载中…'
        }</span>
      </div>
      {status === 'ok' && data.length === 0 ? (
        <div style={{ padding: '20px 16px', color: T.ink4, fontSize: 13, textAlign: 'center' }}>当前 GPU 上无活跃进程</div>
      ) : status === 'ok' ? (
        <div className="edge-table-scroll"><table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ background: '#fafbfc' }}>
              <th style={{ ...head, textAlign: 'left',  padding: '10px 16px' }}>PID</th>
              <th style={{ ...head, textAlign: 'left',  padding: '10px 12px' }}>进程</th>
              <th style={{ ...head, textAlign: 'left',  padding: '10px 12px' }}>容器</th>
              <th style={{ ...head, textAlign: 'left',  padding: '10px 12px' }}>GPU UUID</th>
              <th style={{ ...head, textAlign: 'right', padding: '10px 16px' }}>显存 (MiB)</th>
            </tr>
          </thead>
          <tbody>
            {data.map((p, i) => (
              <tr key={`${p.pid}-${p.gpuUuid}`} style={{ borderTop: i ? `1px solid ${T.borderSoft}` : 'none' }}>
                <td style={{ padding: '10px 16px', fontSize: 12 }} className="mono tnum">{p.pid}</td>
                <td style={{ padding: '10px 12px', fontSize: 12.5, color: T.ink }}>{p.name}</td>
                <td style={{ padding: '10px 12px', fontSize: 12, color: T.ink2 }} className="mono">{p.containerHint || '—'}</td>
                <td style={{ padding: '10px 12px', fontSize: 11.5, color: T.ink3 }} className="mono">{p.gpuUuid}</td>
                <td style={{ padding: '10px 16px', textAlign: 'right', fontSize: 12, color: T.ink }} className="mono tnum">{p.vramMiB}</td>
              </tr>
            ))}
          </tbody>
        </table></div>
      ) : null}
    </div>
  );
}

// ─── TopResourceBar — Story 9.2-A AC-2 新组件 ──────
// 4 大资源卡（CPU / GPU / MEM / DISK），仿 edge-console 节点 monitors 页风格。
// 当前值 + 容量详情；无 GPU 时显示「无 GPU」。
function TopResourceBar({ cpu, gpuAvg, memPct, memUsed, memTotal, diskPct, diskUsed, diskTotal, hasGpu, gpuCount, gpuModel }) {
  const Cell = ({ icon, label, pct, value, sub, color }) => {
    const loading = pct == null;
    return (
      <div style={{
        background: T.surface, border: `1px solid ${T.border}`, borderRadius: 10,
        padding: '14px 16px',
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 8 }}>
          <Icon name={icon} size={13} stroke={1.8} style={{ color: loading ? T.ink4 : color }}/>
          <div style={{ fontSize: 12, fontWeight: 600, color: T.ink }}>{label}</div>
        </div>
        <div className="mono tnum" style={{
          ...T.type.title, fontWeight: 700, color: loading ? T.ink4 : color, lineHeight: 1,
          letterSpacing: '-0.02em',
        }}>{value}</div>
        <div style={{ fontSize: 11, color: T.ink3, marginTop: 6 }}>{sub}</div>
      </div>
    );
  };

  return (
    <div className="edge-stat-grid" style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 12, marginBottom: 12 }}>
      <Cell icon="cpu" label="CPU 使用率"
        pct={cpu} value={cpu == null ? '—' : `${cpu}%`}
        sub={cpu == null ? '加载中…' : '当前负载'} color={T.blue}/>
      <Cell icon="bolt" label="GPU 平均"
        pct={hasGpu ? gpuAvg : 0}
        value={hasGpu ? (gpuAvg == null ? '—' : `${gpuAvg}%`) : '无 GPU'}
        sub={hasGpu ? `${gpuCount} × ${gpuModel}` : '本节点未检测到 NVIDIA GPU'}
        color={T.indigo}/>
      <Cell icon="memory" label="内存"
        pct={memPct} value={memPct == null ? '—' : `${memPct}%`}
        sub={memUsed != null && memTotal != null ? `${memUsed} GB / ${memTotal} GB` : '加载中…'}
        color={T.teal}/>
      <Cell icon="hardDrive" label="磁盘"
        pct={diskPct} value={diskPct == null ? '—' : `${diskPct}%`}
        sub={diskUsed != null && diskTotal != null ? `${diskUsed} GB / ${diskTotal} GB` : '加载中…'}
        color={T.amber}/>
    </div>
  );
}

// ─── MonitoringApp — 主组件 ─────────────────────────
export default function MonitoringApp() {
  // Story 9.1 时间窗 (沿用 1h/6h/24h)
  const [historyWindow, setHistoryWindow] = useState('1h')
  const HISTORY_WINDOWS = [
    { value: '1h',  label: '1 小时' },
    { value: '6h',  label: '6 小时' },
    { value: '24h', label: '24 小时' },
  ]

  // 数据 state (从 Dashboard.jsx 迁移)
  const [device, setDevice] = useState({});
  const [metrics, setMetrics] = useState(null);
  const [history, setHistory] = useState(null);
  const [gpus, setGpus] = useState([]);
  const [gpuHistory, setGpuHistory] = useState(null);
  const [gpuProcs, setGpuProcs] = useState({ data: [], status: 'idle' });

  // 5s 轮询 (从 Dashboard.jsx 迁移)
  useEffect(() => {
    const fetchAll = async () => {
      try {
        const [mRes, hRes, dRes, gRes] = await Promise.all([
          authFetchJSON('/api/v1/metrics'),
          authFetchJSON('/api/v1/metrics/history?window=' + historyWindow),
          authFetchJSON('/api/v1/device'),
          authFetchJSON('/api/v1/metrics/history/gpu?window=' + historyWindow),
        ]);
        if (mRes) setMetrics(mRes);
        if (hRes) setHistory(hRes);
        if (gRes) setGpuHistory(gRes);
        if (dRes) setDevice({ model: dRes.cpuModel, name: dRes.hostname });
        if (mRes && mRes.gpuData) setGpus(mRes.gpuData);
      } catch {}
    };
    fetchAll();
    const id = setInterval(fetchAll, 5000);
    return () => clearInterval(id);
  }, [historyWindow]);

  // GPU 进程 (从 Dashboard.jsx 迁移)
  useEffect(() => {
    const hasGpu = gpus.length > 0;
    if (!hasGpu) {
      setGpuProcs({ data: [], status: 'no-gpu' });
      return;
    }
    let stopped = false;
    const fetchGpuProcs = async () => {
      try {
        const r = await fetch('/api/v1/gpu/processes', {
          headers: { Authorization: 'Bearer ' + (getAuthToken() || '') },
        });
        if (stopped) return;
        if (r.status === 503) { setGpuProcs({ data: [], status: 'no-nvidia-smi' }); return; }
        if (!r.ok) { setGpuProcs(prev => ({ ...prev, status: 'error' })); return; }
        const data = await r.json();
        setGpuProcs({ data: Array.isArray(data) ? data : [], status: 'ok' });
      } catch {
        if (!stopped) setGpuProcs(prev => ({ ...prev, status: 'error' }));
      }
    };
    fetchGpuProcs();
    const id = setInterval(fetchGpuProcs, 5000);
    return () => { stopped = true; clearInterval(id); };
  }, [gpus.length]);

  // Derived values (从 Dashboard.jsx 迁移)
  const cpu = metrics ? Math.round(metrics.cpuUsedPercent) : null;
  const gpuAvg = gpus.length > 0 ? Math.round(gpus.reduce((s, g) => s + g.gpuUtil, 0) / gpus.length) : null;
  const memTotal = metrics ? Math.round(metrics.memoryTotal / (1024*1024*1024)) : null;
  const memUsed = metrics ? Math.round(metrics.memoryUsed / (1024*1024*1024)) : null;
  const memPct = metrics ? Math.round(metrics.memoryUsedPercent) : null;
  const mainDisk = metrics && metrics.diskData && metrics.diskData.length > 0
    ? metrics.diskData.reduce((a, b) => (b.total > a.total ? b : a), metrics.diskData[0])
    : null;
  const diskPct = mainDisk ? Math.round(mainDisk.usedPercent) : null;
  const diskUsed = mainDisk ? Math.round(mainDisk.used / (1024*1024*1024)) : null;
  const diskTotal = mainDisk ? Math.round(mainDisk.total / (1024*1024*1024)) : null;

  const cpuSeries = history && history.cpuPercent ? history.cpuPercent : [];
  const gpuSeries = history && history.gpuUtil ? history.gpuUtil : [];
  const memSeries = history && history.memoryPercent ? history.memoryPercent : [];
  const diskSeries = history && history.diskPercent ? history.diskPercent : [];
  const gpuMemSeries = history && history.gpuMemPercent ? history.gpuMemPercent : [];

  // 网络是累计 bytes，差分算速率 (B/s)，agent 重启时 diff 为负 → clamp 0
  // 采样间隔 historyWindow 决定（后端固定 5s），但 history 端点的步长来自 prometheus
  // query_range step；保守用前后两点时间差，没时间戳就回退 5s
  const netSentRate = makeRateSeries(history?.netBytesSent, history?.timestamps);
  const netRecvRate = makeRateSeries(history?.netBytesRecv, history?.timestamps);
  const diskReadRate = makeRateSeries(history?.diskReadBytes, history?.timestamps);
  const diskWriteRate = makeRateSeries(history?.diskWriteBytes, history?.timestamps);

  const windowLabel = (HISTORY_WINDOWS.find(w => w.value === historyWindow) || HISTORY_WINDOWS[0]).label;

  return (
    <div className="edge-page edge-monitoring-page" style={{
      flex: 1, padding: '20px 24px', overflow: 'auto',
      background: T.surfaceAlt,
    }}>
      {/* Header strip — 标题 + 时间窗切换（AC-7 仅返回 + 时间窗，不加刷新/导出）*/}
      <div style={{ display: 'flex', alignItems: 'center', marginBottom: 14 }}>
        <div>
          <div style={{ ...T.type.heading, color: T.ink }}>监控</div>
          <div style={{ fontSize: 12, color: T.ink3, marginTop: 3, display: 'flex', alignItems: 'center', gap: 8 }}>
            <span><span className="edge-live-dot" style={{ display: 'inline-block', width: 6, height: 6, borderRadius: '50%', background: T.green, marginRight: 5, verticalAlign: 'middle' }}/>实时刷新中</span>
            <span style={{ color: '#cbd5e1' }}>·</span>
            <span>采样间隔 5s</span>
            <span style={{ color: '#cbd5e1' }}>·</span>
            <span>{device.model || ''}</span>
          </div>
        </div>
        <div style={{ flex: 1 }}/>
        <div style={{ display: 'flex', gap: 2, background: T.surfaceAlt, padding: 2, borderRadius: 8 }}>
          {HISTORY_WINDOWS.map(w => (
            <button key={w.value} onClick={() => setHistoryWindow(w.value)} style={{
              padding: '6px 12px', fontSize: 12, fontWeight: 500, border: 'none',
              background: historyWindow === w.value ? T.surface : 'transparent',
              color: historyWindow === w.value ? T.blueDeep : T.ink3,
              borderRadius: 6, cursor: 'pointer',
              boxShadow: historyWindow === w.value ? '0 1px 2px rgba(0,0,0,0.04)' : 'none',
            }}>{w.label}</button>
          ))}
        </div>
      </div>

      {/* AC-2: 顶部 4 大资源卡 */}
      <TopResourceBar
        cpu={cpu} gpuAvg={gpuAvg}
        memPct={memPct} memUsed={memUsed} memTotal={memTotal}
        diskPct={diskPct} diskUsed={diskUsed} diskTotal={diskTotal}
        hasGpu={gpus.length > 0}
        gpuCount={gpus.length}
        gpuModel={gpus.length > 0 ? gpus[0].productName : ''}/>

      <PerCoreCPUGrid cores={metrics?.cpuPercent}/>

      <DiskIOPanel disks={metrics?.diskIO}/>

      {/* 趋势卡 —— 全部用 TrendChart 组件（自包含 header/chart/X轴/单位 formatter）*/}
      <div className="edge-chart-grid" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 12 }}>
        <TrendChart title={`CPU 使用率 · 最近 ${windowLabel}`} window={historyWindow}
          series={cpuSeries} color={T.blue} unit="percent" max={100}/>
        <TrendChart title={`GPU 使用率 · 最近 ${windowLabel}`} window={historyWindow}
          series={gpuSeries} color={T.indigo} unit="percent" max={100}/>
      </div>
      <div className="edge-chart-grid" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 12 }}>
        <TrendChart title={`内存使用率 · 最近 ${windowLabel}`} window={historyWindow}
          series={memSeries} color={T.violet} unit="percent" max={100}/>
        <TrendChart title={`磁盘使用率 · 最近 ${windowLabel}`} window={historyWindow}
          series={diskSeries} color={T.cyan} unit="percent" max={100}/>
      </div>
      <div className="edge-chart-grid" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 12 }}>
        <TrendChart title={`网络入站 · 最近 ${windowLabel}`} window={historyWindow}
          series={netRecvRate} color={T.green} unit="bytesPerSec"/>
        <TrendChart title={`网络出站 · 最近 ${windowLabel}`} window={historyWindow}
          series={netSentRate} color={T.amber} unit="bytesPerSec"/>
      </div>
      <div className="edge-chart-grid" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 12 }}>
        <TrendChart title={`磁盘读取 · 最近 ${windowLabel}`} window={historyWindow}
          series={diskReadRate} color={T.teal} unit="bytesPerSec"/>
        <TrendChart title={`磁盘写入 · 最近 ${windowLabel}`} window={historyWindow}
          series={diskWriteRate} color={T.red} unit="bytesPerSec"/>
      </div>

      {/* GPU 卡详情 (每卡 4 sparkline) */}
      <div style={{
        background: T.surface, border: `1px solid ${T.border}`, borderRadius: 10,
        padding: 16, marginBottom: 12,
      }}>
        <div style={{ display: 'flex', alignItems: 'center', marginBottom: 12 }}>
          <div style={{ fontSize: 13, fontWeight: 600, color: T.ink }}>GPU 卡详情</div>
          <div style={{ fontSize: 11.5, color: T.ink3, marginLeft: 10 }}>{gpus.length > 0 ? `${gpus.length} 卡 · ${gpus[0].productName} · 时序 ${historyWindow}` : '本节点未检测到 NVIDIA GPU'}</div>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: gpus.length > 1 ? '1fr 1fr' : '1fr', gap: 12 }}>
          {gpus.length > 0 ? gpus.map((g, idx) => {
            const hist = (gpuHistory && gpuHistory.perGpu)
              ? gpuHistory.perGpu.find(h => h.id === String(g.index)) || gpuHistory.perGpu[idx]
              : null;
            return (
              <GpuRow key={g.index} g={{id: g.index, model: g.productName, util: Math.round(g.gpuUtil), memUsed: g.memUsed, memTotal: g.memTotal, temp: g.temperature, power: g.powerDraw, max: g.powerLimit}} live={Math.round(g.gpuUtil)} history={hist}/>
            );
          }) : (
            <div style={{ padding: '24px 0', textAlign: 'center', color: T.ink4, fontSize: 13 }}>本节点未检测到 NVIDIA GPU</div>
          )}
        </div>
      </div>

      {/* GPU 进程表 (仅有 GPU 时渲染) */}
      {gpus.length > 0 && (
        <GpuProcessTable data={gpuProcs.data} status={gpuProcs.status}/>
      )}
    </div>
  );
}
