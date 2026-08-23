// DiskManager.jsx — 磁盘管理（PRD FR-15 / Story 6.3）
//
// 按 device 分组展示磁盘 + 内嵌分区表（使用情况条形图 + 系统分区 tag + mount options）。
//
// 设计约束（AC-NO-DESTRUCTIVE-UI / AC-NO-KILL-BUTTON）：
//   - **不出现**「立即分区」/ fdisk / parted / mkfs / umount / mount / 格式化 按钮
//   - 所有交互元素 data-action 受白名单约束：仅 view-detail / copy 等只读动作
//
// AC-DF-FAIL-GRACEFUL: usagePct null 显示 "-" + tooltip，**不**应用颜色编码

import { useState, useEffect } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { authFetch } from '../hooks/useApi'

function fmtBytes(n) {
  if (n == null || n < 0) return '-';
  if (n >= 1024 * 1024 * 1024 * 1024) return (n / (1024 * 1024 * 1024 * 1024)).toFixed(1) + ' TB';
  if (n >= 1024 * 1024 * 1024) return (n / (1024 * 1024 * 1024)).toFixed(1) + ' GB';
  if (n >= 1024 * 1024) return (n / (1024 * 1024)).toFixed(0) + ' MB';
  return n + ' B';
}

// I/O 速率格式化：iotop 惯例，低于 1 KB/s 的活动显示为 KB/s 保留小数
function fmtRate(bps) {
  if (bps == null) return '-';
  if (bps < 1024) return bps < 1 ? '0 B/s' : bps.toFixed(0) + ' B/s';
  if (bps < 1024 * 1024) return (bps / 1024).toFixed(1) + ' KB/s';
  if (bps < 1024 * 1024 * 1024) return (bps / (1024 * 1024)).toFixed(1) + ' MB/s';
  return (bps / (1024 * 1024 * 1024)).toFixed(2) + ' GB/s';
}

// 颜色编码：< 70% green, 70-90% amber, >= 90% red, null "-" + 灰
function usageColor(pct) {
  if (pct == null) return { fg: T.ink4, bg: T.surfaceAlt, ind: T.ink4 };
  if (pct >= 90) return { fg: T.red, bg: '#fef2f2', ind: T.red };
  if (pct >= 70) return { fg: T.amber, bg: '#fffbeb', ind: T.amber };
  return { fg: '#059669', bg: '#dcfce7', ind: '#10b981' };
}

const th = {
  padding: '8px 14px', fontSize: 10.5, fontWeight: 600, color: T.ink3,
  letterSpacing: '0.04em', textTransform: 'uppercase',
};
const td = { padding: '10px 14px', fontSize: 12, color: T.ink };

export default function DiskManager() {
  const [disks, setDisks] = useState([]);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(true);
  const [io, setIo] = useState(null); // /api/v1/disks/io: {devices, processes, processesAvailable}
  const [selected, setSelected] = useState('avg'); // 'avg'=全部平均 / 磁盘短名（sda）
  const [history, setHistory] = useState([]); // I/O 时间序列，前端累积（后端只给瞬时值）
	const [hardwareByPath, setHardwareByPath] = useState({});

  useEffect(() => {
    let stopped = false;
    // code-review ENHANCEMENT: setError 调用前必须检查 stopped 防止卸载后写状态；
    // 异常 catch 也要 setError 否则首屏失败会停留在 loading 后误显「未检测到磁盘」
    function safeSetError(msg) { if (!stopped) setError(msg); }

	function load() {
      authFetch('/api/v1/disks')
        .then(async r => {
          if (r.ok) return r.json();
          let body = null;
          try { body = await r.json(); } catch (_) {}
          if (r.status === 500 && body?.code === 'LSBLK_UNAVAILABLE') {
            safeSetError('节点未安装 lsblk，无法读取磁盘信息');
          } else {
            safeSetError('磁盘信息读取失败');
          }
          return null;
        })
        .then(d => { if (!stopped && Array.isArray(d)) { setDisks(d); setError(null); } })
        .catch(() => { safeSetError('磁盘信息读取失败（网络错误）'); })
		.finally(() => { if (!stopped) setLoading(false); });
	  authFetch('/api/v1/hardware').then(r => r.ok ? r.json() : null).then(h => {
		if (!stopped && h?.storage) setHardwareByPath(Object.fromEntries(h.storage.map(d => [d.path, d])));
	  }).catch(() => {});
    }
    load();
    const id = setInterval(load, 30000); // 磁盘变化不频繁，30s 轮询
    return () => { stopped = true; clearInterval(id); };
  }, []);

  // I/O 速率单独 3s 轮询：后端两次请求间做 /proc/diskstats + /proc/<pid>/io
  // 差分，轮询间隔即采样窗口（iotop 的 delay 语义）。失败静默保留上次数据，
  // 不打断磁盘拓扑主视图。
  useEffect(() => {
    let stopped = false;
    function loadIO() {
      authFetch('/api/v1/disks/io')
        .then(r => (r.ok ? r.json() : null))
        .then(d => {
          if (stopped || !d) return;
          setIo(d);
          // 累积时间序列：每盘 r/w/util + 物理盘聚合（平均），最多保留 60 点(~3min)
          setHistory(h => {
            const dev = {};
            let aggR = 0, aggW = 0, utils = [];
            (d.devices || []).forEach(x => {
              const s = x.device.replace(/^\/dev\//, '');
              dev[s] = { read: x.readBps, write: x.writeBps, util: x.utilPct };
              if (x.physical) { aggR += x.readBps; aggW += x.writeBps; utils.push(x.utilPct); }
            });
            const nPhys = utils.length || 1;
            const snap = { t: Date.now(), dev, agg: {
              read: aggR / nPhys, write: aggW / nPhys,
              util: utils.reduce((a, b) => a + b, 0) / nPhys,
            } };
            const next = [...h, snap];
            return next.length > 60 ? next.slice(next.length - 60) : next;
          });
        })
        .catch(() => {});
    }
    loadIO();
    const id = setInterval(loadIO, 3000);
    return () => { stopped = true; clearInterval(id); };
  }, []);

  // 设备名 → 速率映射（后端 device 是 sda 短名，磁盘列表 device 可能是 /dev/sda，归一化匹配）
  const ioByDev = {};
  (io?.devices || []).forEach(d => { ioByDev[d.device.replace(/^\/dev\//, '')] = d; });

  // 物理盘聚合（"平均"卡片 + avg 曲线用）：吞吐取物理盘平均、util 取平均
  const physIO = (io?.devices || []).filter(d => d.physical);
  const nPhys = physIO.length || 1;
  const aggIO = {
    readBps: physIO.reduce((a, d) => a + d.readBps, 0) / nPhys,
    writeBps: physIO.reduce((a, d) => a + d.writeBps, 0) / nPhys,
    utilPct: physIO.reduce((a, d) => a + d.utilPct, 0) / nPhys,
  };

  const activeDev = selected; // 'avg' 或磁盘短名
	const enrichedDisks = disks.map(d => {
	  const h = hardwareByPath[d.device] || {};
	  return { ...d, category: h.category, medium: h.medium, interface: h.interface, health: h.health };
	});
  const activeDisk = activeDev === 'avg' ? null
	: enrichedDisks.find(d => (d.device || d.path || '').replace(/^\/dev\//, '') === activeDev);

  // 选中项的时间序列（read/write），供曲线图渲染
  const series = history.map(s => ({
    t: s.t,
    read: activeDev === 'avg' ? s.agg.read : (s.dev[activeDev]?.read ?? 0),
    write: activeDev === 'avg' ? s.agg.write : (s.dev[activeDev]?.write ?? 0),
  }));

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column',
      background: T.surfaceAlt, overflow: 'hidden' }}>
      {/* Header */}
      <div style={{
        padding: '14px 24px', background: T.surface,
        borderBottom: `1px solid ${T.border}`, flexShrink: 0,
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <div>
            <div style={{ fontSize: 17, fontWeight: 700, color: T.ink, letterSpacing: '-0.01em' }}>
			  磁盘概览
            </div>
            <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3 }}>
              本地 lsblk + df + /proc/mounts · 只读 · 离线可用 · 不可分区 / 卸载 / 格式化
            </div>
          </div>
          <div style={{ flex: 1 }}/>
          {disks.length > 0 && (
            <div style={{ fontSize: 11.5, color: T.ink3 }}>
              共 <span className="mono tnum" style={{ color: T.ink, fontWeight: 700 }}>{disks.length}</span> 块磁盘
            </div>
          )}
        </div>
      </div>

      <div style={{ flex: 1, overflow: 'auto', padding: 16 }}>
        {loading && <div style={{ color: T.ink3, fontSize: 12 }}>加载中...</div>}
        {error && (
          <div style={{
            padding: '10px 14px', borderRadius: 6,
            background: '#fef2f2', border: '1px solid #fecaca',
            color: '#991b1b', fontSize: 12, marginBottom: 14,
          }}>
            {error}
          </div>
        )}
        {!loading && disks.length === 0 && !error && (
          <div style={{ color: T.ink3, fontSize: 12 }}>未检测到磁盘</div>
        )}
        {/* 顶部：统计卡等宽均分 —— [平均] + 每块磁盘一张，点击选中深色高亮 */}
        {disks.length > 0 && (
          <div style={{ display: 'flex', gap: 12, marginBottom: 14 }}>
            <DiskStatCard aggregate io={aggIO} count={physIO.length}
              active={activeDev === 'avg'} onClick={() => setSelected('avg')}/>
			{enrichedDisks.map(d => {
			  const short = (d.device || d.path || '').replace(/^\/dev\//, '');
              return (
                <DiskStatCard key={d.device} disk={d} io={ioByDev[short]}
                  active={short === activeDev}
                  onClick={() => setSelected(short)}/>
              );
            })}
          </div>
        )}
        {/* 下方上下分布：上 = 选中项 I/O 时间曲线(+分区详情)，下 = 可排序进程表 */}
        {disks.length > 0 && (
          <>
            <IOChart series={series}
              title={activeDev === 'avg' ? '全部磁盘（平均）' : '/dev/' + activeDev}
              sampleMs={io?.sampleMs}/>
            {activeDisk && (
              <DiskCard disk={activeDisk}
                io={ioByDev[(activeDisk.device || '').replace(/^\/dev\//, '')]}/>
            )}
            <IOActivityPanel io={io}/>
          </>
        )}
      </div>
    </div>
  );
}

// DiskStatCard — 顶部统计卡（仪表盘"实时资源用量"同款样式），等宽均分。
// aggregate=true 时是"平均"总览卡（物理盘聚合），否则是单块磁盘卡。
// 点击选中，选中卡深色反白。
function DiskStatCard({ disk, io, aggregate, count, active, onClick }) {
  const short = aggregate ? '平均' : (disk.device || '').replace(/^\/dev\//, '');
  const meta = aggregate
    ? `${count} 块物理盘`
	: `${disk.category === 'external' ? '外接' : '内置'} · ${disk.medium || (disk.rotational ? 'HDD' : 'SSD')} · ${fmtBytes(disk.sizeBytes)}`;
  const utilPct = io?.utilPct ?? 0;
  // 深色选中态 / 白色普通态双配色
  const c = active ? {
    bg: '#1e2433', border: '#1e2433', ink: '#fff', ink3: 'rgba(255,255,255,0.55)',
    barBg: 'rgba(255,255,255,0.18)', bar: '#fff',
    read: '#6ee7b7', write: '#fcd34d',
  } : {
    bg: T.surface, border: T.border, ink: T.ink, ink3: T.ink3,
    barBg: T.surfaceAlt, bar: utilPct >= 80 ? T.red : utilPct >= 40 ? T.amber : '#10b981',
    read: '#059669', write: '#d97706',
  };
  return (
    <div onClick={onClick} data-action="view-detail" style={{
      flex: 1, minWidth: 0, cursor: 'pointer', userSelect: 'none',
      background: c.bg, border: `1px solid ${c.border}`, borderRadius: 8,
      padding: '12px 14px',
      boxShadow: active ? '0 2px 8px rgba(15,23,42,0.25)' : 'none',
      transition: 'background 0.15s, box-shadow 0.15s',
    }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 7 }}>
        <Icon name={aggregate ? 'zap' : 'hardDrive'} size={15} stroke={1.8} style={{ color: c.ink3 }}/>
        <span className="mono" style={{ fontSize: 12.5, fontWeight: 700, color: c.ink }}>{short}</span>
        <div style={{ flex: 1 }}/>
        <span style={{ fontSize: 10.5, color: c.ink3 }}>{meta}</span>
      </div>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, marginTop: 8, marginBottom: 9 }}>
        <span className="mono tnum" style={{ fontSize: 16, fontWeight: 700, color: c.read }}>
          ↓ {fmtRate(io?.readBps)}
        </span>
        <span className="mono tnum" style={{ fontSize: 16, fontWeight: 700, color: c.write }}>
          ↑ {fmtRate(io?.writeBps)}
        </span>
      </div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <div style={{ flex: 1, height: 5, borderRadius: 3, background: c.barBg, overflow: 'hidden' }}>
          <div style={{ width: Math.min(utilPct, 100) + '%', height: '100%',
            background: c.bar, transition: 'width 0.3s' }}/>
        </div>
        <span className="mono tnum" title="采样窗口内设备忙的时间占比 (iostat %util)"
          style={{ fontSize: 10, color: c.ink3, minWidth: 46, textAlign: 'right' }}>
          util {utilPct.toFixed(0)}%
        </span>
      </div>
    </div>
  );
}

// IOChart — 选中项读/写速率的时间曲线（前端累积 history 渲染）。
// SVG 用 0..100 归一坐标 + preserveAspectRatio=none 自适应宽度，描边用
// non-scaling-stroke 防止横向拉伸变形；坐标轴文字走 HTML 叠加不受缩放影响。
const READ_C = '#10b981', WRITE_C = '#f59e0b';
function IOChart({ series, title, sampleMs }) {
  const pts = (series || []).filter(Boolean);
  const n = pts.length;
  let dataMax = 0;
  pts.forEach(p => { dataMax = Math.max(dataMax, p.read, p.write); });
  const max = dataMax > 0 ? dataMax * 1.2 : 1024; // 空闲时给 1KB 底避免除零
  const H = 100, W = 100;
  const px = i => (n <= 1 ? 0 : (i / (n - 1)) * W);
  const py = v => H - (v / max) * H;
  const poly = key => pts.map((p, i) => `${px(i).toFixed(2)},${py(p[key]).toFixed(2)}`).join(' ');
  const area = key => n === 0 ? '' :
    `M ${px(0).toFixed(2)},${H} ` +
    pts.map((p, i) => `L ${px(i).toFixed(2)},${py(p[key]).toFixed(2)}`).join(' ') +
    ` L ${px(n - 1).toFixed(2)},${H} Z`;
  const fmtT = t => new Date(t).toLocaleTimeString('zh-CN', { hour12: false });
  return (
    <div style={{
      background: T.surface, border: `1px solid ${T.border}`, borderRadius: 8,
      marginBottom: 14, overflow: 'hidden',
    }}>
      <div style={{
        padding: '10px 16px', borderBottom: `1px solid ${T.borderSoft}`,
        display: 'flex', alignItems: 'center', gap: 10,
      }}>
        <Icon name="zap" size={15} stroke={1.8} style={{ color: T.ink2 }}/>
        <span style={{ fontSize: 12.5, fontWeight: 700, color: T.ink }}>I/O 速率曲线</span>
        <span className="mono" style={{ fontSize: 11, color: T.ink3 }}>{title}</span>
        <div style={{ flex: 1 }}/>
        <Legend color={READ_C} label="读"/>
        <Legend color={WRITE_C} label="写"/>
        <span style={{ fontSize: 10.5, color: T.ink4 }}>
          {((sampleMs || 0) / 1000).toFixed(1)}s 窗口 · 3s 刷新
        </span>
      </div>
      {n < 2 ? (
        <div style={{ padding: '28px 16px', fontSize: 11.5, color: T.ink4, textAlign: 'center' }}>
          采集中…（每 3 秒一个采样点）
        </div>
      ) : (
        <div style={{ display: 'flex', padding: '12px 14px 6px' }}>
          {/* 左 Y 轴刻度 */}
          <div style={{ width: 62, flexShrink: 0, height: 150, position: 'relative',
            fontSize: 10, color: T.ink4, textAlign: 'right', paddingRight: 8 }}>
            <span className="mono tnum" style={{ position: 'absolute', top: -4, right: 8 }}>{fmtRate(max)}</span>
            <span className="mono tnum" style={{ position: 'absolute', top: '50%', right: 8, transform: 'translateY(-50%)' }}>{fmtRate(max / 2)}</span>
            <span className="mono tnum" style={{ position: 'absolute', bottom: -4, right: 8 }}>0</span>
          </div>
          {/* 绘图区 */}
          <div style={{ flex: 1, minWidth: 0 }}>
            <svg width="100%" height="150" viewBox="0 0 100 100" preserveAspectRatio="none"
              style={{ display: 'block', overflow: 'visible' }}>
              {[25, 50, 75].map(g => (
                <line key={g} x1="0" y1={g} x2="100" y2={g}
                  stroke={T.borderSoft} strokeWidth="1" vectorEffect="non-scaling-stroke"/>
              ))}
              <path d={area('write')} fill={WRITE_C} opacity="0.10"/>
              <path d={area('read')} fill={READ_C} opacity="0.12"/>
              <polyline points={poly('write')} fill="none" stroke={WRITE_C}
                strokeWidth="1.5" vectorEffect="non-scaling-stroke"
                strokeLinejoin="round" strokeLinecap="round"/>
              <polyline points={poly('read')} fill="none" stroke={READ_C}
                strokeWidth="1.5" vectorEffect="non-scaling-stroke"
                strokeLinejoin="round" strokeLinecap="round"/>
            </svg>
            {/* 底 X 轴时间刻度 */}
            <div style={{ display: 'flex', justifyContent: 'space-between',
              fontSize: 10, color: T.ink4, marginTop: 4 }}>
              <span className="mono tnum">{fmtT(pts[0].t)}</span>
              <span className="mono tnum">{fmtT(pts[Math.floor((n - 1) / 2)].t)}</span>
              <span className="mono tnum">{fmtT(pts[n - 1].t)}</span>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function Legend({ color, label }) {
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4, fontSize: 10.5, color: T.ink3 }}>
      <span style={{ width: 10, height: 3, borderRadius: 2, background: color }}/>
      {label}
    </span>
  );
}

// IOActivityPanel — iotop 式 Top 进程磁盘读写（/proc/<pid>/io 差分，
// 仅显示有活动的进程）。表头可点击排序（读/写/进程名），系统级 I/O 与磁盘选择无关。
function IOActivityPanel({ io }) {
  const [sortKey, setSortKey] = useState('total'); // total|read|write|name
  const [sortDir, setSortDir] = useState('desc');
  if (!io) return null;
  const procs = [...(io.processes || [])];

  function onSort(key) {
    if (key === sortKey) { setSortDir(d => (d === 'desc' ? 'asc' : 'desc')); }
    else { setSortKey(key); setSortDir(key === 'name' ? 'asc' : 'desc'); }
  }
  const val = (p) => sortKey === 'read' ? p.readBps
    : sortKey === 'write' ? p.writeBps
    : sortKey === 'name' ? p.name
    : (p.readBps + p.writeBps); // total
  procs.sort((a, b) => {
    const va = val(a), vb = val(b);
    let cmp = typeof va === 'string' ? va.localeCompare(vb) : va - vb;
    return sortDir === 'asc' ? cmp : -cmp;
  });

  // 渲染辅助（非组件，避免 render 内新建组件重置状态）
  const sortTh = (label, k, align) => (
    <th onClick={() => onSort(k)} style={{ ...th, textAlign: align, cursor: 'pointer',
      userSelect: 'none', width: align === 'right' ? 100 : undefined }}>
      {label}
      <span style={{ marginLeft: 4, color: sortKey === k ? T.ink2 : 'transparent' }}>
        {sortKey === k ? (sortDir === 'asc' ? '▲' : '▼') : '▾'}
      </span>
    </th>
  );

  return (
    <div style={{
      background: T.surface, border: `1px solid ${T.border}`, borderRadius: 8,
      marginBottom: 14, overflow: 'hidden',
    }}>
      <div style={{
        padding: '10px 16px', borderBottom: `1px solid ${T.borderSoft}`,
        display: 'flex', alignItems: 'center', gap: 8,
      }}>
        <Icon name="cpu" size={15} stroke={1.8} style={{ color: T.ink2 }}/>
        <span style={{ fontSize: 12.5, fontWeight: 700, color: T.ink }}>进程 I/O（iotop）</span>
        <span style={{ fontSize: 10.5, color: T.ink4 }}>
          /proc/&lt;pid&gt;/io 差分 · 系统全局 · 点击表头排序
        </span>
      </div>

      {!io.processesAvailable ? (
        <div style={{ padding: '10px 16px', fontSize: 11.5, color: T.ink4 }}>
          进程级 I/O 需要 root 权限读取 /proc/&lt;pid&gt;/io，当前不可用
        </div>
      ) : procs.length === 0 ? (
        <div style={{ padding: '10px 16px', fontSize: 11.5, color: T.ink4 }}>
          采样窗口内无进程产生磁盘 I/O
        </div>
      ) : (
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12,
          tableLayout: 'fixed' /* 命令行超长时省略号截断而不是撑破列 */ }}>
          <thead>
            <tr style={{ background: '#fafbfc' }}>
              {sortTh('进程', 'name', 'left')}
              {sortTh('磁盘读', 'read', 'right')}
              {sortTh('磁盘写', 'write', 'right')}
            </tr>
          </thead>
          <tbody>
            {procs.map((p, i) => (
              <tr key={p.pid} style={{ borderTop: i ? `1px solid ${T.borderSoft}` : 'none' }}>
                <td style={{ ...td, overflow: 'hidden' }} title={p.cmdline}>
                  <div style={{ display: 'flex', alignItems: 'baseline', gap: 6 }}>
                    <span style={{ fontWeight: 600 }}>{p.name}</span>
                    <span className="mono tnum" style={{ fontSize: 10.5, color: T.ink4 }}>
                      {p.pid}{p.user ? ' · ' + p.user : ''}
                    </span>
                  </div>
                  {p.cmdline && p.cmdline !== p.name && (
                    <div className="mono" style={{
                      fontSize: 10.5, color: T.ink4, marginTop: 1,
                      whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis',
                    }}>
                      {p.cmdline}
                    </div>
                  )}
                </td>
                <td className="mono tnum" style={{ ...td, textAlign: 'right', verticalAlign: 'top',
                  color: p.readBps > 0 ? '#059669' : T.ink4 }}>{fmtRate(p.readBps)}</td>
                <td className="mono tnum" style={{ ...td, textAlign: 'right', verticalAlign: 'top',
                  color: p.writeBps > 0 ? '#d97706' : T.ink4 }}>{fmtRate(p.writeBps)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

// UtilBadge — iostat %util 语义：>= 80% 红（接近饱和）、>= 40% 琥珀、其余灰
function UtilBadge({ pct }) {
  if (pct == null) return null;
  const c = pct >= 80 ? { bg: '#fef2f2', fg: T.red }
    : pct >= 40 ? { bg: '#fffbeb', fg: T.amber }
    : { bg: T.surfaceAlt, fg: T.ink4 };
  return (
    <span className="mono tnum" title="采样窗口内设备忙的时间占比 (iostat %util)" style={{
      padding: '1px 6px', borderRadius: 3, fontSize: 10, fontWeight: 600,
      background: c.bg, color: c.fg,
    }}>
      util {pct.toFixed(0)}%
    </span>
  );
}

function DiskCard({ disk, io }) {
	const health = disk.health || { status: 'unsupported', detail: 'SMART 状态不支持' };
	const healthLabel = { healthy: '健康', warning: '警告', failing: '异常', permission_required: '需要权限', unsupported: '不支持' }[health.status] || '不支持';
	const healthColor = { healthy: '#047857', warning: '#b45309', failing: '#b91c1c', permission_required: '#b45309', unsupported: T.ink3 }[health.status];
  return (
    <div style={{
      background: T.surface, border: `1px solid ${T.border}`, borderRadius: 8,
      marginBottom: 14, overflow: 'hidden',
    }}>
      {/* 磁盘头部 */}
      <div style={{
        padding: '12px 16px', borderBottom: `1px solid ${T.borderSoft}`,
        display: 'flex', alignItems: 'center', gap: 12,
      }}>
        <div style={{
          width: 36, height: 36, borderRadius: 6,
          background: T.surfaceAlt, border: `1px solid ${T.border}`,
          display: 'flex', alignItems: 'center', justifyContent: 'center',
        }}>
          <Icon name="hardDrive" size={18} stroke={1.8} style={{ color: T.ink2 }}/>
        </div>
        <div>
          <div style={{ fontSize: 14, fontWeight: 700, color: T.ink, fontFamily: 'ui-monospace, monospace' }}>
            {/* 后端 device 是 lsblk 的 path（已含 /dev/ 前缀），不要重复拼接 */}
            {disk.device?.startsWith('/dev/') ? disk.device : '/dev/' + disk.device}
          </div>
          <div style={{ fontSize: 11, color: T.ink3, marginTop: 2 }}>
            <span style={{
              display: 'inline-block', padding: '1px 6px', borderRadius: 3,
              background: T.blueSoft, color: T.blueDeep, fontWeight: 600, marginRight: 6,
            }}>{disk.type}</span>
			<span>{disk.model || '未知型号'} · {disk.category === 'external' ? '外接' : '内置'} · {disk.medium || (disk.rotational ? 'HDD' : 'SSD')} · {disk.interface || disk.transport?.toUpperCase() || '接口未知'}</span>
            {disk.serial && (
              <span style={{ marginLeft: 8, color: T.ink4, fontFamily: 'ui-monospace, monospace' }}>
                S/N: {disk.serial}
              </span>
            )}
          </div>
        </div>
		<div style={{ flex: 1 }}/>
		<div title={health.detail} style={{ marginRight: 14, color: healthColor, fontSize: 11.5, fontWeight: 600 }}><StatusMark status={health.status}/> SMART {healthLabel}<div style={{ color: T.ink4, fontWeight: 400, fontSize: 10, marginTop: 2 }}>{health.detail}</div></div>
        {io && (
          <div style={{ textAlign: 'right', marginRight: 18 }}>
            <div style={{ display: 'flex', gap: 10, justifyContent: 'flex-end', alignItems: 'center' }}>
              <span className="mono tnum" style={{ fontSize: 12, fontWeight: 600, color: '#059669' }}>
                ↓ {fmtRate(io.readBps)}
              </span>
              <span className="mono tnum" style={{ fontSize: 12, fontWeight: 600, color: '#d97706' }}>
                ↑ {fmtRate(io.writeBps)}
              </span>
              <UtilBadge pct={io.utilPct}/>
            </div>
            <div style={{ fontSize: 10.5, color: T.ink4, marginTop: 2 }}>实时 I/O</div>
          </div>
        )}
        <div style={{ textAlign: 'right' }}>
          <div className="mono tnum" style={{ fontSize: 14, fontWeight: 700, color: T.ink }}>
            {fmtBytes(disk.sizeBytes)}
          </div>
          <div style={{ fontSize: 10.5, color: T.ink4, marginTop: 2 }}>容量</div>
        </div>
      </div>

      {/* 分区表 */}
      <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
        <thead>
          <tr style={{ background: '#fafbfc' }}>
            <th style={{ ...th, textAlign: 'left' }}>分区</th>
            <th style={{ ...th, textAlign: 'right', width: 90 }}>容量</th>
            <th style={{ ...th, textAlign: 'left', width: 250 }}>使用情况</th>
            <th style={{ ...th, textAlign: 'left' }}>挂载点</th>
            <th style={{ ...th, textAlign: 'left', width: 80 }}>文件系统</th>
            <th style={{ ...th, textAlign: 'left' }}>挂载选项</th>
          </tr>
        </thead>
        <tbody>
          {(disk.partitions || []).map((p, i) => (
            <PartitionRow key={p.name} partition={p} idx={i}/>
          ))}
          {(!disk.partitions || disk.partitions.length === 0) && (
            <tr><td colSpan={6} style={{ ...td, color: T.ink3, textAlign: 'center', padding: 16 }}>
              无分区
            </td></tr>
          )}
        </tbody>
      </table>
    </div>
  );
}

function StatusMark({ status }) { return <span style={{ display: 'inline-block', width: 6, height: 6, borderRadius: '50%', marginRight: 5, background: status === 'healthy' ? T.green : status === 'failing' ? T.red : status === 'warning' || status === 'permission_required' ? T.amber : T.ink4 }}/> }

// containerFSType "中介"分区的 fstype → 用途标签 + tooltip。
// 这些分区物理上不直接挂载，而是承载上层映射设备（LVM LV / LUKS 解密设备 / md raid 阵列）。
// 之前一律标「未挂载」是误导 —— 它们其实在被使用，只是要看 children 才知道用途。
const CONTAINER_FSTYPES = {
  LVM2_member:        { label: 'LVM PV',   tip: '该分区是 LVM 物理卷，承载下方逻辑卷的数据' },
  crypto_LUKS:        { label: 'LUKS',     tip: '该分区是 LUKS 加密容器，解密后挂载在下方设备上' },
  linux_raid_member:  { label: 'RAID 成员', tip: '该分区是 mdadm RAID 阵列的成员盘，阵列在下方设备上' },
};

function PartitionRow({ partition: p, idx }) {
  const color = usageColor(p.usagePct);
  // 三种状态区分：
  //   - LVM PV / LUKS / RAID 等"中介"分区：fstype 标识用途，children 才是真实挂载点
  //   - 未挂载：物理事实，分区存在但没挂到任何路径（swap / 备用分区 / 工厂出厂未初始化），df 本就不会报告
  //   - df 读取失败：已挂载但权限/超时/I/O 错，需关注
  // 前面把三者都显示成 "—" 让人误以为系统出错；分开展示让 LF 一眼分辨
  const containerKind = !p.mountPoint && CONTAINER_FSTYPES[p.fsType];
  const isUnmounted = !p.mountPoint && !containerKind;
  // 非顶层（LVM LV / md device / 解密设备）展示时加缩进 + 「└─」前缀
  // 让用户能直观看出"sda3 → ubuntu--vg-ubuntu--lv"的承载关系
  const isChild = !!p.parent;
  return (
    <tr style={{ borderTop: idx ? `1px solid ${T.borderSoft}` : 'none' }}>
      <td style={{ ...td, fontFamily: 'ui-monospace, monospace' }}>
        {isChild && <span style={{ color: T.ink4, marginRight: 4 }}>└─</span>}
        {p.name}
      </td>
      <td style={{ ...td, textAlign: 'right', fontFamily: 'ui-monospace, monospace' }}>
        {fmtBytes(p.sizeBytes)}
      </td>
      <td style={td}>
        {containerKind ? (
          <span title={containerKind.tip} style={{
            display: 'inline-block', padding: '1px 7px', borderRadius: 3,
            background: '#dbeafe', color: '#1e40af', fontSize: 11, fontWeight: 600,
          }}>
            {containerKind.label}
          </span>
        ) : isUnmounted ? (
          <span title="该分区未挂载到任何路径，df 不报告使用率" style={{ color: T.ink4, fontSize: 11.5 }}>
            未挂载
          </span>
        ) : p.usagePct == null ? (
          <span title="df 读取失败 / 不可用" style={{ color: T.ink4 }}>—</span>
        ) : (
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <div style={{
              flex: 1, height: 6, borderRadius: 3,
              background: color.bg, overflow: 'hidden',
            }}>
              <div style={{
                width: p.usagePct + '%', height: '100%', background: color.ind,
              }}/>
            </div>
            <span className="mono tnum" style={{ fontSize: 11, color: color.fg, fontWeight: 600, minWidth: 40 }}>
              {Number(p.usagePct).toFixed(1)}%
            </span>
          </div>
        )}
      </td>
      <td style={{ ...td, fontFamily: 'ui-monospace, monospace' }}>
        {p.mountPoint || <span style={{ color: T.ink4 }}>—</span>}
        {p.isSystemDisk && (
          // code-review ENHANCEMENT: 改用深色背景以满足 WCAG AA 对比度
          // (白字叠 #f59e0b amber 对比度 ~2.2:1 不达标；改 #b45309 amber-700
          // 对比度 ~4.8:1 达标)
          <span style={{
            marginLeft: 6, padding: '1px 6px', borderRadius: 3,
            background: '#b45309', color: 'white', fontSize: 10, fontWeight: 600,
          }} title="系统分区，谨慎操作">系统</span>
        )}
      </td>
      <td style={{ ...td, fontSize: 11 }}>{p.fsType || '-'}</td>
      <td style={{ ...td, fontSize: 11, color: T.ink2 }}>
        {p.mountOptions && p.mountOptions.length > 0
          ? p.mountOptions.slice(0, 5).join(', ') + (p.mountOptions.length > 5 ? '...' : '')
          : <span style={{ color: T.ink4 }}>—</span>
        }
      </td>
    </tr>
  );
}
