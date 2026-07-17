import { useState, useEffect, useRef } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, Chip, Ring, Sparkline, Card, useTicker } from '../components/ui'
import { useDevice, useMetrics, useMetricsHistory, useApps, useAlerts, useNetwork, authFetch } from '../hooks/useApi'
import { btnSecondary, btnPrimary, btnDanger } from '../components/AppWindow'
import TabBar from '../components/TabBar'

function NetFirewall() {
  const rules = [
    { dir: 'in',  proto: 'TCP',  port: '22',         src: '192.168.0.0/16',   action: 'allow', note: 'SSH 运维接入 · 仅内网' },
    { dir: 'in',  proto: 'TCP',  port: '8443',       src: 'any',              action: 'allow', note: 'VS Code Server · HTTPS' },
    { dir: 'in',  proto: 'TCP',  port: '8888',       src: 'any',              action: 'allow', note: 'JupyterLab · HTTPS' },
    { dir: 'in',  proto: 'TCP',  port: '11434',      src: '192.168.0.0/16',   action: 'allow', note: 'Ollama API · 仅内网' },
    { dir: 'in',  proto: 'TCP',  port: '7861,8188',  src: 'any',              action: 'allow', note: 'SD WebUI · ComfyUI' },
    { dir: 'in',  proto: 'TCP',  port: '23',         src: 'any',              action: 'deny',  note: 'Telnet · 默认拒绝' },
    { dir: 'out', proto: 'TCP',  port: '443',        src: 'edgex-cloud.huaneng.cn', action: 'allow', note: '云端隧道' },
  ];

  return (
    <div style={{ padding: 16 }}>
      <div style={{ display: 'flex', gap: 10, marginBottom: 10 }}>
        <div style={{
          flex: 1, padding: '10px 14px', borderRadius: 8,
          background: '#ecfdf5', border: '1px solid #a7f3d0',
          display: 'flex', alignItems: 'center', gap: 10,
        }}>
          <Icon name="shield" size={20} stroke={1.6} style={{ color: '#047857' }}/>
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 12.5, fontWeight: 700, color: '#047857' }}>防火墙已启用</div>
            <div style={{ fontSize: 11, color: '#065f46', marginTop: 2 }}>nftables · 7 条规则 · 今日拦截 <span className="mono tnum" style={{ fontWeight: 600 }}>142</span> 个请求</div>
          </div>
          <button style={{ ...btnSecondary, height: 28, padding: '0 10px', fontSize: 11.5,
            background: 'white', color: '#047857', borderColor: '#a7f3d0' }}>
            临时禁用
          </button>
        </div>
        <div style={{
          width: 140, padding: '10px 14px', borderRadius: 8,
          background: T.surfaceAlt, border: `1px solid ${T.borderSoft}`,
        }}>
          <div style={{ fontSize: 10.5, color: T.ink3, fontWeight: 600,
            letterSpacing: '0.04em', textTransform: 'uppercase' }}>默认策略</div>
          <div style={{ marginTop: 6, fontSize: 11.5, color: T.ink2, lineHeight: 1.7 }}>
            入站：<span style={{ color: T.red, fontWeight: 600 }}>DENY</span><br/>
            出站：<span style={{ color: T.green, fontWeight: 600 }}>ALLOW</span>
          </div>
        </div>
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
        <span style={{ fontSize: 11.5, fontWeight: 600, color: T.ink2 }}>规则列表</span>
        <span style={{ fontSize: 11, color: T.ink4 }}>· 按优先级从上到下匹配</span>
        <div style={{ flex: 1 }}/>
        <button style={{ ...btnSecondary, height: 28, padding: '0 10px', fontSize: 11.5 }}>
          <Icon name="download" size={12} stroke={1.8}/>导出 nftables
        </button>
        <button style={{ ...btnPrimary, height: 28, padding: '0 10px', fontSize: 11.5 }}>
          <Icon name="plus" size={12} stroke={2}/>新增规则
        </button>
      </div>

      <div style={{ border: `1px solid ${T.borderSoft}`, borderRadius: 8, overflow: 'hidden' }}>
        <div style={{
          display: 'grid', gridTemplateColumns: '50px 80px 70px 130px 200px 1fr 60px',
          padding: '8px 12px', background: T.surfaceAlt,
          fontSize: 10.5, color: T.ink3, fontWeight: 600,
          letterSpacing: '0.04em', textTransform: 'uppercase',
          borderBottom: `1px solid ${T.borderSoft}`,
        }}>
          <span>#</span><span>方向</span><span>协议</span><span>端口</span>
          <span>源 / 目标</span><span>说明</span><span style={{ textAlign: 'right' }}>动作</span>
        </div>
        {rules.map((r, i) => (
          <div key={i} style={{
            display: 'grid', gridTemplateColumns: '50px 80px 70px 130px 200px 1fr 60px',
            padding: '10px 12px', alignItems: 'center', fontSize: 12,
            borderTop: i ? `1px solid ${T.borderSoft}` : 'none',
          }}>
            <span className="mono tnum" style={{ color: T.ink4 }}>{i + 1}</span>
            <span>
              <Chip tone={r.dir === 'in' ? 'blue' : 'violet'} style={{ padding: '1px 6px' }}>
                <Icon name={r.dir === 'in' ? 'arrowDown' : 'arrowUp'} size={10} stroke={2}/>
                {r.dir === 'in' ? '入站' : '出站'}
              </Chip>
            </span>
            <span className="mono" style={{ color: T.ink2, fontSize: 11.5 }}>{r.proto}</span>
            <span className="mono tnum" style={{ color: T.ink, fontWeight: 600 }}>{r.port}</span>
            <span className="mono" style={{ color: T.ink2, fontSize: 11.5 }}>{r.src}</span>
            <span style={{ color: T.ink3, fontSize: 11.5 }}>{r.note}</span>
            <div style={{ display: 'flex', justifyContent: 'flex-end', alignItems: 'center', gap: 6 }}>
              <Chip tone={r.action === 'allow' ? 'green' : 'red'} style={{ padding: '1px 7px' }}>
                {r.action === 'allow' ? 'ALLOW' : 'DENY'}
              </Chip>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function NetConnections() {
  const listening = [
    { proto: 'tcp',  addr: '0.0.0.0',       port: 8443,  proc: 'code-server',    pid: 4128 },
    { proto: 'tcp',  addr: '0.0.0.0',       port: 8888,  proc: 'jupyter-lab',    pid: 5302 },
    { proto: 'tcp',  addr: '0.0.0.0',       port: 11434, proc: 'ollama',         pid: 8104 },
    { proto: 'tcp',  addr: '127.0.0.1',     port: 8000,  proc: 'vllm (down)',    pid: '\u2014' },
    { proto: 'tcp',  addr: '0.0.0.0',       port: 7861,  proc: 'sd-webui',       pid: 9128 },
    { proto: 'tcp',  addr: '0.0.0.0',       port: 8188,  proc: 'comfyui',        pid: 9342 },
    { proto: 'tcp',  addr: '0.0.0.0',       port: 3000,  proc: 'open-webui',     pid: 11842 },
    { proto: 'tcp',  addr: '127.0.0.1',     port: 6379,  proc: 'redis-server',   pid: 1024 },
  ];
  const established = [
    { proto: 'tcp', local: '192.168.18.42:48922', remote: '34.107.158.42:443',      state: 'ESTABLISHED', proc: 'edgex-agent',  bytes: '12.4 MB',  note: '云端隧道' },
    { proto: 'tcp', local: '192.168.18.42:51204', remote: '192.168.18.22:22',       state: 'ESTABLISHED', proc: 'sshd',         bytes: '482 KB',   note: '运维 SSH' },
    { proto: 'tcp', local: '192.168.18.42:11434', remote: '192.168.18.31:54012',    state: 'ESTABLISHED', proc: 'ollama',       bytes: '1.8 MB',   note: 'API 调用 · dev-li' },
    { proto: 'tcp', local: '192.168.18.42:11434', remote: '192.168.18.42:60918',    state: 'ESTABLISHED', proc: 'open-webui',   bytes: '320 KB',   note: '本机回环' },
    { proto: 'tcp', local: '192.168.18.42:3128',  remote: '203.0.113.42:443',       state: 'TIME_WAIT',   proc: 'hf-downloader',bytes: '8.2 GB',   note: 'HuggingFace' },
    { proto: 'tcp', local: '192.168.18.42:48214', remote: '54.230.105.71:443',      state: 'ESTABLISHED', proc: 'curl',         bytes: '2.4 MB',   note: 'docker pull' },
  ];

  return (
    <div style={{ padding: 16 }}>
      {/* Summary chips */}
      <div style={{ display: 'flex', gap: 8, marginBottom: 12 }}>
        {[
          { label: '总连接',   val: 42, tone: T.ink },
          { label: 'ESTABLISHED', val: 28, tone: T.green },
          { label: 'LISTEN',     val: 8,  tone: T.blue },
          { label: 'TIME_WAIT',  val: 6,  tone: T.amber },
          { label: '出入带宽', val: '↑ 4.2 / ↓ 12.8 Mbps', tone: T.indigo, wide: true },
        ].map((s, i) => (
          <div key={i} style={{
            flex: s.wide ? 1.4 : 1, padding: '8px 12px',
            background: T.surfaceAlt, border: `1px solid ${T.borderSoft}`, borderRadius: 7,
          }}>
            <div style={{ fontSize: 10, color: T.ink3, fontWeight: 600,
              letterSpacing: '0.04em', textTransform: 'uppercase' }}>{s.label}</div>
            <div className="mono tnum" style={{
              marginTop: 3, fontSize: s.wide ? 14 : 17, fontWeight: 700, color: s.tone,
            }}>{s.val}</div>
          </div>
        ))}
      </div>

      {/* Listening */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
        <span style={{ fontSize: 11.5, fontWeight: 600, color: T.ink2 }}>监听端口</span>
        <span style={{ fontSize: 11, color: T.ink4 }}>· 8 个</span>
        <div style={{ flex: 1 }}/>
        <span style={{ fontSize: 10.5, color: T.ink4, fontFamily: 'ui-monospace, monospace' }}>
          ss -tlnp
        </span>
      </div>
      <div style={{ border: `1px solid ${T.borderSoft}`, borderRadius: 8, overflow: 'hidden', marginBottom: 12 }}>
        <div style={{
          display: 'grid', gridTemplateColumns: '60px 160px 90px 1fr 80px 80px',
          padding: '8px 12px', background: T.surfaceAlt,
          fontSize: 10.5, color: T.ink3, fontWeight: 600,
          letterSpacing: '0.04em', textTransform: 'uppercase',
          borderBottom: `1px solid ${T.borderSoft}`,
        }}>
          <span>协议</span><span>地址</span><span>端口</span>
          <span>进程</span><span>PID</span><span style={{ textAlign: 'right' }}>状态</span>
        </div>
        {listening.map((c, i) => (
          <div key={i} style={{
            display: 'grid', gridTemplateColumns: '60px 160px 90px 1fr 80px 80px',
            padding: '8px 12px', alignItems: 'center', fontSize: 12,
            borderTop: i ? `1px solid ${T.borderSoft}` : 'none',
          }}>
            <span className="mono" style={{ color: T.ink3, fontSize: 11 }}>{c.proto}</span>
            <span className="mono" style={{ color: T.ink2 }}>{c.addr}</span>
            <span className="mono tnum" style={{ color: T.ink, fontWeight: 600 }}>:{c.port}</span>
            <span style={{ color: T.ink2 }} className="mono">{c.proc}</span>
            <span className="mono tnum" style={{ color: T.ink3, fontSize: 11.5 }}>{c.pid}</span>
            <div style={{ textAlign: 'right' }}>
              {String(c.pid) === '\u2014'
                ? <Chip tone="red" style={{ padding: '0 6px' }}>异常</Chip>
                : <Chip tone="blue" style={{ padding: '0 6px' }}>LISTEN</Chip>}
            </div>
          </div>
        ))}
      </div>

      {/* Established */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
        <span style={{ fontSize: 11.5, fontWeight: 600, color: T.ink2 }}>已建立连接</span>
        <span style={{ fontSize: 11, color: T.ink4 }}>· 显示前 6 条 / 共 28 条</span>
        <div style={{ flex: 1 }}/>
        <span style={{ fontSize: 10.5, color: T.ink4, fontFamily: 'ui-monospace, monospace' }}>
          ss -tnp state established
        </span>
      </div>
      <div style={{ border: `1px solid ${T.borderSoft}`, borderRadius: 8, overflow: 'hidden' }}>
        <div style={{
          display: 'grid', gridTemplateColumns: '180px 180px 110px 130px 80px 1fr',
          padding: '8px 12px', background: T.surfaceAlt,
          fontSize: 10.5, color: T.ink3, fontWeight: 600,
          letterSpacing: '0.04em', textTransform: 'uppercase',
          borderBottom: `1px solid ${T.borderSoft}`,
        }}>
          <span>本地</span><span>远端</span><span>状态</span>
          <span>进程</span><span>传输量</span><span>备注</span>
        </div>
        {established.map((c, i) => (
          <div key={i} style={{
            display: 'grid', gridTemplateColumns: '180px 180px 110px 130px 80px 1fr',
            padding: '8px 12px', alignItems: 'center', fontSize: 11.5,
            borderTop: i ? `1px solid ${T.borderSoft}` : 'none',
          }}>
            <span className="mono" style={{ color: T.ink2 }}>{c.local}</span>
            <span className="mono" style={{ color: T.ink2 }}>{c.remote}</span>
            <span>
              <Chip tone={c.state === 'ESTABLISHED' ? 'green' : 'amber'} style={{ padding: '0 6px' }}>
                {c.state}
              </Chip>
            </span>
            <span style={{ color: T.ink2 }} className="mono">{c.proc}</span>
            <span className="mono tnum" style={{ color: T.ink3 }}>{c.bytes}</span>
            <span style={{ color: T.ink3 }}>{c.note}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

function NetworkManagement() {
  const [tab, setTab] = useState('interfaces');
  const [netInfo, setNetInfo] = useState(null);

  useEffect(() => {
    authFetch('/api/v1/network').then(r => r.ok ? r.json() : null).then(d => { if (d) setNetInfo(d); }).catch(() => {});
  }, []);

  const ifaces = netInfo ? netInfo.interfaces.filter(i => i.state === 'up') : [];

  return (
    <div style={{
      background: T.surface, border: `1px solid ${T.border}`, borderRadius: 10,
      boxShadow: '0 1px 2px rgba(15,23,42,0.04)', overflow: 'hidden', marginTop: 4,
    }}>
      {/* Header */}
      <div style={{
        display: 'flex', alignItems: 'center', padding: '12px 16px 0',
        borderBottom: `1px solid ${T.borderSoft}`,
      }}>
        <Icon name="network" size={15} stroke={1.8} style={{ color: T.blueDeep, marginRight: 8 }}/>
        <span style={{ fontSize: 13.5, fontWeight: 700, color: T.ink }}>网络管理</span>
        <Chip tone="green" style={{ marginLeft: 10 }}>
          <StatusDot tone="green" size={6}/>在线
        </Chip>
        <div style={{ flex: 1 }}/>
        <span style={{ fontSize: 11, color: T.ink3 }}>
          本机 IP <span className="mono" style={{ color: T.ink2, fontWeight: 600 }}>{netInfo?.ip || '...'}</span>
          <span style={{ color: '#cbd5e1', margin: '0 6px' }}>·</span>
          默认网关 <span className="mono" style={{ color: T.ink2, fontWeight: 600 }}>{netInfo?.gateway || '...'}</span>
        </span>
      </div>

      {/* Tab bar */}
      <TabBar
        tabs={[{ id: 'interfaces', label: '网络配置', icon: 'network', count: ifaces.length }]}
        active={tab}
        onChange={setTab}
        style={{ padding: '4px 16px 0', gap: 2, marginTop: 8 }}
        itemStyle={{ padding: '8px 12px 10px', fontSize: 12.5, marginBottom: -1 }}
        renderLabel={(t2, on) => (
          <>
            <Icon name={t2.icon} size={13} stroke={1.8}/>{t2.label}
            <span className="mono tnum" style={{
              fontSize: 10, color: on ? T.blueDeep : T.ink4,
              background: on ? '#dbeafe' : T.surfaceAlt,
              padding: '0 5px', borderRadius: 999, lineHeight: '16px', marginLeft: 2,
            }}>{t2.count}</span>
          </>
        )}
      />

      {/* Interfaces from API */}
      <div style={{ padding: 16 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
          <span style={{ fontSize: 11.5, color: T.ink3 }}>
            DNS：<span className="mono" style={{ color: T.ink2, fontWeight: 600 }}>{(netInfo?.dns || []).join('  ')}</span>
          <span style={{ color: '#cbd5e1', margin: '0 4px' }}>·</span>
          <span className="mono" style={{ color: T.ink2, fontWeight: 600 }}>8.8.8.8</span>
          <span style={{ color: '#cbd5e1', margin: '0 6px' }}>·</span>
          MTU 默认 <span className="mono tnum" style={{ color: T.ink2, fontWeight: 600 }}>1500</span>
        </span>
        <div style={{ flex: 1 }}/>
        <button style={{ ...btnSecondary, height: 28, padding: '0 10px', fontSize: 11.5 }}>
          <Icon name="refresh" size={12} stroke={1.8}/>重新扫描
        </button>
        <button style={{ ...btnPrimary, height: 28, padding: '0 10px', fontSize: 11.5 }}>
          <Icon name="plus" size={12} stroke={2}/>新增接口
        </button>
      </div>

      <div style={{ border: `1px solid ${T.borderSoft}`, borderRadius: 8, overflow: 'hidden' }}>
        <div style={{
          display: 'grid', gridTemplateColumns: '140px 80px 200px 130px 160px 90px 1fr 80px',
          padding: '8px 12px', background: T.surfaceAlt,
          fontSize: 10.5, color: T.ink3, fontWeight: 600,
          letterSpacing: '0.04em', textTransform: 'uppercase',
          borderBottom: `1px solid ${T.borderSoft}`,
        }}>
          <span>接口</span><span>状态</span><span>IP / 掩码</span>
          <span>网关</span><span>MAC</span>
          <span>模式</span><span>RX / TX</span><span style={{ textAlign: 'right' }}>操作</span>
        </div>
        {(netInfo?.interfaces || []).map((f, i) => (
          <div key={f.name} style={{
            display: 'grid', gridTemplateColumns: '140px 80px 200px 160px 90px 1fr',
            padding: '11px 12px', alignItems: 'center', fontSize: 12,
            borderTop: i ? `1px solid ${T.borderSoft}` : 'none',
          }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <Icon name={f.type === '本地' ? 'dot' : f.type === '无线' ? 'globe' : 'network'}
                size={14} stroke={1.8}
                style={{ color: f.state === 'up' ? T.blueDeep : T.ink4 }}/>
              <div>
                <div style={{ fontWeight: 600, color: T.ink }} className="mono">{f.name}</div>
                <div style={{ fontSize: 10.5, color: T.ink4, marginTop: 1 }}>{f.type}</div>
              </div>
            </div>
            <div>
              {f.state === 'up'
                ? <Chip tone="green" style={{ padding: '1px 6px' }}><StatusDot tone="green" size={5}/>UP</Chip>
                : <Chip tone="gray"  style={{ padding: '1px 6px' }}><StatusDot tone="gray"  size={5}/>DOWN</Chip>}
            </div>
            <div className="mono" style={{ color: T.ink2 }}>{f.ip || '\u2014'}</div>
            <div className="mono" style={{ color: T.ink3, fontSize: 11 }}>{f.mac || '\u2014'}</div>
            <div className="mono" style={{ color: T.ink3, fontSize: 11 }}>MTU {f.mtu}</div>
            <div/>
          </div>
        ))}
      </div>

      {/* DNS summary */}
      <div style={{
        padding: '10px 12px', background: T.surfaceAlt,
        border: `1px solid ${T.borderSoft}`, borderRadius: 8, marginTop: 12,
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 6 }}>
          <Icon name="globe" size={13} stroke={1.8} style={{ color: T.ink2 }}/>
          <span style={{ fontSize: 12, fontWeight: 600, color: T.ink }}>DNS</span>
        </div>
        <div style={{ fontSize: 11.5, color: T.ink3, lineHeight: 1.7 }}>
          {(netInfo?.dns || []).map((d, i) => (
            <span key={i}>{i > 0 && <br/>}DNS {i+1}：<span className="mono" style={{ color: T.ink2 }}>{d}</span></span>
          ))}
        </div>
      </div>
    </div>
    </div>
  );
}

export default function Diagnostics() {
  const [running, setRunning] = useState(null);
  const [device, setDevice] = useState({});
  useEffect(() => {
    authFetch('/api/v1/device').then(r => r.ok ? r.json() : null).then(d => {
      if (d) setDevice({name: d.hostname, sn: d.hostname, os: d.platform || d.os, agent: 'Edge Platform ' + (d.agentVersion || ''), model: d.cpuModel, site: d.ip, dept: '', uptime: d.uptimeHuman});
    }).catch(() => {});
  }, []);

  const tools = [
    { id: 'ping',     icon: 'network',   name: '网络连通性',   desc: 'ping 云端 · 网关 · DNS',         color: T.blue },
    { id: 'bandwidth',icon: 'arrowUp',   name: '带宽测试',     desc: '上行 / 下行 · 抖动 · 丢包',       color: T.indigo },
    { id: 'syslog',   icon: 'terminal',  name: '系统日志',     desc: 'journalctl · dmesg · syslog',    color: T.violet },
    { id: 'health',   icon: 'shield',    name: '一键体检',     desc: '硬件 · 驱动 · 模型完整性',         color: T.green },
    { id: 'bundle',   icon: 'download',  name: '一键诊断包',   desc: '日志 + 配置 + 指标 · 离线上报',    color: T.amber },
    { id: 'reboot',   icon: 'power',     name: '远程重启',     desc: '需管理员权限',                    color: T.red },
  ];

  return (
    <div style={{ flex: 1, padding: 24, overflow: 'auto', background: T.surfaceAlt }}>
      <div style={{ marginBottom: 14 }}>
        <div style={{ fontSize: 17, fontWeight: 700, color: T.ink }}>系统设置</div>
        <div style={{ fontSize: 12, color: T.ink3, marginTop: 3 }}>诊断工具 · 网络管理 · 设备信息</div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 12, marginBottom: 16 }}>
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
                <div style={{ fontSize: 11.5, color: t.color, marginTop: 8, fontWeight: 600, display: 'flex', alignItems: 'center', gap: 5 }}>
                  <StatusDot tone={t.color === T.green ? 'green' : 'blue'} size={6} pulse/>
                  正在执行…
                </div>
              )}
            </div>
          </div>
        ))}
      </div>

      <NetworkManagement/>

      <Card title="设备信息" padding={0} style={{ marginTop: 16 }}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12.5 }}>
          <tbody>
            {[
              ['设备名称', device.name],
              ['IP 地址', <span className="mono">{device.site}</span>],
              ['运行时间', device.uptime],
              ['操作系统', device.os],
              ['Agent 版本', <span className="mono">{device.agent}</span>],
              ['硬件平台', device.model],
            ].map(([k, v], i) => (
              <tr key={k} style={{ borderTop: i ? `1px solid ${T.borderSoft}` : 'none' }}>
                <td style={{ width: 120, padding: '10px 16px', color: T.ink3 }}>{k}</td>
                <td style={{ padding: '10px 16px', color: T.ink }}>{v}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>
    </div>
  );
}

export { NetworkManagement, NetFirewall, NetConnections };
