// AppStore — 应用商店（Issue #2 阶段4：聚合平台商店 + 第三方 catalog）。
//
// 数据来源：
//   - 平台商店：GET /store/apps（edge-apiserver storeapps），安装走 /store/version + /store/install。
//   - 第三方 catalog：GET /catalogs/apps（多 source 聚合），安装走 /catalogs/version + /catalogs/install。
//   - 来源状态：GET /catalogs（各 source 健康）；POST /catalogs 显式刷新（用上次缓存兜底）。
//
// 安全约束（CEO 裁决第4/5/7条）：
//   - 前端永不接收/发送 compose 模板原文；安装一律由后端从可信 source 重取渲染。
//   - password 字段仅写（进 .env），不回显。
//   - K8s-only 包（installable=false）禁止本机安装，仅展示。
//
// installed / upgradable：用已部署应用（/apps，含 source）精确匹配，实时刷新。
import { useState, useMemo, useEffect } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, Chip, Card } from '../components/ui'
import {
  useApps, useStoreApps, useCatalogApps, useCatalogSources,
  useCatalogSourceConfigs, testCatalogSource, createCatalogSource, updateCatalogSource, deleteCatalogSource, refreshCatalogSource,
  getStoreVersion, getCatalogVersion, installStoreApp, installCatalogApp, refreshCatalogs,
  useTask,
} from '../hooks/useApi'
import { btnSecondary, btnPrimary } from '../components/AppWindow'
import TabBar from '../components/TabBar'
import {
  compareVersions, parseRawValue, fieldLabel, coerceValueForSubmit, missingRequiredFields,
} from '../lib/compose'
import { useToast } from '../components/toastContext'

// ─── 视觉：按 app id/name/category 推断图标/渐变（保持现有设计语言）────────
function guessAppStyle(id, name, category) {
  const n = (id + ' ' + name).toLowerCase();
  const styles = {
    code: { icon: 'code', bg: 'linear-gradient(160deg,#3b82f6,#1e40af)' },
    vscode: { icon: 'code', bg: 'linear-gradient(160deg,#3b82f6,#1e40af)' },
    jupyter: { icon: 'jupyter', bg: 'linear-gradient(160deg,#fb923c,#c2410c)' },
    ollama: { icon: 'ollama', bg: 'linear-gradient(160deg,#475569,#0f172a)' },
    vllm: { icon: 'vllm', bg: 'linear-gradient(160deg,#fb7185,#be123c)' },
    qwen: { icon: 'sparkle', bg: 'linear-gradient(160deg,#818cf8,#4338ca)' },
    deepseek: { icon: 'sparkle', bg: 'linear-gradient(160deg,#1e293b,#020617)' },
    lobe: { icon: 'openwebui', bg: 'linear-gradient(160deg,#a78bfa,#7c3aed)' },
    webui: { icon: 'openwebui', bg: 'linear-gradient(160deg,#22d3ee,#0891b2)' },
    mysql: { icon: 'database', bg: 'linear-gradient(160deg,#22d3ee,#0891b2)' },
    postgres: { icon: 'database', bg: 'linear-gradient(160deg,#60a5fa,#2563eb)' },
    redis: { icon: 'database', bg: 'linear-gradient(160deg,#fb7185,#e11d48)' },
    comfy: { icon: 'palette', bg: 'linear-gradient(160deg,#a78bfa,#6d28d9)' },
  };
  for (const [key, style] of Object.entries(styles)) {
    if (n.includes(key)) return style;
  }
  const catStyles = {
    'ai-inference': { icon: 'sparkle', bg: 'linear-gradient(160deg,#818cf8,#4338ca)' },
    'ai-tools': { icon: 'wrench', bg: 'linear-gradient(160deg,#34d399,#059669)' },
    'dev-environment': { icon: 'code', bg: 'linear-gradient(160deg,#3b82f6,#1d4ed8)' },
    'database': { icon: 'database', bg: 'linear-gradient(160deg,#22d3ee,#0891b2)' },
  };
  return catStyles[category] || { icon: 'apps', bg: 'linear-gradient(160deg,#64748b,#334155)' };
}

const CAT_LABEL = { 'ai-inference': 'AI 推理', 'ai-tools': 'AI 工具', 'dev-environment': '开发环境', 'database': '存储' };
const CAT_ICON = { 'ai-inference': 'sparkle', 'ai-tools': 'wrench', 'dev-environment': 'code', 'database': 'database' };

// ─── DeployDialog：统一安装表单（平台 / catalog 分流）──────────────
// 版本详情由后端取（valuesSchema/defaultValues）；compose 模板 json:"-" 不回前端。
function DeployDialog({ app, onClose, onSuccess }) {
  const [loading, setLoading] = useState(true);
  const [schema, setSchema] = useState(null);
  const [values, setValues] = useState({});
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState(null);
  const [riskFindings, setRiskFindings] = useState([]);
  const [confirmRisky, setConfirmRisky] = useState(false);
  const [showPwd, setShowPwd] = useState({});

  const isCatalog = app.origin === 'catalog';

  useEffect(() => {
    let cancelled = false;
    const fetcher = isCatalog
      ? getCatalogVersion(app.sourceId, app.id, app.ver || '')
      : getStoreVersion(app.id, app.ver || '');
    fetcher
      .then((data) => {
        if (cancelled) return;
        const s = data.valuesSchema;
        const d = data.defaultValues || {};
        setSchema(s);
        const init = {};
        if (s && s.fields) s.fields.forEach((f) => { init[f.key] = parseRawValue(d[f.key]); });
        setValues(init);
        // 二次确认可安装性（权威值以版本详情为准；列表 provisioner 粗判可能乐观）。
        if (data.installable === false) {
          setError(data.notInstallableReason || '该应用不可在本机安装');
        }
      })
      .catch((e) => { if (!cancelled) setError(e.message || '获取版本信息失败'); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [app.id, app.ver, app.sourceId, isCatalog]);

  const setField = (key, val) => {
    setValues((prev) => ({ ...prev, [key]: val }));
    setRiskFindings([]);
    setConfirmRisky(false);
    setError(null);
  };

  async function handleSubmit() {
    const fields = schema?.fields || [];
    const missing = missingRequiredFields(fields, values);
    if (missing.length > 0) { setError(`请填写必填项: ${missing.join(', ')}`); return; }
    setSubmitting(true); setError(null);
    try {
      const converted = {};
      fields.forEach((f) => { converted[f.key] = coerceValueForSubmit(f, values[f.key]); });
      const payload = isCatalog
        ? { sourceId: app.sourceId, appId: app.id, version: app.ver || '', values: converted, confirmRisky }
        : { appId: app.id, version: app.ver || '', values: converted, confirmRisky };
      const result = isCatalog ? await installCatalogApp(payload) : await installStoreApp(payload);
      onSuccess(result);
    } catch (e) {
      if (e.reason === 'risk_blocked' && Array.isArray(e.findings)) {
        setRiskFindings(e.findings);
        setConfirmRisky(false);
      }
      setError(e.message || '部署失败');
    } finally {
      setSubmitting(false);
    }
  }

  const fields = schema?.fields || [];
  const hasBlockedRisk = riskFindings.some((f) => f.level === 'blocked');
  const hasConfirmableRisk = riskFindings.some((f) => f.level === 'confirmation');
  const notInstallable = error && /不可|installable|Kubernetes/i.test(error) && !fields.length;
  const inputStyle = {
    width: '100%', height: 34, padding: '0 10px', borderRadius: 7, border: `1px solid ${T.border}`,
    fontSize: 13, background: T.surface, color: T.ink, outline: 'none', boxSizing: 'border-box',
  };

  return (
    <div style={{
      position: 'fixed', inset: 0, zIndex: 999, display: 'flex', alignItems: 'center', justifyContent: 'center',
      background: 'rgba(15,23,42,0.5)', backdropFilter: 'blur(4px)',
    }} onClick={onClose}>
      <div onClick={(e) => e.stopPropagation()} style={{
        width: 480, maxWidth: '94vw', maxHeight: '84vh', borderRadius: 14, background: T.surface,
        border: `1px solid ${T.border}`, boxShadow: '0 24px 48px -12px rgba(0,0,0,0.2)',
        display: 'flex', flexDirection: 'column', overflow: 'hidden',
      }}>
        <div style={{ padding: '16px 20px', borderBottom: `1px solid ${T.border}`, display: 'flex', alignItems: 'center', gap: 10 }}>
          <div style={{
            width: 32, height: 32, borderRadius: 8, background: app.iconUrl ? T.surfaceAlt : app.bg,
            color: 'white', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0, overflow: 'hidden',
          }}>
            {app.iconUrl ? <img src={app.iconUrl} alt="" style={{ width: 32, height: 32, objectFit: 'cover' }}/> : <Icon name={app.icon} size={18} stroke={1.7}/>}
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ ...T.type.heading, color: T.ink }}>部署 {app.name}</div>
            <div style={{ fontSize: 11, color: T.ink3 }} className="mono">
              {app.ver} · {isCatalog ? (app.sourceName || '第三方 Catalog') : '平台商店'}
            </div>
          </div>
          <button onClick={onClose} aria-label="关闭" style={{ background: 'none', border: 'none', cursor: 'pointer', padding: 4, color: T.ink3 }}>
            <Icon name="x" size={16} stroke={2}/>
          </button>
        </div>

        <div style={{ flex: 1, overflow: 'auto', padding: '16px 20px' }}>
          {loading && <div style={{ textAlign: 'center', padding: 40, color: T.ink3 }}>加载配置中…</div>}
          {!loading && notInstallable && (
            <div style={{ padding: 20, textAlign: 'center', color: '#b45309', fontSize: 12.5 }}>{error}</div>
          )}
          {!loading && !notInstallable && fields.length === 0 && !error && (
            <div style={{ textAlign: 'center', padding: 40, color: T.ink3 }}>此应用无需配置参数</div>
          )}
          {!loading && fields.length > 0 && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
              {fields.map((f) => {
                const label = fieldLabel(f);
                return (
                  <div key={f.key}>
                    <label style={{ fontSize: 12, color: T.ink2, fontWeight: 600, marginBottom: 5, display: 'block' }}
                           htmlFor={`fld-${f.key}`}>
                      {label}{f.required && <span style={{ color: '#ef4444', marginLeft: 2 }}>*</span>}
                    </label>
                    {f.type === 'select' ? (
                      <select id={`fld-${f.key}`} value={values[f.key] || ''} onChange={(e) => setField(f.key, e.target.value)}
                        style={{ ...inputStyle, cursor: 'pointer' }}>
                        {(f.options || []).map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
                      </select>
                    ) : f.type === 'password' ? (
                      <div style={{ position: 'relative' }}>
                        <input id={`fld-${f.key}`} type={showPwd[f.key] ? 'text' : 'password'} value={values[f.key] || ''}
                          onChange={(e) => setField(f.key, e.target.value)} placeholder={label} autoComplete="new-password"
                          style={{ ...inputStyle, paddingRight: 36 }}/>
                        <button type="button" onClick={() => setShowPwd((p) => ({ ...p, [f.key]: !p[f.key] }))}
                          aria-label={showPwd[f.key] ? '隐藏' : '显示'}
                          style={{ position: 'absolute', right: 6, top: 7, background: 'none', border: 'none', cursor: 'pointer', color: T.ink4, padding: 2 }}>
                          <Icon name="eye" size={14} stroke={1.8}/>
                        </button>
                      </div>
                    ) : f.type === 'boolean' ? (
                      <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer' }}>
                        <input id={`fld-${f.key}`} type="checkbox" checked={values[f.key] === true || values[f.key] === 'true'}
                          onChange={(e) => setField(f.key, e.target.checked)}/>
                        <span style={{ fontSize: 12.5, color: T.ink2 }}>{label}</span>
                      </label>
                    ) : (
                      <input id={`fld-${f.key}`} type={f.type === 'number' ? 'number' : 'text'} value={values[f.key] ?? ''}
                        onChange={(e) => setField(f.key, e.target.value)} placeholder={label} style={inputStyle}/>
                    )}
                  </div>
                );
              })}
            </div>
          )}
          {error && !notInstallable && (
            <div style={{ fontSize: 12, color: '#ef4444', marginTop: 10, padding: '8px 10px', background: '#fef2f2', borderRadius: 6 }}>{error}</div>
          )}
          {riskFindings.length > 0 && (
            <div style={{ marginTop: 10, padding: '10px 12px', borderRadius: 7, background: '#fffbeb', border: '1px solid #fde68a' }}>
              <div style={{ fontSize: 12, fontWeight: 700, color: '#92400e', marginBottom: 6 }}>运行权限风险</div>
              {riskFindings.map((f, i) => (
                <div key={`${f.field || 'risk'}-${i}`} style={{ fontSize: 11.5, color: f.level === 'blocked' ? '#b91c1c' : '#92400e', marginTop: 3 }}>
                  {f.level === 'blocked' ? '已阻断' : f.level === 'confirmation' ? '需确认' : '警告'}：{f.message}
                </div>
              ))}
              {hasConfirmableRisk && !hasBlockedRisk && (
                <label style={{ display: 'flex', alignItems: 'flex-start', gap: 7, marginTop: 9, cursor: 'pointer', fontSize: 11.5, color: '#78350f' }}>
                  <input type="checkbox" checked={confirmRisky} onChange={(e) => setConfirmRisky(e.target.checked)}/>
                  <span>我了解该应用将获得列出的宿主权限，并确认继续部署</span>
                </label>
              )}
            </div>
          )}
        </div>

        <div style={{ padding: '12px 20px', borderTop: `1px solid ${T.border}`, display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
          <button onClick={onClose} className="edge-press edge-btn-secondary" style={{ ...btnSecondary, height: 34, padding: '0 16px' }}>取消</button>
          <button onClick={handleSubmit} disabled={submitting || loading || notInstallable || hasBlockedRisk || (hasConfirmableRisk && !confirmRisky)}
            className="edge-press edge-btn-primary"
            style={{ ...btnPrimary, height: 34, padding: '0 20px', opacity: (submitting || loading || notInstallable || hasBlockedRisk || (hasConfirmableRisk && !confirmRisky)) ? 0.6 : 1 }}>
            <Icon name="download" size={13} stroke={2}/>{submitting ? '部署中...' : confirmRisky ? '确认风险并部署' : '确认部署'}
          </button>
        </div>
      </div>
    </div>
  );
}

// DeployStatus：轮询安装 Task（后端 store/catalog install 返回 202+Task）。
function DeployStatus({ taskId }) {
  const { task } = useTask(taskId, 1500);
  const phase = !task || !task.status ? 'Pending'
    : task.status === 'succeeded' ? 'Running'
    : task.status === 'failed' ? 'Failed' : 'Pending';
  const label = { Pending: '部署中', Running: '已部署', Failed: '部署失败' }[phase];
  const message = task?.message || '';
  const toneMap = { Pending: '#f59e0b', Running: '#10b981', Failed: '#ef4444' };
  const color = toneMap[phase] || T.ink3;
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '8px 12px', borderRadius: 8, background: T.surfaceAlt, border: `1px solid ${T.borderSoft}` }}>
      <StatusDot tone={phase === 'Running' ? 'green' : phase === 'Failed' ? 'red' : 'amber'} size={8}/>
      <span style={{ fontSize: 12.5, fontWeight: 600, color }}>{label}</span>
      {message && <span style={{ fontSize: 11, color: T.ink3 }}>— {message}</span>}
    </div>
  );
}

// ─── 详情页 ──────────────────────────────────────────────────────
function AppStoreDetail({ app, onBack, onOpenApp }) {
  const [showDeploy, setShowDeploy] = useState(false);
  const [deployResult, setDeployResult] = useState(null);

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
        <div style={{ background: T.surface, border: `1px solid ${T.border}`, borderRadius: 12, padding: 24, marginBottom: 16,
          display: 'flex', alignItems: 'flex-start', gap: 18 }}>
          <div style={{ width: 64, height: 64, borderRadius: 14, background: app.iconUrl ? T.surfaceAlt : app.bg, color: 'white',
            display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0, overflow: 'hidden' }}>
            {app.iconUrl ? <img src={app.iconUrl} alt="" style={{ width: 64, height: 64, objectFit: 'cover' }}/>
              : <Icon name={app.icon} size={30} stroke={1.6}/>}
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
              <div style={{ ...T.type.title, fontWeight: 700, color: T.ink }}>{app.name}</div>
              <Chip tone={app.runtime === 'compose' ? 'blue' : 'gray'}>{app.runtime === 'compose' ? 'Docker Compose' : 'Kubernetes'}</Chip>
              <Chip tone="gray"><span className="mono">{app.ver}</span></Chip>
              {app.cat && <Chip tone="blue">{app.cat}</Chip>}
              <Chip tone={app.origin === 'catalog' ? 'violet' : 'blue'}>
                {app.origin === 'catalog' ? (app.sourceName || '第三方 Catalog') : '平台商店'}
              </Chip>
              {(app.installed || deployResult) && <Chip tone="green"><StatusDot tone="green" size={6}/>{deployResult ? '已部署' : '已安装'}</Chip>}
            </div>
            <div style={{ fontSize: 12, color: T.ink3, marginTop: 6, display: 'flex', gap: 10, flexWrap: 'wrap' }}>
              {app.provider && <><span>提供方 <span style={{ color: T.ink2, fontWeight: 600 }}>{app.provider}</span></span><span style={{ color: '#cbd5e1' }}>·</span></>}
              {app.date && <span>更新于 <span className="mono">{app.date}</span></span>}
            </div>
            <div style={{ fontSize: 13, color: T.ink2, marginTop: 12, lineHeight: 1.7 }}>{app.desc}</div>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8, flexShrink: 0, alignItems: 'flex-end' }}>
            {app.installable ? (
              <button onClick={() => setShowDeploy(true)} className="edge-press edge-btn-primary" style={{ ...btnPrimary, height: 36, padding: '0 18px' }}>
                <Icon name="download" size={13} stroke={2}/>{app.upgradable ? `升级到 ${app.ver}` : app.installed ? '重新部署' : '部署'}
              </button>
            ) : (
              <div title={app.notInstallableReason || ''} style={{ fontSize: 11.5, color: T.ink3, padding: '8px 12px', borderRadius: 8, background: T.surfaceAlt, border: `1px solid ${T.borderSoft}`, maxWidth: 180, textAlign: 'center' }}>
                {app.notInstallableReason || '仅 Kubernetes 环境支持'}
              </div>
            )}
            {app.installed && app.installable && (
              <button onClick={() => onOpenApp && onOpenApp({ id: app.devboxId || app.id })} className="edge-press edge-btn-secondary" style={{ ...btnSecondary, height: 30, padding: '0 14px' }}>
                <Icon name="play" size={12} stroke={2}/>打开应用
              </button>
            )}
          </div>
        </div>

        {deployResult && (
          <Card title="部署状态" padding={16} style={{ marginBottom: 12 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
              <DeployStatus taskId={deployResult.taskId}/>
              <span style={{ fontSize: 12, color: T.ink3 }} className="mono">{deployResult.appId || deployResult.name}</span>
            </div>
          </Card>
        )}

        <Card title="README · 简介" padding={20}>
          <div style={{ fontSize: 13, color: T.ink2, lineHeight: 1.75 }}>
            <p style={{ margin: '0 0 10px' }}><strong style={{ color: T.ink }}>{app.name}</strong> — {app.desc}</p>
          </div>
        </Card>
      </div>

      {showDeploy && <DeployDialog app={app} onClose={() => setShowDeploy(false)} onSuccess={(result) => { setShowDeploy(false); setDeployResult(result); }}/>}
    </div>
  );
}

// ─── 列表卡片 ────────────────────────────────────────────────────
function StoreCard({ app, onOpen, onOpenApp }) {
  return (
    <div onClick={onOpen} className="edge-row-hover" role="button" tabIndex={0}
      onKeyDown={(e) => { if (e.key === 'Enter') onOpen(); }}
      style={{
        background: T.surface, border: `1px solid ${T.border}`, borderRadius: 10, padding: 14, cursor: 'pointer',
        transition: 'all 0.15s ease', display: 'flex', flexDirection: 'column', gap: 10,
        '--edge-row-hover-bg': T.surface, '--edge-row-hover-border-color': '#bfdbfe',
        '--edge-row-hover-box-shadow': '0 6px 14px -6px rgba(15,23,42,0.12)',
      }}>
      <div style={{ display: 'flex', alignItems: 'flex-start', gap: 10 }}>
        <div style={{ width: 40, height: 40, borderRadius: 10, background: app.iconUrl ? T.surfaceAlt : app.bg, color: 'white',
          display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0, overflow: 'hidden',
          boxShadow: app.iconUrl ? '0 1px 3px rgba(0,0,0,0.08)' : 'inset 0 1px 0 rgba(255,255,255,0.3)' }}>
          {app.iconUrl ? <img src={app.iconUrl} alt="" style={{ width: 40, height: 40, objectFit: 'cover' }}/>
            : <Icon name={app.icon} size={20} stroke={1.7}/>}
        </div>
        <div style={{ minWidth: 0, flex: 1 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 5 }}>
            <div style={{ fontSize: 14, fontWeight: 600, color: T.ink, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{app.name}</div>
            {app.installed && !app.upgradable && (
              <span style={{ fontSize: 9.5, padding: '1px 5px', borderRadius: 3, background: '#ecfdf5', color: '#047857', border: '1px solid #a7f3d0', fontWeight: 600, flexShrink: 0 }}>已安装</span>
            )}
            {app.upgradable && (
              <span title={`已装 ${app.installedVersion} → ${app.ver}`} style={{ fontSize: 9.5, padding: '1px 5px', borderRadius: 3, background: '#fffbeb', color: '#b45309', border: '1px solid #fde68a', fontWeight: 600, flexShrink: 0 }}>可升级</span>
            )}
          </div>
          <div style={{ fontSize: 11, color: T.ink3, marginTop: 3, display: 'flex', gap: 6, alignItems: 'center' }}>
            <span className="mono">{app.ver}</span>
            <span style={{ color: '#cbd5e1' }}>·</span>
            <span>{app.origin === 'catalog' ? (app.sourceName || 'Catalog') : '平台'}</span>
          </div>
        </div>
      </div>

      <div style={{ fontSize: 12, color: T.ink2, lineHeight: 1.55, display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical', overflow: 'hidden', minHeight: 38 }}>{app.desc}</div>

      <div style={{ display: 'flex', alignItems: 'center', gap: 6, paddingTop: 10, borderTop: `1px dashed ${T.border}` }}>
        {app.cat && <span style={{ fontSize: 10.5, padding: '2px 6px', borderRadius: 3, background: T.surfaceAlt, color: T.ink3, border: `1px solid ${T.borderSoft}` }}>{app.cat}</span>}
        <span style={{ fontSize: 9.5, padding: '1px 5px', borderRadius: 3, fontWeight: 600, flexShrink: 0,
          background: app.runtime === 'compose' ? '#eff6ff' : T.surfaceAlt,
          color: app.runtime === 'compose' ? '#1d4ed8' : T.ink3,
          border: `1px solid ${app.runtime === 'compose' ? '#bfdbfe' : T.borderSoft}` }}>
          {app.runtime === 'compose' ? 'Compose' : 'K8s'}
        </span>
        <div style={{ flex: 1 }}/>
        {!app.installable
          ? <span title={app.notInstallableReason || ''} style={{ fontSize: 10.5, color: '#b45309', fontWeight: 600, flexShrink: 0 }}>{app.runtime === 'compose' ? '暂不可安装' : '仅 Kubernetes'}</span>
          : app.installed
            ? <button onClick={(e) => { e.stopPropagation(); onOpenApp && onOpenApp({ id: app.devboxId || app.id }); }} className="edge-press edge-btn-secondary" style={{ ...btnSecondary, height: 26, padding: '0 10px', fontSize: 11.5 }}>
                <Icon name="play" size={11} stroke={2}/>打开
              </button>
            : <button onClick={(e) => { e.stopPropagation(); onOpen(); }} className="edge-press edge-btn-primary" style={{ ...btnPrimary, height: 26, padding: '0 10px', fontSize: 11.5 }}>
                <Icon name="download" size={11} stroke={2}/>部署
              </button>}
      </div>
    </div>
  );
}

// ─── catalog 来源状态面板 ────────────────────────────────────────
function CatalogSourceDialog({ source, onClose, onSaved }) {
  const editing = !!source;
  const [form, setForm] = useState({
    id: source?.id || '', name: source?.name || '', kind: source?.kind || 'auto',
    url: source?.url || 'https://github.com/1Panel-dev/appstore', ref: source?.ref || '', token: '',
  });
  const [busy, setBusy] = useState('');
  const [probe, setProbe] = useState(null);
  const [error, setError] = useState('');
  const set = (key, value) => setForm((v) => ({ ...v, [key]: value }));
  useEffect(() => {
    const closeOnEscape = (event) => { if (event.key === 'Escape') onClose(); };
    window.addEventListener('keydown', closeOnEscape);
    return () => window.removeEventListener('keydown', closeOnEscape);
  }, [onClose]);

  async function runTest() {
    setBusy('test'); setError(''); setProbe(null);
    try { setProbe(await testCatalogSource(form)); }
    catch (e) { setError(e.message); }
    finally { setBusy(''); }
  }
  async function save() {
    setBusy('save'); setError('');
    try {
      const result = editing ? await updateCatalogSource(source.id, form) : await createCatalogSource(form);
      onSaved(result);
    } catch (e) { setError(e.message); }
    finally { setBusy(''); }
  }

  const inputStyle = { width: '100%', boxSizing: 'border-box', height: 36, padding: '0 10px', borderRadius: 7, border: `1px solid ${T.border}`, background: T.surface, color: T.ink, fontSize: 12.5, outline: 'none' };
  return (
    <div role="dialog" aria-modal="true" aria-label={editing ? '编辑应用市场' : '添加应用市场'}
      style={{ position: 'fixed', inset: 0, zIndex: 1100, display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 20, background: 'rgba(15,23,42,0.42)', backdropFilter: 'blur(3px)' }}
      onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div style={{ width: 520, maxWidth: '100%', borderRadius: 13, background: T.surface, boxShadow: '0 24px 70px rgba(15,23,42,0.24), 0 2px 10px rgba(15,23,42,0.08)', overflow: 'hidden' }}>
        <div style={{ padding: '18px 20px 14px', borderBottom: `1px solid ${T.borderSoft}`, display: 'flex', alignItems: 'center', gap: 10 }}>
          <div style={{ width: 34, height: 34, borderRadius: 9, display: 'flex', alignItems: 'center', justifyContent: 'center', background: T.blueSoft, color: T.blueDeep }}><Icon name="store" size={17}/></div>
          <div><div style={{ fontSize: 15, fontWeight: 700, color: T.ink }}>{editing ? '编辑应用市场' : '添加 1Panel 应用市场'}</div><div style={{ fontSize: 11.5, color: T.ink4, marginTop: 2 }}>直接填写官方或兼容的开源 Git 仓库地址</div></div>
          <div style={{ flex: 1 }}/><button onClick={onClose} aria-label="关闭" className="edge-press" style={{ width: 40, height: 40, border: 0, background: 'transparent', color: T.ink3, cursor: 'pointer' }}><Icon name="x" size={16}/></button>
        </div>
        <div style={{ padding: 20, display: 'grid', gap: 13 }}>
          <label style={{ display: 'grid', gap: 6, fontSize: 11.5, color: T.ink2 }}>名称<input autoFocus value={form.name} onChange={(e) => set('name', e.target.value)} placeholder="例如：1Panel 官方商店" style={inputStyle}/></label>
          {!editing && <label style={{ display: 'grid', gap: 6, fontSize: 11.5, color: T.ink2 }}>来源 ID（可留空自动生成）<input value={form.id} onChange={(e) => set('id', e.target.value)} placeholder="onepanel-official" style={inputStyle}/></label>}
          <label style={{ display: 'grid', gap: 6, fontSize: 11.5, color: T.ink2 }}>Git 仓库地址<input value={form.url} onChange={(e) => set('url', e.target.value)} spellCheck={false} style={{ ...inputStyle, fontFamily: T.type?.mono }}/></label>
          <div style={{ display: 'grid', gridTemplateColumns: '140px 1fr', gap: 10 }}>
            <label style={{ display: 'grid', gap: 6, fontSize: 11.5, color: T.ink2 }}>格式<select value={form.kind} onChange={(e) => set('kind', e.target.value)} style={inputStyle}><option value="auto">自动识别</option><option value="1panel">1Panel</option></select></label>
            <label style={{ display: 'grid', gap: 6, fontSize: 11.5, color: T.ink2 }}>分支 / Tag（留空使用远端默认）<input value={form.ref} onChange={(e) => set('ref', e.target.value)} placeholder="官方源默认 dev" style={inputStyle}/></label>
          </div>
          <label style={{ display: 'grid', gap: 6, fontSize: 11.5, color: T.ink2 }}>只读 Token（可选，仅写不回显）<input type="password" value={form.token} onChange={(e) => set('token', e.target.value)} placeholder={source?.tokenConfigured ? '已配置；留空保持不变' : '公开仓库无需填写'} autoComplete="new-password" style={inputStyle}/></label>
          {probe && <div style={{ padding: '9px 11px', borderRadius: 7, background: '#ecfdf5', color: '#047857', fontSize: 11.5 }}><b>连接成功</b> · 已识别 {probe.kind}，发现 {probe.appCount} 个应用</div>}
          {error && <div style={{ padding: '9px 11px', borderRadius: 7, background: '#fff7ed', color: '#c2410c', fontSize: 11.5 }}>{error}</div>}
        </div>
        <div style={{ padding: '13px 20px 17px', display: 'flex', justifyContent: 'flex-end', gap: 8, borderTop: `1px solid ${T.borderSoft}` }}>
          <button onClick={runTest} disabled={!!busy || !form.url} className="edge-press edge-btn-secondary" style={{ ...btnSecondary, minHeight: 40 }}>{busy === 'test' ? '检测中…' : '测试连接'}</button>
          <button onClick={save} disabled={!!busy || !form.url} className="edge-press edge-btn-primary" style={{ ...btnPrimary, minHeight: 40 }}>{busy === 'save' ? '保存中…' : '保存来源'}</button>
        </div>
      </div>
    </div>
  );
}

function CatalogSourcesPanel({ configs, onRefresh, onConfigsChanged, refreshing }) {
  const [dialog, setDialog] = useState(null);
  const toast = useToast();
  const stateMap = { ok: { tone: 'green', label: '正常' }, error: { tone: 'red', label: '异常' }, syncing: { tone: 'amber', label: '同步中' }, unconfigured: { tone: 'gray', label: '未配置' } };
  return (
    <div style={{ padding: 10, borderRadius: 8, background: T.surface, border: `1px solid ${T.borderSoft}` }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 8 }}>
        <Icon name="layers" size={13} stroke={1.8} style={{ color: T.violet }}/>
        <span style={{ fontSize: 12, fontWeight: 700, color: T.ink }}>Catalog 数据源</span>
        <div style={{ flex: 1 }}/>
        <button onClick={() => setDialog({})} title="添加来源" aria-label="添加应用市场来源" className="edge-press edge-btn-secondary" style={{ ...btnSecondary, minWidth: 40, height: 32, padding: '0 8px', fontSize: 11 }}><Icon name="plus" size={12}/></button>
        <button onClick={onRefresh} disabled={refreshing} className="edge-press edge-btn-secondary" style={{ ...btnSecondary, height: 32, padding: '0 8px', fontSize: 11 }}><Icon name="refresh" size={11} stroke={2}/>{refreshing ? '刷新中' : '刷新'}</button>
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        {(configs || []).map((s) => {
          const st = stateMap[s.status?.state] || stateMap.unconfigured;
          return (
            <div key={s.id} style={{ display: 'flex', alignItems: 'center', gap: 7, fontSize: 11.5 }}>
              <StatusDot tone={st.tone} size={7} pulse={s.status?.state === 'error'}/>
              <span title={s.url} style={{ color: T.ink2, fontWeight: 600, flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{s.name || s.id}</span>
              <span style={{ fontSize: 10, color: T.ink4 }}>{s.status?.appCount ?? 0} 个应用</span>
              <span style={{ fontSize: 10, color: st.tone === 'red' ? '#dc2626' : T.ink4 }}>{st.label}</span>
              {!s.readOnly && <button title="刷新" aria-label={`刷新来源 ${s.name || s.id}`} disabled={!s.enabled} onClick={async () => { try { await refreshCatalogSource(s.id); onConfigsChanged(); toast.ok('来源已刷新'); } catch (e) { toast.err(e.message); } }} className="edge-press" style={{ width: 40, height: 40, border: 0, background: 'transparent', color: T.ink3, cursor: s.enabled ? 'pointer' : 'default' }}><Icon name="refresh" size={12}/></button>}
              {!s.readOnly && <button title="编辑" aria-label={`编辑来源 ${s.name || s.id}`} onClick={() => setDialog(s)} className="edge-press" style={{ width: 40, height: 40, border: 0, background: 'transparent', color: T.ink3, cursor: 'pointer' }}><Icon name="edit" size={12}/></button>}
              {!s.readOnly && <button title={s.enabled ? '停用' : '启用'} aria-label={`${s.enabled ? '停用' : '启用'}来源 ${s.name || s.id}`} onClick={async () => { try { await updateCatalogSource(s.id, { enabled: !s.enabled }); onConfigsChanged(); } catch (e) { toast.err(e.message); } }} className="edge-press" style={{ width: 40, height: 40, border: 0, background: 'transparent', color: s.enabled ? '#16a34a' : T.ink4, cursor: 'pointer' }}><Icon name={s.enabled ? 'stop' : 'play'} size={12}/></button>}
              {!s.readOnly && <button title="删除" aria-label={`删除来源 ${s.name || s.id}`} onClick={async () => { if (!window.confirm(`删除来源「${s.name || s.id}」？已安装应用不会被删除。`)) return; try { await deleteCatalogSource(s.id); onConfigsChanged(); } catch (e) { toast.err(e.message); } }} className="edge-press" style={{ width: 40, height: 40, border: 0, background: 'transparent', color: '#dc2626', cursor: 'pointer' }}><Icon name="trash" size={12}/></button>}
            </div>
          );
        })}
        {(!configs || configs.length === 0) && <div style={{ color: T.ink4, fontSize: 11, lineHeight: 1.5 }}>尚未添加市场来源。可直接添加 1Panel 官方或兼容仓库。</div>}
      </div>
      {dialog && <CatalogSourceDialog source={dialog.id ? dialog : null} onClose={() => setDialog(null)} onSaved={() => { setDialog(null); onConfigsChanged(); toast.ok('应用市场来源已保存'); }}/>}
    </div>
  );
}

// ─── AppStore 主组件 ─────────────────────────────────────────────
export default function AppStore({ onOpenApp, authed, onRequireAuth }) {
  const [cat, setCat] = useState('all');
  const [tab, setTab] = useState('all');
  const [sourceFilter, setSourceFilter] = useState('all'); // all | platform | <catalogId>
  const [q, setQ] = useState('');
  const [detail, setDetail] = useState(null);
  const [refreshing, setRefreshing] = useState(false);
  const toast = useToast();

  const { data: deployedApps } = useApps(10000);
  const { data: platformRaw, error: storeErr, refresh: refreshStore } = useStoreApps();
  const { data: catalogRaw, error: catalogErr, refresh: refreshCatalogApps } = useCatalogApps();
  const { data: catalogSources, refresh: refreshSources } = useCatalogSources();
  const { data: catalogSourceConfigs, refresh: refreshSourceConfigs } = useCatalogSourceConfigs();

  const platformApps = Array.isArray(platformRaw) ? platformRaw : null;
  const catalogApps = Array.isArray(catalogRaw) ? catalogRaw : null;
  const sources = Array.isArray(catalogSources) ? catalogSources : [];

  // 已部署 compose 应用的精确来源映射（source.kind + storeId/catalogId）。
  const deployedBySource = useMemo(() => {
    const m = {};
    (deployedApps || []).forEach((d) => {
      if (!d.source || (d.runtime || 'kubernetes') !== 'compose') return;
      if (d.source.kind === 'store') m[`store:${d.source.storeId}`] = { version: d.source.version || d.version, devboxId: d.id };
      if (d.source.kind === 'catalog') m[`catalog:${d.source.catalogId}:${d.source.storeId}`] = { version: d.source.version || d.version, devboxId: d.id };
    });
    return m;
  }, [deployedApps]);

  // 聚合平台 + catalog，标注 origin/sourceId/sourceName/installed/upgradable。
  const storeApps = useMemo(() => {
    const out = [];
    (platformApps || []).forEach((a) => {
      const hit = deployedBySource[`store:${a.id}`];
      const installedVersion = hit?.version || null;
      const upgradable = installedVersion != null && a.version != null && compareVersions(installedVersion, a.version) < 0;
      const { bg, icon } = guessAppStyle(a.id, a.name, a.category);
      out.push({
        id: a.id, name: a.name, cat: CAT_LABEL[a.category] || a.category, ver: a.version,
        desc: a.description || '', iconUrl: a.icon, icon, bg, provider: a.provider,
        runtime: a.runtime || 'kubernetes', installable: a.installable === true,
        notInstallableReason: a.notInstallableReason || '',
        origin: 'platform', sourceId: '', sourceName: '平台商店',
        installed: installedVersion != null, installedVersion, upgradable, devboxId: hit?.devboxId,
        pinned: a.pinned, date: '',
      });
    });
    (catalogApps || []).forEach((a) => {
      const hit = deployedBySource[`catalog:${a.catalogId}:${a.id}`];
      const installedVersion = hit?.version || null;
      const upgradable = installedVersion != null && a.version != null && compareVersions(installedVersion, a.version) < 0;
      const { bg, icon } = guessAppStyle(a.id, a.name, a.category);
      out.push({
        id: a.id, name: a.name, cat: CAT_LABEL[a.category] || a.category, ver: a.version,
        desc: a.description || '', iconUrl: a.icon, icon, bg, provider: a.provider,
        runtime: a.runtime || 'compose', installable: a.installable === true,
        notInstallableReason: a.notInstallableReason || '',
        origin: 'catalog', sourceId: a.catalogId, sourceName: a.catalogName,
        installed: installedVersion != null, installedVersion, upgradable, devboxId: hit?.devboxId,
        pinned: a.pinned, date: '',
      });
    });
    return out;
  }, [platformApps, catalogApps, deployedBySource]);

  // 分类 sidebar
  const categories = storeApps.length > 0 ? (() => {
    const catMap = {};
    storeApps.forEach((a) => { if (a.cat) catMap[a.cat] = (catMap[a.cat] || 0) + 1; });
    const cats = [{ id: 'all', name: '全部', icon: 'apps', count: storeApps.length }];
    Object.entries(catMap).forEach(([k, v]) => cats.push({ id: k, name: k, icon: 'apps', count: v }));
    return cats;
  })() : [{ id: 'all', name: '全部', icon: 'apps', count: 0 }];

  const allCount = storeApps.length;
  const installedCount = storeApps.filter((a) => a.installed).length;
  const upgradableCount = storeApps.filter((a) => a.upgradable).length;

  const list = storeApps.filter((a) => {
    if (tab === 'installed' && !a.installed) return false;
    if (tab === 'upgradable' && !a.upgradable) return false;
    if (sourceFilter === 'platform' && a.origin !== 'platform') return false;
    if (sourceFilter !== 'all' && sourceFilter !== 'platform' && a.sourceId !== sourceFilter) return false;
    if (cat !== 'all' && a.cat !== cat) return false;
    if (q && !a.name.toLowerCase().includes(q.toLowerCase()) && !(a.desc || '').includes(q)) return false;
    return true;
  });

  // 状态：未配置任何来源 / 平台错误 / catalog 单源错误但缓存可用。
  const noSourcesConfigured = !platformApps && !catalogApps && sources.length === 0 && !storeErr && !catalogErr;
  // useStoreApps 单次加载中（platformApps===null 表示还没拿到结果或失败）。
  const loading = platformRaw === null && catalogRaw === null;

  async function onRefresh() {
    if (!authed) { onRequireAuth?.(); return; }
    setRefreshing(true);
    try {
      await refreshCatalogs();
      refreshSources();
      refreshSourceConfigs();
      refreshCatalogApps();
      refreshStore();
      toast.ok('已刷新应用清单');
    } catch (e) {
      toast.err(`刷新失败：${e.message}`);
    } finally {
      setRefreshing(false);
    }
  }

  if (detail) {
    return <AppStoreDetail app={detail} onBack={() => setDetail(null)} onOpenApp={onOpenApp}/>;
  }

  // 来源筛选选项：全部 / 平台商店 / 各 catalog source。
  const sourceOptions = [{ id: 'all', label: '全部来源' }];
  if (platformApps && platformApps.length > 0) sourceOptions.push({ id: 'platform', label: '平台商店' });
  sources.forEach((s) => sourceOptions.push({ id: s.sourceId, label: s.sourceName || s.sourceId }));

  return (
    <div style={{ flex: 1, display: 'flex', background: T.surfaceAlt, overflow: 'hidden' }}>
      {/* Sidebar */}
      <div style={{ width: 208, flexShrink: 0, padding: '20px 12px', background: T.surface, borderRight: `1px solid ${T.border}`, overflow: 'auto' }}>
        <div style={{ fontSize: 10.5, fontWeight: 600, color: T.ink3, letterSpacing: '0.06em', textTransform: 'uppercase', padding: '0 8px 6px' }}>发现</div>
        <div style={{ fontSize: 10.5, color: T.ink4, padding: '0 8px 8px' }}>应用分类</div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 1 }}>
          {categories.map((c) => {
            const on = cat === c.id;
            return (
              <div key={c.id} onClick={() => setCat(c.id)} role="button" tabIndex={0}
                onKeyDown={(e) => { if (e.key === 'Enter') setCat(c.id); }}
                style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '8px 10px', borderRadius: 7, cursor: 'pointer',
                  background: on ? T.blueSoft : 'transparent', color: on ? T.blueDeep : T.ink2, fontSize: 12.5, fontWeight: on ? 600 : 500 }}>
                <Icon name={CAT_ICON[c.id] || c.icon || 'apps'} size={14} stroke={1.8}/>
                <span style={{ flex: 1 }}>{c.name}</span>
                <span className="mono tnum" style={{ fontSize: 10.5, color: on ? T.blueDeep : T.ink4, background: on ? '#dbeafe' : T.surfaceAlt, padding: '0 6px', borderRadius: 999, lineHeight: '17px' }}>{c.count}</span>
              </div>
            );
          })}
        </div>

        <div style={{ height: 1, background: T.borderSoft, margin: '16px 8px' }}/>
        <CatalogSourcesPanel configs={Array.isArray(catalogSourceConfigs) && catalogSourceConfigs.length > 0 ? catalogSourceConfigs : sources.map((s) => ({ id: s.sourceId, name: s.sourceName, kind: s.kind, url: '', enabled: true, managedBy: 'config', readOnly: true, status: s.status }))} onRefresh={onRefresh} onConfigsChanged={() => { refreshSourceConfigs(); refreshSources(); refreshCatalogApps(); }} refreshing={refreshing}/>

        {/* 单源错误但缓存可用提示 */}
        {sources.some((s) => s.status?.state === 'error') && (
          <div style={{ marginTop: 10, padding: '8px 10px', borderRadius: 7, background: '#fffbeb', border: '1px solid #fde68a', fontSize: 11, color: '#92400e', lineHeight: 1.5 }}>
            部分 catalog 数据源同步异常，已使用上次缓存的应用清单。
          </div>
        )}
      </div>

      {/* Main */}
      <div style={{ flex: 1, overflow: 'auto', padding: '20px 24px' }}>
        <TabBar
          tabs={[{ id: 'all', label: '全部', count: allCount }, { id: 'installed', label: '已安装', count: installedCount }, { id: 'upgradable', label: '可升级', count: upgradableCount }]}
          active={tab} onChange={setTab}
          style={{ gap: 4, marginBottom: 16, borderBottom: `1px solid ${T.borderSoft}` }}
          itemStyle={{ padding: '10px 16px 12px', fontSize: 13, marginBottom: -1 }}
          renderLabel={(t2, on) => (<>{t2.label}<span style={{ fontSize: 11, color: T.ink4, fontWeight: 500, padding: '1px 6px', borderRadius: 3, background: on ? T.blueSoft : T.surfaceAlt }} className="mono tnum">{t2.count}</span></>)}
        />

        {/* Toolbar: 来源筛选 + 搜索 + 同步 */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 12, flexWrap: 'wrap' }}>
          <div style={{ fontSize: 14, fontWeight: 700, color: T.ink }}>{categories.find((c) => c.id === cat)?.name}</div>
          <div style={{ fontSize: 11.5, padding: '2px 8px', borderRadius: 999, background: T.surface, border: `1px solid ${T.border}`, color: T.ink3 }}>共 {list.length} 个应用</div>
          <div style={{ flex: 1 }}/>
          {sourceOptions.length > 1 && (
            <div style={{ display: 'flex', gap: 4 }}>
              {sourceOptions.map((o) => (
                <button key={o.id} onClick={() => setSourceFilter(o.id)} title={o.label}
                  style={sourceFilter === o.id ? srcChipActive : srcChip}>
                  {o.label}
                </button>
              ))}
            </div>
          )}
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, height: 30, padding: '0 10px', borderRadius: 7, background: T.surface, border: `1px solid ${T.border}`, width: 200 }}>
            <Icon name="search" size={13} stroke={1.8} style={{ color: T.ink4 }}/>
            <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="按名称搜索" aria-label="搜索"
              style={{ flex: 1, border: 'none', outline: 'none', fontSize: 12.5, background: 'transparent' }}/>
          </div>
          <button onClick={onRefresh} disabled={refreshing} className="edge-press edge-btn-secondary" style={{ ...btnSecondary, height: 30 }}>
            <Icon name="refresh" size={13} stroke={1.8}/>{refreshing ? '同步中' : '同步'}
          </button>
        </div>

        {/* Featured（pinned）— 仅 all tab + 平台 pinned */}
        {tab === 'all' && sourceFilter !== 'platform' && storeApps.filter((a) => a.pinned).length > 0 && (
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 18 }}>
            {storeApps.filter((a) => a.pinned).slice(0, 4).map((f, i) => (
              <div key={i} style={{ padding: '20px 22px', borderRadius: 12, background: f.bg, color: 'white', position: 'relative', overflow: 'hidden', minHeight: 140, display: 'flex', flexDirection: 'column', justifyContent: 'space-between', boxShadow: '0 4px 14px -4px rgba(15,23,42,0.18)' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                  <div style={{ width: 36, height: 36, borderRadius: 9, background: 'rgba(255,255,255,0.18)', display: 'flex', alignItems: 'center', justifyContent: 'center', backdropFilter: 'blur(8px)' }}>
                    {f.iconUrl ? <img src={f.iconUrl} alt="" style={{ width: 20, height: 20 }}/> : <Icon name={f.icon} size={20} stroke={1.7}/>}
                  </div>
                  <div>
                    <div style={{ ...T.type.heading }}>{f.name}</div>
                    <div style={{ fontSize: 11.5, opacity: 0.85, marginTop: 2 }}>{f.cat || f.origin}</div>
                  </div>
                </div>
                <div style={{ fontSize: 12.5, opacity: 0.95, lineHeight: 1.55, marginBottom: 10 }}>{f.desc}</div>
                <div style={{ position: 'absolute', top: -20, right: -20, width: 140, height: 140, background: 'radial-gradient(circle, rgba(255,255,255,0.18), transparent 70%)', pointerEvents: 'none' }}/>
              </div>
            ))}
          </div>
        )}

        {/* Grid */}
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(290px, 1fr))', gap: 12 }}>
          {list.map((a) => (
            <StoreCard key={(a.origin === 'catalog' ? 'c:' : 'p:') + a.sourceId + ':' + a.id} app={a} onOpen={() => setDetail(a)} onOpenApp={onOpenApp}/>
          ))}
        </div>

        {/* Empty / loading states */}
        {list.length === 0 && (
          <div style={{ textAlign: 'center', padding: '60px 20px', color: T.ink3, fontSize: 13 }}>
            {loading ? (
              <><Icon name="refresh" size={28} stroke={1.5} style={{ color: T.ink4, marginBottom: 8 }}/><div>正在加载应用商店…</div></>
            ) : noSourcesConfigured ? (
              <>
                <Icon name="store" size={28} stroke={1.5} style={{ color: T.ink4, marginBottom: 8 }}/>
                <div>未配置应用市场与 Catalog</div>
                <div style={{ fontSize: 11.5, color: T.ink4, marginTop: 6 }}>本机未配置平台应用商店或第三方 Catalog 数据源</div>
              </>
            ) : storeErr && !catalogApps ? (
              <>
                <Icon name="cloudOff" size={28} stroke={1.5} style={{ color: T.ink4, marginBottom: 8 }}/>
                <div>无法访问平台应用商店</div>
                <div style={{ fontSize: 11.5, color: T.ink4, marginTop: 6 }}>{storeErr.message}</div>
              </>
            ) : (
              <>
                <Icon name={tab === 'upgradable' ? 'check' : tab === 'installed' ? 'apps' : 'search'} size={28} stroke={1.5} style={{ color: T.ink4, marginBottom: 8 }}/>
                <div>{tab === 'upgradable' ? '所有已安装应用都是最新版本' : tab === 'installed' ? '本节点尚未安装任何应用' : '没有匹配的应用'}</div>
              </>
            )}
          </div>
        )}

        <div style={{ textAlign: 'center', margin: '24px 0 8px', fontSize: 11.5, color: T.ink4, display: 'flex', alignItems: 'center', gap: 12, justifyContent: 'center' }}>
          <span style={{ height: 1, width: 60, background: T.border }}/>已加载全部<span style={{ height: 1, width: 60, background: T.border }}/>
        </div>
      </div>
    </div>
  );
}

const srcChip = { padding: '4px 10px', fontSize: 11.5, borderRadius: 999, border: `1px solid ${T.border}`, background: T.surface, color: T.ink2, cursor: 'pointer' };
const srcChipActive = { ...srcChip, background: T.blue, color: '#fff', borderColor: T.blue };
