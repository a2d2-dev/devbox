// ContainerShellFace.jsx — Story 4.7 应用容器 Shell（FR-5.10）
//
// xterm.js 真终端：
//   - 直接在窗口内键入（无底部 input 框）
//   - 完整 ANSI 颜色 / 光标 / 行编辑 / vim 类应用
//   - 自动 fit 容器尺寸 + WS resize 帧通知后端 PTY 同步
//
// WS endpoint /api/v1/pod-shell/{appId}?access_token=<token>
//   (浏览器原生 WS 不支持 Authorization header，middleware 已限定该路径接受 query token)
//
// 安全：路径与 /api/v1/terminal/exec（宿主 shell，已禁用）完全独立，
// 不共享 wsUpgrader / 控制点，防止权限混淆漏洞（LF 2026-06-21 安全要求）。

import { useState, useEffect, useRef } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { T } from '../tokens'
import { Icon } from '../icons'

function getStoredToken() {
  try {
    return localStorage.getItem('edge_token') ||
           sessionStorage.getItem('edge_token') || ''
  } catch {
    return ''
  }
}

export default function ContainerShellFace({ appId, app }) {
  const [status, setStatus] = useState('connecting') // connecting / open / closed / error / disabled
  const [errorMsg, setErrorMsg] = useState('')
  const termContainerRef = useRef(null)
  const termRef = useRef(null)
  const fitRef = useRef(null)
  const wsRef = useRef(null)

  const isRunning = app?.state === 'running' || app?.status === 'Running'

  useEffect(() => {
    if (!appId || !isRunning) {
      setStatus('disabled')
      return
    }

    const token = getStoredToken()
    if (!token) {
      setStatus('error')
      setErrorMsg('未登录或 token 缺失')
      return
    }

    // 创建 xterm 实例
    const term = new Terminal({
      cursorBlink: true,
      fontFamily: 'ui-monospace, "SF Mono", Menlo, Consolas, monospace',
      fontSize: 13,
      theme: {
        background: '#0b1220',
        foreground: '#e2e8f0',
        cursor: '#60a5fa',
        selectionBackground: 'rgba(96,165,250,0.32)',
      },
      scrollback: 5000,
      convertEol: false, // 不转换 LF → CRLF，由 PTY 自己输出 CRLF
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    termRef.current = term
    fitRef.current = fit

    if (termContainerRef.current) {
      term.open(termContainerRef.current)
      // 等下一帧 layout 完成再 fit
      requestAnimationFrame(() => {
        try { fit.fit() } catch {}
      })
    }

    // WS 连接
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const url = `${proto}//${window.location.host}/api/v1/pod-shell/${encodeURIComponent(appId)}?access_token=${encodeURIComponent(token)}`
    const ws = new WebSocket(url)
    ws.binaryType = 'arraybuffer'
    wsRef.current = ws

    ws.onopen = () => {
      setStatus('open')
      term.focus()
      // 连上后发首个 resize 帧让后端 PTY 同步尺寸
      sendResize()
    }
    ws.onclose = () => {
      setStatus('closed')
      term.write('\r\n\x1b[33m[连接已断开]\x1b[0m\r\n')
    }
    ws.onerror = () => {
      setStatus('error')
      setErrorMsg('WebSocket 连接错误')
    }
    ws.onmessage = (e) => {
      // 容器 stdout (PTY) → 写入 xterm
      if (typeof e.data === 'string') {
        // 服务端可能在升级失败时发文本帧错误 JSON
        try {
          const obj = JSON.parse(e.data)
          if (obj && obj.error) {
            term.write(`\r\n\x1b[31m[错误] ${obj.error}\x1b[0m\r\n`)
            return
          }
        } catch {}
        term.write(e.data)
      } else if (e.data instanceof ArrayBuffer) {
        term.write(new Uint8Array(e.data))
      }
    }

    // 键盘输入 → WS stdin
    const dataHandler = term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(new TextEncoder().encode(data))
      }
    })

    // resize → 发送 resize 帧（前端用 text frame JSON 通知后端 PTY 调整尺寸）
    function sendResize() {
      if (ws.readyState !== WebSocket.OPEN) return
      const msg = JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows })
      ws.send(msg)
    }
    const resizeHandler = term.onResize(() => sendResize())

    // 窗口大小变化触发 fit
    const onWinResize = () => {
      try { fit.fit() } catch {}
    }
    window.addEventListener('resize', onWinResize)
    window.visualViewport?.addEventListener('resize', onWinResize)
    window.visualViewport?.addEventListener('scroll', onWinResize)
    // 监听抽屉尺寸变化（drawer 拖动）
    let ro = null
    if (typeof ResizeObserver !== 'undefined' && termContainerRef.current) {
      ro = new ResizeObserver(() => {
        try { fit.fit() } catch {}
      })
      ro.observe(termContainerRef.current)
    }

    return () => {
      try { dataHandler.dispose() } catch {}
      try { resizeHandler.dispose() } catch {}
      window.removeEventListener('resize', onWinResize)
      window.visualViewport?.removeEventListener('resize', onWinResize)
      window.visualViewport?.removeEventListener('scroll', onWinResize)
      if (ro) ro.disconnect()
      try { ws.close() } catch {}
      try { term.dispose() } catch {}
      wsRef.current = null
      termRef.current = null
      fitRef.current = null
    }
  }, [appId, isRunning])

  return (
    <div className="edge-container-shell" style={{ display: 'flex', flexDirection: 'column', height: '100%',
      background: '#0b1220', overflow: 'hidden' }}>
      {/* 状态栏 */}
      <div className="edge-container-shell-status" style={{ display: 'flex', alignItems: 'center', gap: 8,
        padding: '8px 12px', borderBottom: `1px solid ${T.ink2}`,
        background: '#0f172a', flexShrink: 0 }}>
        <Icon name="terminal" size={14} stroke={1.8} style={{ color: '#60a5fa' }}/>
        <span style={{ fontSize: 11, color: '#94a3b8', fontFamily: 'ui-monospace, monospace' }}>
          容器 Shell · {appId}
        </span>
        <span style={{ flex: 1 }}/>
        <span style={{
          padding: '1px 6px', borderRadius: 3, fontSize: 10,
          background: status === 'open' ? '#065f46' :
                      status === 'connecting' ? '#854d0e' :
                      status === 'disabled' ? '#374151' : '#7f1d1d',
          color: '#f8fafc',
        }}>
          {status === 'open' ? '已连接' :
           status === 'connecting' ? '连接中...' :
           status === 'closed' ? '已断开' :
           status === 'disabled' ? '应用未运行' : '错误'}
        </span>
      </div>

      {/* xterm 容器（占满剩余空间） */}
      <div style={{ flex: 1, position: 'relative', overflow: 'hidden', padding: 8 }}>
        {status === 'disabled' && (
          <div style={{ color: '#94a3b8', fontSize: 12.5, padding: 12, fontFamily: 'ui-monospace, monospace' }}>
            应用未运行，无法打开容器 Shell（应用 status = Running 时启用）
          </div>
        )}
        {errorMsg && (
          <div style={{ color: '#fca5a5', fontSize: 12.5, padding: 12, fontFamily: 'ui-monospace, monospace' }}>
            {errorMsg}
          </div>
        )}
        <div ref={termContainerRef} style={{
          width: '100%', height: '100%',
          display: (status === 'disabled' || errorMsg) ? 'none' : 'block',
        }}/>
      </div>
    </div>
  )
}
