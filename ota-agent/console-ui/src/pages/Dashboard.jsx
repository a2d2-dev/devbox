import { useState, useEffect, useRef } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, Chip, Ring, Sparkline, Card, useTicker } from '../components/ui'
import { useDevice, useMetrics, useMetricsHistory, useApps, useAlerts, useNetwork } from '../hooks/useApi'
import { METRICS as MOCK_METRICS, GPUS as MOCK_GPUS, STORE_CATEGORIES, STORE_FEATURED, STORE_APPS, ALERTS as MOCK_ALERTS } from '../data/mock'
import { btnSecondary, btnPrimary, btnDanger } from '../components/AppWindow'

function ResourceCard({ icon, title, pct, detail, color, abs }) {
  return (
    <div style={{
      background: T.surface, border: `1px solid ${T.border}`, borderRadius: 10,
      padding: 16, display: 'flex', alignItems: 'center', gap: 14,
    }}>
      <Ring value={pct} size={88} thickness={9} color={color}/>
      <div style={{ minWidth: 0, flex: 1 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 6 }}>
          <div style={{ color: color }}><Icon name={icon} size={14} stroke={1.8}/></div>
          <div style={{ fontSize: 13, fontWeight: 600, color: T.ink }}>{title}</div>
        </div>
        <div className="mono tnum" style={{ fontSize: 12, color: T.ink2, fontWeight: 600 }}>{abs}</div>
        <div style={{ fontSize: 11, color: T.ink3, marginTop: 3 }}>{detail}</div>
      </div>
    </div>
  );
}

function TrendCard({ title, series, color, avg, peak }) {
  return (
    <div style={{
      background: T.surface, border: `1px solid ${T.border}`, borderRadius: 10,
      padding: 16,
    }}>
      <div style={{ display: 'flex', alignItems: 'center', marginBottom: 10 }}>
        <div style={{ fontSize: 13, fontWeight: 600, color: T.ink }}>{title}</div>
        <div style={{ flex: 1 }}/>
        <div style={{ display: 'flex', gap: 14, fontSize: 11.5, color: T.ink3 }}>
          <span>平均 <span className="mono tnum" style={{ color: T.ink, fontWeight: 600 }}>{avg}</span></span>
          <span>峰值 <span className="mono tnum" style={{ color: T.ink, fontWeight: 600 }}>{peak}</span></span>
        </div>
      </div>
      <Sparkline data={series} color={color} width={520} height={84} showAxis max={100}/>
      <div style={{ display: 'flex', justifyContent: 'space-between',
        fontSize: 10.5, color: T.ink4, marginTop: 4 }} className="mono">
        <span>-30m</span><span>-20m</span><span>-10m</span><span>现在</span>
      </div>
    </div>
  );
}

function Metric({ label, value, max, pct, color, mono }) {
  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between',
        fontSize: 11, color: T.ink3, marginBottom: 4 }}>
        <span>{label}</span>
        <span className="mono">{max}</span>
      </div>
      <div className={mono ? 'mono tnum' : 'tnum'} style={{
        fontSize: 16, fontWeight: 700, color: T.ink, lineHeight: 1, marginBottom: 5,
        letterSpacing: '-0.01em',
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

function GpuRow({ g, live }) {
  const memPct = (g.memUsed / g.memTotal) * 100;
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

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10, marginBottom: 10 }}>
        <Metric label="利用率" value={`${live.toFixed(0)}%`} max="100%" pct={live} color={T.indigo}/>
        <Metric label="显存" value={`${g.memUsed} GB`} max={`${g.memTotal} GB`} pct={memPct} color={T.cyan}/>
        <Metric label="温度" value={`${g.temp}°`}    max="85°"  pct={(g.temp/85)*100} color={T.amber} mono/>
        <Metric label="功耗" value={`${g.power} W`}  max={`${g.max} W`} pct={(g.power/g.max)*100} color={T.green} mono/>
      </div>

      <div style={{ paddingTop: 8, borderTop: `1px dashed ${T.border}` }}>
        <div style={{ fontSize: 10.5, color: T.ink3, marginBottom: 4 }}>当前任务</div>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 5 }}>
          {g.tasks.map(t => (
            <span key={t} style={{
              fontSize: 11, padding: '2px 7px', borderRadius: 4,
              background: t.includes('已退出') ? '#fef2f2' : '#eff6ff',
              color: t.includes('已退出') ? '#b91c1c' : '#1d4ed8',
              border: `1px solid ${t.includes('已退出') ? '#fecaca' : '#bfdbfe'}`,
            }}>{t}</span>
          ))}
        </div>
      </div>
    </div>
  );
}

function ServiceTable() {
  const { data: liveApps } = useApps();
  const services = (liveApps || []).filter(a => a.kind === 'app');
  const head = { fontSize: 10.5, fontWeight: 600, color: T.ink3,
    letterSpacing: '0.06em', textTransform: 'uppercase' };
  return (
    <table style={{ width: '100%', borderCollapse: 'collapse' }}>
      <thead>
        <tr style={{ background: '#fafbfc' }}>
          <th style={{ ...head, textAlign: 'left',  padding: '10px 16px' }}>服务</th>
          <th style={{ ...head, textAlign: 'left',  padding: '10px 12px' }}>状态</th>
          <th style={{ ...head, textAlign: 'left',  padding: '10px 12px' }}>版本</th>
          <th style={{ ...head, textAlign: 'left',  padding: '10px 12px' }}>关联资源</th>
          <th style={{ ...head, textAlign: 'right', padding: '10px 12px' }}>QPS</th>
          <th style={{ ...head, textAlign: 'right', padding: '10px 12px' }}>延迟 P99</th>
          <th style={{ ...head, textAlign: 'right', padding: '10px 16px' }}>占用</th>
        </tr>
      </thead>
      <tbody>
        {services.map((s, i) => {
          const ok = s.state === 'running';
          return (
            <tr key={s.id} style={{ borderTop: i ? `1px solid ${T.borderSoft}` : 'none' }}>
              <td style={{ padding: '10px 16px' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <div style={{ width: 22, height: 22, borderRadius: 6,
                    background: s.bg, color: 'white',
                    display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                    <Icon name={s.icon} size={12} stroke={1.8}/>
                  </div>
                  <span style={{ fontSize: 12.5, color: T.ink, fontWeight: 500 }}>{s.name}</span>
                </div>
              </td>
              <td style={{ padding: '10px 12px' }}>
                {ok
                  ? <Chip tone="green"><StatusDot tone="green" size={6}/>运行中</Chip>
                  : <Chip tone="red"><StatusDot tone="red" size={6} pulse/>异常</Chip>}
              </td>
              <td style={{ padding: '10px 12px', fontSize: 12 }} className="mono">{s.version}</td>
              <td style={{ padding: '10px 12px', fontSize: 12, color: T.ink2 }}>{s.gpu}</td>
              <td style={{ padding: '10px 12px', textAlign: 'right', fontSize: 12, color: T.ink }} className="mono tnum">{ok ? s.qps : '—'}</td>
              <td style={{ padding: '10px 12px', textAlign: 'right', fontSize: 12, color: T.ink }} className="mono tnum">{ok ? `${s.p99} ms` : '—'}</td>
              <td style={{ padding: '10px 16px', textAlign: 'right', fontSize: 12, color: T.ink2 }} className="mono tnum">{ok && s.gpuPct ? `${s.gpuPct}%` : '—'}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

export default function DashboardApp() {
  // Live data from Agent API
  const [device, setDevice] = useState({});
  const [metrics, setMetrics] = useState(null);
  const [history, setHistory] = useState(null);
  const [gpus, setGpus] = useState([]);
  const [liveApps, setLiveApps] = useState([]);

  useEffect(() => {
    const fetchAll = async () => {
      try {
        const [mRes, hRes, dRes, aRes] = await Promise.all([
          fetch('/api/v1/metrics').then(r => r.ok ? r.json() : null).catch(() => null),
          fetch('/api/v1/metrics/history').then(r => r.ok ? r.json() : null).catch(() => null),
          fetch('/api/v1/device').then(r => r.ok ? r.json() : null).catch(() => null),
          fetch('/api/v1/apps').then(r => r.ok ? r.json() : null).catch(() => null),
        ]);
        if (mRes) setMetrics(mRes);
        if (hRes) setHistory(hRes);
        if (dRes) setDevice(prev => ({...prev, model: dRes.cpuModel, name: dRes.hostname, os: dRes.platform || dRes.os, agent: 'Edge Platform ' + (dRes.agentVersion || ''), sn: dRes.hostname, uptime: dRes.uptimeHuman}));
        if (mRes && mRes.gpuData) setGpus(mRes.gpuData);
        if (aRes && Array.isArray(aRes) && aRes.length > 0) {
          const sysApps = (liveApps).filter(a => a.kind === 'system');
          const mapped = aRes.map(a => ({id: a.id, kind: 'app', name: a.name, state: a.state, version: a.version || 'latest', icon: 'apps', color: '#3b82f6', bg: 'linear-gradient(160deg,#3b82f6,#1d4ed8)', category: '', desc: a.image}));
          setLiveApps([...sysApps, ...mapped]);
        }
      } catch {}
    };
    fetchAll();
    const id = setInterval(fetchAll, 5000);
    return () => clearInterval(id);
  }, []);

  // Derived values from API metrics (no hooks in conditional -- React rule of hooks)
  const cpu = metrics ? Math.round(metrics.cpuUsedPercent) : 0;
  const gpuAvg = gpus.length > 0 ? Math.round(gpus.reduce((s, g) => s + g.gpuUtil, 0) / gpus.length) : 0;
  const memTotal = metrics ? Math.round(metrics.memoryTotal / (1024*1024*1024)) : 64;
  const memUsed = metrics ? Math.round(metrics.memoryUsed / (1024*1024*1024)) : 37;
  const memPct = metrics ? Math.round(metrics.memoryUsedPercent) : 61;
  // select largest partition as main display disk
  const mainDisk = metrics && metrics.diskData && metrics.diskData.length > 0
    ? metrics.diskData.reduce((a, b) => (b.total > a.total ? b : a), metrics.diskData[0])
    : null;
  const diskPct = mainDisk ? Math.round(mainDisk.usedPercent) : 47;
  const diskUsed = mainDisk ? Math.round(mainDisk.used / (1024*1024*1024)) : 478;
  const diskTotal = mainDisk ? Math.round(mainDisk.total / (1024*1024*1024)) : 1000;
  const diskPath = mainDisk ? mainDisk.path : '/';

  const cpuDetail = device.model ? device.model + ' · ' + (metrics ? (metrics.cpuPercent || []).length : 8) + 'C' : 'CPU';
  const gpuDetail = gpus.length > 0 ? gpus.length + ' \u00d7 ' + gpus[0].productName : '无 GPU';
  const memDetail = metrics ? `已用 ${memUsed} GB / 共 ${memTotal} GB` : '';
  const diskDetail = mainDisk ? `${mainDisk.device} \u00b7 ${diskPath}` : '';

  const cpuSeries = history && history.cpuPercent ? history.cpuPercent : MOCK_METRICS.cpu.series;
  const gpuSeries = history && history.gpuUtil ? history.gpuUtil : MOCK_METRICS.gpu.series;

  return (
    <div style={{
      flex: 1, padding: '20px 24px', overflow: 'auto',
      background: T.surfaceAlt,
    }}>
      {/* Header strip */}
      <div style={{ display: 'flex', alignItems: 'center', marginBottom: 14 }}>
        <div>
          <div style={{ fontSize: 18, fontWeight: 700, color: T.ink, letterSpacing: '-0.01em' }}>资源监控大盘</div>
          <div style={{ fontSize: 12, color: T.ink3, marginTop: 3, display: 'flex', alignItems: 'center', gap: 8 }}>
            <span><span className="edge-live-dot" style={{ display: 'inline-block', width: 6, height: 6, borderRadius: '50%', background: T.green, marginRight: 5, verticalAlign: 'middle' }}/>实时刷新中</span>
            <span style={{ color: '#cbd5e1' }}>·</span>
            <span>采样间隔 2s</span>
            <span style={{ color: '#cbd5e1' }}>·</span>
            <span>{device.model || ''}</span>
          </div>
        </div>
        <div style={{ flex: 1 }}/>
        <div style={{ display: 'flex', gap: 8 }}>
          <button style={btnSecondary}>
            <Icon name="download" size={13} stroke={1.8}/>导出诊断包
          </button>
          <button style={btnSecondary}>
            <Icon name="refresh" size={13} stroke={1.8}/>立即刷新
          </button>
        </div>
      </div>

      {/* Row 1: 4 resource rings */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 12, marginBottom: 12 }}>
        <ResourceCard icon="cpu"        title="CPU 使用率" pct={cpu}    detail={cpuDetail}   color={T.blue}  abs={`${cpu}% / 100%`}/>
        <ResourceCard icon="bolt"       title="GPU 平均"   pct={gpuAvg} detail={gpuDetail} color={T.indigo} abs={gpus.length > 0 ? `${gpuAvg}% / 100%` : '无 GPU'}/>
        <ResourceCard icon="memory"     title="内存"       pct={memPct} detail={memDetail}  color={T.teal}  abs={`${memUsed} GB / ${memTotal} GB`}/>
        <ResourceCard icon="hardDrive"  title="磁盘"       pct={diskPct} detail={diskDetail} color={T.amber} abs={`${diskUsed} GB / ${diskTotal} GB`}/>
      </div>

      {/* Row 2: trend charts */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 12 }}>
        <TrendCard title="CPU 使用率 · 最近 30 分钟" series={cpuSeries} color={T.blue}    avg={`${(cpuSeries.reduce((a,b)=>a+b,0)/cpuSeries.length).toFixed(0)}%`} peak={`${Math.max(...cpuSeries).toFixed(0)}%`}/>
        <TrendCard title="GPU 使用率 · 最近 30 分钟" series={gpuSeries} color={T.indigo} avg={`${(gpuSeries.reduce((a,b)=>a+b,0)/gpuSeries.length).toFixed(0)}%`} peak={`${Math.max(...gpuSeries).toFixed(0)}%`}/>
      </div>

      {/* Row 3: GPU cards detail */}
      <div style={{
        background: T.surface, border: `1px solid ${T.border}`, borderRadius: 10,
        padding: 16, marginBottom: 12,
      }}>
        <div style={{ display: 'flex', alignItems: 'center', marginBottom: 12 }}>
          <div style={{ fontSize: 13, fontWeight: 600, color: T.ink }}>GPU 卡详情</div>
          <div style={{ fontSize: 11.5, color: T.ink3, marginLeft: 10 }}>{gpus.length > 0 ? `${gpus.length} 卡 · ${gpus[0].productName}` : '未检测到 GPU'}</div>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: gpus.length > 1 ? '1fr 1fr' : '1fr', gap: 12 }}>
          {gpus.length > 0 ? gpus.map((g, idx) => (
            <GpuRow key={g.index} g={{id: g.index, model: g.productName, util: Math.round(g.gpuUtil), memUsed: g.memUsed, memTotal: g.memTotal, temp: g.temperature, power: g.powerDraw, max: g.powerLimit, tasks: []}} live={Math.round(g.gpuUtil)}/>
          )) : (
            <div style={{ padding: '20px 0', textAlign: 'center', color: T.ink4, fontSize: 13 }}>当前设备未检测到 GPU</div>
          )}
        </div>
      </div>

      {/* Row 4: services summary */}
      <div style={{
        background: T.surface, border: `1px solid ${T.border}`, borderRadius: 10,
        padding: 0, overflow: 'hidden',
      }}>
        <div style={{ display: 'flex', alignItems: 'center', padding: '12px 16px',
          borderBottom: `1px solid ${T.borderSoft}` }}>
          <div style={{ fontSize: 13, fontWeight: 600, color: T.ink }}>应用摘要</div>
          <div style={{ flex: 1 }}/>
          <span style={{ fontSize: 11.5, color: T.ink3 }}>共 {liveApps.filter(a=>a.kind==='app').length} 个 · {liveApps.filter(a=>a.kind==='app'&&a.state==='running').length} 运行中 · {liveApps.filter(a=>a.kind==='app'&&a.state==='error').length} 异常</span>
        </div>
        <ServiceTable/>
      </div>
    </div>
  );
}
