import { useMemo, useState, useEffect } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { useVirtualMachines, vmControl, vmConfigure } from '../hooks/useApi'

const statusTone = {
  running: { bg: '#dcfce7', fg: '#047857', dot: '#22c55e' },
  'shut off': { bg: '#f1f5f9', fg: '#475569', dot: '#94a3b8' },
  paused: { bg: '#fef3c7', fg: '#a16207', dot: '#f59e0b' },
  crashed: { bg: '#fee2e2', fg: '#991b1b', dot: '#ef4444' },
  unknown: { bg: '#f1f5f9', fg: '#64748b', dot: '#94a3b8' },
};

function toneFor(state) {
  return statusTone[(state || '').toLowerCase()] || statusTone.unknown;
}

function fmtBytes(bytes) {
  const n = Number(bytes) || 0;
  if (n >= 1024 ** 4) return `${(n / 1024 ** 4).toFixed(1)} TB`;
  if (n >= 1024 ** 3) return `${(n / 1024 ** 3).toFixed(1)} GB`;
  if (n >= 1024 ** 2) return `${(n / 1024 ** 2).toFixed(1)} MB`;
  if (n >= 1024) return `${(n / 1024).toFixed(0)} KB`;
  return `${n} B`;
}

function fmtKiB(kib) {
  return fmtBytes((Number(kib) || 0) * 1024);
}

function pct(used, total) {
  if (!total) return 0;
  return Math.max(0, Math.min(100, Math.round((used / total) * 100)));
}

function StateBadge({ state }) {
  const t = toneFor(state);
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: 6,
      height: 24, padding: '0 9px', borderRadius: 12,
      background: t.bg, color: t.fg, fontSize: 11.5, fontWeight: 700,
    }}>
      <span style={{ width: 7, height: 7, borderRadius: '50%', background: t.dot }}/>
      {state || 'unknown'}
    </span>
  );
}

function Metric({ label, value, sub }) {
  return (
    <div style={{
      minHeight: 76, padding: 12, border: `1px solid ${T.border}`,
      borderRadius: 8, background: T.surface,
    }}>
      <div style={{ fontSize: 11, color: T.ink3, marginBottom: 7 }}>{label}</div>
      <div className="tnum" style={{ fontSize: 20, lineHeight: 1, fontWeight: 750, color: T.ink }}>{value}</div>
      {sub && <div style={{ fontSize: 11, color: T.ink3, marginTop: 7 }}>{sub}</div>}
    </div>
  );
}

function ActionButton({ icon, label, tone = 'neutral', disabled, onClick }) {
  const color = tone === 'danger' ? '#dc2626' : tone === 'ok' ? '#047857' : T.ink2;
  const border = tone === 'danger' ? '#fecaca' : tone === 'ok' ? '#bbf7d0' : T.border;
  const bg = tone === 'danger' ? '#fff7f7' : tone === 'ok' ? '#f0fdf4' : T.surface;
  return (
    <button disabled={disabled} onClick={onClick} title={label} style={{
      height: 32, padding: '0 11px', borderRadius: 7,
      display: 'inline-flex', alignItems: 'center', gap: 7,
      background: disabled ? '#f8fafc' : bg,
      border: `1px solid ${disabled ? T.borderSoft : border}`,
      color: disabled ? T.ink4 : color,
      fontSize: 12, fontWeight: 650,
      cursor: disabled ? 'not-allowed' : 'pointer',
    }}>
      <Icon name={icon} size={13} stroke={2}/>
      {label}
    </button>
  );
}

function Field({ label, children, hint }) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 6, minWidth: 0 }}>
      <span style={{ fontSize: 11.5, color: T.ink3, fontWeight: 650 }}>{label}</span>
      {children}
      {hint && <span style={{ fontSize: 10.5, color: T.ink4 }}>{hint}</span>}
    </label>
  );
}

const inputStyle = {
  height: 34,
  borderRadius: 7,
  border: `1px solid ${T.border}`,
  background: T.surface,
  color: T.ink,
  padding: '0 10px',
  fontSize: 12.5,
  outline: 'none',
  minWidth: 0,
};

function VMRow({ vm, active, onClick }) {
  const memPct = pct(vm.memory?.usedKiB || vm.memory?.actualKiB || 0, vm.memory?.maxKiB || 0);
  const deviceCount = (vm.disks?.length || 0) + (vm.filesystems?.length || 0);
  return (
    <button onClick={onClick} style={{
      width: '100%', padding: 12, borderRadius: 8, textAlign: 'left',
      border: `1px solid ${active ? '#bfdbfe' : T.border}`,
      background: active ? T.blueSoft : T.surface,
      cursor: 'pointer',
      display: 'grid', gridTemplateColumns: '1fr auto', gap: 8,
    }}>
      <div style={{ minWidth: 0 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
          <Icon name="server" size={15} stroke={1.8} style={{ color: active ? T.blueDeep : T.ink2, flexShrink: 0 }}/>
          <span style={{
            fontSize: 13, fontWeight: 750, color: T.ink,
            whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis',
          }}>{vm.name}</span>
        </div>
        <div style={{ fontSize: 11, color: T.ink3, marginTop: 6 }}>
          {vm.vcpus || 0} vCPU · {fmtKiB(vm.memory?.maxKiB)} · {deviceCount} 设备
        </div>
        <div style={{ marginTop: 8, height: 5, borderRadius: 3, background: '#e2e8f0', overflow: 'hidden' }}>
          <div style={{ width: `${memPct}%`, height: '100%', background: active ? T.blue : '#64748b' }}/>
        </div>
      </div>
      <StateBadge state={vm.state}/>
    </button>
  );
}

function guestMountFor(fs, mounts) {
  const source = fs?.target || '';
  const mount = (mounts || []).find(m => m.source === source || m.target === source);
  return mount?.target || source || '-';
}

function DiskTable({ disks }) {
  const rows = (disks || []).filter(d => d.device !== 'cdrom');
  if (!rows.length) {
    return <div style={{ padding: 14, color: T.ink3, fontSize: 12 }}>暂无块设备</div>;
  }
  return (
    <table style={{ width: '100%', borderCollapse: 'collapse' }}>
      <thead>
        <tr style={{ background: '#f8fafc' }}>
          {['目标', '容量', '已分配', '读写', '路径'].map(h => (
            <th key={h} style={{
              padding: '8px 10px', textAlign: h === '路径' ? 'left' : 'right',
              fontSize: 10.5, color: T.ink3, fontWeight: 700,
              borderBottom: `1px solid ${T.borderSoft}`,
            }}>{h}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map(d => (
          <tr key={d.target} style={{ borderTop: `1px solid ${T.borderSoft}` }}>
            <td style={{ padding: '9px 10px', fontSize: 12, fontWeight: 700, color: T.ink }}>{d.target}</td>
            <td className="tnum" style={cellRight}>{fmtBytes(d.capacityBytes)}</td>
            <td className="tnum" style={cellRight}>{fmtBytes(d.physicalBytes || d.allocationBytes)}</td>
            <td className="tnum" style={cellRight}>{fmtBytes(d.readBytes)} / {fmtBytes(d.writeBytes)}</td>
            <td title={d.source} style={{
              padding: '9px 10px', fontSize: 11.5, color: T.ink2,
              fontFamily: 'ui-monospace, monospace', maxWidth: 360,
              whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis',
            }}>{d.source || '-'}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function SharedMountTable({ filesystems, mounts }) {
  const rows = filesystems || [];
  if (!rows.length) {
    return <div style={{ padding: 14, color: T.ink3, fontSize: 12 }}>暂无共享挂载</div>;
  }
  return (
    <table style={{ width: '100%', borderCollapse: 'collapse' }}>
      <thead>
        <tr style={{ background: '#f8fafc' }}>
          {['类型', 'Host 路径', 'Guest 挂载点', '模式'].map(h => (
            <th key={h} style={{
              padding: '8px 10px', textAlign: h === 'Host 路径' ? 'left' : 'right',
              fontSize: 10.5, color: T.ink3, fontWeight: 700,
              borderBottom: `1px solid ${T.borderSoft}`,
            }}>{h}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map(fs => {
          const guestMount = guestMountFor(fs, mounts);
          return (
            <tr key={`${fs.source}-${fs.target}`} style={{ borderTop: `1px solid ${T.borderSoft}` }}>
              <td style={{ padding: '9px 10px', fontSize: 12, fontWeight: 700, color: T.ink, textAlign: 'right' }}>
                {fs.driver || fs.type || '-'}
              </td>
              <td title={fs.source} style={{
                padding: '9px 10px', fontSize: 11.5, color: T.ink2,
                fontFamily: 'ui-monospace, monospace', maxWidth: 520,
                whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis',
              }}>{fs.source || '-'}</td>
              <td title={guestMount} style={{
                padding: '9px 10px', fontSize: 11.5, color: T.ink2, textAlign: 'right',
                fontFamily: 'ui-monospace, monospace', whiteSpace: 'nowrap',
              }}>{guestMount}</td>
              <td style={cellRight}>{fs.accessMode || '-'}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

const cellRight = {
  padding: '9px 10px',
  textAlign: 'right',
  fontSize: 12,
  color: T.ink2,
  fontFamily: 'ui-monospace, monospace',
  whiteSpace: 'nowrap',
};

export default function VirtualMachines() {
  const { data, loading, error, refresh } = useVirtualMachines(5000);
  const [selected, setSelected] = useState('');
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState('');
  const [config, setConfig] = useState({ vcpus: '', memoryMiB: '', autostart: false });

  const vms = Array.isArray(data) ? data : [];
  useEffect(() => {
    if (!selected && vms.length) setSelected(vms[0].name);
    if (selected && vms.length && !vms.some(vm => vm.name === selected)) setSelected(vms[0].name);
  }, [selected, vms]);

  const current = useMemo(() => vms.find(vm => vm.name === selected) || vms[0] || null, [vms, selected]);
  useEffect(() => {
    if (!current) return;
    setConfig({
      vcpus: String(current.vcpus || ''),
      memoryMiB: String(Math.round((current.memory?.maxKiB || 0) / 1024)),
      autostart: !!current.autostart,
    });
  }, [current?.name, current?.vcpus, current?.memory?.maxKiB, current?.autostart]);

  const runningCount = vms.filter(vm => (vm.state || '').toLowerCase() === 'running').length;
  const shutoffCount = vms.filter(vm => (vm.state || '').toLowerCase() === 'shut off').length;
  const mem = current?.guest?.memory;
  const memUsed = mem ? mem.usedBytes : (current?.memory?.usedKiB || current?.memory?.actualKiB || 0) * 1024;
  const memTotal = mem ? mem.totalBytes : (current?.memory?.maxKiB || 0) * 1024;
  const memAvail = mem ? mem.availableBytes : (current?.memory?.usableKiB || current?.memory?.availableKiB || 0) * 1024;
  const load = current?.guest?.loadAverage || [];
  const ips = current?.interfaces?.map(i => i.address).filter(Boolean) || [];
  const isRunning = (current?.state || '').toLowerCase() === 'running';
  const isOff = (current?.state || '').toLowerCase() === 'shut off';

  const runAction = async (action) => {
    if (!current || busy) return;
    setBusy(action);
    setMessage('');
    try {
      await vmControl(current.name, action);
      setMessage(`${current.name}: ${action} 已提交`);
      refresh();
    } catch (err) {
      setMessage(err.message || '操作失败');
    } finally {
      setBusy('');
    }
  };

  const saveConfig = async () => {
    if (!current || busy) return;
    const vcpus = Number(config.vcpus);
    const memoryMiB = Number(config.memoryMiB);
    if (!Number.isInteger(vcpus) || vcpus < 1 || vcpus > 128) {
      setMessage('vCPU 必须是 1 到 128 的整数');
      return;
    }
    if (!Number.isInteger(memoryMiB) || memoryMiB < 512) {
      setMessage('内存必须是不小于 512 的 MiB 整数');
      return;
    }
    setBusy('config');
    setMessage('');
    try {
      await vmConfigure(current.name, { vcpus, memoryMiB, autostart: !!config.autostart });
      setMessage(isRunning ? '配置已保存；运行中 VM 需要重启后完全生效' : '配置已保存');
      refresh();
    } catch (err) {
      setMessage(err.message || '保存配置失败');
    } finally {
      setBusy('');
    }
  };

  return (
    <div style={{
      flex: 1, width: '100%', minWidth: 0, height: '100%',
      display: 'flex', flexDirection: 'column', background: T.surfaceAlt, overflow: 'hidden',
    }}>
      <div style={{
        height: 62, padding: '0 18px', borderBottom: `1px solid ${T.border}`,
        background: T.surface, display: 'flex', alignItems: 'center', gap: 14, flexShrink: 0,
      }}>
        <Icon name="server" size={18} stroke={1.8} style={{ color: T.ink2 }}/>
        <div>
          <div style={{ fontSize: 16, fontWeight: 780, color: T.ink }}>虚拟机管理</div>
          <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 2 }}>
            libvirt 本机实例 · {runningCount} 运行 · {shutoffCount} 关机
          </div>
        </div>
        <div style={{ flex: 1 }}/>
        {message && <div style={{ fontSize: 12, color: message.includes('失败') || message.includes('error') ? '#dc2626' : T.ink3 }}>{message}</div>}
        <ActionButton icon="refresh" label="刷新" disabled={loading} onClick={refresh}/>
      </div>

      <div style={{ flex: 1, minHeight: 0, width: '100%', minWidth: 0, display: 'grid', gridTemplateColumns: '320px minmax(0, 1fr)' }}>
        <aside style={{
          borderRight: `1px solid ${T.border}`, background: '#f8fafc',
          padding: 14, overflow: 'auto',
        }}>
          {loading && !vms.length && <div style={{ padding: 20, color: T.ink3, fontSize: 12 }}>加载虚拟机...</div>}
          {error && !vms.length && <div style={{ padding: 20, color: '#dc2626', fontSize: 12 }}>无法读取虚拟机列表</div>}
          {!loading && !vms.length && <div style={{ padding: 20, color: T.ink3, fontSize: 12 }}>没有发现 libvirt 虚拟机</div>}
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {vms.map(vm => (
              <VMRow key={vm.name} vm={vm} active={current?.name === vm.name} onClick={() => setSelected(vm.name)}/>
            ))}
          </div>
        </aside>

        <main style={{ minWidth: 0, overflow: 'auto', padding: 18 }}>
          {!current ? (
            <div style={{ color: T.ink3, fontSize: 13 }}>请选择虚拟机</div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
              <section style={{
                border: `1px solid ${T.border}`, borderRadius: 8, background: T.surface,
                padding: 16, display: 'grid', gridTemplateColumns: '1fr auto', gap: 16,
              }}>
                <div style={{ minWidth: 0 }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                    <h2 style={{ margin: 0, fontSize: 20, color: T.ink, letterSpacing: 0 }}>{current.name}</h2>
                    <StateBadge state={current.state}/>
                  </div>
                  <div style={{ marginTop: 8, fontSize: 12, color: T.ink3, display: 'flex', gap: 14, flexWrap: 'wrap' }}>
                    <span>UUID <span className="mono">{current.uuid || '-'}</span></span>
                    <span>{current.vcpus || 0} vCPU</span>
                    <span>Autostart {current.autostart ? 'enable' : 'disable'}</span>
                    {ips.length > 0 && <span>IP {ips.join(', ')}</span>}
                  </div>
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
                  {isOff && <ActionButton icon="play" label="启动" tone="ok" disabled={!!busy} onClick={() => runAction('start')}/>}
                  {isRunning && <ActionButton icon="refresh" label="重启" disabled={!!busy} onClick={() => runAction('reboot')}/>}
                  {isRunning && <ActionButton icon="power" label="关机" disabled={!!busy} onClick={() => runAction('shutdown')}/>}
                  {isRunning && <ActionButton icon="stop" label="强制断电" tone="danger" disabled={!!busy} onClick={() => runAction('destroy')}/>}
                </div>
              </section>

              <section style={{ display: 'grid', gridTemplateColumns: 'repeat(4, minmax(140px, 1fr))', gap: 10 }}>
                <Metric label="CPU 时间" value={`${Math.round(current.cpuTimeSec || 0)}s`} sub={`${current.vcpus || 0} 个 vCPU`}/>
                <Metric label="内存" value={`${pct(memUsed, memTotal)}%`} sub={`${fmtBytes(memUsed)} / ${fmtBytes(memTotal)}`}/>
                <Metric label="可用内存" value={fmtBytes(memAvail)} sub={current.guest?.agentOK ? 'guest agent' : 'libvirt balloon'}/>
                <Metric label="Load" value={load.length ? load.map(n => n.toFixed(2)).join(' / ') : '-'} sub={current.guest?.agentOK ? '1 / 5 / 15 min' : 'guest agent 不可用'}/>
              </section>

              <section style={{
                border: `1px solid ${T.border}`, borderRadius: 8, background: T.surface,
                padding: 14, display: 'grid', gridTemplateColumns: '1fr 1fr 1fr auto', gap: 12,
                alignItems: 'end',
              }}>
                <Field label="vCPU">
                  <input type="number" min="1" max="128" step="1" value={config.vcpus}
                    onChange={e => setConfig(c => ({ ...c, vcpus: e.target.value }))}
                    style={inputStyle}/>
                </Field>
                <Field label="内存" hint="MiB，保存到 libvirt 持久配置">
                  <input type="number" min="512" step="512" value={config.memoryMiB}
                    onChange={e => setConfig(c => ({ ...c, memoryMiB: e.target.value }))}
                    style={inputStyle}/>
                </Field>
                <Field label="开机自启">
                  <button onClick={() => setConfig(c => ({ ...c, autostart: !c.autostart }))} style={{
                    height: 34, borderRadius: 7, border: `1px solid ${config.autostart ? '#bbf7d0' : T.border}`,
                    background: config.autostart ? '#f0fdf4' : T.surface,
                    color: config.autostart ? '#047857' : T.ink2,
                    display: 'inline-flex', alignItems: 'center', justifyContent: 'center', gap: 7,
                    fontSize: 12.5, fontWeight: 700, cursor: 'pointer',
                  }}>
                    <Icon name={config.autostart ? 'check' : 'minus'} size={13} stroke={2}/>
                    {config.autostart ? '已启用' : '未启用'}
                  </button>
                </Field>
                <ActionButton icon="check" label={busy === 'config' ? '保存中' : '保存配置'} tone="ok" disabled={!!busy} onClick={saveConfig}/>
                {isRunning && (
                  <div style={{ gridColumn: '1 / -1', fontSize: 11.5, color: T.ink3 }}>
                    当前 VM 正在运行：vCPU/内存写入持久配置，重启后完整生效；开机自启立即生效。
                  </div>
                )}
              </section>

              <section style={{ border: `1px solid ${T.border}`, borderRadius: 8, background: T.surface, overflow: 'hidden' }}>
                <div style={{
                  height: 42, padding: '0 14px', display: 'flex', alignItems: 'center',
                  borderBottom: `1px solid ${T.borderSoft}`, gap: 8,
                }}>
                  <Icon name="hardDrive" size={15} stroke={1.8} style={{ color: T.ink2 }}/>
                  <span style={{ fontSize: 13, fontWeight: 720, color: T.ink }}>块设备</span>
                </div>
                <DiskTable disks={current.disks}/>
              </section>

              <section style={{ border: `1px solid ${T.border}`, borderRadius: 8, background: T.surface, overflow: 'hidden' }}>
                <div style={{
                  height: 42, padding: '0 14px', display: 'flex', alignItems: 'center',
                  borderBottom: `1px solid ${T.borderSoft}`, gap: 8,
                }}>
                  <Icon name="folder" size={15} stroke={1.8} style={{ color: T.ink2 }}/>
                  <span style={{ fontSize: 13, fontWeight: 720, color: T.ink }}>共享挂载</span>
                </div>
                <SharedMountTable filesystems={current.filesystems} mounts={current.guest?.mounts}/>
              </section>

              <section style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
                <div style={{ border: `1px solid ${T.border}`, borderRadius: 8, background: T.surface, padding: 14 }}>
                  <div style={{ fontSize: 13, fontWeight: 720, color: T.ink, marginBottom: 10 }}>网络</div>
                  {(current.interfaces || []).length ? current.interfaces.map(n => (
                    <div key={`${n.name}-${n.address}`} style={{
                      display: 'grid', gridTemplateColumns: '90px 1fr', gap: 8,
                      padding: '7px 0', borderTop: `1px solid ${T.borderSoft}`,
                      fontSize: 12, color: T.ink2,
                    }}>
                      <span style={{ fontWeight: 700, color: T.ink }}>{n.name}</span>
                      <span className="mono">{n.address}</span>
                    </div>
                  )) : <div style={{ fontSize: 12, color: T.ink3 }}>暂无租约地址</div>}
                </div>
                <div style={{ border: `1px solid ${T.border}`, borderRadius: 8, background: T.surface, padding: 14 }}>
                  <div style={{ fontSize: 13, fontWeight: 720, color: T.ink, marginBottom: 10 }}>Memory Pressure</div>
                  {(current.guest?.memoryPressure || []).length ? current.guest.memoryPressure.map(p => (
                    <div key={p.kind} style={{
                      display: 'grid', gridTemplateColumns: '70px repeat(3, 1fr)', gap: 8,
                      padding: '7px 0', borderTop: `1px solid ${T.borderSoft}`,
                      fontSize: 12, color: T.ink2,
                    }}>
                      <span style={{ fontWeight: 700, color: T.ink }}>{p.kind}</span>
                      <span className="tnum">10s {p.avg10.toFixed(2)}</span>
                      <span className="tnum">60s {p.avg60.toFixed(2)}</span>
                      <span className="tnum">300s {p.avg300.toFixed(2)}</span>
                    </div>
                  )) : <div style={{ fontSize: 12, color: T.ink3 }}>guest agent 未返回压力数据</div>}
                </div>
              </section>
            </div>
          )}
        </main>
      </div>
    </div>
  );
}
