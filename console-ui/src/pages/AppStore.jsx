import { useState, useEffect, useRef } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, Chip, Ring, Sparkline, Card, useTicker } from '../components/ui'
import { useDevice, useMetrics, useMetricsHistory, useApps, useAlerts, useNetwork, getAuthToken } from '../hooks/useApi'
import { btnSecondary, btnPrimary, btnDanger } from '../components/AppWindow'
import TabBar from '../components/TabBar'

function guessAppStyle(id, name, category) {
  const n = (id + ' ' + name).toLowerCase();
  const styles = {
    code:       { icon: 'code',     color: '#0078d4', bg: 'linear-gradient(160deg,#3b82f6,#1e40af)' },
    vscode:     { icon: 'code',     color: '#0078d4', bg: 'linear-gradient(160deg,#3b82f6,#1e40af)' },
    jupyter:    { icon: 'jupyter',  color: '#f37726', bg: 'linear-gradient(160deg,#fb923c,#c2410c)' },
    ollama:     { icon: 'ollama',   color: '#0f172a', bg: 'linear-gradient(160deg,#475569,#0f172a)' },
    vllm:       { icon: 'vllm',     color: '#ef4444', bg: 'linear-gradient(160deg,#fb7185,#be123c)' },
    qwen:       { icon: 'sparkle',  color: '#6366f1', bg: 'linear-gradient(160deg,#818cf8,#4338ca)' },
    deepseek:   { icon: 'sparkle',  color: '#0f172a', bg: 'linear-gradient(160deg,#1e293b,#020617)' },
    openclaw:   { icon: 'sparkle',  color: '#fb7185', bg: 'linear-gradient(160deg,#fb7185,#be123c)' },
    lobe:       { icon: 'openwebui',color: '#8b5cf6', bg: 'linear-gradient(160deg,#a78bfa,#7c3aed)' },
    webui:      { icon: 'openwebui',color: '#0891b2', bg: 'linear-gradient(160deg,#22d3ee,#0891b2)' },
    maxkb:      { icon: 'brain',    color: '#7c3aed', bg: 'linear-gradient(160deg,#a78bfa,#6d28d9)' },
    hermes:     { icon: 'brain',    color: '#10b981', bg: 'linear-gradient(160deg,#34d399,#059669)' },
    n8n:        { icon: 'wrench',   color: '#fb7185', bg: 'linear-gradient(160deg,#fb7185,#e11d48)' },
    mysql:      { icon: 'database', color: '#0891b2', bg: 'linear-gradient(160deg,#22d3ee,#0891b2)' },
    postgres:   { icon: 'database', color: '#3b82f6', bg: 'linear-gradient(160deg,#60a5fa,#2563eb)' },
    redis:      { icon: 'database', color: '#ef4444', bg: 'linear-gradient(160deg,#fb7185,#e11d48)' },
    comfy:      { icon: 'palette',  color: '#7c3aed', bg: 'linear-gradient(160deg,#a78bfa,#6d28d9)' },
    train:      { icon: 'flame',    color: '#ea580c', bg: 'linear-gradient(160deg,#fb923c,#9a3412)' },
  };
  for (const [key, style] of Object.entries(styles)) {
    if (n.includes(key)) return style;
  }
  // 按分类 fallback
  const catStyles = {
    'ai-inference':    { icon: 'sparkle',  color: '#6366f1', bg: 'linear-gradient(160deg,#818cf8,#4338ca)' },
    'ai-tools':        { icon: 'wrench',   color: '#10b981', bg: 'linear-gradient(160deg,#34d399,#059669)' },
    'dev-environment': { icon: 'code',     color: '#3b82f6', bg: 'linear-gradient(160deg,#3b82f6,#1d4ed8)' },
    'database':        { icon: 'database', color: '#0891b2', bg: 'linear-gradient(160deg,#22d3ee,#0891b2)' },
  };
  return catStyles[category] || { icon: 'apps', color: '#475569', bg: 'linear-gradient(160deg,#64748b,#334155)' };
}

function parseRawValue(v) {
  if (v && typeof v === 'object' && 'raw' in v) return String(v.raw);
  return v == null ? '' : String(v);
}

function DeployDialog({ app, onClose, onSuccess }) {
  const [loading, setLoading] = useState(true);
  const [schema, setSchema] = useState(null);
  const [defaults, setDefaults] = useState({});
  const [values, setValues] = useState({});
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState(null);
  const [showPwd, setShowPwd] = useState({});

  useEffect(() => {
    const token = getAuthToken();
    const headers = token ? { Authorization: 'Bearer ' + token } : {};
    fetch(`/api/v1/store/version?appId=${encodeURIComponent(app.id)}&v=${encodeURIComponent(app.ver || '')}`, { headers })
      .then(r => r.ok ? r.json() : Promise.reject(new Error('获取版本信息失败')))
      .then(data => {
        const s = data.valuesSchema;
        const d = data.defaultValues || {};
        setSchema(s);
        setDefaults(d);
        const init = {};
        if (s && s.fields) {
          s.fields.forEach(f => {
            init[f.key] = parseRawValue(d[f.key]);
          });
        }
        setValues(init);
      })
      .catch(e => setError(e.message))
      .finally(() => setLoading(false));
  }, [app.id, app.ver]);

  const setField = (key, val) => setValues(prev => ({ ...prev, [key]: val }));

  const handleSubmit = async () => {
    if (schema && schema.fields) {
      const missing = schema.fields.filter(f => f.required && !values[f.key] && values[f.key] !== 0);
      if (missing.length > 0) {
        setError(`请填写必填项: ${missing.map(f => f.label?.zh || f.key).join(', ')}`);
        return;
      }
    }
    const token = getAuthToken();
    if (!token) { setError('请先登录'); return; }
    setSubmitting(true);
    setError(null);
    try {
      const payload = { appId: app.id, version: app.ver || '' };
      if (Object.keys(values).length > 0) {
        const converted = {};
        if (schema && schema.fields) {
          schema.fields.forEach(f => {
            let v = values[f.key];
            if (f.type === 'number' && v !== '' && v != null) v = Number(v);
            if (f.type === 'boolean') v = v === true || v === 'true';
            converted[f.key] = v;
          });
        }
        payload.values = converted;
      }
      const res = await fetch('/api/v1/store/install', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token },
        body: JSON.stringify(payload),
      });
      if (!res.ok) { const msg = await res.text(); throw new Error(msg); }
      const result = await res.json();
      onSuccess(result);
    } catch (e) {
      setError(e.message || '部署失败');
    } finally {
      setSubmitting(false);
    }
  };

  const fields = schema?.fields || [];
  const inputStyle = {
    width: '100%', height: 34, padding: '0 10px', borderRadius: 7, border: `1px solid ${T.border}`,
    fontSize: 13, background: T.surface, color: T.ink, outline: 'none', boxSizing: 'border-box',
  };

  return (
    <div style={{
      position: 'fixed', inset: 0, zIndex: 999, display: 'flex', alignItems: 'center', justifyContent: 'center',
      background: 'rgba(15,23,42,0.5)', backdropFilter: 'blur(4px)',
    }} onClick={onClose}>
      <div onClick={e => e.stopPropagation()} style={{
        width: 480, maxHeight: '80vh', borderRadius: 14, background: T.surface,
        border: `1px solid ${T.border}`, boxShadow: '0 24px 48px -12px rgba(0,0,0,0.2)',
        display: 'flex', flexDirection: 'column', overflow: 'hidden',
      }}>
        <div style={{ padding: '16px 20px', borderBottom: `1px solid ${T.border}`, display: 'flex', alignItems: 'center', gap: 10 }}>
          <div style={{
            width: 32, height: 32, borderRadius: 8, background: app.iconUrl ? T.surfaceAlt : app.bg,
            color: 'white', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0, overflow: 'hidden',
          }}>
            {app.iconUrl ? <img src={app.iconUrl} style={{ width: 32, height: 32, objectFit: 'cover' }} /> : <Icon name={app.icon} size={18} stroke={1.7}/>}
          </div>
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 15, fontWeight: 700, color: T.ink }}>部署 {app.name}</div>
            <div style={{ fontSize: 11, color: T.ink3 }} className="mono">{app.ver}</div>
          </div>
          <button onClick={onClose} style={{ background: 'none', border: 'none', cursor: 'pointer', padding: 4, color: T.ink3 }}>
            <Icon name="x" size={16} stroke={2}/>
          </button>
        </div>

        <div style={{ flex: 1, overflow: 'auto', padding: '16px 20px' }}>
          {loading && <div style={{ textAlign: 'center', padding: 40, color: T.ink3 }}>加载配置中...</div>}
          {!loading && fields.length === 0 && !error && (
            <div style={{ textAlign: 'center', padding: 40, color: T.ink3 }}>此应用无需配置参数</div>
          )}
          {!loading && fields.length > 0 && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
              {fields.map(f => {
                const label = f.label?.zh || f.label?.en || f.key;
                return (
                  <div key={f.key}>
                    <div style={{ fontSize: 12, color: T.ink2, fontWeight: 600, marginBottom: 5 }}>
                      {label}{f.required && <span style={{ color: '#ef4444', marginLeft: 2 }}>*</span>}
                    </div>
                    {f.type === 'select' ? (
                      <select value={values[f.key] || ''} onChange={e => setField(f.key, e.target.value)}
                        style={{ ...inputStyle, cursor: 'pointer' }}>
                        {(f.options || []).map(o => (
                          <option key={o.value} value={o.value}>{o.label}</option>
                        ))}
                      </select>
                    ) : f.type === 'password' ? (
                      <div style={{ position: 'relative' }}>
                        <input type={showPwd[f.key] ? 'text' : 'password'} value={values[f.key] || ''}
                          onChange={e => setField(f.key, e.target.value)} placeholder={label} style={{ ...inputStyle, paddingRight: 36 }}/>
                        <button onClick={() => setShowPwd(p => ({ ...p, [f.key]: !p[f.key] }))}
                          style={{ position: 'absolute', right: 6, top: 7, background: 'none', border: 'none', cursor: 'pointer', color: T.ink4, padding: 2 }}>
                          <Icon name="eye" size={14} stroke={1.8}/>
                        </button>
                      </div>
                    ) : f.type === 'boolean' ? (
                      <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer' }}>
                        <input type="checkbox" checked={values[f.key] === true || values[f.key] === 'true'}
                          onChange={e => setField(f.key, e.target.checked)}/>
                        <span style={{ fontSize: 12.5, color: T.ink2 }}>{label}</span>
                      </label>
                    ) : (
                      <input type={f.type === 'number' ? 'number' : 'text'} value={values[f.key] ?? ''}
                        onChange={e => setField(f.key, e.target.value)} placeholder={label} style={inputStyle}/>
                    )}
                  </div>
                );
              })}
            </div>
          )}
          {error && <div style={{ fontSize: 12, color: '#ef4444', marginTop: 10, padding: '8px 10px', background: '#fef2f2', borderRadius: 6 }}>{error}</div>}
        </div>

        <div style={{ padding: '12px 20px', borderTop: `1px solid ${T.border}`, display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
          <button onClick={onClose} className="edge-press edge-btn-secondary" style={{ ...btnSecondary, height: 34, padding: '0 16px' }}>取消</button>
          <button onClick={handleSubmit} disabled={submitting || loading}
            className="edge-press edge-btn-primary"
            style={{ ...btnPrimary, height: 34, padding: '0 20px', opacity: (submitting || loading) ? 0.6 : 1 }}>
            <Icon name="download" size={13} stroke={2}/>{submitting ? '部署中...' : '确认部署'}
          </button>
        </div>
      </div>
    </div>
  );
}

function DeployStatus({ deploymentName, onDone }) {
  const [phase, setPhase] = useState('Pending');
  const [message, setMessage] = useState('');

  useEffect(() => {
    let timer;
    let stopped = false;
    const poll = async () => {
      try {
        const token = getAuthToken();
        const headers = token ? { Authorization: 'Bearer ' + token } : {};
        const res = await fetch('/api/v1/store/deployments', { headers });
        if (res.ok) {
          const data = await res.json();
          const dep = data.find(d => d.name === deploymentName);
          if (dep) {
            setPhase(dep.phase || 'Pending');
            setMessage(dep.message || '');
            if (dep.phase === 'Running' || dep.phase === 'Failed') {
              stopped = true;
              return;
            }
          }
        }
      } catch {}
      if (!stopped) timer = setTimeout(poll, 3000);
    };
    poll();
    return () => { stopped = true; clearTimeout(timer); };
  }, [deploymentName]);

  const toneMap = { Pending: '#f59e0b', Running: '#10b981', Failed: '#ef4444' };
  const color = toneMap[phase] || T.ink3;

  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '8px 12px', borderRadius: 8, background: T.surfaceAlt, border: `1px solid ${T.borderSoft}` }}>
      <StatusDot tone={phase === 'Running' ? 'green' : phase === 'Failed' ? 'red' : 'yellow'} size={8}/>
      <span style={{ fontSize: 12.5, fontWeight: 600, color }}>{phase}</span>
      {message && <span style={{ fontSize: 11, color: T.ink3 }}>— {message}</span>}
    </div>
  );
}

function DeployField({ label, value, hint }) {
  return (
    <div style={{ padding: 12, borderRadius: 8, background: T.surfaceAlt, border: `1px solid ${T.borderSoft}` }}>
      <div style={{ fontSize: 10.5, color: T.ink3, fontWeight: 600,
        letterSpacing: '0.04em', textTransform: 'uppercase' }}>{label}</div>
      <div style={{ fontSize: 13, color: T.ink, fontWeight: 600, marginTop: 4 }}>{value}</div>
      {hint && <div style={{ fontSize: 11, color: T.ink4, marginTop: 4 }}>{hint}</div>}
    </div>
  );
}

function AppStoreDetail({ app, onBack, authed, onRequireAuth, onOpenApp, onInstall }) {
  const [showDeploy, setShowDeploy] = useState(false);
  const [deployResult, setDeployResult] = useState(null);

  const handleDeploySuccess = (result) => {
    setShowDeploy(false);
    setDeployResult(result);
  };

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', background: T.surfaceAlt, overflow: 'auto' }}>
      <div style={{ padding: '12px 24px', borderBottom: `1px solid ${T.border}`, background: T.surface,
        display: 'flex', alignItems: 'center', gap: 10 }}>
        <button onClick={onBack} className="edge-press edge-btn-secondary" style={{ ...btnSecondary, height: 28, padding: '0 10px' }}>
          <Icon name="chevDown" size={12} stroke={2} style={{ transform: 'rotate(90deg)' }}/>
          返回应用列表
        </button>
        <span style={{ fontSize: 11.5, color: T.ink3 }}>
          应用列表 <span style={{ color: T.ink4 }}>/</span> <span style={{ color: T.ink, fontWeight: 600 }}>{app.name}</span>
        </span>
      </div>

      <div style={{ padding: 24 }}>
        <div style={{
          background: T.surface, border: `1px solid ${T.border}`, borderRadius: 12,
          padding: 24, marginBottom: 16,
          display: 'flex', alignItems: 'flex-start', gap: 18,
        }}>
          <div style={{
            width: 64, height: 64, borderRadius: 14,
            background: app.iconUrl ? T.surfaceAlt : app.bg, color: 'white',
            display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0,
            boxShadow: app.iconUrl ? '0 1px 4px rgba(0,0,0,0.1)' : 'inset 0 1px 0 rgba(255,255,255,0.3)',
            overflow: 'hidden',
          }}>
            {app.iconUrl
              ? <img src={app.iconUrl} style={{ width: 64, height: 64, objectFit: 'cover' }} />
              : <Icon name={app.icon} size={30} stroke={1.6}/>}
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <div style={{ fontSize: 22, fontWeight: 700, color: T.ink, letterSpacing: '-0.01em' }}>{app.name}</div>
              <Chip tone="gray"><span className="mono">{app.ver}</span></Chip>
              <Chip tone="blue">{app.cat}</Chip>
              {(app.installed || deployResult) && <Chip tone="green"><StatusDot tone="green" size={6}/>{deployResult ? '已部署' : '已安装'}</Chip>}
            </div>
            <div style={{ fontSize: 12, color: T.ink3, marginTop: 6, display: 'flex', gap: 10 }}>
              {app.dev && <><span>开发者 <span style={{ color: T.ink2, fontWeight: 600 }}>{app.dev}</span></span><span style={{ color: '#cbd5e1' }}>·</span></>}
              {app.provider && <><span>提供方 <span style={{ color: T.ink2, fontWeight: 600 }}>{app.provider}</span></span><span style={{ color: '#cbd5e1' }}>·</span></>}
              {app.rating && <><span>评分 <span style={{ color: T.amber, fontWeight: 600 }}>★ {app.rating}</span></span><span style={{ color: '#cbd5e1' }}>·</span></>}
              {app.downloads && <><span>下载 <span className="mono" style={{ color: T.ink2, fontWeight: 600 }}>{app.downloads}</span></span><span style={{ color: '#cbd5e1' }}>·</span></>}
              {app.date && <span>更新于 <span className="mono">{app.date}</span></span>}
            </div>
            <div style={{ fontSize: 13, color: T.ink2, marginTop: 12, lineHeight: 1.7 }}>{app.desc}</div>
          </div>
          {/* LF 2026-06-22：商店详情页按钮 = 始终「部署」（即使已安装也允许重新部署
              覆盖配置 / 升级版本 / 部署第二实例）。「打开应用」是桌面图标的职责，
              不在商店职责内 —— 已安装状态由左侧绿 chip「已安装」标识就够 */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8, flexShrink: 0 }}>
            <button onClick={() => setShowDeploy(true)} className="edge-press edge-btn-primary" style={{ ...btnPrimary, height: 36, padding: '0 18px' }}>
              <Icon name="download" size={13} stroke={2}/>部署
            </button>
          </div>
        </div>

        {deployResult && (
          <Card title="部署状态" padding={16} style={{ marginBottom: 12 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
              <DeployStatus deploymentName={deployResult.name}/>
              <span style={{ fontSize: 12, color: T.ink3 }} className="mono">{deployResult.name}</span>
            </div>
          </Card>
        )}

        <Card title="README · 简介" padding={20}>
          <div style={{ fontSize: 13, color: T.ink2, lineHeight: 1.75 }}>
            <p style={{ margin: '0 0 10px' }}>
              <strong style={{ color: T.ink }}>{app.name}</strong> — {app.desc}
            </p>
          </div>
        </Card>
      </div>

      {showDeploy && <DeployDialog app={app} onClose={() => setShowDeploy(false)} onSuccess={handleDeploySuccess}/>}
    </div>
  );
}

function StoreCard({ app, onOpen, onOpenApp }) {
  return (
    <div onClick={onOpen}
         className="edge-row-hover"
         style={{
      background: T.surface, border: `1px solid ${T.border}`,
      borderRadius: 10, padding: 14, cursor: 'pointer',
      boxShadow: 'none',
      transition: 'all 0.15s ease',
      display: 'flex', flexDirection: 'column', gap: 10,
      '--edge-row-hover-bg': T.surface,
      '--edge-row-hover-border-color': '#bfdbfe',
      '--edge-row-hover-box-shadow': '0 6px 14px -6px rgba(15,23,42,0.12)',
    }}>
      <div style={{ display: 'flex', alignItems: 'flex-start', gap: 10 }}>
        <div style={{
          width: 40, height: 40, borderRadius: 10,
          background: app.iconUrl ? T.surfaceAlt : app.bg, color: 'white',
          display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0,
          boxShadow: app.iconUrl ? `0 1px 3px rgba(0,0,0,0.08)` : `0 2px 6px -1px ${(app.bg||'').match(/#[0-9a-f]+/i)?.[0] || T.blue}55, inset 0 1px 0 rgba(255,255,255,0.3)`,
          overflow: 'hidden',
        }}>
          {app.iconUrl
            ? <img src={app.iconUrl} style={{ width: 40, height: 40, objectFit: 'cover' }} />
            : <Icon name={app.icon} size={20} stroke={1.7}/>}
        </div>
        <div style={{ minWidth: 0, flex: 1 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 5 }}>
            <div style={{ fontSize: 14, fontWeight: 600, color: T.ink,
              whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
              {app.name}
            </div>
            {app.installed && !app.upgradable && (
              <span style={{
                fontSize: 9.5, padding: '1px 5px', borderRadius: 3,
                background: '#ecfdf5', color: '#047857', border: '1px solid #a7f3d0',
                fontWeight: 600, flexShrink: 0,
              }}>已安装</span>
            )}
            {app.upgradable && (
              <span style={{
                fontSize: 9.5, padding: '1px 5px', borderRadius: 3,
                background: '#fffbeb', color: '#b45309', border: '1px solid #fde68a',
                fontWeight: 600, flexShrink: 0,
              }} title={`已装 ${app.installedVersion} → 商店 ${app.ver}`}>可升级</span>
            )}
          </div>
          <div style={{ fontSize: 11, color: T.ink3, marginTop: 3, display: 'flex', gap: 6, alignItems: 'center' }}>
            <span className="mono">{app.ver}</span>
            <span style={{ color: '#cbd5e1' }}>·</span>
            <span>★ {app.rating}</span>
            <span style={{ color: '#cbd5e1' }}>·</span>
            <span className="mono">↓ {app.downloads}</span>
          </div>
        </div>
      </div>

      <div style={{ fontSize: 12, color: T.ink2, lineHeight: 1.55,
        display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical',
        overflow: 'hidden', minHeight: 38 }}>{app.desc}</div>

      <div style={{
        display: 'flex', alignItems: 'center', gap: 6,
        paddingTop: 10, borderTop: `1px dashed ${T.border}`,
      }}>
        <span style={{
          fontSize: 10.5, padding: '2px 6px', borderRadius: 3,
          background: T.surfaceAlt, color: T.ink3, border: `1px solid ${T.borderSoft}`,
        }}>{app.cat}</span>
        <span style={{ fontSize: 10.5, color: T.ink4 }}>{app.dev}</span>
        <div style={{ flex: 1 }}/>
        <span style={{ fontSize: 10.5, color: T.ink4 }} className="mono">{app.date}</span>
        {app.installed
          ? <button onClick={(e) => { e.stopPropagation(); onOpenApp && onOpenApp({ id: app.id }); }}
              className="edge-press edge-btn-secondary"
              style={{ ...btnSecondary, height: 26, padding: '0 10px', fontSize: 11.5 }}>
              <Icon name="play" size={11} stroke={2}/>打开
            </button>
          : <button onClick={(e) => { e.stopPropagation(); onOpen(); }}
              className="edge-press edge-btn-primary"
              style={{ ...btnPrimary, height: 26, padding: '0 10px', fontSize: 11.5 }}>
              <Icon name="download" size={11} stroke={2}/>部署
            </button>
        }
      </div>
    </div>
  );
}

// compareVersions 简单语义版本比较，返 -1/0/1。非数字段 fallback 到字符串比较。
// 不引 semver 库，只 cover 常见格式 (1.2.3 / v1.2.3 / 1.2.3-beta)。
function compareVersions(a, b) {
  if (!a || !b) return 0;
  const norm = s => String(s).replace(/^v/, '').split(/[.\-+]/);
  const pa = norm(a), pb = norm(b);
  const len = Math.max(pa.length, pb.length);
  for (let i = 0; i < len; i++) {
    const na = parseInt(pa[i] || '0', 10);
    const nb = parseInt(pb[i] || '0', 10);
    if (!isNaN(na) && !isNaN(nb)) {
      if (na !== nb) return na < nb ? -1 : 1;
    } else {
      const sa = pa[i] || '', sb = pb[i] || '';
      if (sa !== sb) return sa < sb ? -1 : 1;
    }
  }
  return 0;
}

export default function AppStore({ onOpenApp, authed, onRequireAuth }) {
  const [cat, setCat] = useState('all');
  const [tab, setTab] = useState('all'); // 'all' | 'installed' | 'upgradable'  对齐 1Panel 应用商店
  const [q, setQ] = useState('');
  const [detail, setDetail] = useState(null);
  const [platformApps, setPlatformApps] = useState(null);

  // 已部署应用清单（10s 轮询）—— 用于判断 store app 的 installed / upgradable 状态
  const { data: deployedApps } = useApps(10000);

  const catLabelMap = { 'ai-inference': 'AI 推理', 'ai-tools': 'AI 工具', 'dev-environment': '开发环境', 'database': '存储' };
  const catIconMap = { 'ai-inference': 'sparkle', 'ai-tools': 'wrench', 'dev-environment': 'code', 'database': 'database' };

  // 已部署 name → version map（用于 cross-reference store apps）
  // appName 优先用 name（用户起名），fallback id；matchKey 跟 storeApp.id 比对
  const deployedMap = (() => {
    const m = {};
    (deployedApps || []).forEach(d => {
      // 部署的 app 可能用「应用 id」或「自定义 name」标识；都加进 map 多一种命中机会
      if (d.name) m[d.name] = d.version;
      if (d.id) m[d.id] = d.version;
    });
    return m;
  })();

  useEffect(() => {
    const token = getAuthToken();
    const headers = token ? { Authorization: 'Bearer ' + token } : {};
    fetch('/api/v1/store/apps', { headers }).then(r => r.ok ? r.json() : null).then(data => {
      if (data && Array.isArray(data) && data.length > 0) setPlatformApps(data);
    }).catch(() => {});
  }, []);

  // 从平台数据计算分类（每次 render 重新计算，避免 state 时序问题）
  const categories = platformApps ? (() => {
    const catMap = {};
    platformApps.forEach(a => { if (a.category) catMap[a.category] = (catMap[a.category] || 0) + 1; });
    const cats = [{ id: 'all', name: '全部', icon: 'apps', count: platformApps.length }];
    Object.entries(catMap).forEach(([k, v]) => {
      cats.push({ id: k, name: catLabelMap[k] || k, icon: catIconMap[k] || 'apps', count: v });
    });
    return cats;
  })() : [{ id: 'all', name: '全部', icon: 'apps', count: 0 }];

  // 使用平台数据或 fallback 到 mock
  // 给每个 store app 注入 installedVersion / upgradable 状态，由 deployedMap cross-reference
  const storeApps = platformApps
    ? platformApps.map(a => {
        const { color, bg } = guessAppStyle(a.id, a.name, a.category);
        const installedVersion = deployedMap[a.id] || deployedMap[a.name] || null;
        const upgradable = installedVersion != null && a.version != null
          && compareVersions(installedVersion, a.version) < 0;
        return {
          id: a.id, name: a.name, cat: catLabelMap[a.category] || a.category,
          ver: a.version, desc: a.description || '',
          iconUrl: a.icon,
          icon: null, bg, color,
          provider: a.provider,
          installed: installedVersion != null,
          installedVersion,
          upgradable,
        };
      })
    : [];

  // tab 计数（在 category filter 之前算，让 tab 计数反映全局总量不被 sidebar 干扰）
  const allCount = storeApps.length;
  const installedCount = storeApps.filter(a => a.installed).length;
  const upgradableCount = storeApps.filter(a => a.upgradable).length;

  const list = storeApps.filter(a => {
    // tab 过滤优先（1Panel 风格：3 tab 是主轴）
    if (tab === 'installed' && !a.installed) return false;
    if (tab === 'upgradable' && !a.upgradable) return false;
    // category sidebar 二次过滤
    if (cat !== 'all') {
      const catName = catLabelMap[cat] || cat;
      if (a.cat !== catName) return false;
    }
    if (q && !a.name.toLowerCase().includes(q.toLowerCase()) && !(a.desc||'').includes(q)) return false;
    return true;
  });

  if (detail) {
    return <AppStoreDetail app={detail} onBack={() => setDetail(null)} authed={authed} onRequireAuth={onRequireAuth} onOpenApp={onOpenApp} onInstall={() => setDetail(null)}/>;
  }

  return (
    <div style={{ flex: 1, display: 'flex', background: T.surfaceAlt, overflow: 'hidden' }}>
      {/* Sidebar */}
      <div style={{
        width: 200, flexShrink: 0, padding: '20px 12px',
        background: T.surface, borderRight: `1px solid ${T.border}`,
        overflow: 'auto',
      }}>
        <div style={{
          fontSize: 10.5, fontWeight: 600, color: T.ink3,
          letterSpacing: '0.06em', textTransform: 'uppercase',
          padding: '0 8px 6px',
        }}>发现</div>
        <div style={{
          fontSize: 10.5, color: T.ink4, padding: '0 8px 8px',
        }}>应用分类</div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
          {categories.map(c => {
            const on = cat === c.id;
            return (
              <div key={c.id} onClick={() => setCat(c.id)} style={{
                display: 'flex', alignItems: 'center', gap: 8,
                padding: '8px 10px', borderRadius: 7, cursor: 'pointer',
                background: on ? T.blueSoft : 'transparent',
                color: on ? T.blueDeep : T.ink2,
                fontSize: 12.5, fontWeight: on ? 600 : 500,
              }}>
                <Icon name={c.icon} size={14} stroke={1.8}/>
                <span style={{ flex: 1 }}>{c.name}</span>
                <span className="mono tnum" style={{
                  fontSize: 10.5, color: on ? T.blueDeep : T.ink4,
                  background: on ? '#dbeafe' : T.surfaceAlt,
                  padding: '0 6px', borderRadius: 999, lineHeight: '17px',
                }}>{c.count}</span>
              </div>
            );
          })}
        </div>

        <div style={{ height: 1, background: T.borderSoft, margin: '16px 8px' }}/>

        <div style={{
          padding: 10, borderRadius: 8,
          background: 'linear-gradient(155deg, #eff6ff, #ede9fe)',
          border: '1px solid #ddd6fe',
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 6 }}>
            <Icon name="cloud" size={13} stroke={1.8} style={{ color: T.blueDeep }}/>
            <span style={{ fontSize: 12, fontWeight: 600, color: T.ink }}>云端同步</span>
          </div>
          <div style={{ fontSize: 11, color: T.ink3, lineHeight: 1.5 }}>
            应用清单由云端下发。本机最近一次同步 <span className="mono tnum" style={{ color: T.ink2, fontWeight: 600 }}>5 分钟前</span>。
          </div>
        </div>
      </div>

      {/* Main */}
      <div style={{ flex: 1, overflow: 'auto', padding: '20px 24px' }}>
        {/* 顶部 3 tab —— 对齐 1Panel 应用商店「全部 / 已安装 / 可升级」交互。
            tab 是状态主轴，sidebar category 是二次过滤维度。tab 计数实时反映
            cross-reference 结果（store apps × 已部署 apps） */}
        <TabBar
          tabs={[
            { id: 'all', label: '全部', count: allCount },
            { id: 'installed', label: '已安装', count: installedCount },
            { id: 'upgradable', label: '可升级', count: upgradableCount },
          ]}
          active={tab}
          onChange={setTab}
          style={{ gap: 4, marginBottom: 18, borderBottom: `1px solid ${T.borderSoft}` }}
          itemStyle={{ padding: '10px 16px 12px', fontSize: 13, marginBottom: -1 }}
          renderLabel={(t2, on) => (
            <>
              {t2.label}
              <span style={{
                fontSize: 11, color: T.ink4, fontWeight: 500,
                padding: '1px 6px', borderRadius: 3,
                background: on ? T.blueSoft : T.surfaceAlt,
              }} className="mono tnum">{t2.count}</span>
            </>
          )}
        />

        {/* Featured — 仅展示 StoreApp CR 上有 label app.theriseunion.io/pinned=true 的应用
            （之前是 slice(0,4) 粗暴拿前 4 个，没有"推荐"机制）
            tab=all 时显示推荐区；installed/upgradable tab 隐藏（避免推荐位干扰用户的「已安装」视图） */}
        {tab === 'all' && platformApps && platformApps.filter(a => a.pinned).length > 0 && (
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 18 }}>
          {platformApps.filter(a => a.pinned).slice(0, 4).map((f, i) => {
            const { bg } = guessAppStyle(f.id, f.name, f.category);
            return (
            <div key={i} style={{
              padding: '20px 22px', borderRadius: 12,
              background: bg, color: 'white',
              position: 'relative', overflow: 'hidden',
              minHeight: 140, display: 'flex', flexDirection: 'column', justifyContent: 'space-between',
              boxShadow: '0 4px 14px -4px rgba(15,23,42,0.18)',
            }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                <div style={{
                  width: 36, height: 36, borderRadius: 9,
                  background: 'rgba(255,255,255,0.18)',
                  display: 'flex', alignItems: 'center', justifyContent: 'center',
                  backdropFilter: 'blur(8px)',
                }}>
                  {f.icon ? <img src={f.icon} alt="" style={{ width: 20, height: 20 }}/> : <Icon name="sparkle" size={20} stroke={1.7}/>}
                </div>
                <div>
                  <div style={{ fontSize: 16, fontWeight: 700, letterSpacing: '-0.01em' }}>{f.name}</div>
                  <div style={{ fontSize: 11.5, opacity: 0.85, marginTop: 2 }}>{f.category}</div>
                </div>
              </div>
              <div>
                <div style={{ fontSize: 12.5, opacity: 0.95, lineHeight: 1.55, marginBottom: 10 }}>{f.description || ''}</div>
              </div>
              <div style={{
                position: 'absolute', top: -20, right: -20, width: 140, height: 140,
                background: 'radial-gradient(circle, rgba(255,255,255,0.18), transparent 70%)',
                pointerEvents: 'none',
              }}/>
            </div>
            );
          })}
        </div>
        )}

        {/* Toolbar */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 12 }}>
          <div style={{ fontSize: 14, fontWeight: 700, color: T.ink }}>
            {categories.find(c => c.id === cat)?.name}
          </div>
          <div style={{
            fontSize: 11.5, padding: '2px 8px', borderRadius: 999,
            background: T.surface, border: `1px solid ${T.border}`, color: T.ink3,
          }}>共 {list.length} 个应用</div>
          <div style={{ flex: 1 }}/>
          <div style={{
            display: 'flex', alignItems: 'center', gap: 8,
            height: 32, padding: '0 10px', borderRadius: 7,
            background: T.surface, border: `1px solid ${T.border}`,
            width: 240,
          }}>
            <Icon name="search" size={13} stroke={1.8} style={{ color: T.ink4 }}/>
            <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="按名称搜索"
              style={{ flex: 1, border: 'none', outline: 'none', fontSize: 12.5, background: 'transparent' }}/>
          </div>
          <button className="edge-press edge-btn-secondary" style={btnSecondary}>
            <Icon name="refresh" size={13} stroke={1.8}/>同步
          </button>
        </div>

        {/* Grid */}
        <div style={{
          display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(290px, 1fr))', gap: 12,
        }}>
          {list.map(a => (
            <StoreCard key={a.id} app={a} onOpen={() => setDetail(a)} onOpenApp={onOpenApp}/>
          ))}
        </div>

        {list.length === 0 && (
          <div style={{
            textAlign: 'center', padding: '60px 20px', color: T.ink3, fontSize: 13,
          }}>
            <Icon
              name={tab === 'upgradable' ? 'check' : tab === 'installed' ? 'apps' : 'search'}
              size={28} stroke={1.5} style={{ color: T.ink4, marginBottom: 8 }}
            />
            <div>{
              tab === 'upgradable' ? '所有已安装应用都是最新版本' :
              tab === 'installed'  ? '本节点尚未安装任何应用' :
              '没有匹配的应用'
            }</div>
          </div>
        )}

        <div style={{
          textAlign: 'center', margin: '24px 0 8px',
          fontSize: 11.5, color: T.ink4,
          display: 'flex', alignItems: 'center', gap: 12, justifyContent: 'center',
        }}>
          <span style={{ height: 1, width: 60, background: T.border }}/>
          已加载全部
          <span style={{ height: 1, width: 60, background: T.border }}/>
        </div>
      </div>
    </div>
  );
}
