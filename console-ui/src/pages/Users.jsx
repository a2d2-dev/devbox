import { useCallback, useEffect, useMemo, useState } from 'react'
import { Icon } from '../icons'
import { T } from '../tokens'
import { Chip, StatusDot } from '../components/ui'
import { authFetch } from '../hooks/useApi'

const button = { height: 30, borderRadius: 6, border: `1px solid ${T.border}`, background: '#fff', color: T.ink2, display: 'inline-flex', alignItems: 'center', justifyContent: 'center', gap: 6, padding: '0 10px', fontSize: 12, cursor: 'pointer' }
const primary = { ...button, background: T.blueDeep, borderColor: T.blueDeep, color: '#fff' }
const field = { width: '100%', height: 34, border: `1px solid ${T.border}`, borderRadius: 6, padding: '0 10px', fontSize: 12.5, color: T.ink, background: '#fff', boxSizing: 'border-box' }
const label = { display: 'block', fontSize: 11.5, fontWeight: 600, color: T.ink2, marginBottom: 5 }

async function api(url, options) {
  const r = await authFetch(url, options)
  const data = r.status === 204 ? null : await r.json().catch(() => ({}))
  if (!r.ok) {
    const raw = data.error || data.message || `HTTP ${r.status}`
    const mapped = raw.includes('already exists') ? '名称已存在'
      : raw.includes('last administrator') ? '不能删除、禁用或降级最后一个管理员'
      : raw.includes('password must') ? '密码至少 10 位，且大写、小写、数字、符号中至少包含三类'
      : raw.includes('username must') ? '用户名需为 3-32 位字母、数字、点、下划线或连字符'
      : raw
    const e = new Error(mapped); e.status = r.status; throw e
  }
  return data
}

export default function Users() {
  const [tab, setTab] = useState('users')
  const [users, setUsers] = useState([])
  const [groups, setGroups] = useState([])
  const [roots, setRoots] = useState([])
  const [search, setSearch] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [dialog, setDialog] = useState(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [u, g, r] = await Promise.all([api('/api/v1/users'), api('/api/v1/user-groups'), api('/api/v1/file-roots')])
      setUsers(u || []); setGroups(g || []); setRoots(r || []); setError('')
    } catch (e) {
      setError(e.status === 403 ? '当前账户不是管理员，无法访问用户管理。' : e.message)
    } finally { setLoading(false) }
  }, [])

	// Initial remote synchronization.
	// eslint-disable-next-line react-hooks/set-state-in-effect
	useEffect(() => { load() }, [load])
  const q = search.trim().toLowerCase()
  const shownUsers = useMemo(() => users.filter(u => !q || `${u.username} ${u.displayName}`.toLowerCase().includes(q)), [users, q])
  const shownGroups = useMemo(() => groups.filter(g => !q || `${g.name} ${g.description}`.toLowerCase().includes(q)), [groups, q])
  const rootNames = ids => ids?.map(id => roots.find(r => r.id === id)?.name).filter(Boolean) || []

  const remove = async (kind, item) => {
    if (!window.confirm(`确认删除${kind === 'user' ? '用户' : '用户组'}“${item.username || item.name}”？`)) return
    try { await api(kind === 'user' ? `/api/v1/users/${item.id}` : `/api/v1/user-groups/${item.id}`, { method: 'DELETE' }); await load() }
    catch (e) { setError(e.message) }
  }

  return (
    <div style={{ flex: 1, minWidth: 0, height: '100%', display: 'flex', flexDirection: 'column', background: T.surfaceAlt }}>
      <header style={{ padding: '14px 20px 0', background: '#fff', borderBottom: `1px solid ${T.border}`, flexShrink: 0 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, paddingBottom: 12 }}>
          <div><div style={{ fontSize: 17, fontWeight: 700, color: T.ink }}>用户与权限</div><div style={{ fontSize: 11.5, color: T.ink3, marginTop: 2 }}>控制台账户、用户组与文件根目录授权</div></div>
          <div style={{ flex: 1 }}/>
          <button title="管理文件根目录" style={button} onClick={() => setDialog({ type: 'roots' })}><Icon name="folder" size={13}/>文件根目录</button>
          <button style={primary} onClick={() => setDialog({ type: tab === 'users' ? 'user' : 'group', item: null })}><Icon name="plus" size={13}/>{tab === 'users' ? '新增用户' : '新增用户组'}</button>
        </div>
        <div style={{ display: 'flex', alignItems: 'end', gap: 16 }}>
          <div style={{ display: 'flex', gap: 2 }}>
            {[['users', '用户', users.length], ['groups', '用户组', groups.length]].map(([id, text, count]) => <button key={id} onClick={() => setTab(id)} style={{ padding: '8px 12px 10px', border: 'none', borderBottom: `2px solid ${tab === id ? T.blueDeep : 'transparent'}`, background: 'transparent', color: tab === id ? T.blueDeep : T.ink3, fontSize: 12.5, fontWeight: tab === id ? 700 : 500, cursor: 'pointer' }}>{text} <span className="mono" style={{ marginLeft: 4, fontSize: 10 }}>{count}</span></button>)}
          </div>
          <div style={{ flex: 1 }}/>
          <div style={{ position: 'relative', width: 240, marginBottom: 8 }}><Icon name="search" size={13} style={{ position: 'absolute', left: 9, top: 9, color: T.ink4 }}/><input aria-label="搜索" value={search} onChange={e => setSearch(e.target.value)} placeholder={`搜索${tab === 'users' ? '用户' : '用户组'}`} style={{ ...field, paddingLeft: 29 }}/></div>
        </div>
      </header>
      <main style={{ flex: 1, overflow: 'auto', padding: 16 }}>
        {error && <div style={{ padding: '10px 12px', marginBottom: 12, background: T.redSoft, border: '1px solid #fecaca', borderRadius: 6, color: '#991b1b', fontSize: 12 }}>{error}</div>}
        {loading ? <div style={{ color: T.ink3, fontSize: 12 }}>加载中...</div> : tab === 'users' ? (
          <Table columns="minmax(170px,1.2fr) minmax(130px,1fr) 110px 100px minmax(180px,1fr) 86px" headers={['账户', '显示名', '角色', '状态', '文件根目录', '操作']}>
            {shownUsers.map((u, i) => <Row key={u.id} columns="minmax(170px,1.2fr) minmax(130px,1fr) 110px 100px minmax(180px,1fr) 86px" first={!i}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 9 }}><div style={{ width: 28, height: 28, borderRadius: 6, background: u.role === 'admin' ? '#eff6ff' : '#f1f5f9', color: u.role === 'admin' ? T.blueDeep : T.ink3, display: 'flex', alignItems: 'center', justifyContent: 'center' }}><Icon name="user" size={14}/></div><span className="mono" style={{ fontWeight: 600 }}>{u.username}</span></div>
			  <span>{u.displayName}</span><span style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}><Chip tone={u.role === 'admin' ? 'blue' : 'gray'}>{u.role === 'admin' ? '管理员' : '普通用户'}</Chip><Chip tone={u.role === 'admin' ? 'violet' : 'green'}>{u.role === 'admin' ? '全部管理' : '授权目录'}</Chip></span>
              <span style={{ display: 'inline-flex', gap: 6, alignItems: 'center' }}><StatusDot tone={u.enabled ? 'green' : 'gray'} size={6}/>{u.enabled ? '已启用' : '已禁用'}</span>
              <RootChips names={rootNames(u.rootIds)}/><Actions onEdit={() => setDialog({ type: 'user', item: u })} onDelete={() => remove('user', u)}/>
            </Row>)}
          </Table>
        ) : (
          <Table columns="minmax(180px,1fr) minmax(200px,1.3fr) 120px minmax(180px,1fr) 86px" headers={['用户组', '说明', '成员', '文件根目录', '操作']}>
            {shownGroups.map((g, i) => <Row key={g.id} columns="minmax(180px,1fr) minmax(200px,1.3fr) 120px minmax(180px,1fr) 86px" first={!i}>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, fontWeight: 600 }}><Icon name="layers" size={14} style={{ color: T.indigo }}/>{g.name}</span><span style={{ color: T.ink3 }}>{g.description || '-'}</span><span>{g.memberIds?.length || 0} 人</span><RootChips names={rootNames(g.rootIds)}/><Actions onEdit={() => setDialog({ type: 'group', item: g })} onDelete={() => remove('group', g)}/>
            </Row>)}
          </Table>
        )}
      </main>
      {dialog?.type === 'user' && <UserDialog item={dialog.item} roots={roots} onClose={() => setDialog(null)} onSaved={async () => { setDialog(null); await load() }}/>} 
      {dialog?.type === 'group' && <GroupDialog item={dialog.item} users={users} roots={roots} onClose={() => setDialog(null)} onSaved={async () => { setDialog(null); await load() }}/>} 
      {dialog?.type === 'roots' && <RootsDialog roots={roots} onClose={() => setDialog(null)} onChanged={load}/>} 
    </div>
  )
}

function Table({ columns, headers, children }) { return <div style={{ background: '#fff', border: `1px solid ${T.border}`, borderRadius: 8, overflow: 'hidden', minWidth: 760 }}><div style={{ display: 'grid', gridTemplateColumns: columns, padding: '8px 12px', gap: 12, background: T.surfaceAlt, color: T.ink3, fontSize: 10.5, fontWeight: 700 }}>{headers.map(h => <span key={h}>{h}</span>)}</div>{children}</div> }
function Row({ columns, first, children }) { return <div style={{ display: 'grid', gridTemplateColumns: columns, padding: '10px 12px', gap: 12, alignItems: 'center', borderTop: first ? `1px solid ${T.borderSoft}` : `1px solid ${T.borderSoft}`, color: T.ink2, fontSize: 12 }}>{children}</div> }
function RootChips({ names }) { return <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>{names.length ? names.map(n => <Chip key={n} tone="violet"><Icon name="folder" size={10}/>{n}</Chip>) : <span style={{ color: T.ink4 }}>未授权</span>}</div> }
function Actions({ onEdit, onDelete }) { return <div style={{ display: 'flex', gap: 4, justifyContent: 'flex-end' }}><button title="编辑" onClick={onEdit} style={{ ...button, width: 30, padding: 0 }}><Icon name="edit" size={13}/></button><button title="删除" onClick={onDelete} style={{ ...button, width: 30, padding: 0, color: T.red }}><Icon name="trash" size={13}/></button></div> }

function Modal({ title, children, onClose, onSubmit, saving, submitText = '保存', wide = false }) { const Tag = onSubmit ? 'form' : 'div'; return <div role="dialog" aria-modal="true" aria-label={title} style={{ position: 'fixed', inset: 0, zIndex: 1200, background: 'rgba(15,23,42,.45)', display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 16 }}><Tag onSubmit={onSubmit} style={{ width: wide ? 620 : 480, maxWidth: '100%', maxHeight: '90vh', overflow: 'auto', background: '#fff', borderRadius: 8, boxShadow: T.shadow.lg }}><div style={{ padding: '14px 16px', display: 'flex', alignItems: 'center', borderBottom: `1px solid ${T.border}` }}><strong style={{ fontSize: 14 }}>{title}</strong><div style={{ flex: 1 }}/><button type="button" title="关闭" onClick={onClose} style={{ ...button, border: 'none', width: 28, padding: 0 }}><Icon name="x" size={14}/></button></div><div style={{ padding: 16 }}>{children}</div>{onSubmit && <div style={{ padding: '10px 16px', borderTop: `1px solid ${T.border}`, display: 'flex', justifyContent: 'flex-end', gap: 8 }}><button type="button" style={button} onClick={onClose}>取消</button><button disabled={saving} style={primary}>{saving ? '保存中...' : submitText}</button></div>}</Tag></div> }

function CheckList({ title, items, selected, onChange, text }) { return <div style={{ marginTop: 14 }}><span style={label}>{title}</span><div style={{ border: `1px solid ${T.border}`, borderRadius: 6, maxHeight: 132, overflow: 'auto', padding: 5 }}>{items.length ? items.map(item => <label key={item.id} style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '6px 7px', fontSize: 12, cursor: 'pointer' }}><input type="checkbox" checked={selected.includes(item.id)} onChange={e => onChange(e.target.checked ? [...selected, item.id] : selected.filter(id => id !== item.id))}/><span>{text(item)}</span></label>) : <div style={{ padding: 8, color: T.ink4, fontSize: 11.5 }}>暂无项目</div>}</div></div> }

function UserDialog({ item, roots, onClose, onSaved }) {
  const [form, setForm] = useState({ username: item?.username || '', displayName: item?.displayName || '', password: '', role: item?.role || 'user', enabled: item?.enabled ?? true, rootIds: item?.rootIds || [] })
  const [error, setError] = useState(''); const [saving, setSaving] = useState(false)
  const submit = async e => { e.preventDefault(); setSaving(true); setError(''); try { const body = item ? { displayName: form.displayName, role: form.role, enabled: form.enabled, ...(form.password ? { password: form.password } : {}) } : { username: form.username, displayName: form.displayName, password: form.password, role: form.role, enabled: form.enabled }; const saved = await api(item ? `/api/v1/users/${item.id}` : '/api/v1/users', { method: item ? 'PUT' : 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }); await api(`/api/v1/users/${saved.id}/access-roots`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ rootIds: form.rootIds }) }); onSaved() } catch (e2) { setError(e2.message); setSaving(false) } }
  return <Modal title={item ? `编辑用户 · ${item.username}` : '新增用户'} onClose={onClose} onSubmit={submit} saving={saving}>{error && <Error text={error}/>} {!item && <Field title="用户名"><input autoFocus required value={form.username} onChange={e => setForm({ ...form, username: e.target.value })} style={field}/></Field>}<Field title="显示名"><input required value={form.displayName} onChange={e => setForm({ ...form, displayName: e.target.value })} style={field}/></Field><Field title={item ? '重置密码（留空则不变）' : '密码'}><input required={!item} type="password" value={form.password} onChange={e => setForm({ ...form, password: e.target.value })} style={field}/></Field><div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}><Field title="角色"><select value={form.role} onChange={e => setForm({ ...form, role: e.target.value })} style={field}><option value="user">普通用户</option><option value="admin">管理员</option></select></Field><Field title="账户状态"><label style={{ height: 34, display: 'flex', alignItems: 'center', gap: 8, fontSize: 12 }}><input type="checkbox" checked={form.enabled} onChange={e => setForm({ ...form, enabled: e.target.checked })}/>启用账户</label></Field></div><CheckList title="可访问文件根目录" items={roots} selected={form.rootIds} onChange={v => setForm({ ...form, rootIds: v })} text={r => `${r.name} · ${r.path}`}/></Modal>
}

function GroupDialog({ item, users, roots, onClose, onSaved }) {
  const [form, setForm] = useState({ name: item?.name || '', description: item?.description || '', memberIds: item?.memberIds || [], rootIds: item?.rootIds || [] }); const [error, setError] = useState(''); const [saving, setSaving] = useState(false)
  const submit = async e => { e.preventDefault(); setSaving(true); setError(''); try { await api(item ? `/api/v1/user-groups/${item.id}` : '/api/v1/user-groups', { method: item ? 'PUT' : 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(form) }); onSaved() } catch (e2) { setError(e2.message); setSaving(false) } }
  return <Modal title={item ? `编辑用户组 · ${item.name}` : '新增用户组'} onClose={onClose} onSubmit={submit} saving={saving} wide>{error && <Error text={error}/>}<Field title="名称"><input autoFocus required value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} style={field}/></Field><Field title="说明"><input value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} style={field}/></Field><div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}><CheckList title="成员" items={users} selected={form.memberIds} onChange={v => setForm({ ...form, memberIds: v })} text={u => `${u.displayName} (${u.username})`}/><CheckList title="可访问文件根目录" items={roots} selected={form.rootIds} onChange={v => setForm({ ...form, rootIds: v })} text={r => r.name}/></div></Modal>
}

function RootsDialog({ roots, onClose, onChanged }) {
  const [name, setName] = useState(''); const [path, setPath] = useState(''); const [error, setError] = useState(''); const [saving, setSaving] = useState(false)
  const add = async e => { e.preventDefault(); setSaving(true); setError(''); try { await api('/api/v1/file-roots', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name, path }) }); setName(''); setPath(''); await onChanged() } catch (e2) { setError(e2.message) } finally { setSaving(false) } }
  const remove = async root => { if (!window.confirm(`确认删除文件根目录“${root.name}”？`)) return; try { await api(`/api/v1/file-roots/${root.id}`, { method: 'DELETE' }); await onChanged() } catch (e) { setError(e.message) } }
  return <Modal title="文件根目录" onClose={onClose}><form onSubmit={add}>{error && <Error text={error}/>}<div style={{ display: 'grid', gridTemplateColumns: '150px 1fr auto', gap: 8 }}><input required value={name} onChange={e => setName(e.target.value)} placeholder="名称" style={field}/><input required value={path} onChange={e => setPath(e.target.value)} placeholder="/data/team" style={{ ...field, fontFamily: T.mono }}/><button disabled={saving} style={primary} title="新增文件根目录"><Icon name="plus" size={13}/>新增</button></div></form><div style={{ marginTop: 14, border: `1px solid ${T.border}`, borderRadius: 6, overflow: 'hidden' }}>{roots.map((r, i) => <div key={r.id} style={{ display: 'grid', gridTemplateColumns: '140px 1fr 34px', gap: 8, alignItems: 'center', padding: '8px 10px', borderTop: i ? `1px solid ${T.borderSoft}` : 'none', fontSize: 12 }}><strong>{r.name}</strong><span className="mono" style={{ color: T.ink3 }}>{r.path}</span><button type="button" title="删除" onClick={() => remove(r)} style={{ ...button, width: 28, height: 28, padding: 0, color: T.red }}><Icon name="trash" size={12}/></button></div>)}{!roots.length && <div style={{ padding: 14, color: T.ink4, fontSize: 12 }}>暂无文件根目录</div>}</div></Modal>
}

function Field({ title, children }) { return <label style={{ display: 'block', marginBottom: 12 }}><span style={label}>{title}</span>{children}</label> }
function Error({ text }) { return <div style={{ padding: '8px 10px', marginBottom: 12, background: T.redSoft, border: '1px solid #fecaca', borderRadius: 6, color: '#991b1b', fontSize: 11.5 }}>{text}</div> }
