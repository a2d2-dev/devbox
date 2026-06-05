import { useState, useEffect, useRef } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, Chip, Ring, Sparkline, Card, useTicker } from '../components/ui'
import { useDevice, useMetrics, useMetricsHistory, useApps, useAlerts, useNetwork } from '../hooks/useApi'
import { STORE_CATEGORIES, STORE_FEATURED, STORE_APPS } from '../data/mock'
import { btnSecondary, btnPrimary, btnDanger } from '../components/AppWindow'

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

function AppStoreDetail({ app, onBack, authed, onRequireAuth, onOpenApp }) {
  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', background: T.surfaceAlt, overflow: 'auto' }}>
      <div style={{ padding: '12px 24px', borderBottom: `1px solid ${T.border}`, background: T.surface,
        display: 'flex', alignItems: 'center', gap: 10 }}>
        <button onClick={onBack} style={{ ...btnSecondary, height: 28, padding: '0 10px' }}>
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
              {app.installed && <Chip tone="green"><StatusDot tone="green" size={6}/>已安装</Chip>}
            </div>
            <div style={{ fontSize: 12, color: T.ink3, marginTop: 6, display: 'flex', gap: 10 }}>
              <span>开发者 <span style={{ color: T.ink2, fontWeight: 600 }}>{app.dev}</span></span>
              <span style={{ color: '#cbd5e1' }}>·</span>
              <span>评分 <span style={{ color: T.amber, fontWeight: 600 }}>★ {app.rating}</span></span>
              <span style={{ color: '#cbd5e1' }}>·</span>
              <span>下载 <span className="mono" style={{ color: T.ink2, fontWeight: 600 }}>{app.downloads}</span></span>
              <span style={{ color: '#cbd5e1' }}>·</span>
              <span>更新于 <span className="mono">{app.date}</span></span>
            </div>
            <div style={{ fontSize: 13, color: T.ink2, marginTop: 12, lineHeight: 1.7 }}>{app.desc}</div>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8, flexShrink: 0 }}>
            {app.installed ? (
              <>
                <button onClick={() => onOpenApp && onOpenApp({ id: app.id })} style={{ ...btnPrimary, height: 36, padding: '0 18px' }}>
                  <Icon name="play" size={13} stroke={2}/>打开应用
                </button>
                <button style={{ ...btnSecondary, height: 32 }}>
                  <Icon name="refresh" size={12} stroke={1.8}/>检查更新
                </button>
              </>
            ) : (
              <>
                <button onClick={() => !authed && onRequireAuth(`部署 ${app.name}`)}
                  style={{ ...btnPrimary, height: 36, padding: '0 18px' }}>
                  <Icon name="download" size={13} stroke={2}/>{authed ? '一键部署' : '需验证后部署'}
                </button>
                <button style={{ ...btnSecondary, height: 32 }}>
                  <Icon name="terminal" size={12} stroke={1.8}/>导出 YAML
                </button>
              </>
            )}
          </div>
        </div>

        {/* Deploy form preview */}
        <Card title="部署配置 · 边缘节点" padding={16} style={{ marginBottom: 12 }}>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 14 }}>
            <DeployField label="目标节点" value="xpark-e274 (本机)" hint="可在云端 dev 租户空间切换"/>
            <DeployField label="节点组"   value="数字规划部节点组"/>
            <DeployField label="GPU 分配" value="GPU 0 · 共享" hint="当前占用 53%，剩余 ~50 GB 显存"/>
            <DeployField label="服务端口" value="18789" hint="自动分配，避开已占用端口"/>
            <DeployField label="模型提供商" value="Anthropic"/>
            <DeployField label="模型名称" value="claude-sonnet-4-20250514"/>
          </div>
        </Card>

        <Card title="README · 简介" padding={20}>
          <div style={{ fontSize: 13, color: T.ink2, lineHeight: 1.75 }}>
            <p style={{ margin: '0 0 10px' }}>
              <strong style={{ color: T.ink }}>{app.name}</strong> {app.desc.replace(/^.{0,20}?，/, '')}
            </p>
            <p style={{ margin: '0 0 10px' }}>
              本应用在 Sealos 模板基础上经过边缘端裁剪，支持 GPU 共享调度与显存隔离。
              所有数据持久化在 <span className="mono" style={{ background: T.surfaceAlt, padding: '1px 5px', borderRadius: 3 }}>/var/lib/edgex/apps/{app.id}</span>，
              重启不丢失。
            </p>
            <div style={{ fontSize: 12, color: T.ink3, fontWeight: 600,
              letterSpacing: '0.04em', textTransform: 'uppercase', marginTop: 14, marginBottom: 8 }}>
              典型用例
            </div>
            <ul style={{ paddingLeft: 22, margin: 0 }}>
              <li>边缘端私有知识问答 / 客服 / 内部助手</li>
              <li>多终端聊天接入（飞书 / 钉钉 / 企业微信）</li>
              <li>结合 Ollama 本地模型，端到端离线运行</li>
            </ul>
          </div>
        </Card>
      </div>
    </div>
  );
}

function StoreCard({ app, onOpen, onOpenApp }) {
  const [hover, setHover] = useState(false);
  return (
    <div onClick={onOpen}
         onMouseEnter={() => setHover(true)}
         onMouseLeave={() => setHover(false)}
         style={{
      background: T.surface, border: `1px solid ${hover ? '#bfdbfe' : T.border}`,
      borderRadius: 10, padding: 14, cursor: 'pointer',
      boxShadow: hover ? '0 6px 14px -6px rgba(15,23,42,0.12)' : 'none',
      transition: 'all 0.15s ease',
      display: 'flex', flexDirection: 'column', gap: 10,
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
            {app.installed && (
              <span style={{
                fontSize: 9.5, padding: '1px 5px', borderRadius: 3,
                background: '#ecfdf5', color: '#047857', border: '1px solid #a7f3d0',
                fontWeight: 600, flexShrink: 0,
              }}>已安装</span>
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
              style={{ ...btnSecondary, height: 26, padding: '0 10px', fontSize: 11.5 }}>
              <Icon name="play" size={11} stroke={2}/>打开
            </button>
          : <button onClick={(e) => { e.stopPropagation(); onOpen(); }}
              style={{ ...btnPrimary, height: 26, padding: '0 10px', fontSize: 11.5 }}>
              <Icon name="download" size={11} stroke={2}/>部署
            </button>
        }
      </div>
    </div>
  );
}

export default function AppStore({ onOpenApp, authed, onRequireAuth }) {
  const [cat, setCat] = useState('all');
  const [q, setQ] = useState('');
  const [detail, setDetail] = useState(null);
  const [platformApps, setPlatformApps] = useState(null);

  const catLabelMap = { 'ai-inference': 'AI 推理', 'ai-tools': 'AI 工具', 'dev-environment': '开发环境', 'database': '存储' };
  const catIconMap = { 'ai-inference': 'sparkle', 'ai-tools': 'wrench', 'dev-environment': 'code', 'database': 'database' };

  // 从平台 API 获取真实应用商店数据
  useEffect(() => {
    fetch('/api/v1/store/apps').then(r => r.ok ? r.json() : null).then(data => {
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
  })() : STORE_CATEGORIES;

  // 使用平台数据或 fallback 到 mock
  const storeApps = platformApps
    ? platformApps.map(a => {
        const { color, bg } = guessAppStyle(a.id, a.name, a.category);
        return {
          id: a.id, name: a.name, cat: catLabelMap[a.category] || a.category,
          ver: a.version, desc: a.description || '',
          iconUrl: a.icon,
          icon: null, bg, color,
          provider: a.provider, installed: false,
        };
      })
    : STORE_APPS;

  const list = storeApps.filter(a => {
    if (cat !== 'all') {
      const catName = catLabelMap[cat] || cat;
      if (a.cat !== catName) return false;
    }
    if (q && !a.name.toLowerCase().includes(q.toLowerCase()) && !(a.desc||'').includes(q)) return false;
    return true;
  });

  if (detail) {
    return <AppStoreDetail app={detail} onBack={() => setDetail(null)} authed={authed} onRequireAuth={onRequireAuth} onOpenApp={onOpenApp}/>;
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
        {/* Featured */}
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 18 }}>
          {STORE_FEATURED.map((f, i) => (
            <div key={i} style={{
              padding: '20px 22px', borderRadius: 12,
              background: f.bg, color: 'white',
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
                  <Icon name={f.icon} size={20} stroke={1.7}/>
                </div>
                <div>
                  <div style={{ fontSize: 16, fontWeight: 700, letterSpacing: '-0.01em' }}>{f.title}</div>
                  <div style={{ fontSize: 11.5, opacity: 0.85, marginTop: 2 }}>{f.subtitle}</div>
                </div>
              </div>
              <div>
                <div style={{ fontSize: 12.5, opacity: 0.95, lineHeight: 1.55, marginBottom: 10 }}>{f.long}</div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, fontWeight: 600 }}>
                  <Icon name="sparkle" size={13} stroke={2}/>
                  {f.tagline}
                </div>
              </div>
              {/* faint pattern */}
              <div style={{
                position: 'absolute', top: -20, right: -20, width: 140, height: 140,
                background: 'radial-gradient(circle, rgba(255,255,255,0.18), transparent 70%)',
                pointerEvents: 'none',
              }}/>
            </div>
          ))}
        </div>

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
          <button style={btnSecondary}>
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
            <Icon name="search" size={28} stroke={1.5} style={{ color: T.ink4, marginBottom: 8 }}/>
            <div>没有匹配的应用</div>
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
