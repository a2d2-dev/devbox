import { useState, useEffect, useRef, useMemo, useLayoutEffect, useCallback, useId } from 'react'
import { useAnimationControls, useDragControls, useMotionValue, useTransform } from 'motion/react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, Chip } from '../components/ui'
import { useAppLogs, useAppVersions, switchAppVersion, appOp, deleteApp, useAppDetail, appActionAsync, useTask } from '../hooks/useApi'
import { motion, project, springs, useMotionPref } from '../motion'
import { useOverlayLayer } from '../overlays/OverlayProvider'
import { btnSecondary, btnPrimary, btnDanger } from './AppWindow'
import TabBar from './TabBar'
import UninstallDialog from './UninstallDialog'
import {
  ComposeOverview, ComposeServices, ComposeLogs, ComposeEditor, ComposeEnv,
  ComposeStorage, ComposeRevisions, ComposeOperations,
} from './ComposeMgmtPanels'
// Story 4.7：「容器 Shell」tab + 渲染分支在 merge commit 693efd5 中被吞，2026-06-22 恢复
import ContainerShellFace from './ContainerShellFace'

function KpiCell({ label, value, tone, mono }) {
  return (
    <div style={{
      padding: '8px 10px', borderRadius: 7,
      background: T.surfaceAlt, border: `1px solid ${T.borderSoft}`,
    }}>
      <div style={{ ...T.type.label, color: T.ink3 }}>{label}</div>
      <div className={mono === false ? '' : 'mono tnum'} style={{
        marginTop: 4, ...T.type.heading, color: tone,
      }}>{value}</div>
    </div>
  );
}

function formatUptime(startTime) {
  if (!startTime) return '—';
  const now = Date.now();
  const start = new Date(startTime).getTime();
  if (isNaN(start)) return '—';
  const diff = Math.max(0, now - start);
  const days = Math.floor(diff / 86400000);
  const hours = Math.floor((diff % 86400000) / 3600000);
  const mins = Math.floor((diff % 3600000) / 60000);
  if (days > 0) return `${days}天 ${hours}h`;
  if (hours > 0) return `${hours}h ${mins}m`;
  return `${mins}m`;
}

function formatContainerID(id) {
  if (!id) return '—';
  const short = id.replace(/^(docker|containerd):\/\//, '');
  return short.length > 12 ? short.slice(0, 12) : short;
}

function MgmtOverview({ app, cpu, gpu }) {
  const isError = app.state === 'error';
  const uptimeSource = app.startedAt || app.createdAt;
  const uptimeText = isError ? '已停止' : formatUptime(uptimeSource);

  const portsText = (app.ports && app.ports.length > 0)
    ? app.ports.map(p => p.hostPort ? `${p.hostPort}→${p.containerPort}` : `${p.containerPort}`).join(', ')
    : '—';

  const mountsText = (app.volumeMounts && app.volumeMounts.length > 0)
    ? app.volumeMounts.map(vm => vm.hostPath ? `${vm.hostPath} → ${vm.mountPath}` : vm.mountPath).join('; ')
    : '—';

  return (
    <div style={{ padding: '14px 18px 18px' }}>
      {/* KPI grid */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8, marginBottom: 14 }}>
        <KpiCell label="CPU"    value={`${cpu.toFixed(0)}%`} tone={T.blue}/>
        <KpiCell label="GPU"    value={isError ? '—' : `${gpu.toFixed(0)}%`} tone={isError ? T.ink4 : T.indigo}/>
        <KpiCell label="运行时长" value={uptimeText} tone={isError ? T.red : T.green} mono={!isError}/>
        <KpiCell label="重启次数" value={`${app.restartCount || 0}`} tone={app.restartCount > 0 ? T.amber : T.green}/>
      </div>

      {/* Quick info table */}
      <div style={{ background: T.surfaceAlt, border: `1px solid ${T.borderSoft}`,
        borderRadius: 8, overflow: 'hidden' }}>
        {[
          ['镜像',     <span className="mono" style={{ fontSize: 11 }}>{app.image || app.desc || '—'}</span>],
          ['容器 ID',  <span className="mono" style={{ fontSize: 11 }}>{formatContainerID(app.containerID)}</span>],
          ['GPU',      app.gpu || 'CPU'],
          ['部署时间', <span className="mono" style={{ fontSize: 11 }}>{app.deployedAt || '—'}</span>],
          ['端口',     <span className="mono" style={{ fontSize: 11 }}>{portsText}</span>],
          ['挂载',     <span className="mono" style={{ fontSize: 11, wordBreak: 'break-all' }}>{mountsText}</span>],
          ['重启策略', app.restartPolicy || '—'],
        ].map(([k, v], i) => (
          <div key={k} style={{
            display: 'flex', padding: '8px 12px',
            borderTop: i ? `1px solid ${T.borderSoft}` : 'none',
            fontSize: 12,
          }}>
            <span style={{ width: 80, color: T.ink3, flexShrink: 0 }}>{k}</span>
            <span style={{ color: T.ink, fontWeight: 500, flex: 1 }}>{v}</span>
          </div>
        ))}
      </div>

      {/* Resource limits */}
      {(app.cpuLimit || app.memLimit) && (
        <div style={{ marginTop: 14 }}>
          <div style={{ ...T.type.label, color: T.ink3, marginBottom: 6 }}>
            资源配额
          </div>
          <div style={{ background: T.surfaceAlt, border: `1px solid ${T.borderSoft}`,
            borderRadius: 8, overflow: 'hidden' }}>
            {[
              app.cpuRequest || app.cpuLimit ? ['CPU', `${app.cpuRequest || '—'} / ${app.cpuLimit || '—'}`] : null,
              app.memRequest || app.memLimit ? ['内存', `${app.memRequest || '—'} / ${app.memLimit || '—'}`] : null,
            ].filter(Boolean).map(([k, v], i) => (
              <div key={k} style={{
                display: 'flex', padding: '8px 12px',
                borderTop: i ? `1px solid ${T.borderSoft}` : 'none',
                fontSize: 12,
              }}>
                <span style={{ width: 80, color: T.ink3, flexShrink: 0 }}>{k}</span>
                <span className="mono" style={{ color: T.ink, fontWeight: 500, flex: 1, fontSize: 11 }}>{v}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Health status bar */}
      <div style={{ marginTop: 14 }}>
        <div style={{ fontSize: 10.5, color: T.ink3, fontWeight: 600,
          letterSpacing: '0.04em', textTransform: 'uppercase', marginBottom: 6 }}>
          当前状态
        </div>
        <div style={{
          padding: '8px 12px', borderRadius: 7,
          background: isError ? '#fef2f2' : '#f0fdf4',
          border: `1px solid ${isError ? '#fecaca' : '#bbf7d0'}`,
          fontSize: 12, fontWeight: 500,
          color: isError ? '#b91c1c' : '#166534',
          display: 'flex', alignItems: 'center', gap: 6,
        }}>
          <StatusDot tone={isError ? 'red' : 'green'} size={7} pulse={isError}/>
          {isError ? '运行异常' : '运行正常'} · {app.ready || 0}/{app.replicas || 1} Pod 就绪
        </div>
      </div>
    </div>
  );
}

// (removed mock series generator — using real API data)

// ─── Reusable metric panel: collapsible line chart with axes ────
function MetricPanel({ open, onToggle, icon, title, valueLabel, series, max,
                       timeLabels, color, avg, peak, emptyHint }) {
  const gradId = 'mgrad-' + useId().replaceAll(':', '');
  const chartH = 96;
  const padL = 32, padR = 6, padT = 8, padB = 18;

  // Measure container width so the SVG (and its coordinate system) tracks
  // the drawer's actual width. Children draw in real CSS pixels — no viewBox,
  // so fontSize="9" stays 9px regardless of container size.
  const containerRef = useRef(null);
  const [chartW, setChartW] = useState(358);
  useEffect(() => {
    if (!open || !containerRef.current) return;
    const el = containerRef.current;
    const update = () => {
      const w = Math.max(120, Math.floor(el.clientWidth));
      setChartW(w);
    };
    update();
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => ro.disconnect();
  }, [open]);

  const innerW = chartW - padL - padR;
  const innerH = chartH - padT - padB;

  // Build path
  const data = series || [];
  const stepX = innerW / Math.max(1, data.length - 1);
  const pts = data.map((v, i) => [
    padL + i * stepX,
    padT + innerH - (Math.min(max, v) / max) * innerH,
  ]);
  const linePath = pts.map((p, i) => `${i ? 'L' : 'M'}${p[0].toFixed(1)} ${p[1].toFixed(1)}`).join(' ');
  const fillPath = pts.length
    ? `${linePath} L ${pts[pts.length-1][0].toFixed(1)} ${padT+innerH} L ${pts[0][0].toFixed(1)} ${padT+innerH} Z`
    : '';

  const yTicks = [0, 0.25, 0.5, 0.75, 1].map(f => f * max);
  const fmtY = (v) => max <= 1 ? v.toFixed(2) : max <= 10 ? v.toFixed(2) : v.toFixed(0);

  return (
    <div style={{
      marginBottom: 8, background: 'white',
      border: `1px solid ${T.border}`, borderRadius: 7, overflow: 'hidden',
    }}>
      {/* Header */}
      <div onClick={onToggle} style={{
        display: 'flex', alignItems: 'center', gap: 6,
        padding: '7px 10px 7px 8px', cursor: 'pointer',
        background: open ? '#fcfdfe' : T.surfaceAlt,
        borderBottom: open ? `1px solid ${T.borderSoft}` : 'none',
      }}>
        <Icon name="chevDown" size={11} stroke={2.2} style={{
          color: T.ink3, transform: open ? 'none' : 'rotate(-90deg)',
          transition: 'transform 0.15s ease',
        }}/>
        <Icon name={icon} size={12} stroke={1.8} style={{ color: T.ink2 }}/>
        <span style={{ fontSize: 11.5, color: T.ink, fontWeight: 600 }}>{title}</span>
        <div style={{ flex: 1 }}/>
        <span style={{ fontSize: 11, color: T.ink2 }}>{valueLabel}</span>
      </div>

      {open && (
        <div ref={containerRef} style={{ padding: '4px 4px 0' }}>
          <svg width={chartW} height={chartH} style={{ display: 'block' }}>
            {/* Y gridlines + labels */}
            {yTicks.map((v, i) => {
              const y = padT + innerH - (v / max) * innerH;
              return (
                <g key={i}>
                  <line x1={padL} x2={chartW - padR} y1={y} y2={y}
                    stroke="#eef1f5" strokeWidth="1"
                    strokeDasharray={i === 0 || i === yTicks.length - 1 ? '0' : '2 3'}/>
                  <text x={padL - 4} y={y + 3} fontSize="9" fill={T.ink4}
                    textAnchor="end" className="mono">{fmtY(v)}</text>
                </g>
              );
            })}
            {/* X axis baseline */}
            <line x1={padL} x2={chartW - padR} y1={padT + innerH} y2={padT + innerH}
              stroke="#e2e8f0" strokeWidth="1"/>

            {/* Fill + line */}
            {pts.length > 0 && (
              <>
                <defs>
                  <linearGradient id={gradId} x1="0" x2="0" y1="0" y2="1">
                    <stop offset="0%"   stopColor={color} stopOpacity="0.18"/>
                    <stop offset="100%" stopColor={color} stopOpacity="0.02"/>
                  </linearGradient>
                </defs>
                <path d={fillPath} fill={`url(#${gradId})`}/>
                <path d={linePath} fill="none" stroke={color} strokeWidth="1.4"
                  strokeLinejoin="round" strokeLinecap="round"/>
              </>
            )}

            {/* X-axis time labels */}
            {timeLabels.map((t, i) => {
              const x = padL + (i / (timeLabels.length - 1)) * innerW;
              return (
                <text key={i} x={x} y={chartH - 4} fontSize="9" fill={T.ink4}
                  textAnchor="middle" className="mono">{t}</text>
              );
            })}

            {/* Empty hint overlay */}
            {emptyHint && (
              <text x={chartW / 2} y={padT + innerH / 2 + 3} fontSize="10"
                fill={T.ink4} textAnchor="middle">{emptyHint}</text>
            )}
          </svg>
          <div style={{
            display: 'flex', gap: 14, padding: '4px 10px 8px 32px',
            fontSize: 10, color: T.ink3,
          }}>
            <span>平均 <span className="mono tnum" style={{ color: T.ink2, fontWeight: 600 }}>{avg}</span></span>
            <span>峰值 <span className="mono tnum" style={{ color: T.ink2, fontWeight: 600 }}>{peak}</span></span>
          </div>
        </div>
      )}
    </div>
  );
}

function MgmtMetrics({ app, metricsData, historyData }) {
  const isError = app.state === 'error';
  const hasGpu = (metricsData.metrics?.gpu?.used || 0) > 0 || (historyData.gpuUtil && historyData.gpuUtil.some(v => v > 0));
  const [openKeys, setOpenKeys] = useState({
    cpu: true, mem: true, netRx: true, netTx: true,
    gpuCompute: true, gpuMem: true,
  });
  const toggle = (k) => setOpenKeys(o => ({ ...o, [k]: !o[k] }));

  const cpuSeries = historyData.cpuPercent || [];
  const memSeries = historyData.memoryPercent || [];
  const gpuUtilSeries = historyData.gpuUtil || [];
  const gpuMemSeries = historyData.gpuMemPercent || [];
  const netSentSeries = useMemo(() => {
    const raw = historyData.netBytesSent || [];
    const ts = historyData.timestamps || [];
    if (raw.length < 2) return [];
    const rates = [];
    for (let i = 1; i < raw.length; i++) {
      const dt = (new Date(ts[i]) - new Date(ts[i - 1])) / 1000;
      rates.push(dt > 0 ? Math.round((raw[i] - raw[i - 1]) / dt / 1024) : 0);
    }
    return rates;
  }, [historyData.netBytesSent, historyData.timestamps]);
  const netRecvSeries = useMemo(() => {
    const raw = historyData.netBytesRecv || [];
    const ts = historyData.timestamps || [];
    if (raw.length < 2) return [];
    const rates = [];
    for (let i = 1; i < raw.length; i++) {
      const dt = (new Date(ts[i]) - new Date(ts[i - 1])) / 1000;
      rates.push(dt > 0 ? Math.round((raw[i] - raw[i - 1]) / dt / 1024) : 0);
    }
    return rates;
  }, [historyData.netBytesRecv, historyData.timestamps]);

  const timeLabels = useMemo(() => {
    const timestamps = historyData.timestamps || [];
    if (!timestamps.length) return ['', '', '', '', ''];
    const step = Math.max(1, Math.floor((timestamps.length - 1) / 5));
    const labels = [];
    for (let i = 0; i < timestamps.length; i += step) {
      const d = new Date(timestamps[i]);
      labels.push(`${String(d.getHours()).padStart(2,'0')}:${String(d.getMinutes()).padStart(2,'0')}`);
    }
    return labels;
  }, [historyData.timestamps]);

  const safeArr = (s) => s && s.length > 0 ? s : [0];
  const last = (s) => { const a = safeArr(s); return a[a.length - 1]; };
  const avg  = (s) => { const a = safeArr(s); return a.reduce((x,y)=>x+y,0) / a.length; };

  return (
    <div style={{ padding: '12px 14px 18px' }}>
      {/* Header */}
      <div style={{
        display: 'flex', alignItems: 'center', gap: 6, marginBottom: 10,
      }}>
        <Icon name="history" size={12} stroke={1.8} style={{ color: T.ink3 }}/>
        <span style={{ fontSize: 11.5, color: T.ink2, fontWeight: 600 }}>系统监控指标</span>
        <span style={{ fontSize: 10, color: T.ink4 }}>· 5s 刷新</span>
        <div style={{ flex: 1 }}/>
      </div>

      <MetricPanel open={openKeys.cpu} onToggle={() => toggle('cpu')}
        icon="cpu" title="CPU 用量 (%)"
        valueLabel={<>当前: <span className="mono tnum">{last(cpuSeries).toFixed(1)}%</span></>}
        series={cpuSeries} max={100} unit="%" timeLabels={timeLabels} color={T.green}
        avg={avg(cpuSeries).toFixed(1) + '%'} peak={Math.max(...safeArr(cpuSeries)).toFixed(1) + '%'}/>

      <MetricPanel open={openKeys.mem} onToggle={() => toggle('mem')}
        icon="memory" title="内存用量 (%)"
        valueLabel={<>当前: <span className="mono tnum">{last(memSeries).toFixed(0)}%</span></>}
        series={memSeries} max={100} unit="%" timeLabels={timeLabels} color={T.green}
        avg={avg(memSeries).toFixed(0) + '%'} peak={Math.max(...safeArr(memSeries)).toFixed(0) + '%'}/>

      <MetricPanel open={openKeys.netRx} onToggle={() => toggle('netRx')}
        icon="arrowDown" title="网络接收 (KB/s)"
        valueLabel={<>当前: <span className="mono tnum">{last(netRecvSeries).toFixed(0)}</span> KB/s</>}
        series={netRecvSeries} max={Math.max(8, Math.max(...safeArr(netRecvSeries)) * 1.15)} unit=""
        timeLabels={timeLabels} color={T.green}
        avg={avg(netRecvSeries).toFixed(0)} peak={Math.max(...safeArr(netRecvSeries)).toFixed(0)}/>

      <MetricPanel open={openKeys.netTx} onToggle={() => toggle('netTx')}
        icon="arrowUp" title="网络发送 (KB/s)"
        valueLabel={<>当前: <span className="mono tnum">{last(netSentSeries).toFixed(0)}</span> KB/s</>}
        series={netSentSeries} max={Math.max(8, Math.max(...safeArr(netSentSeries)) * 1.15)} unit=""
        timeLabels={timeLabels} color={T.green}
        avg={avg(netSentSeries).toFixed(0)} peak={Math.max(...safeArr(netSentSeries)).toFixed(0)}/>

      {hasGpu && (
        <MetricPanel open={openKeys.gpuCompute} onToggle={() => toggle('gpuCompute')}
          icon="bolt" title="GPU 算力 (%)"
          valueLabel={isError
            ? <span style={{ color: T.ink4 }}>当前: 无数据</span>
            : <>当前: <span className="mono tnum">{last(gpuUtilSeries).toFixed(1)}%</span></>}
          series={gpuUtilSeries} max={100} unit="%" timeLabels={timeLabels}
          color={isError ? T.ink4 : T.green}
          avg={isError ? '—' : avg(gpuUtilSeries).toFixed(1) + '%'}
          peak={isError ? '—' : Math.max(...safeArr(gpuUtilSeries)).toFixed(1) + '%'}
          emptyHint={isError ? '进程已退出 · 暂无 GPU 采集' : null}/>
      )}

      {hasGpu && (
        <MetricPanel open={openKeys.gpuMem} onToggle={() => toggle('gpuMem')}
          icon="memory" title="GPU 显存 (%)"
          valueLabel={isError
            ? <span style={{ color: T.ink4 }}>当前: 无数据</span>
            : <>当前: <span className="mono tnum">{last(gpuMemSeries).toFixed(0)}%</span></>}
          series={gpuMemSeries} max={100} unit="%" timeLabels={timeLabels}
          color={isError ? T.ink4 : T.green}
          avg={isError ? '—' : avg(gpuMemSeries).toFixed(0) + '%'}
          peak={isError ? '—' : Math.max(...safeArr(gpuMemSeries)).toFixed(0) + '%'}
          emptyHint={isError ? '进程已退出 · 显存已释放' : null}/>
      )}
    </div>
  );
}

function MgmtLogs({ app }) {
  const { lines, loading } = useAppLogs(app.name, 3000, 200);
  const containerRef = useRef(null);
  const [autoScroll, setAutoScroll] = useState(true);

  useEffect(() => {
    if (autoScroll && containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [lines, autoScroll]);

  const handleScroll = () => {
    if (!containerRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = containerRef.current;
    setAutoScroll(scrollHeight - scrollTop - clientHeight < 40);
  };

  return (
    <div style={{ padding: 14 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
        <Chip tone="green"><StatusDot tone="green" size={6} pulse/>实时日志</Chip>
        <span style={{ fontSize: 10, color: T.ink4 }}>3s 刷新 · 最近 200 行</span>
        <div style={{ flex: 1 }}/>
      </div>
      <div ref={containerRef} onScroll={handleScroll} style={{
        background: '#0b1020', borderRadius: 7, padding: '10px 12px',
        height: 'calc(100vh - 360px)', minHeight: 280, overflowY: 'auto',
        fontFamily: 'ui-monospace, monospace', fontSize: 11, lineHeight: 1.55,
        color: '#cbd5e1', border: '1px solid #0f1729',
      }}>
        {loading && lines.length === 0 && (
          <div style={{ color: '#64748b', padding: '20px 0', textAlign: 'center' }}>加载日志中…</div>
        )}
        {!loading && lines.length === 0 && (
          <div style={{ color: '#64748b', padding: '20px 0', textAlign: 'center' }}>暂无日志</div>
        )}
        {lines.map((line, i) => (
          <div key={i} style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all', color: '#e2e8f0' }}>{line}</div>
        ))}
        <span className="edge-cursor" style={{ color: '#34d399' }}>▍</span>
      </div>
    </div>
  );
}

function MgmtConfig({ app, onUninstallClick }) {
  const isError = app.state === 'error';

  const appSummary = {
    name: app.name || app.id,
    image: app.image || app.desc || '—',
    version: app.version || 'latest',
    namespace: app.namespace || '—',
    nodeName: app.nodeName || '—',
    podName: app.podName || '—',
    restartPolicy: app.restartPolicy || '—',
    ports: (app.ports || []).map(p => `${p.containerPort}/${p.protocol || 'TCP'}${p.hostPort ? ' → ' + p.hostPort : ''}`).join('\n    ') || 'none',
    mounts: (app.volumeMounts || []).map(vm => `${vm.mountPath}${vm.hostPath ? ' ← ' + vm.hostPath : ''}${vm.readOnly ? ' (ro)' : ''}`).join('\n    ') || 'none',
    resources: [
      app.cpuRequest || app.cpuLimit ? `cpu: ${app.cpuRequest || '—'}/${app.cpuLimit || '—'}` : null,
      app.memRequest || app.memLimit ? `memory: ${app.memRequest || '—'}/${app.memLimit || '—'}` : null,
    ].filter(Boolean).join(', ') || '—',
  };

  const configText = `name: ${appSummary.name}
image: ${appSummary.image}
version: ${appSummary.version}
namespace: ${appSummary.namespace}
node: ${appSummary.nodeName}
pod: ${appSummary.podName}
restartPolicy: ${appSummary.restartPolicy}
ports:
    ${appSummary.ports}
volumeMounts:
    ${appSummary.mounts}
resources: ${appSummary.resources}`;

  return (
    <div style={{ padding: 14 }}>
      {/* Pod meta */}
      <div style={{
        background: T.surfaceAlt, border: `1px solid ${T.borderSoft}`,
        borderRadius: 8, padding: 12, marginBottom: 12,
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 10 }}>
          <Icon name="apps" size={13} stroke={1.8} style={{ color: T.indigo }}/>
          <span style={{ fontSize: 12, fontWeight: 700, color: T.ink }}>Pod 调度状态</span>
          <div style={{ flex: 1 }}/>
          <Chip tone={isError ? 'red' : 'green'}>
            <StatusDot tone={isError ? 'red' : 'green'} size={6} pulse={isError}/>
            {isError ? 'Error' : 'Running'}
          </Chip>
        </div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 5, fontSize: 11.5 }}>
          {[
            ['Pod 名称',  <span className="mono">{app.podName || '—'}</span>],
            ['命名空间',  <span className="mono">{app.namespace || '—'}</span>],
            ['节点',     <span className="mono">{app.nodeName || '—'}</span>],
            ['部署时间', <span className="mono">{app.deployedAt || '—'}</span>],
            ['重启次数', <span className="mono tnum">{app.restartCount || 0}</span>],
          ].map(([k, v]) => (
            <div key={k} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <span style={{ color: T.ink3, width: 64, flexShrink: 0 }}>{k}</span>
              <span style={{ color: T.ink, fontWeight: 500, flex: 1 }}>{v}</span>
            </div>
          ))}
        </div>
      </div>

      {/* Config display */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
        <span style={{ fontSize: 11.5, color: T.ink3 }}>应用配置摘要</span>
        <div style={{ flex: 1 }}/>
        <button onClick={() => navigator.clipboard.writeText(configText)} className="edge-press edge-btn-secondary" style={{ ...btnSecondary, height: 24, padding: '0 8px', fontSize: 11 }}>
          <Icon name="copy" size={11} stroke={1.8}/>复制
        </button>
      </div>
      <pre style={{
        margin: 0, background: '#0b1020', color: '#cbd5e1',
        borderRadius: 7, padding: '12px 14px', border: '1px solid #0f1729',
        fontFamily: 'ui-monospace, "JetBrains Mono", monospace',
        fontSize: 11, lineHeight: 1.65, whiteSpace: 'pre',
        overflow: 'auto', maxHeight: 360,
      }}>{configText.split('\n').map((line, i) => {
        const m = line.match(/^(\s*)([\w./-]+)(:.*)$/);
        if (m) return <div key={i}>
          <span>{m[1]}</span>
          <span style={{ color: '#7dd3fc' }}>{m[2]}</span>
          <span>{m[3]}</span>
        </div>;
        return <div key={i}>{line}</div>;
      })}</pre>

      {/* 部署参数（Helm values，用户在应用商店 Install Form 填写） */}
      <DeployValuesSection values={app.deployValues}/>

      {/* 环境变量（Pod env，含 Secret/ConfigMap 解出真值） */}
      <EnvVarsSection env={app.env}/>

      <div style={{ marginTop: 10, padding: 10,
        background: T.blueSoft, border: '1px solid #99c7ff', borderRadius: 7,
        fontSize: 11.5, color: T.ink2, lineHeight: 1.55,
        display: 'flex', gap: 8, alignItems: 'flex-start',
      }}>
        <Icon name="info" size={13} stroke={1.8} style={{ color: T.blueDeep, marginTop: 1, flexShrink: 0 }}/>
        <div>本机 Pod 由 <strong>云端调度器</strong> 下发，本地无法直接修改。
        如需调整资源/端口/挂载，请在云端「应用编排」中重新部署。</div>
      </div>

      {/* Danger zone */}
      <div style={{
        marginTop: 14, borderRadius: 8,
        border: '1px solid #fecaca', overflow: 'hidden',
      }}>
        <div style={{
          padding: '8px 12px',
          background: '#fef2f2', borderBottom: '1px solid #fecaca',
          fontSize: 11.5, fontWeight: 700, color: '#b91c1c',
          letterSpacing: '0.02em',
          display: 'flex', alignItems: 'center', gap: 6,
        }}>
          <Icon name="alertTri" size={12} stroke={2}/>危险操作区
        </div>
        <div style={{ padding: 12, background: 'white',
          display: 'flex', alignItems: 'center', gap: 12 }}>
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 12.5, color: T.ink, fontWeight: 600 }}>卸载应用</div>
            <div style={{ fontSize: 11, color: T.ink3, marginTop: 3, lineHeight: 1.5 }}>
              立即终止 Pod，释放 GPU 与端口；保留 <span className="mono">/workspace</span> 数据
            </div>
          </div>
          <button onClick={onUninstallClick} className="edge-press edge-btn-danger" style={{
            ...btnDanger, height: 30, padding: '0 12px', fontSize: 11.5, flexShrink: 0,
          }}>
            <Icon name="x" size={12} stroke={2}/>卸载
          </button>
        </div>
      </div>
    </div>
  );
}

// ============================================================================
// 配置 tab 扩展：部署参数 (Helm values) + 环境变量 (Pod env) 两段
// LF 2026-06-22 反馈：「应用的配置也应该出现在滑出的侧边栏，比如密码等信息」
// 密码字段（key 含 PASSWORD/PASS/PWD/TOKEN/SECRET/KEY/AUTH）默认 mask，
// 眼睛 toggle 显示，旁边复制按钮直拷明文（用户已登录控制台，能看到 = 合法）
// ============================================================================

const SENSITIVE_KEY_PATTERNS = /password|pass(?!port)|pwd|token|secret|apikey|api_key|auth/i;

function isSensitiveKey(key) {
  if (!key) return false;
  return SENSITIVE_KEY_PATTERNS.test(String(key));
}

// SensitiveField key + value 单行展示，敏感字段默认 mask + 眼睛 toggle + 复制
function SensitiveField({ name, value, source, error }) {
  const sensitive = isSensitiveKey(name);
  const [shown, setShown] = useState(!sensitive);
  const display = error ? '' : (sensitive && !shown ? '••••••••' : String(value ?? ''));

  return (
    <div style={{
      display: 'grid', gridTemplateColumns: '160px 1fr auto',
      gap: 8, padding: '6px 0', alignItems: 'center', borderTop: `1px solid ${T.borderSoft}`,
    }}>
      <div style={{ fontSize: 11.5, color: T.ink2, fontWeight: 500, wordBreak: 'break-all' }}>
        {name}
        {source && (
          <div style={{
            display: 'inline-block', marginLeft: 6, padding: '0 5px', borderRadius: 3,
            background: '#cce4ff', color: '#0043b8', fontSize: 10, fontWeight: 500,
            verticalAlign: 'middle',
          }} title={`来自 ${source}`}>
            {source.split(':')[0]}
          </div>
        )}
      </div>
      <div style={{
        fontFamily: 'ui-monospace, "JetBrains Mono", monospace', fontSize: 11.5,
        color: error ? T.red : T.ink, wordBreak: 'break-all', minWidth: 0,
      }}>
        {error ? <span title={error}>⚠ {error.length > 60 ? error.slice(0, 60) + '…' : error}</span> : display}
      </div>
      <div style={{ display: 'flex', gap: 4, alignItems: 'center' }}>
        {sensitive && !error && (
          <button onClick={() => setShown(s => !s)} title={shown ? '隐藏' : '显示'} style={{
            border: 'none', background: 'transparent', cursor: 'pointer',
            padding: 3, color: shown ? T.blue : T.ink4, display: 'flex',
          }}>
            <Icon name="eye" size={13} stroke={1.8}/>
          </button>
        )}
        {!error && value != null && value !== '' && (
          <button onClick={() => navigator.clipboard?.writeText(String(value))} title="复制明文" style={{
            border: 'none', background: 'transparent', cursor: 'pointer',
            padding: 3, color: T.ink4, display: 'flex',
          }}>
            <Icon name="copy" size={13} stroke={1.8}/>
          </button>
        )}
      </div>
    </div>
  );
}

// EnvVarsSection 渲染 ResolvedEnvVar[] 列表；env 未拉取 (undefined) 或空列表 → 不渲染
function EnvVarsSection({ env }) {
  if (env == null) return null; // detail 未拉取（useAppDetail loading 中）
  if (env.length === 0) {
    return (
      <div style={{ marginTop: 14 }}>
        <div style={{ fontSize: 11.5, color: T.ink3, marginBottom: 6, display: 'flex', alignItems: 'center', gap: 6 }}>
          <Icon name="key" size={12} stroke={1.8}/>环境变量 (env)
        </div>
        <div style={{
          padding: '10px 12px', background: T.surfaceAlt, border: `1px solid ${T.borderSoft}`,
          borderRadius: 7, fontSize: 11.5, color: T.ink3,
        }}>该容器未声明任何环境变量</div>
      </div>
    );
  }
  return (
    <div style={{ marginTop: 14 }}>
      <div style={{ fontSize: 11.5, color: T.ink3, marginBottom: 6, display: 'flex', alignItems: 'center', gap: 6 }}>
        <Icon name="key" size={12} stroke={1.8}/>
        环境变量 (env) · {env.length} 项
      </div>
      <div style={{
        background: T.surface, border: `1px solid ${T.borderSoft}`,
        borderRadius: 7, padding: '0 12px',
      }}>
        {env.map((e, i) => (
          <SensitiveField key={i} name={e.name} value={e.value} source={e.source} error={e.error}/>
        ))}
      </div>
    </div>
  );
}

// DeployValuesSection 渲染部署 Form values；非平铺 map 时 stringify 展示
function DeployValuesSection({ values }) {
  if (values == null) return null; // detail 未拉取或部署时未传 values
  const entries = Object.entries(values);
  if (entries.length === 0) return null;

  return (
    <div style={{ marginTop: 14 }}>
      <div style={{ fontSize: 11.5, color: T.ink3, marginBottom: 6, display: 'flex', alignItems: 'center', gap: 6 }}>
        <Icon name="edit" size={12} stroke={1.8}/>
        部署参数 (Helm values) · {entries.length} 项
      </div>
      <div style={{
        background: T.surface, border: `1px solid ${T.borderSoft}`,
        borderRadius: 7, padding: '0 12px',
      }}>
        {entries.map(([k, v]) => (
          <SensitiveField
            key={k}
            name={k}
            value={typeof v === 'object' && v !== null ? JSON.stringify(v) : v}
          />
        ))}
      </div>
    </div>
  );
}

function MgmtVersions({ app }) {
  const { data, loading, error } = useAppVersions(app.name);
  const [switching, setSwitching] = useState(null);
  const [switchError, setSwitchError] = useState(null);
  const [switchSuccess, setSwitchSuccess] = useState(null);

  const handleSwitch = async (version) => {
    setSwitching(version);
    setSwitchError(null);
    setSwitchSuccess(null);
    try {
      await switchAppVersion(app.name, version);
      setSwitchSuccess(version);
    } catch (err) {
      setSwitchError(err.message || '切换失败');
    } finally {
      setSwitching(null);
    }
  };

  if (loading) {
    return (
      <div style={{ padding: 14, textAlign: 'center', color: T.ink3, fontSize: 12 }}>
        加载版本信息…
      </div>
    );
  }

  if (error || !data) {
    return (
      <div style={{ padding: 14 }}>
        <div style={{
          padding: 12, borderRadius: 7,
          background: '#fffbeb', border: '1px solid #fde68a',
          fontSize: 12, color: '#92400e', display: 'flex', alignItems: 'center', gap: 6,
        }}>
          <Icon name="alertTri" size={13} stroke={1.8}/>
          {error ? '无法获取版本信息' : '暂无版本数据'}
        </div>
      </div>
    );
  }

  const versions = data.versions || [];

  return (
    <div style={{ padding: 14 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
        <Chip tone="blue">
          <Icon name="gear" size={10} stroke={1.8}/>当前版本: {data.currentVersion}
        </Chip>
        <span style={{ fontSize: 10, color: T.ink4 }}>共 {versions.length} 个版本</span>
      </div>

      {switchSuccess && (
        <div style={{
          padding: '8px 12px', borderRadius: 7, marginBottom: 10,
          background: '#f0fdf4', border: '1px solid #bbf7d0',
          fontSize: 12, color: '#166534', display: 'flex', alignItems: 'center', gap: 6,
        }}>
          <StatusDot tone="green" size={6}/>已切换到版本 {switchSuccess}，正在滚动更新…
        </div>
      )}

      {switchError && (
        <div style={{
          padding: '8px 12px', borderRadius: 7, marginBottom: 10,
          background: '#fef2f2', border: '1px solid #fecaca',
          fontSize: 12, color: '#b91c1c', display: 'flex', alignItems: 'center', gap: 6,
        }}>
          <Icon name="alertTri" size={12} stroke={1.8}/>{switchError}
        </div>
      )}

      <div style={{
        background: T.surfaceAlt, border: `1px solid ${T.borderSoft}`,
        borderRadius: 8, overflow: 'hidden',
      }}>
        {/* Table header */}
        <div style={{
          display: 'grid', gridTemplateColumns: '1fr 80px 120px 70px',
          padding: '8px 12px', fontSize: 10.5, color: T.ink3, fontWeight: 600,
          letterSpacing: '0.04em', textTransform: 'uppercase',
          borderBottom: `1px solid ${T.borderSoft}`,
        }}>
          <span>版本号</span>
          <span>状态</span>
          <span>创建时间</span>
          <span style={{ textAlign: 'right' }}>操作</span>
        </div>

        {versions.length === 0 && (
          <div style={{ padding: '20px 12px', textAlign: 'center', color: T.ink4, fontSize: 12 }}>
            暂无版本记录
          </div>
        )}

        {versions.map((v, i) => {
          const statusColor = v.isCurrent ? T.green : v.status === 'Approved' ? T.blue : T.ink4;
          const statusLabel = v.isCurrent ? '当前版本' : v.status === 'Approved' ? '已审核' : v.status;
          const createdDate = v.createdAt ? new Date(v.createdAt).toLocaleString('zh-CN', {
            year: 'numeric', month: '2-digit', day: '2-digit',
            hour: '2-digit', minute: '2-digit',
          }) : '—';

          return (
            <div key={v.version} style={{
              display: 'grid', gridTemplateColumns: '1fr 80px 120px 70px',
              padding: '10px 12px', fontSize: 12, alignItems: 'center',
              borderTop: i > 0 ? `1px solid ${T.borderSoft}` : 'none',
              background: v.isCurrent ? '#f0fdf4' : 'transparent',
            }}>
              <span className="mono" style={{ fontWeight: v.isCurrent ? 700 : 500, color: T.ink }}>
                {v.version}
              </span>
              <span style={{
                fontSize: 11, color: statusColor, fontWeight: 600,
                display: 'inline-flex', alignItems: 'center', gap: 4,
              }}>
                <StatusDot tone={v.isCurrent ? 'green' : v.status === 'Approved' ? 'blue' : 'grey'} size={5}/>
                {statusLabel}
              </span>
              <span className="mono" style={{ fontSize: 11, color: T.ink3 }}>{createdDate}</span>
              <div style={{ textAlign: 'right' }}>
                {v.isCurrent ? (
                  <span style={{ fontSize: 11, color: T.ink4 }}>当前运行中</span>
                ) : v.status === 'Approved' ? (
                  <button
                    onClick={() => handleSwitch(v.version)}
                    disabled={!!switching}
                    className="edge-press edge-btn-primary"
                    style={{
                      ...btnPrimary, height: 26, padding: '0 10px', fontSize: 11,
                      opacity: switching ? 0.6 : 1,
                      cursor: switching ? 'not-allowed' : 'pointer',
                    }}
                  >
                    {switching === v.version ? '切换中…' : '切换'}
                  </button>
                ) : (
                  <span style={{ fontSize: 11, color: T.ink4 }}>未审核</span>
                )}
              </div>
            </div>
          );
        })}
      </div>

      <div style={{
        marginTop: 10, padding: 10,
        background: T.blueSoft, border: '1px solid #99c7ff', borderRadius: 7,
        fontSize: 11.5, color: T.ink2, lineHeight: 1.55,
        display: 'flex', gap: 8, alignItems: 'flex-start',
      }}>
        <Icon name="info" size={13} stroke={1.8} style={{ color: T.blueDeep, marginTop: 1, flexShrink: 0 }}/>
        <div>版本切换会触发<strong>滚动更新</strong>，新版本 Pod 就绪后旧 Pod 自动回收。只能切换到已审核的版本。</div>
      </div>
    </div>
  );
}

function UninstallConfirm({ app, onCancel, onConfirm }) {
  const [typed, setTyped] = useState('');
  const matches = typed === app.name;
  const dialogRef = useRef(null);
  const inputRef = useRef(null);
  const { backdropProps, layerProps } = useOverlayLayer({
    id: 'uninstall-confirm', onDismiss: onCancel, layerRef: dialogRef, initialFocusRef: inputRef, modal: true,
  });

  return (
    <div className="edge-backdrop-in" {...backdropProps} style={{
      position: 'fixed', inset: 0, zIndex: 250,
      background: 'rgba(15,23,41,0.5)',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      backdropFilter: 'blur(4px)', WebkitBackdropFilter: 'blur(4px)',
    }}>
      <div ref={dialogRef} role="alertdialog" aria-modal="true" aria-labelledby="uninstall-title" tabIndex={-1}
        className="edge-fade-in" {...layerProps} style={{
        width: 460, background: 'white', borderRadius: 12,
        boxShadow: '0 28px 64px -12px rgba(15,23,42,0.45)',
        overflow: 'hidden',
      }}>
        <div style={{
          padding: '14px 20px', display: 'flex', alignItems: 'center', gap: 10,
          background: '#fef2f2', borderBottom: '1px solid #fecaca',
        }}>
          <div style={{
            width: 30, height: 30, borderRadius: 8,
            background: T.red, color: 'white',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
          }}>
            <Icon name="alertTri" size={16} stroke={2}/>
          </div>
          <div style={{ flex: 1 }}>
            <div id="uninstall-title" style={{ fontSize: 14, fontWeight: 700, color: T.ink }}>卸载应用</div>
            <div style={{ fontSize: 11.5, color: '#b91c1c', marginTop: 2 }}>此操作不可撤销</div>
          </div>
          <div onClick={onCancel} style={{ width: 28, height: 28, borderRadius: 7, cursor: 'pointer',
            display: 'flex', alignItems: 'center', justifyContent: 'center', color: T.ink3 }}>
            <Icon name="x" size={15} stroke={2}/>
          </div>
        </div>

        <div style={{ padding: '18px 20px 20px' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 14 }}>
            <div style={{
              width: 44, height: 44, borderRadius: 11,
              background: app.bg, color: 'white',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              boxShadow: 'inset 0 1px 0 rgba(255,255,255,0.3)', flexShrink: 0,
            }}>
              <Icon name={app.icon} size={22} stroke={1.6}/>
            </div>
            <div>
              <div style={{ ...T.type.heading, color: T.ink }}>{app.name}</div>
              <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 2 }} className="mono">{app.version} · {app.category}</div>
            </div>
          </div>

          <div style={{
            padding: 12, borderRadius: 8, marginBottom: 14,
            background: T.surfaceAlt, border: `1px solid ${T.borderSoft}`,
          }}>
            <div style={{ fontSize: 12, color: T.ink2, lineHeight: 1.65 }}>
              <strong style={{ color: T.ink }}>将会发生什么</strong>
              <ul style={{ margin: '6px 0 0', paddingLeft: 20, fontSize: 11.5, color: T.ink3 }}>
                <li>立即终止该 Pod，释放占用的 GPU / 内存 / 端口</li>
                <li>删除容器镜像与配置（保留模型权重和数据集）</li>
                <li>云端「应用编排」状态同步为 <span className="mono">Uninstalled</span></li>
                <li>本机暴露的公网链接 <span className="mono">share.edgex.cloud/...</span> 将失效</li>
              </ul>
            </div>
          </div>

          <div style={{ marginBottom: 14 }}>
            <div style={{ fontSize: 12, color: T.ink2, marginBottom: 6 }}>
              请输入应用名称 <span className="mono" style={{
                background: T.surfaceAlt, padding: '1px 6px', borderRadius: 3,
                color: T.ink, fontWeight: 600, border: `1px solid ${T.border}`,
              }}>{app.name}</span> 确认：
            </div>
            <input ref={inputRef} value={typed} onChange={(e) => setTyped(e.target.value)}
              autoFocus
              placeholder={app.name}
              style={{
                width: '100%', height: 38, padding: '0 12px',
                border: `1px solid ${matches ? T.red : T.border}`, borderRadius: 7,
                fontSize: 13, color: T.ink, background: 'white', outline: 'none',
                boxShadow: matches ? `0 0 0 3px ${T.red}22` : 'none',
              }} className="mono"/>
          </div>

          <div style={{ display: 'flex', gap: 8 }}>
            <button onClick={onCancel} style={{
              flex: 1, height: 38, borderRadius: 8,
              background: 'white', border: `1px solid ${T.border}`,
              fontSize: 13, fontWeight: 500, color: T.ink2, cursor: 'pointer',
            }}>取消</button>
            <button onClick={matches ? onConfirm : null} disabled={!matches} style={{
              flex: 1.4, height: 38, borderRadius: 8,
              background: matches ? T.red : '#fca5a5',
              color: 'white', border: 'none',
              fontSize: 13, fontWeight: 600,
              cursor: matches ? 'pointer' : 'not-allowed',
              display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 6,
              opacity: matches ? 1 : 0.7,
            }}>
              <Icon name="x" size={13} stroke={2}/>我已了解，卸载
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

function UninstallProgress({ app }) {
  return (
    <div className="edge-backdrop-in" style={{
      position: 'fixed', inset: 0, zIndex: 250,
      background: 'rgba(15,23,41,0.5)',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      backdropFilter: 'blur(4px)', WebkitBackdropFilter: 'blur(4px)',
    }}>
      <div className="edge-fade-in" style={{
        width: 360, background: 'white', borderRadius: 12,
        boxShadow: '0 28px 64px -12px rgba(15,23,42,0.45)',
        padding: '20px 24px 22px', textAlign: 'center',
      }}>
        <div style={{
          width: 44, height: 44, borderRadius: 11, margin: '0 auto 12px',
          background: app.bg, color: 'white',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          opacity: 0.7,
        }}>
          <Icon name={app.icon} size={22} stroke={1.6}/>
        </div>
        <div style={{ fontSize: 14, fontWeight: 700, color: T.ink, marginBottom: 4 }}>
          正在卸载 {app.name}…
        </div>
        <div style={{ fontSize: 11.5, color: T.ink3, marginBottom: 14 }}>
          释放资源 · 删除容器 · 通知云端
        </div>
        <div style={{ height: 4, borderRadius: 2, background: T.surfaceAlt, overflow: 'hidden' }}>
          <div style={{
            width: '60%', height: '100%', background: T.red,
            animation: 'edgeProgress 2s ease-out forwards',
          }}/>
        </div>
        <style>{`
          @keyframes edgeProgress {
            from { width: 0%; }
            to { width: 100%; }
          }
        `}</style>
      </div>
    </div>
  );
}

export default function AppMgmtDrawer({ app, open, onClose, onUninstall, metricsData, historyData }) {
  const [tab, setTab] = useState('overview');
  const [uninstall, setUninstall] = useState(null); // null | 'confirm' | 'running' | 'done'
  const [operationTaskId, setOperationTaskId] = useState(null);
  const { task: operationTask } = useTask(operationTaskId);
  const pref = useMotionPref();
  const drawerRef = useRef(null);
  const drawerWidthRef = useRef(980);
  const releaseVelocity = useRef(0);
  const dragControls = useDragControls();
  const controls = useAnimationControls();
  const x = useMotionValue(open ? 0 : 1000);
  const [drawerWidth, setDrawerWidth] = useState(980);
  const backdropOpacity = useTransform(x, [0, drawerWidth || 1], [0.18, 0]);
  const closeRef = useRef(null);
  const { backdropProps, layerProps } = useOverlayLayer({
    id: 'app-management',
    onDismiss: onClose,
    layerRef: drawerRef,
    initialFocusRef: closeRef,
    active: open,
  });

  const measureDrawer = useCallback(() => {
    const width = drawerRef.current?.getBoundingClientRect().width;
    return width || drawerWidthRef.current;
  }, []);

  useLayoutEffect(() => {
    const width = measureDrawer();
    drawerWidthRef.current = width;
    if (pref.reduced) {
      x.set(0);
    } else if (!open) {
      x.set(width);
    }
  }, [open, pref.reduced, x, measureDrawer]);

  useEffect(() => {
    const width = measureDrawer();
    drawerWidthRef.current = width;

    if (pref.reduced) {
      x.set(0);
      controls.start({
        opacity: open ? 1 : 0,
        transition: { duration: 0.2 },
      });
      return;
    }

    const velocity = releaseVelocity.current;
    releaseVelocity.current = 0;
    controls.start({
      x: open ? 0 : width,
      opacity: 1,
      transition: velocity
        ? { ...springs.momentum, velocity }
        : springs.default,
    });
  }, [open, pref.reduced, controls, x, measureDrawer]);

  useEffect(() => {
    if (!drawerRef.current) return undefined;
    const observer = new ResizeObserver(() => {
      const width = measureDrawer();
      drawerWidthRef.current = width;
      setDrawerWidth(width);
      if (!open && !pref.reduced) x.set(width);
    });
    observer.observe(drawerRef.current);
    return () => observer.disconnect();
  }, [open, pref.reduced, x, measureDrawer]);

  // drawer 打开时拉 detail（含 env + deployValues），与 base app prop merge。
  // 关闭传 null 跳过 fetch，避免列表态拖累后端
  const { data: detail } = useAppDetail(open && app?.id ? app.id : null);
  const appWithDetail = useMemo(() => {
    if (!app) return null;
    if (!detail) return app;
    // detail 字段补到 app；base app（来自 useApps）字段优先（id/name 等已稳定）
    return { ...app, env: detail.env, deployValues: detail.deployValues };
  }, [app, detail]);

  const safeMetrics = metricsData || { metrics: {}, gpus: [] };
  const safeHistory = historyData || {};

  const cpu = safeMetrics.metrics?.cpu?.used || 0;
  const gpu = safeMetrics.metrics?.gpu?.used || 0;

  if (!app) return null;

  const isCompose = app.runtime === 'compose';
  const phase = isCompose ? (app.observed?.phase || 'unknown') : (app.state === 'error' ? 'failed' : 'running');
  const isError = phase === 'failed' || phase === 'degraded';
  const composeState = {
    running: { tone: 'green', label: '运行中', dot: 'green', pulse: false },
    degraded: { tone: 'amber', label: '运行降级', dot: 'yellow', pulse: true },
    failed: { tone: 'red', label: '运行异常', dot: 'red', pulse: true },
    stopped: { tone: 'gray', label: '已停止', dot: 'gray', pulse: false },
    pending: { tone: 'blue', label: '等待中', dot: 'blue', pulse: true },
    deploying: { tone: 'blue', label: '部署中', dot: 'blue', pulse: true },
    removing: { tone: 'amber', label: '卸载中', dot: 'yellow', pulse: true },
    unknown: { tone: 'gray', label: '未知', dot: 'gray', pulse: false },
  };
  const stateCfg = isCompose ? (composeState[phase] || composeState.unknown) : isError
    ? { tone: 'red', label: '运行异常', dot: 'red', pulse: true }
    : { tone: 'green', label: '运行中', dot: 'green', pulse: false };

  const tabs = isCompose ? [
    { id: 'overview', label: '概览', icon: 'info' },
    { id: 'services', label: '服务', icon: 'apps' },
    { id: 'logs', label: '日志', icon: 'terminal' },
    { id: 'compose', label: 'Compose', icon: 'code' },
    { id: 'env', label: '环境变量', icon: 'gear' },
    { id: 'storage', label: '存储', icon: 'database' },
    { id: 'revisions', label: '版本', icon: 'refresh' },
    { id: 'operations', label: '操作记录', icon: 'clock' },
  ] : [
    { id: 'overview', label: '概览',  icon: 'info'      },
    { id: 'metrics',  label: '指标',  icon: 'dashboard' },
    { id: 'logs',     label: '日志',  icon: 'terminal'  },
    { id: 'shell',    label: '容器 Shell', icon: 'terminal' }, // Story 4.7（恢复自 5ec8beb）
    { id: 'versions', label: '版本',  icon: 'refresh'   },
    { id: 'config',   label: '配置',  icon: 'gear'      },
  ];
  const activeTab = tabs.some((item) => item.id === tab) ? tab : 'overview';

  return (
    <>
      {/* Backdrop */}
      <motion.div
        {...backdropProps}
        animate={pref.reduced ? { opacity: open ? 0.18 : 0 } : undefined}
        transition={{ duration: 0.2 }}
        style={{
          position: 'absolute', inset: 0, zIndex: 40,
          background: 'rgb(15,23,42)',
          opacity: pref.reduced ? 0 : backdropOpacity,
          pointerEvents: open ? 'auto' : 'none',
        }}
      />

      {/* Drawer */}
      <motion.div
        ref={drawerRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="app-management-title"
        tabIndex={-1}
        {...layerProps}
        drag={pref.reduced ? false : 'x'}
        dragControls={dragControls}
        dragListener={false}
        dragMomentum={false}
        dragConstraints={{ left: 0, right: drawerWidth }}
        dragElastic={{ left: 0.08, right: 0 }}
        animate={controls}
        onDragEnd={(_, info) => {
          const width = measureDrawer();
          drawerWidthRef.current = width;
          setDrawerWidth(width);
          const projected = info.offset.x + project(info.velocity.x);
          if (projected > width / 2 || info.velocity.x > 500) {
            releaseVelocity.current = info.velocity.x;
            onClose();
            return;
          }
          controls.start({
            x: 0,
            transition: { ...springs.momentum, velocity: info.velocity.x },
          });
        }}
        style={{
        position: 'absolute', top: 0, right: 0, bottom: 0,
        // clamp(640, 65%, 980) 让 drawer 跟窗口尺寸响应式（最少 640 让版本 tab
        // 镜像 URL + tab 头部 5 个 tab 不挤；最多 980 防止超大屏占满）
        width: 'clamp(640px, 65%, 980px)', background: T.surface, zIndex: 41,
        borderLeft: `1px solid ${T.border}`,
        boxShadow: '-10px 0 30px -8px rgba(15,23,42,0.22)',
        x: pref.reduced ? 0 : x,
        opacity: pref.reduced ? 0 : 1,
        pointerEvents: open ? 'auto' : 'none',
        display: 'flex', flexDirection: 'column', overflow: 'hidden',
      }}>
        {/* Header */}
        <div style={{
          padding: '14px 18px 0', flexShrink: 0,
          borderBottom: `1px solid ${T.borderSoft}`,
          background: 'linear-gradient(180deg, #fafbfc, white)',
        }}>
          <div
            onPointerDown={(event) => {
              if (!pref.reduced) dragControls.start(event);
            }}
            style={{
              display: 'flex', alignItems: 'center', gap: 12, marginBottom: 14,
              cursor: pref.reduced ? 'default' : 'grab', touchAction: 'none',
            }}
          >
            <div style={{
              width: 44, height: 44, borderRadius: 11,
              background: app.bg, color: 'white',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              boxShadow: `0 4px 12px -2px ${app.color}55, inset 0 1px 0 rgba(255,255,255,0.4)`,
              flexShrink: 0,
            }}>
              <Icon name={app.icon} size={22} stroke={1.6}/>
            </div>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                <div id="app-management-title" style={{ ...T.type.heading, color: T.ink }}>{app.name}</div>
                <Chip tone={stateCfg.tone}>
                  <StatusDot tone={stateCfg.dot} size={6} pulse={stateCfg.pulse}/>{stateCfg.label}
                </Chip>
              </div>
              <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3,
                display: 'flex', alignItems: 'center', gap: 6 }}>
                <span className="mono">{isCompose ? `Docker Compose · r${app.revision || 0}` : app.version}</span>
                <span style={{ color: '#cbd5e1' }}>·</span>
                <span>{app.category || '应用'}</span>
                <span style={{ color: '#cbd5e1' }}>·</span>
                <span>{app.gpu || 'CPU'}</span>
              </div>
            </div>
            <button ref={closeRef} aria-label="关闭应用管理" onPointerDown={(event) => event.stopPropagation()} onClick={onClose} className="edge-press edge-menu-item" style={{
              width: 28, height: 28, borderRadius: 7, cursor: 'pointer',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              color: T.ink3, background: 'transparent', border: 'none',
            }}>
              <Icon name="x" size={15} stroke={2}/>
            </button>
          </div>

          {/* Tabs */}
          <TabBar
            tabs={tabs}
            active={activeTab}
            onChange={setTab}
            style={{ gap: 2, overflowX: 'auto', scrollbarWidth: 'thin' }}
            itemStyle={{ padding: '8px 12px 10px', fontSize: 12.5, marginBottom: -1, gap: 5, flexShrink: 0 }}
            renderLabel={(t2) => (
              <>
                <Icon name={t2.icon} size={12} stroke={1.8}/>{t2.label}
              </>
            )}
          />
        </div>

        {/* Body */}
        <div style={{ flex: 1, overflowY: 'auto' }}>
          {isCompose && operationTask && (
            <div style={{ margin: '10px 14px 0', padding: '8px 10px', borderRadius: 7, background: operationTask.status === 'failed' ? '#fef2f2' : '#e6f4ff', border: `1px solid ${operationTask.status === 'failed' ? '#fecaca' : '#99c7ff'}`, color: operationTask.status === 'failed' ? '#b91c1c' : '#0043b8', fontSize: 11.5 }}>
              操作任务 {operationTask.status || 'queued'} · {operationTask.phase || 'queued'}{operationTask.message ? ` · ${operationTask.message}` : ''}
            </div>
          )}
          {isCompose ? (
            <>
              {activeTab === 'overview' && <ComposeOverview app={app}/>}
              {activeTab === 'services' && <ComposeServices app={app}/>}
              {activeTab === 'logs' && <ComposeLogs app={app}/>}
              {activeTab === 'compose' && <ComposeEditor app={app}/>}
              {activeTab === 'env' && <ComposeEnv app={app}/>}
              {activeTab === 'storage' && <ComposeStorage app={app}/>}
              {activeTab === 'revisions' && <ComposeRevisions app={app}/>}
              {activeTab === 'operations' && <ComposeOperations app={app}/>}
            </>
          ) : (
            <>
              {activeTab === 'overview' && <MgmtOverview app={app} cpu={cpu} gpu={gpu}/>}
              {activeTab === 'metrics'  && <MgmtMetrics app={app} metricsData={safeMetrics} historyData={safeHistory}/>}
              {activeTab === 'logs' && <MgmtLogs app={app}/>}
              {activeTab === 'shell' && <ContainerShellFace appId={app.id} app={app}/>}
              {activeTab === 'versions' && <MgmtVersions app={app}/>}
              {activeTab === 'config' && <MgmtConfig app={appWithDetail} onUninstallClick={() => setUninstall('confirm')}/>}
            </>
          )}
        </div>

        {/* Footer actions */}
        <div style={{
          padding: '10px 16px', background: T.surfaceAlt,
          borderTop: `1px solid ${T.borderSoft}`, flexShrink: 0,
          display: 'flex', gap: 6, alignItems: 'center',
        }}>
          <div style={{ flex: 1 }}/>
          {isCompose && (
            <button className="edge-press edge-btn-danger" style={{ ...btnDanger, height: 30, padding: '0 10px', fontSize: 11.5 }} onClick={() => setUninstall('confirm')}>
              <Icon name="trash" size={12} stroke={1.8}/>卸载
            </button>
          )}
          {isCompose && phase === 'stopped' ? (
            <button className="edge-press edge-btn-primary" style={{ ...btnPrimary, height: 30, padding: '0 12px', fontSize: 11.5 }}
              onClick={() => appActionAsync(app.id, 'start').then(t => setOperationTaskId(t.id)).catch(e => console.error('Start failed:', e))}>
              <Icon name="play" size={12} stroke={2}/>启动
            </button>
          ) : !isError ? (
            <>
              <button className="edge-press edge-btn-secondary" style={{ ...btnSecondary, height: 30, padding: '0 10px', fontSize: 11.5 }}
                onClick={() => appActionAsync(app.id, 'restart').then(t => setOperationTaskId(t.id)).catch(e => console.error('Restart failed:', e))}>
                <Icon name="refresh" size={12} stroke={1.8}/>重启
              </button>
              <button className="edge-press edge-btn-danger" style={{ ...btnDanger, height: 30, padding: '0 10px', fontSize: 11.5 }}
                onClick={() => appActionAsync(app.id, 'stop').then(t => setOperationTaskId(t.id)).catch(e => console.error('Stop failed:', e))}>
                <Icon name="stop" size={12} stroke={1.8}/>停止
              </button>
            </>
          ) : (
            <button className="edge-press edge-btn-primary" style={{ ...btnPrimary, background: T.red, border: 'none',
              height: 30, padding: '0 12px', fontSize: 11.5 }}
              onClick={() => appOp(app.id, 'restart').catch(e => console.error('Restart failed:', e))}>
              <Icon name="refresh" size={12} stroke={2}/>强制重启
            </button>
          )}
        </div>
      </motion.div>

      {/* Uninstall confirm dialog */}
      {uninstall === 'confirm' && isCompose && (
        <UninstallDialog app={app} trackTask onClose={() => setUninstall(null)} onDone={() => { setUninstall(null); onClose(); onUninstall && onUninstall(app); }}/>
      )}
      {uninstall === 'confirm' && !isCompose && (
        <UninstallConfirm app={app}
          onCancel={() => setUninstall(null)}
          onConfirm={async () => {
            setUninstall('running');
            try {
              await deleteApp(app.id);
            } catch (e) {
              console.error('Uninstall failed:', e);
            }
            setUninstall(null);
            onClose();
            onUninstall && onUninstall(app);
          }}/>
      )}
      {uninstall === 'running' && (
        <UninstallProgress app={app}/>
      )}
    </>
  );
}
