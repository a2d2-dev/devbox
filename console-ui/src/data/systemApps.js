// SYSTEM_APPS — 桌面"系统工具"段固定图标列表
//
// 这些是 agent 本地内置能力（仪表盘 / 文件 / 进程 / 磁盘 / 网络连接 / 告警 /
// 应用商店 / 系统设置），**永远显示**，不依赖任何云端 API。
// 与 /api/v1/apps（云端下发的已部署应用）完全独立。
//
// 这是 UI 配置（产品定义的图标布局），不是 mock 数据。
//
// 2026-06-22 LF 调整：移除「模型仓库」桌面入口（受控开发界面收敛桌面图标白名单；
// models 页面代码 src/pages/Models.jsx 保留，未来需要时直接恢复一行配置即可）。

import { T } from '../tokens'

export const SYSTEM_APPS = [
  { id: 'dashboard',           kind: 'system', name: '仪表盘',   icon: 'dashboard', color: T.blue,    bg: 'linear-gradient(160deg,#3b82f6,#1d4ed8)' },
  { id: 'monitoring',          kind: 'system', name: '资源管理', icon: 'sparkle',   color: T.indigo,  bg: 'linear-gradient(160deg,#6366f1,#4f46e5)' },
  { id: 'ai-activity',         kind: 'system', name: 'AI 活动',  icon: 'brain',     color: T.violet,  bg: 'linear-gradient(160deg,#a855f7,#6d28d9)' },
  { id: 'store',               kind: 'system', name: '应用商店', icon: 'store',     color: T.green,   bg: 'linear-gradient(160deg,#34d399,#059669)' },
  { id: 'docker',              kind: 'system', name: 'Docker',   icon: 'server',    color: '#2563eb', bg: 'linear-gradient(160deg,#3b82f6,#0f766e)' },
  { id: 'compose-manager',     kind: 'system', name: 'Compose 应用', icon: 'apps', color: '#0891b2', bg: 'linear-gradient(160deg,#22d3ee,#0891b2)' },
  { id: 'files',               kind: 'system', name: '文件',     icon: 'folder',    color: '#0891b2', bg: 'linear-gradient(160deg,#22d3ee,#0891b2)' },
  { id: 'backup',              kind: 'system', name: '备份',     icon: 'layers',    color: '#047857', bg: 'linear-gradient(160deg,#34d399,#047857)' },
  { id: 'downloads',           kind: 'system', name: '下载',     icon: 'download',  color: '#2563eb', bg: 'linear-gradient(160deg,#60a5fa,#2563eb)' },
  { id: 'processes',           kind: 'system', name: '进程',     icon: 'cpu',       color: '#475569', bg: 'linear-gradient(160deg,#64748b,#1e293b)' },
  { id: 'supervisor',          kind: 'system', name: '进程守护', icon: 'shield',    color: '#0d9488', bg: 'linear-gradient(160deg,#14b8a6,#0f766e)' },
  { id: 'virtual-machines',    kind: 'system', name: '虚拟机',   icon: 'server',    color: '#155e75', bg: 'linear-gradient(160deg,#22d3ee,#155e75)' },
  { id: 'hardware',            kind: 'system', name: '硬件中心', icon: 'cpu',       color: '#7c3aed', bg: 'linear-gradient(160deg,#a855f7,#6d28d9)' },
	{ id: 'users',               kind: 'system', name: '用户与权限', icon: 'user',     color: '#0f766e', bg: 'linear-gradient(160deg,#14b8a6,#0f766e)' },
  { id: 'links',               kind: 'system', name: '服务导航', icon: 'network',   color: '#0369a1', bg: 'linear-gradient(160deg,#38bdf8,#0369a1)' },
  { id: 'disks',               kind: 'system', name: '磁盘',     icon: 'hardDrive', color: '#1e40af', bg: 'linear-gradient(160deg,#3b82f6,#1e3a8a)' },
  { id: 'network-connections', kind: 'system', name: '网络连接', icon: 'network',   color: '#0e7490', bg: 'linear-gradient(160deg,#06b6d4,#0e7490)' },
  { id: 'network-security',    kind: 'system', name: '网络与安全', icon: 'shield',   color: '#b45309', bg: 'linear-gradient(160deg,#f59e0b,#b45309)' },
  { id: 'alerts',              kind: 'system', name: '告警中心', icon: 'bell',      color: T.amber,   bg: 'linear-gradient(160deg,#fbbf24,#d97706)' },
  { id: 'audit',               kind: 'system', name: '日志',     icon: 'shield',    color: '#7c3aed', bg: 'linear-gradient(160deg,#a78bfa,#5b21b6)' },
  { id: 'settings',            kind: 'system', name: '系统设置', icon: 'gear',      color: T.slate,   bg: 'linear-gradient(160deg,#64748b,#334155)' },
  { id: 'browser',             kind: 'system', name: '浏览器',   icon: 'globe',     color: T.blue,    bg: 'linear-gradient(160deg,#3b82f6,#1d4ed8)' },
]
