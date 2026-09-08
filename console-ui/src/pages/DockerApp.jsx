// DockerApp — 容器域唯一入口（2026-09-08 LF 裁决的 IA 合并，T1）。
//
// 「Docker」桌面应用 tab 化：
//   - 概览（overview）：原 DockerOverview 全量（daemon 状态卡、服务控制、开机自启、
//     存储迁移、实时监控），组件原样复用。
//   - Compose 应用（compose）：原 ComposeManager（compose 列表、新建向导、接管、
//     生命周期、卸载），组件原样复用。
//
// 旧 compose-manager appId 的打开请求经 lib/appRoutes.js 别名进入本组件的
// compose tab（App.jsx 以 initialTab 传入）。后续 T2（云端应用页）/ T3（网络、
// 存储 tab）在此基础上扩展。
import { useState } from 'react';
import { T } from '../tokens';
import { Icon } from '../icons';
import DockerOverview from './DockerOverview';
import { ComposeManager } from './ComposeManager';

const DOCKER_APP_TABS = [
  { id: 'overview', label: '概览', icon: 'server' },
  { id: 'compose', label: 'Compose 应用', icon: 'apps' },
];

export default function DockerApp({ authed, onRequireAuth, onOpenStore, onOpenApp, initialTab = 'overview' }) {
  const [tab, setTab] = useState(() => (
    DOCKER_APP_TABS.some((t) => t.id === initialTab) ? initialTab : 'overview'
  ));

  return (
    <div style={{ flex: 1, minWidth: 0, height: '100%', display: 'flex', flexDirection: 'column', background: T.bg, overflow: 'hidden' }}>
      <nav aria-label="Docker 功能切换" style={{
        display: 'flex', gap: 2, padding: '0 20px', background: T.surface,
        borderBottom: `1px solid ${T.border}`, overflowX: 'auto', flexShrink: 0,
      }}>
        {DOCKER_APP_TABS.map(({ id, label, icon }) => (
          <button key={id} onClick={() => setTab(id)}
            aria-current={tab === id ? 'page' : undefined}
            style={{
              height: 40, padding: '0 12px', border: 0,
              borderBottom: `2px solid ${tab === id ? T.blueDeep : 'transparent'}`,
              background: 'transparent',
              color: tab === id ? T.blueDeep : T.ink3,
              fontSize: 12.5, fontWeight: tab === id ? 700 : 550,
              cursor: 'pointer', display: 'flex', alignItems: 'center', gap: 6,
              flexShrink: 0, whiteSpace: 'nowrap',
            }}>
            <Icon name={icon} size={13}/>
            {label}
          </button>
        ))}
      </nav>
      <div style={{ flex: 1, minHeight: 0 }}>
        {tab === 'overview' && (
          <DockerOverview onRequireAuth={onRequireAuth} onOpenCompose={() => setTab('compose')}/>
        )}
        {tab === 'compose' && (
          <ComposeManager authed={authed} onRequireAuth={onRequireAuth}
            onOpenStore={onOpenStore} onOpenApp={onOpenApp}/>
        )}
      </div>
    </div>
  );
}
