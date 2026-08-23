import { useState } from 'react';
import { T } from '../tokens';
import { Icon } from '../icons';

// WelcomeWidget — fnOS 桌面"欢迎来到 fnOS"引导卡片同款
//
// 对照真机 fnOS：桌面中部一列引导卡片（创建存储空间 / 安装飞牛相册 /
// 安装飞牛影视 / FN Connect / 设置安全邮箱），每张卡 = 图标 + 标题 +
// 副文案，点击直达对应功能。
//
// devbox 语义映射：入口换成本机最常用的四件事（仪表盘 / 应用商店 /
// 终端 / 服务导航），点击 onOpen(appId) 打开对应窗口。
// localStorage 记忆关闭状态（fnOS 的引导卡完成后也会消失）。

const HIDE_KEY = 'edge-console.welcome-hidden';

const GUIDES = [
  {
    appId: 'dashboard',
    icon: 'dashboard', iconBg: 'linear-gradient(150deg,#3b82f6,#0066FF)',
    title: '查看系统状态',
    desc: 'CPU · 内存 · 磁盘 · GPU 实时监控',
  },
  {
    appId: 'store',
    icon: 'store', iconBg: 'linear-gradient(150deg,#34d399,#059669)',
    title: '安装应用',
    desc: '从应用商店部署开发环境',
  },
  {
    appId: 'terminal',
    icon: 'terminal', iconBg: 'linear-gradient(150deg,#64748b,#1e293b)',
    title: '打开终端',
    desc: '浏览器里直接执行 Shell 命令',
  },
  {
    appId: 'links',
    icon: 'network', iconBg: 'linear-gradient(150deg,#38bdf8,#0369a1)',
    title: '服务导航',
    desc: '本机各服务入口一站直达',
  },
];

export function WelcomeWidget({ onOpen, deviceName, dark = false }) {
  const [hidden, setHidden] = useState(() => {
    try { return localStorage.getItem(HIDE_KEY) === '1'; } catch { return false; }
  });
  if (hidden) return null;

  const dismiss = () => {
    setHidden(true);
    try { localStorage.setItem(HIDE_KEY, '1'); } catch { /* ignore */ }
  };

  return (
    <div className={dark ? 'edge-material-dark' : ''} style={{
      padding: '18px 20px',
      background: dark ? undefined : 'rgba(255,255,255,0.7)',
      backdropFilter: dark ? undefined : 'blur(12px)',
      WebkitBackdropFilter: dark ? undefined : 'blur(12px)',
      border: dark ? '1px solid rgba(255,255,255,0.12)' : '1px solid rgba(255,255,255,0.9)',
      borderRadius: 14,
      boxShadow: dark ? '0 8px 24px -8px rgba(0,0,0,0.4)' : '0 6px 20px -6px rgba(15,23,42,0.10)',
    }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 14 }}>
        <div style={{
          fontSize: 15, fontWeight: 700,
          color: dark ? '#fff' : T.ink,
        }}>欢迎来到 {deviceName || 'Devbox'}</div>
        <div style={{ flex: 1 }}/>
        <button onClick={dismiss} title="不再显示" aria-label="关闭欢迎引导" style={{
          width: 22, height: 22, borderRadius: 6, border: 'none', padding: 0,
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          background: 'transparent', cursor: 'pointer',
          color: dark ? 'rgba(255,255,255,0.5)' : T.ink4,
        }}>
          <Icon name="x" size={13} stroke={2}/>
        </button>
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        {GUIDES.map(g => (
          <div key={g.appId} onClick={() => onOpen && onOpen(g.appId)}
            className="edge-row-hover"
            style={{
              display: 'flex', alignItems: 'center', gap: 12,
              padding: '10px 12px', borderRadius: 10, cursor: 'pointer',
              background: dark ? 'rgba(255,255,255,0.05)' : 'rgba(255,255,255,0.6)',
              border: dark ? '1px solid rgba(255,255,255,0.08)' : `1px solid ${T.borderSoft}`,
              '--edge-row-hover-bg': dark ? 'rgba(255,255,255,0.12)' : 'rgba(255,255,255,0.95)',
              transition: 'background 0.15s ease',
            }}>
            <div style={{
              width: 40, height: 40, borderRadius: 10, flexShrink: 0,
              background: g.iconBg, color: '#fff',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              boxShadow: '0 4px 10px -3px rgba(0,0,0,0.3), inset 0 1px 0 rgba(255,255,255,0.3)',
            }}>
              <Icon name={g.icon} size={20} stroke={1.7}/>
            </div>
            <div style={{ minWidth: 0, flex: 1 }}>
              <div style={{
                fontSize: 13, fontWeight: 600,
                color: dark ? '#fff' : T.ink,
              }}>{g.title}</div>
              <div style={{
                fontSize: 11, marginTop: 2,
                color: dark ? 'rgba(255,255,255,0.55)' : T.ink3,
                whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis',
              }}>{g.desc}</div>
            </div>
            <Icon name="chevRight" size={15} stroke={2}
              style={{ color: dark ? 'rgba(255,255,255,0.35)' : T.ink4, flexShrink: 0 }}/>
          </div>
        ))}
      </div>
    </div>
  );
}

export default WelcomeWidget;
