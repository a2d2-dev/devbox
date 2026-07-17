import { useState, useEffect } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, Chip } from '../components/ui'
import { useApps, getAuthToken } from '../hooks/useApi'
import { btnPrimary } from '../components/AppWindow'
import { SimpleResourceCard } from '../components/SimpleResourceCard'

// Story 9.2-B: Dashboard 改总览 SimpleResourceCard 风格。
// 详细 TrendCard / GpuRow / GPU 进程表已迁到 Story 9.2-A 的 Monitoring App。
// 本 App 保留：4 SimpleResourceCard 总览 + 应用摘要 ServiceTable + 跳监控按钮。

function authFetchJSON(url) {
  const token = getAuthToken()
  const headers = token ? { Authorization: 'Bearer ' + token } : {}
  return fetch(url, { headers }).then(r => r.ok ? r.json() : null).catch(() => null)
}

// ─── ServiceTable (保留不动) ─────────────────────────
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

// ─── DashboardApp 主组件 ────────────────────────────
export default function DashboardApp({ onOpenApp }) {
  const [device, setDevice] = useState({});
  const [metrics, setMetrics] = useState(null);
  const [gpus, setGpus] = useState([]);
  const [liveApps, setLiveApps] = useState([]);

  // 仅拉 /metrics + /device + /apps（不再拉 history / gpu/processes — 已迁 9.2-A）
  useEffect(() => {
    const fetchAll = async () => {
      try {
        const [mRes, dRes, aRes] = await Promise.all([
          authFetchJSON('/api/v1/metrics'),
          authFetchJSON('/api/v1/device'),
          authFetchJSON('/api/v1/apps'),
        ]);
        if (mRes) setMetrics(mRes);
        if (dRes) setDevice({ model: dRes.cpuModel, name: dRes.hostname });
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

  // Derived
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

  const cpuCount = metrics && metrics.cpuPercent ? metrics.cpuPercent.length : null;

  return (
    <div style={{
      flex: 1, padding: '20px 24px', overflow: 'auto',
      background: T.surfaceAlt,
    }}>
      {/* Header strip */}
      <div style={{ display: 'flex', alignItems: 'center', marginBottom: 14 }}>
        <div>
          <div style={{ ...T.type.heading, color: T.ink }}>仪表盘 · 总览</div>
          <div style={{ fontSize: 12, color: T.ink3, marginTop: 3, display: 'flex', alignItems: 'center', gap: 8 }}>
            <span><span className="edge-live-dot" style={{ display: 'inline-block', width: 6, height: 6, borderRadius: '50%', background: T.green, marginRight: 5, verticalAlign: 'middle' }}/>实时刷新中</span>
            <span style={{ color: '#cbd5e1' }}>·</span>
            <span>采样间隔 5s</span>
            <span style={{ color: '#cbd5e1' }}>·</span>
            <span>{device.model || ''}</span>
          </div>
        </div>
      </div>

      {/* 4 SimpleResourceCard (AC-2) */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 12, marginBottom: 14 }}>
        <SimpleResourceCard icon="cpu" label="CPU 使用率"
          color={T.blue}
          value={cpu == null ? null : `${cpu}%`}
          sub={cpuCount != null ? `${cpuCount}C 当前负载` : '加载中…'}/>
        <SimpleResourceCard icon="bolt" label="GPU 平均"
          color={T.indigo}
          value={gpus.length > 0 ? (gpuAvg == null ? null : `${gpuAvg}%`) : '无 GPU'}
          sub={gpus.length > 0 ? `${gpus.length} × ${gpus[0].productName}` : '本节点未检测到 NVIDIA GPU'}/>
        <SimpleResourceCard icon="memory" label="内存"
          color={T.teal}
          value={memPct == null ? null : `${memPct}%`}
          sub={memUsed != null && memTotal != null ? `${memUsed} GB / ${memTotal} GB` : '加载中…'}/>
        <SimpleResourceCard icon="hardDrive" label="磁盘"
          color={T.amber}
          value={diskPct == null ? null : `${diskPct}%`}
          sub={diskUsed != null && diskTotal != null ? `${diskUsed} GB / ${diskTotal} GB` : '加载中…'}/>
      </div>

      {/* 跳监控按钮 (AC-3) */}
      <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 14 }}>
        <button
          onClick={() => {
            if (onOpenApp) {
              onOpenApp({ id: 'monitoring', kind: 'system', name: '监控' });
            } else {
              window.alert('监控 App 尚未就绪')
            }
          }}
          className="edge-press edge-btn-primary"
          style={{
            ...btnPrimary,
            height: 32, padding: '0 14px', fontSize: 12.5,
          }}>
          <Icon name="sparkle" size={13} stroke={1.8}/>进入监控查看详细曲线 →
        </button>
      </div>

      {/* 应用摘要 (Row 4 保留不动) */}
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
