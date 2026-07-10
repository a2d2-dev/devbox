// [Disabled 2026-06-20] Web 终端（宿主 root shell）= 突破能力，
// 受控开发界面下不再开放。UI 入口已从桌面 / AppShell 路由移除：
//   - data/mock.js: terminal app 条目删除（无桌面图标）
//   - components/AppShell.jsx: appId === 'terminal' 分支删除（即使强制传也不渲染）
// 容器内交互能力由 Story 4.7「应用容器 Shell」承接（本地 CRI exec 进应用容器，不进宿主）。
// 后端 handleTerminalExec 已实现禁用，WS upgrade 后立即发 501 FEATURE_DISABLED 文本帧并关闭。
// 本文件保留供历史追溯。详见 ADR 2026-06-20-agent-console-controlled-desktop.md。
// 注意：xterm.js npm 依赖 + public/terminal.html 保留（Story 4.7 应用容器 Shell 复用）。

import { useState, useEffect, useRef } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, Chip, Sparkline } from '../components/ui'

export default function TerminalFace() {
  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', background: '#0b1020', overflow: 'hidden' }}>
      <iframe
        src="/terminal.html"
        style={{ flex: 1, border: 'none', width: '100%', height: '100%' }}
      />
    </div>
  );
}
