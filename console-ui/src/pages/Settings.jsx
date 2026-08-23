import { useCallback, useEffect, useRef, useState } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { Chip, StatusDot } from '../components/ui'
import { btnDanger, btnPrimary, btnSecondary } from '../components/AppWindow'
import { authFetch } from '../hooks/useApi'

const tabs = [
  { id: 'webdav', label: 'WebDAV', icon: 'globe' },
  { id: 'smb', label: 'SMB', icon: 'server' },
  { id: 'smtp', label: '邮件通知', icon: 'send' },
  { id: 'maintenance', label: '系统维护', icon: 'wrench' },
  { id: 'defaults', label: '默认应用', icon: 'apps' },
  { id: 'about', label: '关于', icon: 'info' },
]

const field = {
  height: 34, border: `1px solid ${T.border}`, borderRadius: 6,
  padding: '0 10px', color: T.ink, background: 'white', fontSize: 12.5,
  boxSizing: 'border-box', outline: 'none', letterSpacing: 0,
}

const panel = {
  background: T.surface, border: `1px solid ${T.border}`, borderRadius: 8,
  overflow: 'hidden', boxShadow: '0 1px 2px rgba(15,23,42,0.04)',
}

async function responseMessage(response) {
  const text = await response.text()
  if (!text) return `HTTP ${response.status}`
  try {
    const data = JSON.parse(text)
    return data.message || data.error || text
  } catch {
    return text
  }
}

function Toggle({ checked, onChange, label }) {
  return (
    <label style={{ display: 'inline-flex', alignItems: 'center', gap: 8, cursor: 'pointer', color: T.ink2, fontSize: 12.5 }}>
      <input type="checkbox" checked={checked} onChange={e => onChange(e.target.checked)} style={{ width: 16, height: 16, accentColor: T.blue }}/>
      {label}
    </label>
  )
}

function FieldRow({ label, hint, children }) {
  return (
    <div className="maintenance-field-row" style={{ display: 'grid', gridTemplateColumns: '150px minmax(220px, 1fr)', gap: 18, alignItems: 'center', padding: '11px 16px', borderTop: `1px solid ${T.borderSoft}` }}>
      <div>
        <div style={{ fontSize: 12.5, fontWeight: 600, color: T.ink2 }}>{label}</div>
        {hint && <div style={{ fontSize: 10.5, color: T.ink4, marginTop: 3, lineHeight: 1.4 }}>{hint}</div>}
      </div>
      {children}
    </div>
  )
}

function Section({ title, icon, action, children }) {
  return (
    <section style={panel}>
      <div style={{ minHeight: 43, padding: '0 16px', display: 'flex', alignItems: 'center', gap: 8, background: T.surfaceAlt, borderBottom: `1px solid ${T.borderSoft}` }}>
        <Icon name={icon} size={15} style={{ color: T.blueDeep }}/>
        <h2 style={{ margin: 0, fontSize: 13.5, fontWeight: 700, color: T.ink, letterSpacing: 0 }}>{title}</h2>
        <div style={{ flex: 1 }}/>{action}
      </div>
      {children}
    </section>
  )
}

function SaveBar({ busy, onSave, message }) {
  return (
    <div style={{ position: 'sticky', bottom: 0, zIndex: 2, display: 'flex', alignItems: 'center', marginTop: 14, padding: '10px 0', background: T.surfaceAlt }}>
      <div style={{ fontSize: 11.5, color: message?.error ? T.red : T.green, minHeight: 18 }}>{message?.text || ''}</div>
      <div style={{ flex: 1 }}/>
      <button type="button" onClick={onSave} disabled={busy} className="edge-press edge-btn-primary" style={{ ...btnPrimary, opacity: busy ? 0.6 : 1 }}>
        <Icon name="check" size={13}/>{busy ? '保存中' : '保存设置'}
      </button>
    </div>
  )
}

function WebDAVPage({ settings, setSettings, status, save, busy, message }) {
  const cfg = settings.webdav
  const update = patch => setSettings(prev => ({ ...prev, webdav: { ...prev.webdav, ...patch } }))
  return (
    <>
      <div style={{ display: 'flex', alignItems: 'center', marginBottom: 12 }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 18, color: T.ink, letterSpacing: 0 }}>WebDAV 文件访问</h1>
          <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3 }}>内置服务 · 控制台账户认证</div>
        </div>
        <div style={{ flex: 1 }}/>
        <Chip tone={status?.running ? 'green' : status?.error ? 'red' : 'gray'}>
          <StatusDot tone={status?.running ? 'green' : status?.error ? 'red' : 'gray'} size={6}/>
          {status?.running ? '运行中' : '已停止'}
        </Chip>
      </div>
      <Section title="服务配置" icon="globe">
        <FieldRow label="启用 WebDAV" hint="保存后立即启停">
          <Toggle checked={cfg.enabled} onChange={enabled => update({ enabled })} label={cfg.enabled ? '已启用' : '已停用'}/>
        </FieldRow>
        <FieldRow label="HTTP 端口" hint="端口被占用时不会保存">
          <input aria-label="WebDAV HTTP 端口" type="number" min="1" max="65535" value={cfg.port} onChange={e => update({ port: Number(e.target.value) })} style={{ ...field, width: 150 }}/>
        </FieldRow>
        <FieldRow label="共享根目录" hint="必须位于控制台数据根内">
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <Icon name="folder" size={15} style={{ color: T.ink3 }}/>
            <input aria-label="WebDAV 共享根目录" value={cfg.path} onChange={e => update({ path: e.target.value })} style={{ ...field, flex: 1, fontFamily: T.mono }}/>
          </div>
        </FieldRow>
        <FieldRow label="访问模式">
          <select aria-label="WebDAV 访问模式" value={cfg.readOnly ? 'readonly' : 'readwrite'} onChange={e => update({ readOnly: e.target.value === 'readonly' })} style={{ ...field, width: 180 }}>
            <option value="readwrite">读写</option><option value="readonly">只读</option>
          </select>
        </FieldRow>
        <FieldRow label="认证账户" hint="密码沿用控制台账户，不单独落盘">
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, color: T.ink2, fontSize: 12.5 }}><Icon name="lock" size={14}/>devbox</div>
        </FieldRow>
      </Section>
      {status?.error && <div style={{ marginTop: 10, padding: '9px 12px', border: '1px solid #fecaca', background: T.redSoft, color: '#b91c1c', borderRadius: 6, fontSize: 11.5 }}>{status.error}</div>}
      <SaveBar busy={busy} onSave={save} message={message}/>
    </>
  )
}

function SMBPage({ settings, setSettings, probe, save, busy, message }) {
  const [preview, setPreview] = useState('')
  const [action, setAction] = useState(null)
  const shares = settings.smb || []
  const updateShare = (index, patch) => setSettings(prev => ({ ...prev, smb: (prev.smb || []).map((share, i) => i === index ? { ...share, ...patch } : share) }))
  const removeShare = index => setSettings(prev => ({ ...prev, smb: (prev.smb || []).filter((_, i) => i !== index) }))
  const addShare = () => setSettings(prev => {
    const current = prev.smb || []
    return { ...prev, smb: [...current, { name: `share${current.length + 1}`, path: prev.webdav.path, readOnly: false, guest: false }] }
  })
  const renderPreview = async () => {
    setAction({ text: '正在生成预览' })
    const response = await authFetch('/api/v1/maintenance/smb/preview', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(shares) })
    if (!response.ok) return setAction({ text: await responseMessage(response), error: true })
    const data = await response.json(); setPreview(data.preview); setAction({ text: '预览已更新' })
  }
  const apply = async () => {
    setAction({ text: '正在校验并应用' })
    const response = await authFetch('/api/v1/maintenance/smb/apply', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(shares) })
    if (!response.ok) return setAction({ text: await responseMessage(response), error: true })
    setAction({ text: '受管配置已写入' })
  }
  return (
    <>
      <div style={{ display: 'flex', alignItems: 'center', marginBottom: 12 }}>
        <div><h1 style={{ margin: 0, fontSize: 18, color: T.ink, letterSpacing: 0 }}>SMB 共享</h1><div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3 }}>系统 Samba 探测与受管配置</div></div>
        <div style={{ flex: 1 }}/>
        <Chip tone={probe?.active ? 'green' : probe?.installed ? 'amber' : 'gray'}>{probe?.active ? 'smbd 运行中' : probe?.installed ? '已安装 · 未运行' : '未安装'}</Chip>
      </div>
      {!probe?.installed && <div style={{ padding: '11px 13px', border: '1px solid #fde68a', background: T.amberSoft, borderRadius: 7, color: '#92400e', fontSize: 11.5, marginBottom: 12 }}>
        <div style={{ fontWeight: 700, marginBottom: 4 }}>Samba 未安装</div>
        <code style={{ fontFamily: T.mono }}>sudo apt install samba</code><span style={{ marginLeft: 8 }}>安装后刷新页面。DevBox 不会自动安装系统包。</span>
      </div>}
      <Section title="共享目录" icon="folder" action={<button type="button" onClick={addShare} className="edge-press edge-btn-secondary" style={{ ...btnSecondary, height: 28, fontSize: 11.5 }}><Icon name="plus" size={12}/>添加共享</button>}>
        {shares.length === 0 && <div style={{ padding: 24, textAlign: 'center', color: T.ink4, fontSize: 12 }}>尚未配置 SMB 共享</div>}
        {shares.map((share, index) => (
          <div key={index} className="maintenance-smb-row" style={{ display: 'grid', gridTemplateColumns: '150px minmax(220px,1fr) 105px 90px 34px', gap: 9, alignItems: 'center', padding: '10px 12px', borderTop: index ? `1px solid ${T.borderSoft}` : 'none' }}>
            <input aria-label={`SMB 共享名 ${index + 1}`} value={share.name} onChange={e => updateShare(index, { name: e.target.value })} placeholder="共享名" style={{ ...field, width: '100%' }}/>
            <input aria-label={`SMB 路径 ${index + 1}`} value={share.path} onChange={e => updateShare(index, { path: e.target.value })} placeholder="数据根内路径" style={{ ...field, width: '100%', fontFamily: T.mono }}/>
            <Toggle checked={share.readOnly} onChange={readOnly => updateShare(index, { readOnly })} label="只读"/>
            <Toggle checked={share.guest} onChange={guest => updateShare(index, { guest })} label="Guest"/>
            <button title="删除共享" aria-label="删除共享" type="button" onClick={() => removeShare(index)} className="edge-press" style={{ border: 0, background: 'transparent', color: T.red, cursor: 'pointer', width: 30, height: 30 }}><Icon name="trash" size={14}/></button>
          </div>
        ))}
      </Section>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 12 }}>
        <button type="button" onClick={renderPreview} className="edge-press edge-btn-secondary" style={btnSecondary}><Icon name="eye" size={13}/>生成配置预览</button>
        <button type="button" onClick={apply} disabled={!probe?.installed || !probe?.testparmInstalled} className="edge-press edge-btn-primary" style={{ ...btnPrimary, opacity: !probe?.installed || !probe?.testparmInstalled ? 0.45 : 1 }}><Icon name="check" size={13}/>校验并应用</button>
        <span style={{ fontSize: 11.5, color: action?.error ? T.red : T.green }}>{action?.text}</span>
      </div>
      {preview && <pre style={{ margin: '10px 0 0', padding: 14, minHeight: 130, maxHeight: 240, overflow: 'auto', borderRadius: 7, background: '#101827', color: '#dbeafe', fontSize: 11.5, lineHeight: 1.55, fontFamily: T.mono, whiteSpace: 'pre-wrap' }}>{preview}</pre>}
      <SaveBar busy={busy} onSave={save} message={message}/>
    </>
  )
}

function SMTPPage({ settings, setSettings, password, setPassword, save, busy, message }) {
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState(null)
  const cfg = settings.smtp
  const update = patch => setSettings(prev => ({ ...prev, smtp: { ...prev.smtp, ...patch } }))
  const test = async () => {
    setTesting(true); setTestResult(null)
    const response = await authFetch('/api/v1/maintenance/smtp/test', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ config: cfg, password }) })
    if (!response.ok) setTestResult({ text: await responseMessage(response), error: true })
    else setTestResult({ text: (await response.json()).message })
    setTesting(false)
  }
  return (
    <>
      <div style={{ marginBottom: 12 }}><h1 style={{ margin: 0, fontSize: 18, color: T.ink, letterSpacing: 0 }}>SMTP 邮件通知</h1><div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3 }}>登录失败告警已接入通知钩子</div></div>
      <Section title="SMTP 账户" icon="send">
        <FieldRow label="启用通知"><Toggle checked={cfg.enabled} onChange={enabled => update({ enabled })} label={cfg.enabled ? '已启用' : '已停用'}/></FieldRow>
        <FieldRow label="服务器与端口"><div style={{ display: 'flex', gap: 8 }}><input aria-label="SMTP 服务器" value={cfg.host} onChange={e => update({ host: e.target.value })} placeholder="smtp.example.com" style={{ ...field, flex: 1 }}/><input aria-label="SMTP 端口" type="number" value={cfg.port || ''} onChange={e => update({ port: Number(e.target.value) })} placeholder="587" style={{ ...field, width: 100 }}/></div></FieldRow>
        <FieldRow label="TLS 模式"><select aria-label="SMTP TLS 模式" value={cfg.tls || 'starttls'} onChange={e => update({ tls: e.target.value })} style={{ ...field, width: 180 }}><option value="starttls">STARTTLS</option><option value="tls">TLS</option><option value="none">无加密</option></select></FieldRow>
        <FieldRow label="账号"><input aria-label="SMTP 账号" value={cfg.username} onChange={e => update({ username: e.target.value })} style={{ ...field, width: '100%' }}/></FieldRow>
        <FieldRow label="密码" hint={settings.smtpPasswordSet ? '已加密保存；留空保持不变' : '保存时使用本机密钥加密'}><input aria-label="SMTP 密码" type="password" value={password} onChange={e => setPassword(e.target.value)} placeholder={settings.smtpPasswordSet ? '已保存' : ''} autoComplete="new-password" style={{ ...field, width: '100%' }}/></FieldRow>
        <FieldRow label="发件人"><input aria-label="SMTP 发件人" value={cfg.from} onChange={e => update({ from: e.target.value })} placeholder="DevBox <devbox@example.com>" style={{ ...field, width: '100%' }}/></FieldRow>
        <FieldRow label="收件人"><input aria-label="SMTP 收件人" value={cfg.to} onChange={e => update({ to: e.target.value })} placeholder="ops@example.com" style={{ ...field, width: '100%' }}/></FieldRow>
      </Section>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginTop: 12 }}><button type="button" onClick={test} disabled={testing} className="edge-press edge-btn-secondary" style={btnSecondary}><Icon name="send" size={13}/>{testing ? '发送中' : '发送测试邮件'}</button><span style={{ fontSize: 11.5, color: testResult?.error ? T.red : T.green }}>{testResult?.text}</span></div>
      <SaveBar busy={busy} onSave={save} message={message}/>
    </>
  )
}

function MaintenancePage({ settings, setSettings, currentVersion, save, busy, message }) {
  const fileRef = useRef(null)
  const [includeSecrets, setIncludeSecrets] = useState(false)
  const [updateResult, setUpdateResult] = useState(null)
  const [restore, setRestore] = useState(null)
  const [restorePhrase, setRestorePhrase] = useState('')
  const [resetChecked, setResetChecked] = useState(false)
  const [resetPhrase, setResetPhrase] = useState('')
  const [action, setAction] = useState(null)
  const updateCfg = patch => setSettings(prev => ({ ...prev, updates: { ...prev.updates, ...patch } }))
  const checkUpdate = async () => {
    setAction({ text: '正在检查 GitHub Releases' })
    const response = await authFetch('/api/v1/maintenance/updates/check')
    if (!response.ok) return setAction({ text: await responseMessage(response), error: true })
    setUpdateResult(await response.json()); setAction({ text: '检查完成' })
  }
  const download = async () => {
    const response = await authFetch(`/api/v1/maintenance/backup?includeSecrets=${includeSecrets}`)
    if (!response.ok) return setAction({ text: await responseMessage(response), error: true })
    const blob = await response.blob(); const url = URL.createObjectURL(blob); const a = document.createElement('a')
    a.href = url; a.download = `devbox-config-${new Date().toISOString().slice(0, 10)}.tar.gz`; a.click(); URL.revokeObjectURL(url)
    setAction({ text: '配置备份已导出' })
  }
  const previewRestore = async file => {
    if (!file) return
    setAction({ text: '正在分析备份' })
    const response = await authFetch('/api/v1/maintenance/restore/preview', { method: 'POST', headers: { 'Content-Type': 'application/gzip' }, body: file })
    if (!response.ok) return setAction({ text: await responseMessage(response), error: true })
    setRestore(await response.json()); setRestorePhrase(''); setAction({ text: '还原预览已生成' })
  }
  const confirmRestore = async () => {
    const response = await authFetch('/api/v1/maintenance/restore/confirm', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token: restore.token, confirmation: restorePhrase }) })
    if (!response.ok) return setAction({ text: await responseMessage(response), error: true })
    setRestore(null); setAction({ text: (await response.json()).message }); window.setTimeout(() => window.location.reload(), 800)
  }
  const reset = async () => {
    const response = await authFetch('/api/v1/maintenance/reset', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ confirm: resetChecked, phrase: resetPhrase }) })
    if (!response.ok) return setAction({ text: await responseMessage(response), error: true })
    setAction({ text: (await response.json()).message }); window.setTimeout(() => window.location.reload(), 800)
  }
  return (
    <>
      <div style={{ marginBottom: 12 }}><h1 style={{ margin: 0, fontSize: 18, color: T.ink, letterSpacing: 0 }}>系统维护</h1><div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3 }}>DevBox 版本、配置备份与恢复出厂设置</div></div>
      <div className="maintenance-two-col" style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) minmax(0, 1fr)', gap: 12 }}>
        <Section title="版本更新" icon="refresh">
          <FieldRow label="当前版本"><code style={{ fontFamily: T.mono, color: T.ink2 }}>{currentVersion || 'dev'}</code></FieldRow>
          <FieldRow label="检查更新"><Toggle checked={settings.updates.checkEnabled} onChange={checkEnabled => updateCfg({ checkEnabled })} label="查询 GitHub Releases"/></FieldRow>
          <FieldRow label="自动更新" hint="仅保存开关，不执行自更新"><Toggle checked={settings.updates.autoUpdate} onChange={autoUpdate => updateCfg({ autoUpdate })} label="允许自动更新"/></FieldRow>
          <div style={{ padding: 12, borderTop: `1px solid ${T.borderSoft}`, display: 'flex', alignItems: 'center', gap: 8 }}><button type="button" disabled={!settings.updates.checkEnabled} onClick={checkUpdate} className="edge-press edge-btn-secondary" style={btnSecondary}><Icon name="refresh" size={13}/>立即检查</button>{updateResult && <Chip tone={updateResult.updateAvailable ? 'amber' : 'green'}>{updateResult.updateAvailable ? `发现 ${updateResult.latestVersion}` : '已是最新版本'}</Chip>}</div>
        </Section>
        <Section title="配置备份" icon="download">
          <div style={{ padding: 16, color: T.ink3, fontSize: 11.5, lineHeight: 1.6 }}>导出文件服务、通知、更新和默认应用配置。</div>
          <FieldRow label="密钥与密码"><Toggle checked={includeSecrets} onChange={setIncludeSecrets} label="包含敏感信息"/></FieldRow>
          <div style={{ padding: 12, borderTop: `1px solid ${T.borderSoft}` }}><button type="button" onClick={download} className="edge-press edge-btn-primary" style={btnPrimary}><Icon name="download" size={13}/>下载 tar.gz</button></div>
        </Section>
      </div>
      <div style={{ marginTop: 12 }}><Section title="配置还原" icon="upload">
        <div style={{ padding: 14, display: 'flex', alignItems: 'center', gap: 10 }}><input ref={fileRef} type="file" accept=".gz,.tgz,application/gzip" onChange={e => previewRestore(e.target.files?.[0])} style={{ display: 'none' }}/><button type="button" onClick={() => fileRef.current?.click()} className="edge-press edge-btn-secondary" style={btnSecondary}><Icon name="upload" size={13}/>选择备份文件</button><span style={{ fontSize: 11.5, color: T.ink3 }}>确认还原前将自动备份当前配置</span></div>
        {restore && <div style={{ margin: '0 14px 14px', padding: 13, border: '1px solid #fde68a', background: T.amberSoft, borderRadius: 7 }}><div style={{ fontSize: 12, fontWeight: 700, color: '#92400e' }}>影响预览</div>{restore.changes.map(change => <div key={change} style={{ fontSize: 11.5, color: '#92400e', marginTop: 5 }}>• {change}</div>)}<div style={{ display: 'flex', gap: 8, marginTop: 10 }}><input aria-label="还原确认词" value={restorePhrase} onChange={e => setRestorePhrase(e.target.value)} placeholder="输入 RESTORE" style={{ ...field, width: 180 }}/><button type="button" disabled={restorePhrase !== 'RESTORE'} onClick={confirmRestore} className="edge-press edge-btn-primary" style={{ ...btnPrimary, opacity: restorePhrase === 'RESTORE' ? 1 : 0.45 }}><Icon name="history" size={13}/>确认还原</button></div></div>}
      </Section></div>
      <div style={{ marginTop: 12 }}><Section title="恢复出厂设置" icon="alertTri">
        <div style={{ padding: 14, borderBottom: `1px solid ${T.borderSoft}`, fontSize: 11.5, color: '#991b1b', background: T.redSoft, lineHeight: 1.6 }}>仅清除 DevBox 维护配置与密钥并停止 WebDAV；不会重置操作系统、磁盘或已安装服务。</div>
        <div style={{ padding: 14, display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}><Toggle checked={resetChecked} onChange={setResetChecked} label="已阅读影响说明"/><input aria-label="重置确认词" value={resetPhrase} onChange={e => setResetPhrase(e.target.value)} placeholder="输入 RESET DEVBOX" style={{ ...field, width: 190 }}/><button type="button" disabled={!resetChecked || resetPhrase !== 'RESET DEVBOX'} onClick={reset} className="edge-press edge-btn-danger" style={{ ...btnDanger, opacity: resetChecked && resetPhrase === 'RESET DEVBOX' ? 1 : 0.45 }}><Icon name="trash" size={13}/>重置 DevBox</button></div>
      </Section></div>
      {action && <div style={{ marginTop: 10, fontSize: 11.5, color: action.error ? T.red : T.green }}>{action.text}</div>}
      <SaveBar busy={busy} onSave={save} message={message}/>
    </>
  )
}

function DefaultsPage({ settings, setSettings, save, busy, message }) {
  const rows = [{ key: 'text/plain', label: '纯文本' }, { key: 'text/markdown', label: 'Markdown' }, { key: 'application/json', label: 'JSON' }]
  const options = [{ value: 'browser', label: '浏览器' }, { value: 'vscode', label: 'VS Code Server' }, { value: 'files', label: '文件' }, { value: 'terminal', label: '终端' }]
  const update = (key, value) => setSettings(prev => ({ ...prev, defaultApps: { ...prev.defaultApps, [key]: value } }))
  return <><div style={{ marginBottom: 12 }}><h1 style={{ margin: 0, fontSize: 18, color: T.ink, letterSpacing: 0 }}>默认应用</h1><div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3 }}>桌面文件类型打开方式</div></div><Section title="文件关联" icon="apps">{rows.map(row => <FieldRow key={row.key} label={row.label} hint={row.key}><select aria-label={`${row.label}默认应用`} value={settings.defaultApps[row.key] || 'browser'} onChange={e => update(row.key, e.target.value)} style={{ ...field, width: 220 }}>{options.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}</select></FieldRow>)}</Section><SaveBar busy={busy} onSave={save} message={message}/></>
}

function AboutPage({ about }) {
  return <><div style={{ marginBottom: 12 }}><h1 style={{ margin: 0, fontSize: 18, color: T.ink, letterSpacing: 0 }}>关于 DevBox</h1><div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3 }}>桌面算力平台</div></div><Section title="产品信息" icon="info"><div style={{ padding: 22, display: 'flex', alignItems: 'center', gap: 16 }}><div style={{ width: 52, height: 52, borderRadius: 8, background: '#172033', color: 'white', display: 'grid', placeItems: 'center' }}><Icon name="cpu" size={27}/></div><div><div style={{ fontSize: 20, fontWeight: 750, color: T.ink, letterSpacing: 0 }}>{about?.name || 'A2D2 DevBox'}</div><div style={{ fontFamily: T.mono, fontSize: 12, color: T.ink3, marginTop: 5 }}>Version {about?.version || 'dev'}</div></div></div><FieldRow label="开源许可证"><a href={about?.license?.url} target="_blank" rel="noreferrer" style={{ color: T.blueDeep, fontSize: 12.5, display: 'inline-flex', alignItems: 'center', gap: 5 }}>{about?.license?.name || 'Apache License 2.0'}<Icon name="external" size={12}/></a></FieldRow><FieldRow label="版权"><span style={{ color: T.ink2, fontSize: 12.5 }}>{about?.license?.copyright}</span></FieldRow><FieldRow label="LICENSE"><div style={{ color: T.ink3, fontSize: 11.5, lineHeight: 1.55 }}>{about?.license?.text}</div></FieldRow></Section><div style={{ marginTop: 12 }}><Section title="依赖 Attribution" icon="book"><div className="maintenance-deps" style={{ display: 'grid', gridTemplateColumns: 'repeat(2,minmax(0,1fr))' }}>{(about?.dependencies || []).map((dep, i) => <div key={dep.name} style={{ padding: '10px 14px', display: 'flex', alignItems: 'center', borderTop: `1px solid ${T.borderSoft}`, borderRight: i % 2 === 0 ? `1px solid ${T.borderSoft}` : 0 }}><span style={{ fontSize: 12.5, color: T.ink2 }}>{dep.name}</span><div style={{ flex: 1 }}/><Chip tone="gray">{dep.license}</Chip></div>)}</div></Section></div></>
}

export default function Settings() {
  const [active, setActive] = useState('webdav')
  const [settings, setSettings] = useState(null)
  const [webdavStatus, setWebDAVStatus] = useState(null)
  const [smbProbe, setSMBProbe] = useState(null)
  const [about, setAbout] = useState(null)
  const [smtpPassword, setSMTPPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState(null)

  const load = useCallback(async () => {
    const [settingsResponse, aboutResponse] = await Promise.all([authFetch('/api/v1/maintenance/settings'), authFetch('/api/v1/maintenance/about')])
    if (!settingsResponse.ok) return setMessage({ text: await responseMessage(settingsResponse), error: true })
    const data = await settingsResponse.json(); setSettings(data.settings); setWebDAVStatus(data.webdavStatus); setSMBProbe(data.smbProbe)
    if (aboutResponse.ok) setAbout(await aboutResponse.json())
  }, [])
  useEffect(() => {
    // Initial API synchronization belongs to this mount effect.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    load()
  }, [load])

  const save = async () => {
    setBusy(true); setMessage(null)
    const response = await authFetch('/api/v1/maintenance/settings', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ...settings, smtpPassword }) })
    if (!response.ok) setMessage({ text: await responseMessage(response), error: true })
    else { const data = await response.json(); setSettings(data.settings); setWebDAVStatus(data.webdavStatus); setSMTPPassword(''); setMessage({ text: '设置已保存' }) }
    setBusy(false)
  }

  if (!settings) return <div style={{ flex: 1, display: 'grid', placeItems: 'center', color: T.ink3, background: T.surfaceAlt, fontSize: 12 }}>{message?.text || '正在加载系统设置'}</div>

  return (
    <div className="maintenance-shell" style={{ flex: 1, minHeight: 0, display: 'grid', gridTemplateColumns: '178px minmax(0,1fr)', background: T.surfaceAlt }}>
      <style>{`@media (max-width: 700px) {
        .maintenance-shell { grid-template-columns: minmax(0,1fr) !important; grid-template-rows: auto minmax(0,1fr); }
        .maintenance-sidebar { padding: 8px !important; }
        .maintenance-sidebar-title { display: none; }
        .maintenance-nav { flex-direction: row !important; overflow-x: auto; }
        .maintenance-nav button { flex: 0 0 auto; }
        .maintenance-main { padding: 14px 10px !important; }
        .maintenance-field-row { grid-template-columns: minmax(0,1fr) !important; gap: 7px !important; }
        .maintenance-two-col, .maintenance-deps { grid-template-columns: minmax(0,1fr) !important; }
        .maintenance-smb-row { grid-template-columns: minmax(0,1fr) auto auto 34px !important; }
        .maintenance-smb-row > input:nth-child(2) { grid-column: 1 / -1; grid-row: 2; }
      }`}</style>
      <aside className="maintenance-sidebar" style={{ background: '#172033', color: 'white', padding: '18px 10px', minHeight: 0 }}>
        <div className="maintenance-sidebar-title" style={{ padding: '0 10px 14px', fontSize: 12.5, fontWeight: 700, letterSpacing: 0 }}>系统设置</div>
        <nav className="maintenance-nav" style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
          {tabs.map(tab => <button key={tab.id} type="button" onClick={() => { setActive(tab.id); setMessage(null) }} style={{ border: 0, borderRadius: 6, height: 36, padding: '0 10px', display: 'flex', alignItems: 'center', gap: 9, cursor: 'pointer', background: active === tab.id ? 'rgba(255,255,255,0.13)' : 'transparent', color: active === tab.id ? 'white' : '#aab5c7', fontSize: 12.5, fontWeight: active === tab.id ? 650 : 500, textAlign: 'left', letterSpacing: 0 }}><Icon name={tab.icon} size={15}/>{tab.label}</button>)}
        </nav>
      </aside>
      <main className="maintenance-main" style={{ minWidth: 0, minHeight: 0, overflow: 'auto', padding: '20px clamp(16px, 3vw, 30px)' }}>
        <div style={{ maxWidth: 980, margin: '0 auto' }}>
          {active === 'webdav' && <WebDAVPage settings={settings} setSettings={setSettings} status={webdavStatus} save={save} busy={busy} message={message}/>}
          {active === 'smb' && <SMBPage settings={settings} setSettings={setSettings} probe={smbProbe} save={save} busy={busy} message={message}/>}
          {active === 'smtp' && <SMTPPage settings={settings} setSettings={setSettings} password={smtpPassword} setPassword={setSMTPPassword} save={save} busy={busy} message={message}/>}
          {active === 'maintenance' && <MaintenancePage settings={settings} setSettings={setSettings} currentVersion={about?.version} save={save} busy={busy} message={message}/>}
          {active === 'defaults' && <DefaultsPage settings={settings} setSettings={setSettings} save={save} busy={busy} message={message}/>}
          {active === 'about' && <AboutPage about={about}/>}
        </div>
      </main>
    </div>
  )
}
