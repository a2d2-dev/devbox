import { useCallback, useEffect, useState } from 'react'
import { T } from '../../tokens'
import { Icon } from '../../icons'
import { authFetch, clearAuth, getAuthToken } from '../../hooks/useApi'

// ProfilePanel —「个人设置 → 我的账号」tab 的真实内容（issue #30 T5）。
//
// 三块内容：资料卡 / 编辑显示名 / 修改密码，末尾一个退出登录按钮。
// 全部走真实端点（GET /api/v1/account、PATCH /api/v1/account、
// POST /api/v1/account/password），无 mock。样式取自 tokens.js 的 T.*，
// 与 Account.jsx 现有卡片外壳一致。密码字段永不写入 log/console。
//
// 注意：修改密码用 localAuthFetch 而非 authFetch。后端在「当前密码错误」时
// 返回 401（reason=invalid_current_password），而 authFetch 把任何 401 都当成
// 会话过期 → clearAuth + 跳回登录页。改密的 401 是业务错误、不是会话失效，
// 必须就地渲染中文提示，因此这里带 token 直接 fetch，不触发全局登出。

const panel = {
  background: T.surface, border: `1px solid ${T.border}`, borderRadius: 8,
  overflow: 'hidden', boxShadow: '0 1px 2px rgba(15,23,42,0.04)',
}

const cardHead = {
  minHeight: 43, padding: '0 16px', display: 'flex', alignItems: 'center', gap: 8,
  background: T.surfaceAlt, borderBottom: `1px solid ${T.borderSoft}`,
}

const inputStyle = {
  width: '100%', height: 34, padding: '0 10px', borderRadius: 6,
  border: `1px solid ${T.border}`, background: T.surface, color: T.ink,
  fontSize: 12.5, fontFamily: T.sans, outline: 'none', boxSizing: 'border-box',
}

const labelStyle = { fontSize: 11.5, fontWeight: 600, color: T.ink2, marginBottom: 5, display: 'block' }

function primaryBtn(disabled) {
  return {
    height: 32, padding: '0 16px', borderRadius: 6, border: 0,
    background: disabled ? T.ink4 : T.blueDeep, color: 'white',
    fontSize: 12, fontWeight: 600, cursor: disabled ? 'not-allowed' : 'pointer',
    fontFamily: T.sans,
  }
}

function ghostBtn() {
  return {
    height: 32, padding: '0 14px', borderRadius: 6, border: `1px solid ${T.border}`,
    background: T.surface, color: T.ink2, fontSize: 12, fontWeight: 600,
    cursor: 'pointer', fontFamily: T.sans,
  }
}

// firstChar 取首字符做首字母头像，displayName 优先，回退 username。
function firstChar(displayName, username) {
  const s = (displayName || username || '?').trim()
  return s ? Array.from(s)[0].toUpperCase() : '?'
}

function roleBadge(role) {
  const admin = role === 'admin'
  return (
    <span style={{
      padding: '2px 9px', borderRadius: 999, fontSize: 10.5, fontWeight: 700, letterSpacing: 0.4,
      background: admin ? T.blueSoft : T.surfaceAlt,
      color: admin ? T.blueDeep : T.ink2,
      border: `1px solid ${admin ? T.blueSoft : T.border}`,
    }}>{admin ? '管理员' : '普通用户'}</span>
  )
}

function formatDate(iso) {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleString('zh-CN', { hour12: false })
}

// 后端 PATCH 错误 reason → 中文提示。
function patchErrorText(reason, fallback) {
  switch (reason) {
    case 'not_a_managed_account': return '当前账号不支持修改资料（非受管账号）。'
    case 'no_change': return '显示名不能为空。'
    default: return fallback || '保存失败，请稍后重试。'
  }
}

// 后端改密错误 reason → 中文提示。
function passwordErrorText(reason, fallback) {
  switch (reason) {
    case 'invalid_current_password': return '当前密码错误。'
    case 'weak_password': return '新密码强度不足：至少 10 位，且需包含大写、小写、数字、符号中的至少 3 类。'
    case 'not_a_managed_account': return '当前账号不支持修改密码（非受管账号）。'
    default: return fallback || '修改失败，请稍后重试。'
  }
}

// localAuthFetch 带 token 直接 fetch，但不接管 401 全局登出逻辑。
// 仅用于「401 是业务错误而非会话失效」的场景（改密的当前密码错误）。
async function localAuthFetch(url, opts = {}) {
  const token = getAuthToken()
  const headers = { ...(opts.headers || {}) }
  if (token) headers.Authorization = `Bearer ${token}`
  return fetch(url, { ...opts, headers })
}

async function readReason(resp) {
  try {
    const j = await resp.json()
    return { reason: j?.reason || '', message: j?.error || '' }
  } catch {
    return { reason: '', message: '' }
  }
}

// ─── 资料卡 ──────────────────────────────────────────────────────

function ProfileCard({ account, loading, error, onRetry, onEdited }) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const [saving, setSaving] = useState(false)
  const [saveErr, setSaveErr] = useState('')

  const startEdit = () => {
    setDraft(account?.displayName || '')
    setSaveErr('')
    setEditing(true)
  }
  const cancelEdit = () => { setEditing(false); setSaveErr('') }

  const submit = async () => {
    const next = draft.trim()
    if (!next) { setSaveErr('显示名不能为空。'); return }
    if (next === (account?.displayName || '')) { setEditing(false); return }
    setSaving(true)
    setSaveErr('')
    try {
      const resp = await authFetch('/api/v1/account', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ displayName: next }),
      })
      if (!resp.ok) {
        const { reason, message } = await readReason(resp)
        setSaveErr(patchErrorText(reason, message))
        return
      }
      const updated = await resp.json().catch(() => null)
      setEditing(false)
      onEdited(updated)
    } catch {
      setSaveErr('网络错误，请稍后重试。')
    } finally {
      setSaving(false)
    }
  }

  return (
    <section style={panel}>
      <div style={cardHead}>
        <Icon name="user" size={15} style={{ color: T.blueDeep }}/>
        <h2 style={{ margin: 0, fontSize: 13.5, fontWeight: 700, color: T.ink }}>账户资料</h2>
      </div>

      {loading && !account ? (
        <div style={{ padding: '32px 16px', fontSize: 12.5, color: T.ink3 }}>正在加载账户信息…</div>
      ) : error && !account ? (
        <div style={{ padding: '28px 16px', display: 'flex', flexDirection: 'column', gap: 10, alignItems: 'flex-start' }}>
          <div style={{ fontSize: 12.5, color: T.red }}>账户信息加载失败，请检查网络后重试。</div>
          <button type="button" onClick={onRetry} style={ghostBtn()}>重新加载</button>
        </div>
      ) : account ? (
        <div style={{ padding: 16, display: 'flex', gap: 16, alignItems: 'flex-start', flexWrap: 'wrap' }}>
          <div style={{
            width: 64, height: 64, borderRadius: '50%', flexShrink: 0,
            display: 'grid', placeItems: 'center', background: T.blueSoft, color: T.blueDeep,
            fontSize: 26, fontWeight: 700, userSelect: 'none',
          }}>{firstChar(account.displayName, account.username)}</div>

          <div style={{ flex: 1, minWidth: 220 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 9, flexWrap: 'wrap' }}>
              {!editing && (
                <span style={{ fontSize: 16, fontWeight: 700, color: T.ink }}>
                  {account.displayName || account.username}
                </span>
              )}
              {!editing && roleBadge(account.role)}
              {!editing && (
                <button
                  type="button"
                  onClick={startEdit}
                  aria-label="编辑显示名"
                  style={{ ...ghostBtn(), height: 26, padding: '0 9px', display: 'inline-flex', alignItems: 'center', gap: 5, fontSize: 11.5 }}
                >
                  <Icon name="edit" size={13}/>编辑
                </button>
              )}
            </div>

            {editing && (
              <div style={{ marginTop: 2, maxWidth: 340 }}>
                <label style={labelStyle}>显示名</label>
                <input
                  style={inputStyle}
                  value={draft}
                  maxLength={64}
                  autoFocus
                  disabled={saving}
                  onChange={(e) => setDraft(e.target.value)}
                  onKeyDown={(e) => { if (e.key === 'Enter') submit(); if (e.key === 'Escape') cancelEdit() }}
                />
                {saveErr && <div style={{ marginTop: 6, fontSize: 11.5, color: T.red }}>{saveErr}</div>}
                <div style={{ marginTop: 10, display: 'flex', gap: 8 }}>
                  <button type="button" style={primaryBtn(saving)} disabled={saving} onClick={submit}>
                    {saving ? '保存中…' : '保存'}
                  </button>
                  <button type="button" style={ghostBtn()} disabled={saving} onClick={cancelEdit}>取消</button>
                </div>
              </div>
            )}

            {!editing && (
              <dl style={{ margin: '14px 0 0', display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '8px 16px', fontSize: 12.5 }}>
                <dt style={{ color: T.ink3 }}>用户名</dt>
                <dd style={{ margin: 0, color: T.ink }}>{account.username}</dd>
                <dt style={{ color: T.ink3 }}>显示名</dt>
                <dd style={{ margin: 0, color: T.ink }}>{account.displayName || '—'}</dd>
                <dt style={{ color: T.ink3 }}>创建时间</dt>
                <dd style={{ margin: 0, color: T.ink }}>{formatDate(account.createdAt)}</dd>
              </dl>
            )}
          </div>
        </div>
      ) : null}
    </section>
  )
}

// ─── 修改密码卡 ──────────────────────────────────────────────────

function PasswordCard() {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')
  const [done, setDone] = useState(false)

  const reset = () => { setCurrent(''); setNext(''); setConfirm('') }

  const submit = async (e) => {
    e.preventDefault()
    setErr('')
    setDone(false)
    if (!current || !next || !confirm) { setErr('请填写所有密码字段。'); return }
    if (next !== confirm) { setErr('两次输入的新密码不一致。'); return }
    if (next.length < 10) { setErr('新密码至少需要 10 位。'); return }
    setSaving(true)
    try {
      const resp = await localAuthFetch('/api/v1/account/password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ currentPassword: current, newPassword: next }),
      })
      if (resp.status === 204) {
        setDone(true)
        reset()
        return
      }
      const { reason, message } = await readReason(resp)
      setErr(passwordErrorText(reason, message))
    } catch {
      setErr('网络错误，请稍后重试。')
    } finally {
      setSaving(false)
    }
  }

  return (
    <section style={panel}>
      <div style={cardHead}>
        <Icon name="lock" size={15} style={{ color: T.blueDeep }}/>
        <h2 style={{ margin: 0, fontSize: 13.5, fontWeight: 700, color: T.ink }}>修改密码</h2>
      </div>
      <form onSubmit={submit} style={{ padding: 16, maxWidth: 360 }}>
        <div style={{ marginBottom: 12 }}>
          <label style={labelStyle} htmlFor="pw-current">当前密码</label>
          <input id="pw-current" type="password" autoComplete="current-password"
            style={inputStyle} value={current} disabled={saving}
            onChange={(e) => { setCurrent(e.target.value); setErr(''); setDone(false) }}/>
        </div>
        <div style={{ marginBottom: 12 }}>
          <label style={labelStyle} htmlFor="pw-new">新密码</label>
          <input id="pw-new" type="password" autoComplete="new-password"
            style={inputStyle} value={next} disabled={saving}
            onChange={(e) => { setNext(e.target.value); setErr(''); setDone(false) }}/>
          <div style={{ marginTop: 5, fontSize: 11, color: T.ink3, lineHeight: 1.5 }}>
            至少 10 位，且包含大写、小写、数字、符号中的至少 3 类。
          </div>
        </div>
        <div style={{ marginBottom: 12 }}>
          <label style={labelStyle} htmlFor="pw-confirm">确认新密码</label>
          <input id="pw-confirm" type="password" autoComplete="new-password"
            style={inputStyle} value={confirm} disabled={saving}
            onChange={(e) => { setConfirm(e.target.value); setErr(''); setDone(false) }}/>
        </div>

        {err && <div style={{ marginBottom: 10, fontSize: 11.5, color: T.red }}>{err}</div>}
        {done && (
          <div style={{
            marginBottom: 10, fontSize: 11.5, color: T.green, background: T.greenSoft,
            border: `1px solid ${T.greenSoft}`, borderRadius: 6, padding: '8px 10px', lineHeight: 1.5,
          }}>密码已修改，其它设备已退出登录。</div>
        )}

        <button type="submit" style={primaryBtn(saving)} disabled={saving}>
          {saving ? '提交中…' : '修改密码'}
        </button>
      </form>
    </section>
  )
}

// ─── 退出登录卡 ──────────────────────────────────────────────────

function LogoutCard() {
  const [busy, setBusy] = useState(false)

  // 复用 App.jsx handleLogout 同一逻辑：POST /auth/logout（fire-and-forget）→
  // 清本地会话 → 整页重载回登录页。这里不引 App 状态，直接 reload 最稳。
  const logout = async () => {
    setBusy(true)
    const token = getAuthToken()
    if (token) {
      try {
        await fetch('/api/v1/auth/logout', {
          method: 'POST',
          headers: { Authorization: `Bearer ${token}` },
        })
      } catch { /* ignore */ }
    }
    clearAuth()
    window.location.reload()
  }

  return (
    <section style={panel}>
      <div style={cardHead}>
        <Icon name="logout" size={15} style={{ color: T.red }}/>
        <h2 style={{ margin: 0, fontSize: 13.5, fontWeight: 700, color: T.ink }}>退出登录</h2>
      </div>
      <div style={{ padding: 16, display: 'flex', flexDirection: 'column', gap: 12, alignItems: 'flex-start' }}>
        <div style={{ fontSize: 12, color: T.ink3, lineHeight: 1.55 }}>
          退出后需要重新登录才能访问本机管理界面。
        </div>
        <button
          type="button"
          onClick={logout}
          disabled={busy}
          style={{
            height: 32, padding: '0 16px', borderRadius: 6,
            background: T.surface, color: T.red, border: `1px solid ${T.redSoft}`,
            fontSize: 12, fontWeight: 600, cursor: busy ? 'not-allowed' : 'pointer',
            display: 'inline-flex', alignItems: 'center', gap: 7, fontFamily: T.sans,
          }}
        >
          <Icon name="logout" size={14}/>{busy ? '正在退出…' : '退出登录'}
        </button>
      </div>
    </section>
  )
}

// ─── 面板容器 ────────────────────────────────────────────────────

export default function ProfilePanel() {
  const [account, setAccount] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  // reloadTick 驱动重新拉取（重试按钮）；fetch 全在 effect 内，避免在 effect
  // 体外同步 setState（repo eslint 的 react-hooks/set-state-in-effect），
  // 与 useApi.js 的 usePoll 同一写法。
  const [reloadTick, setReloadTick] = useState(0)
  const refresh = useCallback(() => setReloadTick((n) => n + 1), [])

  useEffect(() => {
    let alive = true
    ;(async () => {
      if (alive) { setLoading(true); setError(null) }
      try {
        const resp = await authFetch('/api/v1/account')
        if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
        const data = await resp.json()
        if (alive) setAccount(data)
      } catch (e) {
        if (alive) setError(e)
      } finally {
        if (alive) setLoading(false)
      }
    })()
    return () => { alive = false }
  }, [reloadTick])

  return (
    <>
      <div style={{ marginBottom: 12 }}>
        <h1 style={{ margin: 0, fontSize: 18, color: T.ink }}>我的账号</h1>
        <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3 }}>账户信息与安全设置</div>
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        <ProfileCard
          account={account}
          loading={loading}
          error={error}
          onRetry={refresh}
          onEdited={(updated) => { if (updated) setAccount((prev) => ({ ...prev, ...updated })) }}
        />
        <PasswordCard/>
        <LogoutCard/>
      </div>
    </>
  )
}
