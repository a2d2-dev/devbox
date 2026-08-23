import { useState, useEffect } from 'react'
import { Icon } from '../icons'

// LoginScreen v4 — fnOS 风格（LF 要求 2026-08-23：界面与飞牛 fnOS 一致）
//
// 视觉对照真机 https://10.126.126.11:15667/login（agent-browser 提取）：
//   - 壁纸：深空蓝灰渐变 linear-gradient(110deg,#4a5568 .26%,#3a424f 97.78%)
//     + 官方壁纸图（/wallpapers/fnos.webp，styles.css .edge-bg-fnos）
//   - 居中白色圆角卡片；输入框外壳 360x48 r10 纯白（浅灰底），无边框
//   - 登录按钮 360x54 r10 #0066FF，disabled 时文字 40% 白
//   - 顶部品牌 logo + 设备名；卡片下方「保持登录」/「忘记密码」布局此处
//     简化为「保持登录」占位样式（后端 7d session 无开关，不做假功能）
//
// 后端契约（一字不动，与 v3 相同）：
//   GET  /api/v1/device           启动一次 → 设备信息（免鉴权白名单）
//   POST /api/v1/auth/verify      {password} → {authenticated, token}
//   onLogin(token, username, 'local') 回调进入控制台

export function LoginScreen({ onLogin, deviceName }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [showPwd, setShowPwd] = useState(false)
  const [phase, setPhase] = useState('idle') // idle | verifying | success
  const [error, setError] = useState('')
  const [about, setAbout] = useState(null)

  // 设备信息 —— devbox 用 /api/v1/device (免鉴权白名单)
  useEffect(() => {
    let cancelled = false
    fetch('/api/v1/device')
      .then(r => r.ok ? r.json() : null)
      .then(d => {
        if (cancelled || !d) return
        setAbout({
          deviceName: d.hostname || '',
          ip:         d.ip || '',
          model:      d.cpuModel || '',
        })
      })
      .catch(() => {})
    return () => { cancelled = true }
  }, [])

  // devbox 本地认证：单密码 + session token。
  // 后端 /api/v1/auth/verify 只校验 password，username 只做前端展示。
  const submit = async (e) => {
    if (e) e.preventDefault()
    if (phase !== 'idle') return
    const cleanPassword = password.trim()
    if (!username || !cleanPassword) { setError('请输入用户名与密码'); return }
    setError('')
    setPhase('verifying')
    try {
      const r = await fetch('/api/v1/auth/verify', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password: cleanPassword }),
      })
      const data = await r.json().catch(() => ({}))
      if (!r.ok || !data.authenticated) {
        setError(data.message || '密码错误')
        setPhase('idle')
        return
      }
      setPhase('success')
      setTimeout(() => onLogin(data.token || '', username, 'local'), 900)
    } catch {
      setError('无法连接到服务器')
      setPhase('idle')
    }
  }

  const nodeName = deviceName || about?.deviceName || '本地节点'
  const canSubmit = !!username && !!password && phase === 'idle'

  return (
    <div className="edge-bg-fnos" style={{
      display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center',
      width: '100vw', height: '100vh', overflow: 'hidden',
      fontFamily: '"PingFang SC", "Noto Sans SC", -apple-system, BlinkMacSystemFont, "Microsoft YaHei", "Segoe UI", Arial, sans-serif',
      WebkitFontSmoothing: 'antialiased',
    }}>
      <style>{loginCSS}</style>

      {/* ── 品牌区：logo + 设备名（fnOS 登录页顶部构图） ── */}
      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', marginBottom: 30 }}>
        <div style={{
          width: 72, height: 72, borderRadius: 20,
          background: 'linear-gradient(150deg,#3b82f6,#0066FF)',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          boxShadow: '0 12px 32px -8px rgba(0,102,255,0.55), inset 0 1px 0 rgba(255,255,255,0.35)',
        }}>
          <Icon name="cpu" size={38} stroke={1.6} style={{ color: '#fff' }}/>
        </div>
        <div className="login-mono" style={{
          marginTop: 18, fontSize: 26, fontWeight: 700, color: '#fff',
          letterSpacing: '-0.01em',
          textShadow: '0 1px 6px rgba(0,0,0,0.2), 0 0 4px rgba(0,0,0,0.5)',
        }}>{nodeName}</div>
        <div style={{
          marginTop: 6, fontSize: 13, color: 'rgba(255,255,255,0.65)',
          textShadow: '0 1px 4px rgba(0,0,0,0.4)',
        }}>
          A2D2 Devbox 本地控制台{about?.ip ? ` · ${about.ip}` : ''}
        </div>
      </div>

      {/* ── 登录表单（fnOS 白色圆角输入壳 + #0066FF 大按钮） ── */}
      {phase === 'success' ? (
        <SuccessPanel username={username}/>
      ) : (
        <form onSubmit={submit} style={{
          display: 'flex', flexDirection: 'column', gap: 12, width: 360,
        }}>
          <FnField
            icon="user" value={username} onChange={setUsername}
            placeholder="用户名" type="text" autoFocus autoComplete="username"/>
          <FnField
            icon="lock" type={showPwd ? 'text' : 'password'}
            value={password} onChange={setPassword}
            placeholder="密码" autoComplete="current-password"
            trailing={
              <button type="button" onClick={() => setShowPwd(v => !v)} style={{
                border: 'none', background: 'transparent', cursor: 'pointer',
                color: showPwd ? '#0066FF' : '#94a3b8', padding: 4, display: 'flex',
              }} aria-label={showPwd ? '隐藏密码' : '显示密码'}>
                <Icon name="eye" size={16} stroke={1.8}/>
              </button>
            }/>

          {error && (
            <div style={{
              display: 'flex', alignItems: 'center', gap: 6,
              color: '#fca5a5', fontSize: 12.5,
              textShadow: '0 1px 4px rgba(0,0,0,0.5)',
            }}>
              <Icon name="alertTri" size={13} stroke={1.9}/>{error}
            </div>
          )}

          <button type="submit" disabled={!canSubmit} style={{
            height: 54, marginTop: 8, borderRadius: 10, border: 'none',
            cursor: canSubmit ? 'pointer' : 'default',
            background: '#0066FF',
            color: canSubmit ? '#fff' : 'rgba(255,255,255,0.4)',
            fontSize: 15, fontWeight: 600,
            display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 9,
            boxShadow: canSubmit ? '0 10px 24px -10px rgba(0,102,255,0.7)' : 'none',
            transition: 'color .15s, box-shadow .15s',
          }}>
            {phase === 'verifying' ? (<><Spinner/>验证中…</>) : '登录'}
          </button>
        </form>
      )}

      {/* ── 底部信息（fnOS 底部构图：设备型号淡字） ── */}
      <div style={{
        position: 'fixed', bottom: 28, left: 0, right: 0,
        display: 'flex', justifyContent: 'center',
        fontSize: 11.5, color: 'rgba(255,255,255,0.4)',
        textShadow: '0 1px 4px rgba(0,0,0,0.4)',
      }}>
        {about?.model || 'A2D2 Devbox'}
      </div>
    </div>
  )
}

// CSS injected once
const loginCSS = `
  .login-mono { font-family: "JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, monospace; font-feature-settings: "tnum"; }
  @keyframes login-spin { to { transform: rotate(360deg); } }
`

// ─── FnField — fnOS 风白色圆角输入壳（360x48 r10，无边框，聚焦蓝描边） ──
function FnField({ icon, type = 'text', value, onChange, placeholder, trailing, autoFocus, autoComplete }) {
  const [focus, setFocus] = useState(false)
  return (
    <div style={{
      display: 'flex', alignItems: 'center', gap: 10, height: 48, padding: '0 14px',
      borderRadius: 10, background: '#ffffff',
      border: `1px solid ${focus ? '#0066FF' : 'rgba(0,0,0,0)'}`,
      boxShadow: focus
        ? '0 0 0 3px rgba(0,102,255,0.18), 0 4px 14px -6px rgba(0,0,0,0.3)'
        : '0 4px 14px -6px rgba(0,0,0,0.3)',
      transition: 'border-color .15s, box-shadow .15s',
    }}>
      <Icon name={icon} size={17} stroke={1.8}
        style={{ color: focus ? '#0066FF' : '#94a3b8', flexShrink: 0 }}/>
      <input
        type={type} value={value} placeholder={placeholder}
        autoFocus={autoFocus} autoComplete={autoComplete}
        onChange={(e) => onChange(e.target.value)}
        onFocus={() => setFocus(true)} onBlur={() => setFocus(false)}
        style={{
          flex: 1, minWidth: 0, border: 'none', outline: 'none', background: 'transparent',
          fontSize: 14, color: '#202327',
          letterSpacing: type === 'password' ? '0.18em' : 0,
          fontFamily: 'inherit',
        }}/>
      {trailing}
    </div>
  )
}

// ─── SuccessPanel ─────────────────────────────────────────────────
function SuccessPanel({ username }) {
  return (
    <div style={{ textAlign: 'center', width: 360 }}>
      <div style={{
        width: 60, height: 60, borderRadius: '50%', margin: '0 auto 16px',
        background: 'rgba(16,185,129,0.18)', color: '#34d399',
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        boxShadow: '0 0 0 7px rgba(16,185,129,0.10)',
      }}>
        <Icon name="check" size={30} stroke={2.4}/>
      </div>
      <div style={{ fontSize: 18, fontWeight: 700, color: '#fff',
        textShadow: '0 1px 6px rgba(0,0,0,0.3)' }}>登录成功</div>
      <div style={{ fontSize: 13, color: 'rgba(255,255,255,0.7)', marginTop: 8, lineHeight: 1.6,
        textShadow: '0 1px 4px rgba(0,0,0,0.4)' }}>
        欢迎回来，<span className="login-mono" style={{ color: '#fff', fontWeight: 600 }}>{username}</span>
        <br/>正在进入控制台…
      </div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', marginTop: 18 }}>
        <Spinner color="#60a5fa" size={20}/>
      </div>
    </div>
  )
}

// ─── Spinner ──────────────────────────────────────────────────────
function Spinner({ color = '#fff', size = 16 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" style={{ animation: 'login-spin 0.9s linear infinite' }}>
      <circle cx="12" cy="12" r="9" stroke={color} strokeOpacity="0.28" strokeWidth="2.6" fill="none"/>
      <path d="M12 3a9 9 0 0 1 9 9" stroke={color} strokeWidth="2.6" strokeLinecap="round" fill="none"/>
    </svg>
  )
}
