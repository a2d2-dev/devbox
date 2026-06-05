import { useState, useEffect, useRef, useMemo } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, Chip, Sparkline } from '../components/ui'

const btnSecondary = {
  display: 'inline-flex', alignItems: 'center', gap: 5,
  borderRadius: 7, border: `1px solid ${T.border}`,
  background: 'white', color: T.ink2, cursor: 'pointer',
  fontSize: 12.5, fontWeight: 500,
};

const btnPrimary = {
  display: 'inline-flex', alignItems: 'center', gap: 5,
  borderRadius: 7, border: 'none',
  background: T.blue, color: 'white', cursor: 'pointer',
  fontSize: 12.5, fontWeight: 600,
};

const btnDanger = {
  display: 'inline-flex', alignItems: 'center', gap: 5,
  borderRadius: 7, border: 'none',
  background: T.red, color: 'white', cursor: 'pointer',
  fontSize: 12.5, fontWeight: 600,
};

function KpiCell({ label, value, unit, tone, mono }) {
  return (
    <div style={{
      padding: '8px 10px', borderRadius: 7,
      background: T.surfaceAlt, border: `1px solid ${T.borderSoft}`,
    }}>
      <div style={{ fontSize: 10, color: T.ink3, fontWeight: 600,
        letterSpacing: '0.04em', textTransform: 'uppercase' }}>{label}</div>
      <div className={mono === false ? '' : 'mono tnum'} style={{
        marginTop: 4, fontSize: 17, fontWeight: 700, color: tone, letterSpacing: '-0.01em',
      }}>{value}</div>
    </div>
  );
}

function MgmtOverview({ app, cpu, gpu }) {
  const isError = app.state === 'error';
  return (
    <div style={{ padding: '14px 18px 18px' }}>
      {/* KPI grid */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8, marginBottom: 14 }}>
        <KpiCell label="CPU"    value={`${cpu.toFixed(0)}%`} tone={T.blue}/>
        <KpiCell label="GPU"    value={isError ? '—' : `${gpu.toFixed(0)}%`} tone={isError ? T.ink4 : T.indigo}/>
        <KpiCell label="运行时长" value={isError ? '已停止' : '6天 3h'} tone={isError ? T.red : T.green} mono={!isError}/>
        <KpiCell label="QPS"     value={app.qps != null ? `${app.qps}` : '—'} tone={T.amber}/>
      </div>

      {/* Quick info table */}
      <div style={{ background: T.surfaceAlt, border: `1px solid ${T.borderSoft}`,
        borderRadius: 8, overflow: 'hidden' }}>
        {[
          ['镜像',     <span className="mono" style={{ fontSize: 11 }}>registry.edgex.io/{app.preset}:{app.version}</span>],
          ['容器 ID',  <span className="mono" style={{ fontSize: 11 }}>{app.id}-7f8a2c1b</span>],
          ['GPU',      app.gpu],
          ['部署时间', <span className="mono" style={{ fontSize: 11 }}>{app.deployedAt}</span>],
          ['端口',     <span className="mono" style={{ fontSize: 11 }}>{({vscode:8443,jupyter:8888,ollama:11434,vllm:8000,comfyui:8188,sdwebui:7861,openwebui:3000})[app.id] || '—'}</span>],
          ['挂载',     <span className="mono" style={{ fontSize: 11 }}>/workspace → /workspace</span>],
          ['重启策略', 'unless-stopped'],
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

      {/* Health check */}
      <div style={{ marginTop: 14 }}>
        <div style={{ fontSize: 10.5, color: T.ink3, fontWeight: 600,
          letterSpacing: '0.04em', textTransform: 'uppercase', marginBottom: 6 }}>
          健康检查 · 最近 24h
        </div>
        <div style={{ display: 'flex', gap: 2, marginTop: 4 }}>
          {Array.from({ length: 48 }).map((_, i) => {
            const fail = isError ? i >= 42 : i === 32;
            return (
              <div key={i} style={{
                flex: 1, height: 18, borderRadius: 2,
                background: fail ? (isError ? T.red : T.amber) : T.green,
                opacity: 0.85,
              }}/>
            );
          })}
        </div>
        <div style={{ display: 'flex', justifyContent: 'space-between',
          marginTop: 5, fontSize: 10, color: T.ink3 }} className="mono">
          <span>-24h</span><span>-12h</span><span>现在</span>
        </div>
      </div>
    </div>
  );
}

// ─── Per-metric series generator ────────────────────────────────
function makeMetricSeries(seed, base, amp, len = 60) {
  const out = [];
  let v = base;
  for (let i = 0; i < len; i++) {
    const drift = Math.sin(i * 0.27 + seed * 1.3) * amp * 0.55
                + Math.cos(i * 0.13 + seed * 2.1) * amp * 0.35
                + (Math.random() - 0.5) * amp * 0.18;
    v = Math.max(0, base + drift);
    out.push(v);
  }
  return out;
}

// ─── Reusable metric panel: collapsible line chart with axes ────
function MetricPanel({ open, onToggle, icon, title, valueLabel, series, max,
                       timeLabels, color, avg, peak, emptyHint }) {
  const gradId = useMemo(() =>
    'mgrad-' + Math.random().toString(36).slice(2, 9), []);
  const chartW = 358, chartH = 96;
  const padL = 32, padR = 6, padT = 8, padB = 18;
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
        <div style={{ padding: '4px 4px 0' }}>
          <svg width={chartW} height={chartH} style={{ display: 'block', width: '100%', maxWidth: chartW }}>
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

function MgmtMetrics({ app, cpuSeries, gpuSeries }) {
  const isError = app.state === 'error';
  const hasGpu = (app.gpu || '').includes('GPU');
  const [range, setRange] = useState('1h');
  const [openKeys, setOpenKeys] = useState({
    cpu: true, mem: true, netRx: true, netTx: true, netLoss: false,
    gpuCompute: hasGpu, gpuMem: hasGpu,
  });
  const toggle = (k) => setOpenKeys(o => ({ ...o, [k]: !o[k] }));

  // Synthesise per-metric series (60 samples = past 1h @ 1-min)
  const memBase  = ({ vscode: 8, jupyter: 22, ollama: 38, vllm: 0,  comfyui: 24, sdwebui: 18, openwebui: 6, training: 46 })[app.id] ?? 12;
  const rxBase   = ({ vscode: 3, jupyter: 5,  ollama: 28, vllm: 0,  comfyui: 4,  sdwebui: 4,  openwebui: 12, training: 2  })[app.id] ?? 4;
  const txBase   = ({ vscode: 4, jupyter: 6,  ollama: 32, vllm: 0,  comfyui: 5,  sdwebui: 5,  openwebui: 14, training: 3  })[app.id] ?? 4;
  const gpuComputeBase = isError ? 0 : (app.gpuPct || 0);
  const gpuMemBase     = isError ? 0 : Math.min(95, (app.gpuPct || 0) * 1.6 + 12);

  const memSeries        = makeMetricSeries(11, memBase, 4);
  const netRxSeries      = makeMetricSeries(13, rxBase, Math.max(1.5, rxBase * 0.35));
  const netTxSeries      = makeMetricSeries(17, txBase, Math.max(1.5, txBase * 0.35));
  const gpuCompSeries    = hasGpu ? (isError ? Array(60).fill(0) : makeMetricSeries(19, gpuComputeBase, 8)) : null;
  const gpuMemSeries     = hasGpu ? (isError ? Array(60).fill(0) : makeMetricSeries(23, gpuMemBase, 4)) : null;

  // Inject one tiny spike into a network chart for visual interest
  const spike = (s) => { const i = 18; s[i] = s[i] * 1.6 + 2; return s; };
  spike(netRxSeries); spike(netTxSeries);

  const ranges = [
    { id: '15m', label: '过去 15 分钟' },
    { id: '1h',  label: '过去 1 小时'  },
    { id: '6h',  label: '过去 6 小时'  },
    { id: '24h', label: '过去 24 小时' },
  ];
  const activeRange = ranges.find(r => r.id === range);

  // Build time-axis labels for past hour ending at app's "now"
  const now = new Date(2026, 4, 27, 13, 13);
  const timeLabels = [];
  for (let i = 0; i <= 6; i++) {
    const t = new Date(now.getTime() - (60 - i * 10) * 60000);
    timeLabels.push(`${String(t.getHours()).padStart(2,'0')}:${String(t.getMinutes()).padStart(2,'0')}`);
  }

  const last = (s) => s[s.length - 1];
  const avg  = (s) => s.reduce((a,b)=>a+b,0) / s.length;

  return (
    <div style={{ padding: '12px 14px 18px' }}>
      {/* Range selector */}
      <div style={{
        display: 'flex', alignItems: 'center', gap: 6, marginBottom: 10,
      }}>
        <Icon name="history" size={12} stroke={1.8} style={{ color: T.ink3 }}/>
        <span style={{ fontSize: 11.5, color: T.ink2, fontWeight: 600 }}>监控指标</span>
        <div style={{ flex: 1 }}/>
        <div style={{
          display: 'inline-flex', background: T.surfaceAlt,
          border: `1px solid ${T.borderSoft}`, borderRadius: 6, padding: 2,
        }}>
          {ranges.map(r => (
            <button key={r.id} onClick={() => setRange(r.id)} style={{
              padding: '3px 8px', fontSize: 10.5,
              background: range === r.id ? 'white' : 'transparent',
              border: 'none', borderRadius: 4, cursor: 'pointer',
              color: range === r.id ? T.ink : T.ink3, fontWeight: range === r.id ? 600 : 500,
              boxShadow: range === r.id ? '0 1px 2px rgba(15,23,42,0.06)' : 'none',
            }}>{r.label.replace('过去 ', '')}</button>
          ))}
        </div>
        <button style={{
          width: 24, height: 24, borderRadius: 5,
          background: 'white', border: `1px solid ${T.border}`,
          color: T.ink3, cursor: 'pointer',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
        }} title="刷新">
          <Icon name="refresh" size={11} stroke={1.8}/>
        </button>
      </div>

      <MetricPanel open={openKeys.cpu} onToggle={() => toggle('cpu')}
        icon="cpu" title="CPU 用量 (%)"
        valueLabel={<>当前: <span className="mono tnum">{last(cpuSeries).toFixed(1)}%</span> <span style={{ color: T.ink4 }}>·</span> 1 Pods</>}
        series={cpuSeries} max={100} unit="%" timeLabels={timeLabels} color={T.green} avg={avg(cpuSeries).toFixed(1) + '%'} peak={Math.max(...cpuSeries).toFixed(1) + '%'}/>

      <MetricPanel open={openKeys.mem} onToggle={() => toggle('mem')}
        icon="memory" title="内存用量 (%)"
        valueLabel={<>当前: <span className="mono tnum">{last(memSeries).toFixed(0)}%</span> <span style={{ color: T.ink4 }}>·</span> <span className="mono tnum">{(last(memSeries) * 10.4).toFixed(1)} MB</span></>}
        series={memSeries} max={100} unit="%" timeLabels={timeLabels} color={T.green} avg={avg(memSeries).toFixed(0) + '%'} peak={Math.max(...memSeries).toFixed(0) + '%'}/>

      <MetricPanel open={openKeys.netRx} onToggle={() => toggle('netRx')}
        icon="arrowDown" title="网络接收 (包/秒)"
        valueLabel={<>当前: <span className="mono tnum">{last(netRxSeries).toFixed(0)}</span></>}
        series={netRxSeries} max={Math.max(8, Math.max(...netRxSeries) * 1.15)} unit="" timeLabels={timeLabels} color={T.green} avg={avg(netRxSeries).toFixed(1)} peak={Math.max(...netRxSeries).toFixed(0)}/>

      <MetricPanel open={openKeys.netTx} onToggle={() => toggle('netTx')}
        icon="arrowUp" title="网络发送 (包/秒)"
        valueLabel={<>当前: <span className="mono tnum">{last(netTxSeries).toFixed(0)}</span></>}
        series={netTxSeries} max={Math.max(8, Math.max(...netTxSeries) * 1.15)} unit="" timeLabels={timeLabels} color={T.green} avg={avg(netTxSeries).toFixed(1)} peak={Math.max(...netTxSeries).toFixed(0)}/>

      <MetricPanel open={openKeys.netLoss} onToggle={() => toggle('netLoss')}
        icon="alertTri" title="网络丢包 (包/秒)"
        valueLabel={<>接收: <span className="mono tnum">0</span> <span style={{ color: T.ink4 }}>·</span> 发送: <span className="mono tnum">0</span></>}
        series={Array(60).fill(0)} max={1} unit="" timeLabels={timeLabels} color={T.green} avg="0" peak="0"
        emptyHint="近 1 小时无丢包"/>

      {hasGpu && (
        <MetricPanel open={openKeys.gpuCompute} onToggle={() => toggle('gpuCompute')}
          icon="bolt" title="GPU 算力 (%)"
          valueLabel={isError
            ? <span style={{ color: T.ink4 }}>当前: 无数据</span>
            : <>当前: <span className="mono tnum">{last(gpuCompSeries).toFixed(1)}%</span></>}
          series={gpuCompSeries} max={100} unit="%" timeLabels={timeLabels}
          color={isError ? T.ink4 : T.green}
          avg={isError ? '—' : avg(gpuCompSeries).toFixed(1) + '%'}
          peak={isError ? '—' : Math.max(...gpuCompSeries).toFixed(1) + '%'}
          emptyHint={isError ? '进程已退出 · 暂无 GPU 采集' : null}/>
      )}

      {hasGpu && (
        <MetricPanel open={openKeys.gpuMem} onToggle={() => toggle('gpuMem')}
          icon="memory" title="GPU 显存 (%)"
          valueLabel={isError
            ? <span style={{ color: T.ink4 }}>当前: 无数据</span>
            : <>当前: <span className="mono tnum">{last(gpuMemSeries).toFixed(0)}%</span> <span style={{ color: T.ink4 }}>·</span> <span className="mono tnum">{(last(gpuMemSeries) * 1.28).toFixed(1)} GB</span></>}
          series={gpuMemSeries} max={100} unit="%" timeLabels={timeLabels}
          color={isError ? T.ink4 : T.green}
          avg={isError ? '—' : avg(gpuMemSeries).toFixed(0) + '%'}
          peak={isError ? '—' : Math.max(...gpuMemSeries).toFixed(0) + '%'}
          emptyHint={isError ? '进程已退出 · 显存已释放' : null}/>
      )}

      {/* KPI footer for serving apps */}
      {app.qps != null && (
        <div style={{
          marginTop: 14, paddingTop: 12,
          borderTop: `1px solid ${T.borderSoft}`,
        }}>
          <div style={{
            fontSize: 10.5, color: T.ink3, fontWeight: 600,
            letterSpacing: '0.04em', textTransform: 'uppercase', marginBottom: 8,
          }}>服务指标 · 实时</div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 6 }}>
            {[
              { label: 'QPS',        val: `${app.qps}`,   unit: '/s',  tone: T.green },
              { label: 'P99 延迟',   val: `${app.p99}`,   unit: 'ms',  tone: T.indigo },
              { label: '今日请求',   val: '18,423',        unit: '',    tone: T.ink },
              { label: '失败率',     val: '0.02',          unit: '%',   tone: T.green },
            ].map((k, i) => <KpiCell key={i} label={k.label} value={k.val + (k.unit ? ' ' + k.unit : '')} tone={k.tone}/>)}
          </div>
        </div>
      )}
    </div>
  );
}

function sampleLogs(app) {
  const id = app.id;
  if (id === 'vllm') return [
    ['10:23:09.413', 'INFO ', 'loader ', 'loading meta-llama/Llama-3.1-70B-Instruct'],
    ['10:23:09.421', 'INFO ', 'loader ', 'sharding weights across 1 GPU'],
    ['10:23:12.881', 'ERROR', 'cuda   ', 'OOM allocating 4.2GB on device 0'],
    ['10:23:12.901', 'ERROR', 'loader ', 'failed to load weights: torch.cuda.OutOfMemoryError'],
    ['10:23:13.013', 'ERROR', 'super  ', 'process exited with code 137'],
    ['10:23:13.014', 'WARN ', 'super  ', 'restart attempt 1 of 3'],
    ['10:23:24.117', 'ERROR', 'cuda   ', 'OOM allocating 4.2GB on device 0'],
    ['10:23:24.182', 'WARN ', 'super  ', 'restart attempt 2 of 3'],
    ['10:23:48.421', 'ERROR', 'cuda   ', 'OOM allocating 4.2GB on device 0'],
    ['10:23:48.488', 'WARN ', 'super  ', 'restart attempt 3 of 3 failed'],
    ['10:24:08.012', 'ERROR', 'super  ', 'entering recovery mode · manual intervention required'],
  ];
  if (id === 'ollama') return [
    ['14:08:01.103', 'INFO ', 'router ', 'POST /v1/chat/completions model=qwen2.5:14b'],
    ['14:08:01.318', 'INFO ', 'engine ', 'prompt tokens=842 max_new=1024'],
    ['14:08:02.421', 'DEBUG', 'kvcache', 'page=24 hit=18 miss=6 reuse=75%'],
    ['14:08:02.502', 'INFO ', 'engine ', 'gen 28 tok/s · ttft=318ms'],
    ['14:08:03.318', 'INFO ', 'metric ', 'qps=22.4 ttft.p99=412ms'],
  ];
  if (id === 'training') return [
    ['14:08:01.103', 'INFO ', 'train  ', 'step 14300/30000 · loss 1.842'],
    ['14:08:01.318', 'INFO ', 'train  ', 'grad_norm 0.412 · lr 1e-4'],
    ['14:08:11.421', 'INFO ', 'train  ', 'step 14301/30000 · loss 1.821'],
    ['14:08:21.002', 'INFO ', 'train  ', 'step 14302/30000 · loss 1.835'],
    ['14:08:21.502', 'WARN ', 'disk   ', 'free space < 400GB · checkpoints may fail'],
  ];
  return [
    ['14:08:01.103', 'INFO ', 'http   ', `GET / 200 · ${app.name} healthy`],
    ['14:08:11.421', 'INFO ', 'http   ', 'GET /api/status 200'],
    ['14:08:21.002', 'INFO ', 'http   ', 'GET /metrics 200'],
    ['14:08:31.103', 'DEBUG', 'health ', 'liveness probe ok'],
  ];
}

function nextLogLine(app, t) {
  if (app.state === 'error') {
    return [t, 'WARN ', 'super  ', `awaiting manual restart · last failure 3h ago`];
  }
  const pools = {
    vscode:    [['INFO ', 'http   ', 'GET /healthz 200'], ['DEBUG', 'editor ', 'autosave: train.py']],
    jupyter:   [['INFO ', 'kernel ', 'cell execution complete'], ['DEBUG', 'http   ', 'GET /api/sessions 200']],
    ollama:    [['INFO ', 'router ', `POST /v1/chat/completions model=${['llama-3.1-8b','qwen2.5-14b','glm5-9b'][Math.floor(Math.random()*3)]}`], ['INFO ', 'engine ', `gen ${Math.floor(25+Math.random()*8)} tok/s`]],
    comfyui:   [['INFO ', 'queue  ', 'prompt #4281 queued'], ['INFO ', 'sampler', `step ${Math.floor(Math.random()*32)}/32`]],
    sdwebui:   [['INFO ', 'gen    ', `step ${Math.floor(Math.random()*32)}/32 · cfg 7.5`], ['INFO ', 'save   ', '/workspace/outputs/00037.png']],
    openwebui: [['INFO ', 'http   ', `POST /api/chat ${Math.floor(40+Math.random()*30)}ms`]],
    training:  [['INFO ', 'train  ', `step ${14300 + Math.floor(Math.random()*5)} · loss ${(1.8 + Math.random()*0.05).toFixed(3)}`]],
  };
  const pool = pools[app.id] || [['INFO ', 'http   ', 'GET / 200']];
  const [lvl, mod, msg] = pool[Math.floor(Math.random() * pool.length)];
  return [t, lvl, mod, msg];
}

function MgmtLogs({ app }) {
  const [lines, setLines] = useState(() => sampleLogs(app));

  useEffect(() => {
    const id = setInterval(() => {
      const ts = new Date();
      const t = `${String(ts.getHours()).padStart(2,'0')}:${String(ts.getMinutes()).padStart(2,'0')}:${String(ts.getSeconds()).padStart(2,'0')}.${String(ts.getMilliseconds()).padStart(3,'0')}`;
      setLines(L => [...L.slice(-80), nextLogLine(app, t)]);
    }, 1400);
    return () => clearInterval(id);
  }, [app]);

  const levelColor = { INFO: '#34d399', DEBUG: '#94a3b8', WARN: '#fbbf24', ERROR: '#f87171' };

  return (
    <div style={{ padding: 14 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
        <Chip tone="green"><StatusDot tone="green" size={6} pulse/>实时流</Chip>
        <div style={{ flex: 1 }}/>
        <button style={{ ...btnSecondary, height: 24, padding: '0 8px', fontSize: 11 }}>
          <Icon name="search" size={11} stroke={1.8}/>搜索
        </button>
        <button style={{ ...btnSecondary, height: 24, padding: '0 8px', fontSize: 11 }}>
          <Icon name="download" size={11} stroke={1.8}/>导出
        </button>
      </div>
      <div style={{
        background: '#0b1020', borderRadius: 7, padding: '10px 12px',
        height: 'calc(100vh - 360px)', minHeight: 280, overflowY: 'auto',
        fontFamily: 'ui-monospace, monospace', fontSize: 11, lineHeight: 1.55,
        color: '#cbd5e1', border: '1px solid #0f1729',
      }}>
        {lines.slice(-100).map(([t, lvl, mod, msg], i) => (
          <div key={i} style={{ display: 'flex', gap: 8 }}>
            <span style={{ color: '#64748b', flexShrink: 0 }}>{t}</span>
            <span style={{ color: levelColor[lvl.trim()] || '#cbd5e1', fontWeight: 600, flexShrink: 0, width: 38 }}>{lvl.trim()}</span>
            <span style={{ color: '#7dd3fc', flexShrink: 0, width: 46 }}>{mod.trim()}</span>
            <span style={{ color: '#e2e8f0' }}>{msg}</span>
          </div>
        ))}
        <span className="edge-cursor" style={{ color: '#34d399' }}>▍</span>
      </div>
    </div>
  );
}

function MgmtConfig({ app, onUninstallClick }) {
  const portMap = { vscode: [8443, 8443], jupyter: [8888, 8888], ollama: [11434, 11434], vllm: [8000, 8000], comfyui: [8188, 8188], sdwebui: [7861, 7860], openwebui: [3000, 8080] };
  const [hostPort, containerPort] = portMap[app.id] || [8080, 8080];
  const isError = app.state === 'error';

  const podSpec = `apiVersion: v1
kind: Pod
metadata:
  name: ${app.id}-${app.preset}-7f8a
  namespace: dev-zhang
  labels:
    app.edgex.io/name: ${app.id}
    app.edgex.io/preset: ${app.preset}
    app.edgex.io/node: gb10-dev-01
spec:
  nodeSelector:
    edgex.io/node: gb10-dev-01
  restartPolicy: Always
  containers:
  - name: ${app.id}
    image: registry.edgex.io/${app.preset}:${app.version}
    imagePullPolicy: IfNotPresent
    ports:
    - containerPort: ${containerPort}
      protocol: TCP
    resources:
      limits:
        nvidia.com/gpu: "1"
        cpu: "8"
        memory: 16Gi
      requests:
        cpu: "2"
        memory: 4Gi
    env:
    - name: NVIDIA_VISIBLE_DEVICES
      value: "all"
    - name: HF_HOME
      value: "/workspace/.hf-cache"
    - name: TZ
      value: "Asia/Shanghai"
    volumeMounts:
    - name: workspace
      mountPath: /workspace
    - name: models
      mountPath: /models
      readOnly: true
  volumes:
  - name: workspace
    hostPath:
      path: /var/lib/edgex/workspaces/dev-zhang
      type: Directory
  - name: models
    hostPath:
      path: /workspace/models
      type: Directory`;

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
            {isError ? 'CrashLoopBackOff' : 'Running'}
          </Chip>
        </div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 5, fontSize: 11.5 }}>
          {[
            ['Pod 名称',  <span className="mono">{app.id}-{app.preset}-7f8a</span>],
            ['命名空间',  <span className="mono">dev-zhang</span>],
            ['节点',     <span className="mono">gb10-dev-01</span>],
            ['调度时间', <span className="mono">{app.deployedAt}</span>],
            ['重启次数', <span className="mono tnum">{isError ? '3' : '0'}</span>],
          ].map(([k, v]) => (
            <div key={k} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <span style={{ color: T.ink3, width: 64, flexShrink: 0 }}>{k}</span>
              <span style={{ color: T.ink, fontWeight: 500, flex: 1 }}>{v}</span>
            </div>
          ))}
        </div>
      </div>

      {/* YAML */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
        <span style={{ fontSize: 11.5, color: T.ink3 }}>pod.yaml · 云端调度器下发</span>
        <div style={{ flex: 1 }}/>
        <button style={{ ...btnSecondary, height: 24, padding: '0 8px', fontSize: 11 }}>
          <Icon name="copy" size={11} stroke={1.8}/>复制
        </button>
        <button style={{ ...btnSecondary, height: 24, padding: '0 8px', fontSize: 11 }}>
          <Icon name="external" size={11} stroke={1.8}/>云端编辑
        </button>
      </div>
      <pre style={{
        margin: 0, background: '#0b1020', color: '#cbd5e1',
        borderRadius: 7, padding: '12px 14px', border: '1px solid #0f1729',
        fontFamily: 'ui-monospace, "JetBrains Mono", monospace',
        fontSize: 11, lineHeight: 1.65, whiteSpace: 'pre',
        overflow: 'auto', maxHeight: 360,
      }}>{podSpec.split('\n').map((line, i) => {
        if (line.startsWith('#')) return <div key={i} style={{ color: '#6b7280' }}>{line}</div>;
        // YAML key: value
        const m = line.match(/^(\s*-?\s*)([\w\.\/-]+)(:.*)$/);
        if (m) return <div key={i}>
          <span>{m[1]}</span>
          <span style={{ color: '#7dd3fc' }}>{m[2]}</span>
          <span>{m[3].split(/(".*?"|'.*?')/g).map((p, j) =>
            p.match(/^["']/) ? <span key={j} style={{ color: '#fcd34d' }}>{p}</span> : <span key={j}>{p}</span>
          )}</span>
        </div>;
        return <div key={i}>{line}</div>;
      })}</pre>

      <div style={{ marginTop: 10, padding: 10,
        background: T.blueSoft, border: '1px solid #bfdbfe', borderRadius: 7,
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
          <button onClick={onUninstallClick} style={{
            ...btnDanger, height: 30, padding: '0 12px', fontSize: 11.5, flexShrink: 0,
          }}>
            <Icon name="x" size={12} stroke={2}/>卸载
          </button>
        </div>
      </div>
    </div>
  );
}

function UninstallConfirm({ app, onCancel, onConfirm }) {
  const [typed, setTyped] = useState('');
  const matches = typed === app.name;

  return (
    <div className="edge-backdrop-in" onClick={onCancel} style={{
      position: 'fixed', inset: 0, zIndex: 250,
      background: 'rgba(15,23,41,0.5)',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      backdropFilter: 'blur(4px)', WebkitBackdropFilter: 'blur(4px)',
    }}>
      <div onClick={(e) => e.stopPropagation()} className="edge-fade-in" style={{
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
            <div style={{ fontSize: 14, fontWeight: 700, color: T.ink }}>卸载应用</div>
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
              <div style={{ fontSize: 15, fontWeight: 700, color: T.ink }}>{app.name}</div>
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
            <input value={typed} onChange={(e) => setTyped(e.target.value)}
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

export default function AppMgmtDrawer({ app, open, onClose, authed, onRequireAuth, onUninstall }) {
  const [tab, setTab] = useState('overview');
  const [uninstall, setUninstall] = useState(null); // null | 'confirm' | 'running' | 'done'

  // Live mini metrics
  const [cpu, setCpu] = useState(28);
  const [gpu, setGpu] = useState(app?.gpuPct || 0);
  const [cpuSeries, setCpuSeries] = useState(Array.from({length: 30}, (_, i) => 22 + Math.sin(i * 0.3) * 6));
  const [gpuSeries, setGpuSeries] = useState(Array.from({length: 30}, (_, i) => (app?.gpuPct || 0) + Math.sin(i * 0.4) * 5));

  useEffect(() => {
    if (!open || !app) return;
    const id = setInterval(() => {
      const nextCpu = Math.max(5, Math.min(55, cpu + (Math.random() - 0.5) * 6));
      const nextGpu = app.state === 'error' ? 0 : Math.max(5, Math.min(70, (app.gpuPct || 20) + (Math.random() - 0.5) * 8));
      setCpu(nextCpu);
      setGpu(nextGpu);
      setCpuSeries(s => [...s.slice(1), nextCpu]);
      setGpuSeries(s => [...s.slice(1), nextGpu]);
    }, 1800);
    return () => clearInterval(id);
  }, [open, app, cpu]);

  if (!app) return null;

  const isError = app.state === 'error';
  const stateCfg = isError
    ? { tone: 'red',   label: '运行异常', dot: 'red',   pulse: true }
    : { tone: 'green', label: '运行中',   dot: 'green', pulse: false };

  const tabs = [
    { id: 'overview', label: '概览',  icon: 'info'      },
    { id: 'metrics',  label: '指标',  icon: 'dashboard' },
    { id: 'logs',     label: '日志',  icon: 'terminal'  },
    { id: 'config',   label: '配置',  icon: 'gear'      },
  ];

  return (
    <>
      {/* Backdrop */}
      {open && (
        <div onClick={onClose} className="edge-backdrop-in" style={{
          position: 'absolute', inset: 0, zIndex: 40,
          background: 'rgba(15,23,42,0.18)',
        }}/>
      )}

      {/* Drawer */}
      <div style={{
        position: 'absolute', top: 0, right: 0, bottom: 0,
        width: 420, background: T.surface, zIndex: 41,
        borderLeft: `1px solid ${T.border}`,
        boxShadow: open ? '-10px 0 30px -8px rgba(15,23,42,0.22)' : 'none',
        transform: `translateX(${open ? '0' : '100%'})`,
        transition: 'transform 0.28s cubic-bezier(0.2, 0.7, 0.2, 1)',
        display: 'flex', flexDirection: 'column', overflow: 'hidden',
      }}>
        {/* Header */}
        <div style={{
          padding: '14px 18px 0', flexShrink: 0,
          borderBottom: `1px solid ${T.borderSoft}`,
          background: 'linear-gradient(180deg, #fafbfc, white)',
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 14 }}>
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
                <div style={{ fontSize: 15, fontWeight: 700, color: T.ink, letterSpacing: '-0.01em' }}>{app.name}</div>
                <Chip tone={stateCfg.tone}>
                  <StatusDot tone={stateCfg.dot} size={6} pulse={stateCfg.pulse}/>{stateCfg.label}
                </Chip>
              </div>
              <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3,
                display: 'flex', alignItems: 'center', gap: 6 }}>
                <span className="mono">{app.version}</span>
                <span style={{ color: '#cbd5e1' }}>·</span>
                <span>{app.category || '应用'}</span>
                <span style={{ color: '#cbd5e1' }}>·</span>
                <span>{app.gpu || 'CPU'}</span>
              </div>
            </div>
            <button onClick={onClose} style={{
              width: 28, height: 28, borderRadius: 7, cursor: 'pointer',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              color: T.ink3, background: 'transparent', border: 'none',
            }}
            onMouseEnter={(e) => e.currentTarget.style.background = T.surfaceAlt}
            onMouseLeave={(e) => e.currentTarget.style.background = 'transparent'}>
              <Icon name="x" size={15} stroke={2}/>
            </button>
          </div>

          {/* Tabs */}
          <div style={{ display: 'flex', gap: 2 }}>
            {tabs.map(t2 => (
              <div key={t2.id} onClick={() => setTab(t2.id)} style={{
                padding: '8px 12px 10px', fontSize: 12.5, cursor: 'pointer',
                color: tab === t2.id ? T.blueDeep : T.ink3,
                fontWeight: tab === t2.id ? 600 : 500,
                borderBottom: `2px solid ${tab === t2.id ? T.blue : 'transparent'}`,
                marginBottom: -1,
                display: 'flex', alignItems: 'center', gap: 5,
              }}>
                <Icon name={t2.icon} size={12} stroke={1.8}/>{t2.label}
              </div>
            ))}
          </div>
        </div>

        {/* Body */}
        <div style={{ flex: 1, overflowY: 'auto' }}>
          {tab === 'overview' && <MgmtOverview app={app} cpu={cpu} gpu={gpu}/>}
          {tab === 'metrics'  && <MgmtMetrics  app={app} cpuSeries={cpuSeries} gpuSeries={gpuSeries}/>}
          {tab === 'logs'     && <MgmtLogs     app={app}/>}
          {tab === 'config'   && <MgmtConfig   app={app} onUninstallClick={() => setUninstall('confirm')}/>}
        </div>

        {/* Footer actions */}
        <div style={{
          padding: '10px 16px', background: T.surfaceAlt,
          borderTop: `1px solid ${T.borderSoft}`, flexShrink: 0,
          display: 'flex', gap: 6, alignItems: 'center',
        }}>
          {!authed && (
            <span style={{
              fontSize: 11, color: '#b45309', display: 'inline-flex', alignItems: 'center',
              gap: 4, padding: '3px 8px', borderRadius: 999,
              background: '#fffbeb', border: '1px solid #fde68a',
            }}>
              <Icon name="shield" size={11} stroke={1.8}/>需验证
            </span>
          )}
          <div style={{ flex: 1 }}/>
          {!isError ? (
            <>
              <button style={{ ...btnSecondary, height: 30, padding: '0 10px', fontSize: 11.5 }}
                onClick={() => !authed && onRequireAuth(`重启 ${app.name}`)}>
                <Icon name="refresh" size={12} stroke={1.8}/>重启
              </button>
              <button style={{ ...btnDanger, height: 30, padding: '0 10px', fontSize: 11.5 }}
                onClick={() => !authed && onRequireAuth(`停止 ${app.name}`)}>
                <Icon name="stop" size={12} stroke={1.8}/>停止
              </button>
            </>
          ) : (
            <button style={{ ...btnPrimary, background: T.red, border: 'none',
              height: 30, padding: '0 12px', fontSize: 11.5 }}
              onClick={() => !authed && onRequireAuth(`强制重启 ${app.name}`)}>
              <Icon name="refresh" size={12} stroke={2}/>强制重启
            </button>
          )}
        </div>
      </div>

      {/* Uninstall confirm dialog */}
      {uninstall === 'confirm' && (
        <UninstallConfirm app={app}
          onCancel={() => setUninstall(null)}
          onConfirm={() => {
            if (!authed) {
              onRequireAuth(`卸载 ${app.name}`);
              setUninstall(null);
              return;
            }
            setUninstall('running');
            setTimeout(() => {
              setUninstall(null);
              onClose();
              onUninstall && onUninstall(app);
            }, 2000);
          }}/>
      )}
      {uninstall === 'running' && (
        <UninstallProgress app={app}/>
      )}
    </>
  );
}
