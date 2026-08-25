import { useState, useEffect } from 'react'
import { Icon } from '../icons'
import { localLoginInputError } from './loginValidation'

// humanizeAuthError 把 /auth/login 失败响应映射成给用户看的中文提示。
// OIDC 标准 error code 直接返「invalid_grant」/「invalid_client」这种术语
// 用户看不懂；这里集中翻译。未识别的 code 回退到原 error_description / error。
const AUTH_ERROR_MAP = {
  invalid_grant:        '用户名或密码错误',
  invalid_client:       '客户端配置错误，请联系管理员',
  invalid_request:      '请求格式错误',
  unauthorized_client:  '客户端未授权，请联系管理员',
  unsupported_grant_type: '不支持的认证方式',
  invalid_scope:        '权限范围错误',
  access_denied:        '访问被拒绝',
  server_error:         '云端认证服务异常',
  temporarily_unavailable: '云端认证服务暂时不可用',
}
function humanizeAuthError(data) {
  if (!data) return '登录失败'
  const mapped = AUTH_ERROR_MAP[data.error]
  if (mapped) return mapped
  return data.error_description || data.error || '登录失败'
}

// LoginScreen v3 — 按 Edge-X 登录设计稿（local-dash.zip / login-final.png）落地
//
// 设计意图：「这台机器是谁」比「你是谁」更重要 —— 左侧 brand panel 用 K8s Node
// 注册名 + 节点别名 + 硬件规格让用户一眼确认连对了机器；右侧只做最小账号登录。
//
// 后端契约（一字不动）：
//   GET  /api/v1/cloud/status     10s 轮询 → {cloudOnline, lastSyncTime}
//   GET  /api/v1/about            启动一次 → {deviceName, alias, model,
//                                  computePower, memoryTotal, uptime,
//                                  uptimeHuman, agentVersion, osVersion}
//   POST /auth/login              {username, password} → {access_token, source}
//   onLogin(token, username, source) 回调进入控制台
//
// 未实现的云端能力：
//   - 扫码授权 tab + QR
// 保持登录只选择浏览器存储位置，不延长后端 session_ttl；忘记密码入口
// 说明本机配置重置方式，不提供邮件找回。
//
// brand panel 字段降级：每段「有数据才渲染」，绝不用 fallback 假数据。
//   - alias 为空：副标题段不渲染（首启用户未登录前的预期状态）
//   - model 为空：灰字段不渲染
//   - 3 个 BrandStat 单独判断，任一缺失不渲染对应卡片

export function LoginScreen({ onLogin, deviceName }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
	const [accessCode, setAccessCode] = useState('')
	const [otp, setOTP] = useState('')
	const [authRequirements, setAuthRequirements] = useState({ passwordRequired: true, accessCodeRequired: false, twoFactorRequired: false })
	const [showPwd, setShowPwd] = useState(false)
	const [persistence, setPersistence] = useState('persistent')
	const [showResetHelp, setShowResetHelp] = useState(false)
  const [phase, setPhase] = useState('idle') // idle | verifying | success
  const [error, setError] = useState('')
  const [about, setAbout] = useState(null)
  const clock = useLoginClock()

  // 设备信息 —— devbox 用 /api/v1/device (免鉴权白名单)，字段映射到
  // LoginScreen 原本的 about schema (deviceName/model/computePower/osVersion/…)。
  useEffect(() => {
    let cancelled = false
    fetch('/api/v1/device')
      .then(r => r.ok ? r.json() : null)
      .then(d => {
        if (cancelled || !d) return
        setAbout({
          deviceName:   d.hostname || '',
          alias:        d.ip || '',   // 用 LAN IP 顶替云端 alias
          model:        d.cpuModel || '',
          computePower: d.cpuCores ? `${d.cpuCores} 核 CPU` : '',
          memoryTotal:  d.memoryTotal || 0,
          uptime:       d.uptime || 0,
          uptimeHuman:  d.uptimeHuman || '',
          agentVersion: d.agentVersion || '',
          osVersion:    d.platform || '',
        })
      })
      .catch(() => {})
    return () => { cancelled = true }
  }, [])

  useEffect(() => {
    let cancelled = false
    fetch('/api/v1/auth/status')
      .then(r => r.ok ? r.json() : null)
      .then(d => {
        if (!cancelled && d) setAuthRequirements({
          passwordRequired: !!d.passwordRequired,
          accessCodeRequired: !!d.accessCodeRequired,
          twoFactorRequired: !!d.twoFactorRequired,
        })
      })
      .catch(() => {})
    return () => { cancelled = true }
  }, [])

  // Local credentials are checked first; access code and TOTP follow before
  // the server creates a session.
  const submit = async (e) => {
    if (e) e.preventDefault()
    if (phase !== 'idle') return
    const cleanPassword = password.trim()
    const inputError = localLoginInputError(username, cleanPassword, authRequirements.passwordRequired)
    if (inputError) { setError(inputError); return }
    setError('')
    setPhase('verifying')
    try {
      const r = await fetch('/api/v1/auth/verify', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: username.trim(), password: cleanPassword, accessCode, otp }),
      })
      const data = await r.json().catch(() => ({}))
      if (data.twoFactorRequired) {
        setAuthRequirements(current => ({ ...current, twoFactorRequired: true }))
        setError(data.message || '请输入动态验证码或恢复码')
        setPhase('idle')
        return
      }
      if (!r.ok || !data.authenticated) {
        setError(data.message || '密码错误')
        setPhase('idle')
        return
      }
      setPhase('success')
		setTimeout(() => onLogin(data.token || '', data.username || 'admin', persistence), 600)
    } catch {
      setError('无法连接到服务器')
      setPhase('idle')
    }
  }

  // 后端 deviceName prop 优先（来自 App.jsx），其次 about.deviceName，再 fallback "本地节点"
  const nodeName = deviceName || about?.deviceName || '本地节点'
  const alias = about?.alias || ''
  const model = about?.model || ''

  return (
    <div style={{
      display: 'flex', width: '100vw', height: '100vh', overflow: 'hidden', background: '#fff',
      fontFamily: '"PingFang SC", "Noto Sans SC", -apple-system, BlinkMacSystemFont, "Microsoft YaHei", "Segoe UI", Arial, sans-serif',
      WebkitFontSmoothing: 'antialiased',
    }}>
      <style>{loginCSS}</style>

      {/* ───── Left: Brand Panel ───── */}
      <div className="login-brand" style={{
        position: 'relative', width: '52%', flexShrink: 0, overflow: 'hidden',
        display: 'flex', flexDirection: 'column', padding: '46px 52px',
        color: '#e2e8f0',
        background: 'linear-gradient(110deg, #4a5568 0.26%, #3a424f 97.78%)',
        backgroundImage:
          'radial-gradient(ellipse 70% 50% at 22% 12%, rgba(91,127,184,0.45), transparent 60%),' +
          'radial-gradient(ellipse 60% 45% at 90% 96%, rgba(43,95,107,0.40), transparent 60%),' +
          'radial-gradient(ellipse 45% 40% at 72% 28%, rgba(0,102,255,0.18), transparent 60%),' +
          'linear-gradient(110deg, #4a5568 0.26%, #3a424f 97.78%)',
      }}>
        {/* Brand mark */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <div style={{
            width: 40, height: 40, borderRadius: 11,
            background: 'linear-gradient(150deg,#3388ff,#005eeb)',
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            boxShadow: '0 6px 18px -4px rgba(0,102,255,0.6), inset 0 1px 0 rgba(255,255,255,0.4)',
          }}>
            <Icon name="cpu" size={21} stroke={1.8} style={{ color: '#fff' }}/>
          </div>
          <div>
            <div style={{ fontSize: 17, fontWeight: 700, color: '#fff', letterSpacing: '-0.01em' }}>EDGE-X</div>
            <div style={{ fontSize: 11.5, color: 'rgba(226,232,240,0.5)' }}>GPU开发桌面</div>
          </div>
        </div>

        {/* Device hero — 居中带状区 */}
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', justifyContent: 'center', maxWidth: 520 }}>
          <h1 className="login-mono" style={{
            fontSize: 38, fontWeight: 700, color: '#fff', margin: '20px 0 0',
            letterSpacing: '-0.02em', lineHeight: 1.05, wordBreak: 'break-all',
          }}>{nodeName}</h1>

          {alias && (
            <div style={{ fontSize: 15, color: 'rgba(226,232,240,0.7)', marginTop: 10 }}>{alias}</div>
          )}

          {model && (
            <div style={{ fontSize: 12.5, color: 'rgba(226,232,240,0.45)', marginTop: 14, lineHeight: 1.6 }}>
              {model}
            </div>
          )}

          <BrandStats about={about}/>
        </div>

        {/* Footer */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 14, fontSize: 11.5, color: 'rgba(226,232,240,0.4)', flexWrap: 'wrap' }}>
          {about?.agentVersion && <span className="login-mono">Edge Platform {about.agentVersion}</span>}
          {about?.agentVersion && about?.osVersion && <span>·</span>}
          {about?.osVersion && <span>{about.osVersion}</span>}
          <div style={{ flex: 1, minWidth: 8 }}/>
          <span className="login-mono login-tnum">{clock}</span>
        </div>
      </div>

      {/* ───── Right: Form Panel ───── */}
      <div style={{
        flex: 1, minWidth: 0, display: 'flex', alignItems: 'center', justifyContent: 'center',
        padding: '32px 40px', background: '#fff',
      }}>
        <div style={{ width: '100%', maxWidth: 360 }}>
          {phase === 'success' ? (
            <SuccessPanel username={username}/>
          ) : (
            <>
              <div style={{ fontSize: 23, fontWeight: 700, color: T.ink, letterSpacing: '-0.01em' }}>欢迎回来</div>
              <div style={{ fontSize: 13.5, color: T.ink3, marginTop: 7 }}>登录以进入本地控制台</div>

              <form onSubmit={submit} style={{ marginTop: 26, display: 'flex', flexDirection: 'column', gap: 12 }}>
                <div>
                  <label style={lblStyle}>账号</label>
                  <Field icon="user" value={username} onChange={setUsername}
                    placeholder="本地账号" type="text" autoFocus autoComplete="username"/>
                </div>
                {authRequirements.passwordRequired && (
                  <div>
                    <label style={lblStyle}>密码</label>
                    <Field
                      icon="lock" type={showPwd ? 'text' : 'password'}
                      value={password} onChange={setPassword}
                      placeholder="登录密码" autoComplete="current-password"
                      trailing={
                        <button type="button" onClick={() => setShowPwd(v => !v)} style={{
                          border: 'none', background: 'transparent', cursor: 'pointer',
                          color: showPwd ? T.blue : T.ink4, padding: 4, display: 'flex',
                        }} aria-label={showPwd ? '隐藏密码' : '显示密码'}>
                          <Icon name="eye" size={16} stroke={1.8}/>
                        </button>
                      }/>
                  </div>
                )}

                {authRequirements.accessCodeRequired && (
                  <div>
                    <label style={lblStyle}>访问码</label>
                    <Field icon="shield" type="password" value={accessCode} onChange={setAccessCode}
                      placeholder="共享访问码" autoComplete="off"/>
                  </div>
                )}

                {authRequirements.twoFactorRequired && (
                  <div>
                    <label style={lblStyle}>双重验证码</label>
                    <Field icon="key" type="text" value={otp} onChange={setOTP}
                      placeholder="6 位动态码或恢复码" autoComplete="one-time-code"/>
                  </div>
                )}

				{error && (
                  <div style={{ display: 'flex', alignItems: 'center', gap: 6, color: T.red, fontSize: 12 }}>
                    <Icon name="alertTri" size={13} stroke={1.9}/>{error}
                  </div>
				)}

				<div style={{ display: 'flex', alignItems: 'center', gap: 10, marginTop: 2 }}>
					<label htmlFor="login-persistence" style={{ fontSize: 12, color: T.ink3, whiteSpace: 'nowrap' }}>保持登录</label>
					<select id="login-persistence" value={persistence} onChange={(e) => setPersistence(e.target.value)} style={{
						flex: 1, height: 32, borderRadius: 7, border: `1px solid ${T.border}`,
						background: '#fff', color: T.ink2, padding: '0 8px', fontSize: 12,
					}}>
						<option value="persistent">保持至服务端会话过期</option>
						<option value="session">仅本次浏览器会话</option>
					</select>
				</div>

                <button type="submit" disabled={phase === 'verifying' || !username || (authRequirements.passwordRequired && !password)} style={{
                  height: 44, marginTop: 6, borderRadius: 12, border: 'none',
                  cursor: phase === 'verifying' || !username || (authRequirements.passwordRequired && !password) ? 'default' : 'pointer',
                  background: phase === 'verifying' || !username || (authRequirements.passwordRequired && !password)
                    ? '#c9c9c9'
                    : '#0066ff',
                  color: '#fff', fontSize: 14.5, fontWeight: 600,
                  display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 9,
                  boxShadow: phase === 'verifying' || !username || (authRequirements.passwordRequired && !password)
                    ? 'none'
                    : '0 8px 20px -8px rgba(0,102,255,0.6)',
                  transition: 'background .15s, box-shadow .15s',
                }}>
                  {phase === 'verifying' ? (
                    <><Spinner/>验证中…</>
                  ) : (
                    <>登录控制台<Icon name="chevRight" size={17} stroke={2.2}/></>
                  )}
				</button>
				<button type="button" onClick={() => setShowResetHelp(true)} style={{
					alignSelf: 'center', border: 'none', background: 'transparent', color: T.blue,
					fontSize: 12, cursor: 'pointer', padding: '4px 8px',
				}}>忘记密码？</button>
			</form>

              {/* Bottom note */}
              <div style={{
                marginTop: 26, paddingTop: 16, borderTop: `1px solid ${T.borderSoft}`,
                display: 'flex', alignItems: 'center', gap: 7,
                fontSize: 11, color: T.ink3, lineHeight: 1.5,
              }}>
                <Icon name="shield" size={13} stroke={1.8} style={{ color: T.ink4, flexShrink: 0 }}/>
				凭据只提交到当前 DevBox，本地会话过期后需重新登录。
              </div>
            </>
          )}
		</div>
		{showResetHelp && <ResetHelp onClose={() => setShowResetHelp(false)}/>}
	</div>
    </div>
  )
}

function ResetHelp({ onClose }) {
	return (
		<div role="presentation" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose() }} style={{
			position: 'fixed', inset: 0, zIndex: 100, display: 'grid', placeItems: 'center', padding: 20,
			background: 'rgba(15,23,42,0.42)', backdropFilter: 'blur(3px)',
		}}>
			<div role="dialog" aria-modal="true" aria-labelledby="reset-help-title" style={{
				width: 430, maxWidth: '100%', borderRadius: 10, background: '#fff',
				boxShadow: '0 24px 70px rgba(15,23,42,0.25)', overflow: 'hidden',
			}}>
				<div style={{ padding: '16px 18px', borderBottom: `1px solid ${T.borderSoft}`, display: 'flex', alignItems: 'center', gap: 10 }}>
					<div id="reset-help-title" style={{ flex: 1, fontSize: 15, fontWeight: 700, color: T.ink }}>重置本地登录密码</div>
					<button type="button" onClick={onClose} aria-label="关闭" style={{ width: 30, height: 30, border: 'none', background: 'transparent', color: T.ink3, cursor: 'pointer' }}>
						<Icon name="x" size={16} stroke={2}/>
					</button>
				</div>
				<div style={{ padding: 18, color: T.ink2, fontSize: 12.5, lineHeight: 1.75 }}>
					<div>在 DevBox 主机上编辑生效配置，将 <code style={codeStyle}>auth.password</code> 更新为新密码，或设置 <code style={codeStyle}>DEVBOX_AUTH_PASSWORD</code>。</div>
					<div style={{ marginTop: 10 }}>随后通过当前部署使用的 supervisor、systemd 或容器编排方式重启 DevBox。配置文件默认从 <code style={codeStyle}>/etc/devbox/config.yaml</code> 读取。</div>
					<div style={{ marginTop: 10, color: T.ink3 }}>密码不会通过网页或邮件找回；已有会话会在进程重启后失效。</div>
				</div>
				<div style={{ padding: '12px 18px', borderTop: `1px solid ${T.borderSoft}`, display: 'flex', justifyContent: 'flex-end' }}>
					<button type="button" onClick={onClose} style={{ height: 34, padding: '0 14px', borderRadius: 7, border: 'none', background: T.blue, color: '#fff', fontWeight: 600, cursor: 'pointer' }}>知道了</button>
				</div>
			</div>
		</div>
	)
}

const codeStyle = { padding: '2px 5px', borderRadius: 4, background: '#f1f5f9', color: '#0f172a', fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace' }

// ─── Style constants ──────────────────────────────────────────────
const T = {
  ink: '#0f172a', ink2: '#334155', ink3: '#64748b', ink4: '#94a3b8',
  border: '#e2e8f0', borderSoft: '#eef1f5', surfaceAlt: '#f8fafc',
  blue: '#0066ff', blueDeep: '#005eeb',
  red: '#ef4444', green: '#10b981',
}

const lblStyle = {
  display: 'block', fontSize: 11.5, fontWeight: 600, color: T.ink2, marginBottom: 6,
}

// CSS injected once（@media + 字体 feature settings + spin keyframes）
const loginCSS = `
  .login-mono { font-family: "JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, monospace; font-feature-settings: "tnum"; }
  .login-tnum { font-variant-numeric: tabular-nums; }
  @keyframes login-spin { to { transform: rotate(360deg); } }
  @media (max-width: 880px) {
    .login-brand { display: none !important; }
  }
`

// ─── BrandStats ───────────────────────────────────────────────────
// 3 个可能卡片：算力 / 统一内存 / 在线时长。每个独立判断"有字段才渲染"。
// 全部缺则整段不渲染。
function BrandStats({ about }) {
  const cards = []

  if (about?.computePower) {
    cards.push({ icon: 'bolt', label: '算力', value: about.computePower })
  }
  if (about?.memoryTotal > 0) {
    const memGB = Math.round(about.memoryTotal / (1024 * 1024 * 1024))
    // label 用 model 字符串里的"统一内存"语义反推（GB10/GH200 这种 SoC 才叫统一内存）
    // 非 SoC 场景叫"内存"避免误导。model 串里已经带了完整语义，这里只决定卡片 label
    const memLabel = about?.model?.includes('统一内存') ? '统一内存' : '内存'
    cards.push({ icon: 'memory', label: memLabel, value: `${memGB} GB` })
  }
  if (about?.uptimeHuman) {
    cards.push({ icon: 'history', label: '在线', value: about.uptimeHuman })
  }

  if (cards.length === 0) return null

  return (
    <div style={{ display: 'flex', gap: 10, marginTop: 28, flexWrap: 'wrap' }}>
      {cards.map((c, i) => (
        <div key={i} style={{
          flex: '1 1 140px', minWidth: 140, padding: '12px 14px', borderRadius: 11,
          background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)',
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6, color: 'rgba(226,232,240,0.55)' }}>
            <Icon name={c.icon} size={13} stroke={1.7}/>
            <span style={{ fontSize: 11 }}>{c.label}</span>
          </div>
          <div className="login-mono login-tnum" style={{
            fontSize: 16, fontWeight: 700, color: '#f1f5f9',
            marginTop: 6, letterSpacing: '-0.01em',
          }}>{c.value}</div>
        </div>
      ))}
    </div>
  )
}

// ─── Field ────────────────────────────────────────────────────────
function Field({ icon, type = 'text', value, onChange, placeholder, trailing, autoFocus, autoComplete }) {
  const [focus, setFocus] = useState(false)
  return (
    <div style={{
      display: 'flex', alignItems: 'center', gap: 10, height: 44, padding: '0 12px',
      borderRadius: 12, background: focus ? '#ffffff' : T.surfaceAlt,
      border: `1.5px solid ${focus ? T.blue : T.border}`,
      boxShadow: focus ? '0 0 0 3px rgba(0,102,255,0.12)' : 'none',
      transition: 'border-color .15s, box-shadow .15s, background .15s',
    }}>
      <Icon name={icon} size={17} stroke={1.8}
        style={{ color: focus ? T.blue : T.ink4, flexShrink: 0 }}/>
      <input
        type={type} value={value} placeholder={placeholder}
        autoFocus={autoFocus} autoComplete={autoComplete}
        onChange={(e) => onChange(e.target.value)}
        onFocus={() => setFocus(true)} onBlur={() => setFocus(false)}
        style={{
          flex: 1, minWidth: 0, border: 'none', outline: 'none', background: 'transparent',
          fontSize: 14, color: T.ink,
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
    <div style={{ textAlign: 'center' }}>
      <div style={{
        width: 60, height: 60, borderRadius: '50%', margin: '0 auto 16px',
        background: '#ecfdf5', color: T.green,
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        boxShadow: '0 0 0 7px #d1fae5',
      }}>
        <Icon name="check" size={30} stroke={2.4}/>
      </div>
      <div style={{ fontSize: 18, fontWeight: 700, color: T.ink }}>登录成功</div>
      <div style={{ fontSize: 13, color: T.ink3, marginTop: 8, lineHeight: 1.6 }}>
        欢迎回来，<span className="login-mono" style={{ color: T.ink2, fontWeight: 600 }}>{username}</span>
        <br/>正在进入控制台…
      </div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', marginTop: 18 }}>
        <Spinner color={T.blue} size={20}/>
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

// ─── useLoginClock ────────────────────────────────────────────────
function useLoginClock() {
  const [s, setS] = useState('')
  useEffect(() => {
    const p = (n) => String(n).padStart(2, '0')
    const fmt = () => {
      const d = new Date()
      setS(`${d.getFullYear()}/${p(d.getMonth() + 1)}/${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`)
    }
    fmt()
    const id = setInterval(fmt, 1000)
    return () => clearInterval(id)
  }, [])
  return s
}
