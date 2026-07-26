// 浏览器应用 —— Chrome 风格的多标签页浏览器，内容区用 iframe 加载经过后端代理的网页。
//
// 后端 /api/v1/browser/proxy?url=<X> 会剥离 X-Frame-Options / CSP 等嵌套限制头并
// 注入 <base>，让相对资源解析回原站。详见 pkg/console/handlers_browser.go。
//
// 已知限制（iframe 固有，非 bug）：
//   - 父页面读不到 iframe 的 document.title，tab 标题只能用 host 占位；
//   - 强反点击劫持的外部大站（Google/GitHub 等 frame-ancestors 'none'）仍可能空白；
//   - 被嵌页面的 cookie/login 域与 console 不同源，需在 iframe 内自行登录。

import { useState, useEffect, useMemo, useCallback } from 'react';
import { T } from '../tokens';
import { Icon } from '../icons';
import { useBookmarks, useHistory, addBookmark, removeBookmark, addHistory, clearHistory, getAuthToken, probeDirectEmbed } from '../hooks/useApi';

const MONO = { fontFamily: '"JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, monospace' };

// ─── helpers ────────────────────────────────────────────────────

function randId() {
  return (crypto.randomUUID && crypto.randomUUID()) || String(Math.random()).slice(2);
}

function titleForUrl(url) {
  if (!url) return '新标签页';
  try { return new URL(url).host || url; } catch { return url; }
}

// 地址栏输入 → 可导航 URL：已带 scheme 原样；localhost/IP/含点号补 http://；其余补 https://。
function normalizeUrl(input) {
  const v = (input || '').trim();
  if (!v) return '';
  if (/^https?:\/\//i.test(v)) return v;
  const looksLikeHost = /^(localhost|\[?[0-9a-fA-F:]+\]?|(\d{1,3}\.){3}\d{1,3})/.test(v) || v.includes('.');
  return (looksLikeHost ? 'http://' : 'https://') + v;
}

// iframe 无法注入 Authorization header，所以通过 query token 鉴权
// （auth.Middleware 支持 r.URL.Query().Get("token")，见 pkg/auth/auth.go）。
function proxySrc(url) {
  const t = getAuthToken();
  return '/api/v1/browser/proxy?url=' + encodeURIComponent(url) + (t ? '&token=' + encodeURIComponent(t) : '');
}

function newTab(url = '') {
  return {
    id: randId(),
    entries: url ? [url] : [],
    index: url ? 0 : -1,
    title: titleForUrl(url),
    loading: !!url,
    mode: 'direct', // 'direct'（iframe 直连原始 URL）| 'proxy'（走后端代理剥离嵌套限制头）
    _nonce: randId(),
  };
}

// ─── atoms ──────────────────────────────────────────────────────

function IconBtn({ name, size = 16, stroke = 1.8, disabled, active, title, onClick, style }) {
  return (
    <button
      title={title}
      onClick={onClick}
      disabled={disabled}
      className="edge-press"
      style={{
        width: 30, height: 30, borderRadius: T.radius.sm,
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        border: 'none', background: active ? T.blueSoft : 'transparent',
        cursor: disabled ? 'default' : 'pointer', flexShrink: 0,
        color: disabled ? T.ink4 : (active ? T.blueDeep : T.ink2),
        transition: `background ${T.duration?.hover || 120}ms, color ${T.duration?.hover || 120}ms`,
        ...style,
      }}
    >
      <Icon name={name} size={size} stroke={stroke} />
    </button>
  );
}

// ─── 主组件 ─────────────────────────────────────────────────────

export default function BrowserFace() {
  const { data: bookmarks, refresh: refreshBookmarks } = useBookmarks();
  const { data: history, refresh: refreshHistory } = useHistory();

  const [tabs, setTabs] = useState(() => [newTab()]);
  const [activeId, setActiveId] = useState(() => tabs[0].id);
  const [address, setAddress] = useState('');
  const [panel, setPanel] = useState(null); // null | 'bookmarks' | 'history'

  const active = useMemo(() => tabs.find(t => t.id === activeId) || tabs[0], [tabs, activeId]);
  const currentUrl = active && active.index >= 0 ? active.entries[active.index] : '';

  // 切换 tab 或导航后，地址栏同步到当前 URL
  useEffect(() => { setAddress(currentUrl); }, [activeId, currentUrl]);

  // ─── tab 状态更新（index 游标，避免后退分叉） ───────────────
  const patchTab = useCallback((id, fn) => {
    setTabs(ts => ts.map(t => (t.id === id ? fn(t) : t)));
  }, []);

  const navigate = useCallback((url) => {
    if (!url) return;
    patchTab(active.id, t => {
      const next = t.entries.slice(0, t.index + 1); // 截断分叉
      next.push(url);
      return { ...t, entries: next, index: next.length - 1, title: titleForUrl(url), loading: true, mode: 'direct', _nonce: randId() };
    });
    addHistory(url, titleForUrl(url)).then(refreshHistory);
    setPanel(null);
  }, [active.id, patchTab, refreshHistory]);

  const commitAddress = useCallback(() => {
    const u = normalizeUrl(address);
    if (u) { setAddress(u); navigate(u); }
  }, [address, navigate]);

  const goBack = useCallback(() => {
    patchTab(active.id, t => t.index > 0 ? { ...t, index: t.index - 1, loading: true, mode: 'direct', _nonce: randId() } : t);
  }, [active.id, patchTab]);
  const goFwd = useCallback(() => {
    patchTab(active.id, t => t.index < t.entries.length - 1 ? { ...t, index: t.index + 1, loading: true, mode: 'direct', _nonce: randId() } : t);
  }, [active.id, patchTab]);
  const reload = useCallback(() => {
    patchTab(active.id, t => ({ ...t, loading: true, _nonce: randId() }));
  }, [active.id, patchTab]);
  const goHome = useCallback(() => {
    patchTab(active.id, t => ({ ...t, entries: [], index: -1, title: '新标签页', loading: false }));
    setAddress('');
  }, [active.id, patchTab]);

  // 智能混合：导航乐观直连（mode='direct'），后台 probe 检测目标是否禁止 iframe 嵌套，
  // 设了 X-Frame-Options / frame-ancestors 才切到代理（mode='proxy'）。
  // 前端无法自行检测 iframe 是否被拒（同源策略），所以这步放后端 HEAD 探。
  useEffect(() => {
    if (!active || !currentUrl) return;
    const tabId = active.id;
    const url = currentUrl;
    probeDirectEmbed(url).then(res => {
      if (res && !res.direct) {
        // 防竞态：仅当该 tab 当前仍指向同一 url 时才切代理
        patchTab(tabId, t => (t.entries[t.index] === url ? { ...t, mode: 'proxy' } : t));
      }
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active?.id, currentUrl]);

  const closeTab = useCallback((id, e) => {
    if (e) e.stopPropagation();
    setTabs(ts => {
      const filtered = ts.filter(t => t.id !== id);
      const next = filtered.length ? filtered : [newTab()];
      if (id === activeId) setActiveId(next[next.length - 1].id);
      return next;
    });
  }, [activeId]);

  const openNewTab = useCallback((url = '') => {
    const t = newTab(url);
    setTabs(ts => [...ts, t]);
    setActiveId(t.id);
  }, []);

  const canBack = active && active.index > 0;
  const canFwd = active && active.index < active.entries.length - 1;

  // ─── 书签 ─────────────────────────────────────────────────────
  const isBookmarked = useMemo(
    () => !!currentUrl && (bookmarks || []).some(b => b.url === currentUrl),
    [bookmarks, currentUrl]
  );
  const toggleBookmark = useCallback(async () => {
    if (!currentUrl) return;
    const existing = (bookmarks || []).find(b => b.url === currentUrl);
    if (existing) {
      await removeBookmark(existing.id);
    } else {
      await addBookmark(titleForUrl(currentUrl), currentUrl);
    }
    refreshBookmarks();
  }, [currentUrl, bookmarks, refreshBookmarks]);

  const onClearHistory = useCallback(async () => {
    await clearHistory();
    refreshHistory();
  }, [refreshHistory]);

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden', background: T.surfaceAlt, position: 'relative' }}>
      {/* ─── 标签条 ─── */}
      <div style={{
        display: 'flex', alignItems: 'center', gap: 4,
        padding: '6px 8px', background: T.surfaceAlt,
        borderBottom: `1px solid ${T.borderSoft}`, flexShrink: 0, overflowX: 'auto',
      }}>
        {tabs.map(t => {
          const isActive = t.id === activeId;
          return (
            <div
              key={t.id}
              onClick={() => setActiveId(t.id)}
              className="edge-press"
              style={{
                display: 'flex', alignItems: 'center', gap: 6, height: 30,
                padding: '0 6px 0 10px', borderRadius: T.radius.md,
                background: isActive ? T.surface : 'transparent',
                border: `1px solid ${isActive ? T.border : 'transparent'}`,
                fontSize: 12, color: T.ink, whiteSpace: 'nowrap', cursor: 'pointer',
                flexShrink: 0, maxWidth: 220,
              }}
            >
              <Icon name="globe" size={12} stroke={1.8} style={{ color: T.ink3, flexShrink: 0 }} />
              <span style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{t.title}</span>
              <button
                onClick={(e) => closeTab(t.id, e)}
                className="edge-press"
                title="关闭标签页"
                style={{
                  width: 18, height: 18, borderRadius: T.radius.xs, border: 'none',
                  background: 'transparent', cursor: 'pointer', display: 'flex',
                  alignItems: 'center', justifyContent: 'center', color: T.ink3, flexShrink: 0,
                }}
              >
                <Icon name="x" size={11} stroke={2} />
              </button>
            </div>
          );
        })}
        <button onClick={() => openNewTab()} className="edge-press" title="新建标签页"
          style={{
            width: 28, height: 28, borderRadius: T.radius.sm, border: 'none',
            background: 'transparent', cursor: 'pointer', display: 'flex',
            alignItems: 'center', justifyContent: 'center', color: T.ink2, flexShrink: 0,
          }}>
          <Icon name="plus" size={15} stroke={2} />
        </button>
      </div>

      {/* ─── 工具条 ─── */}
      <div style={{
        display: 'flex', alignItems: 'center', gap: 4,
        padding: '8px 12px', background: T.surface,
        borderBottom: `1px solid ${T.borderSoft}`, flexShrink: 0,
      }}>
        <IconBtn name="chevLeft" title="后退" disabled={!canBack} onClick={goBack} />
        <IconBtn name="chevRight" title="前进" disabled={!canFwd} onClick={goFwd} />
        <IconBtn name="refresh" title="刷新" disabled={!currentUrl} onClick={reload} />
        <IconBtn name="home" title="主页" onClick={goHome} />

        {/* 地址栏 */}
        <div style={{
          flex: 1, display: 'flex', alignItems: 'center', gap: 8,
          background: T.surfaceAlt, border: `1px solid ${T.border}`,
          borderRadius: T.radius.pill, padding: '0 14px', height: 34,
          marginLeft: 4, marginRight: 4,
        }}>
          <Icon name="lock" size={12} style={{ color: currentUrl.startsWith('https://') ? T.green : T.ink3, flexShrink: 0 }} />
          <input
            value={address}
            onChange={e => setAddress(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter') commitAddress(); }}
            placeholder="输入网址（如 192.168.1.10:3000 或 example.com）"
            spellCheck={false}
            style={{
              flex: 1, border: 'none', background: 'transparent', outline: 'none',
              fontSize: 12.5, color: T.ink, ...MONO,
            }}
          />
          {active && active.loading && (
            <span style={{ width: 6, height: 6, borderRadius: '50%', background: T.blue, flexShrink: 0 }} className="edge-pulse" />
          )}
        </div>

        <IconBtn name="star" title={isBookmarked ? '取消收藏' : '收藏当前页'} active={isBookmarked} disabled={!currentUrl} onClick={toggleBookmark} />
        <IconBtn name="history" title="历史记录" active={panel === 'history'} onClick={() => setPanel(p => p === 'history' ? null : 'history')} />
      </div>

      {/* ─── 内容区 ─── */}
      {currentUrl ? (
        <iframe
          key={`${currentUrl}-${active.mode}-${active._nonce}`}
          src={active.mode === 'proxy' ? proxySrc(currentUrl) : currentUrl}
          sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-popups-to-escape-sandbox"
          allow="clipboard-read; clipboard-write"
          onLoad={() => patchTab(active.id, t => ({ ...t, loading: false }))}
          style={{ flex: 1, width: '100%', border: 'none', background: '#fff' }}
          title="browser-content"
        />
      ) : (
        <NewTabPage bookmarks={bookmarks || []} history={history || []} onNavigate={navigate} onOpenBookmarks={() => setPanel('bookmarks')} />
      )}

      {/* ─── 书签 / 历史 下拉面板 ─── */}
      {panel === 'bookmarks' && (
        <BrowserPanel
          title="书签" items={(bookmarks || []).map(b => ({ id: b.id, title: b.title || b.url, url: b.url }))}
          emptyHint="还没有书签，点地址栏右侧 ★ 收藏当前页"
          onOpen={(b) => navigate(b.url)}
          onDelete={(b) => { removeBookmark(b.id).then(refreshBookmarks); }}
          onClose={() => setPanel(null)}
        />
      )}
      {panel === 'history' && (
        <BrowserPanel
          title="历史记录" items={(history || []).map((h, i) => ({ id: h.url + i, title: h.title || h.url, url: h.url, sub: h.visitedAt }))}
          emptyHint="还没有访问记录"
          onOpen={(h) => navigate(h.url)}
          onClear={onClearHistory}
          onClose={() => setPanel(null)}
        />
      )}
    </div>
  );
}

// ─── 新标签页（空状态） ─────────────────────────────────────────

function NewTabPage({ bookmarks, history, onNavigate, onOpenBookmarks }) {
  const [val, setVal] = useState('');
  const submit = () => { const u = normalizeUrl(val); if (u) onNavigate(u); };
  const recent = (history || []).slice(0, 8);
  return (
    <div style={{
      flex: 1, display: 'flex', flexDirection: 'column', alignItems: 'center',
      justifyContent: 'flex-start', background: T.surfaceAlt, padding: '48px 24px', overflow: 'auto',
    }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 24 }}>
        <Icon name="globe" size={36} stroke={1.6} style={{ color: T.blue }} />
        <span style={{ fontSize: 24, fontWeight: 700, color: T.ink }}>浏览器</span>
      </div>
      <div style={{
        display: 'flex', alignItems: 'center', gap: 8, width: '100%', maxWidth: 560,
        background: T.surface, border: `1px solid ${T.border}`, borderRadius: T.radius.pill,
        padding: '0 16px', height: 44, boxShadow: T.shadow.sm,
      }}>
        <Icon name="search" size={16} style={{ color: T.ink3 }} />
        <input
          autoFocus value={val} onChange={e => setVal(e.target.value)}
          onKeyDown={e => { if (e.key === 'Enter') submit(); }}
          placeholder="输入网址（如 192.168.1.10:3000 或 example.com）"
          spellCheck={false}
          style={{ flex: 1, border: 'none', background: 'transparent', outline: 'none', fontSize: 14, color: T.ink, ...MONO }}
        />
      </div>

      {(bookmarks && bookmarks.length > 0) && (
        <div style={{ width: '100%', maxWidth: 560, marginTop: 32 }}>
          <div style={{ fontSize: 11.5, fontWeight: 600, color: T.ink3, marginBottom: 10, letterSpacing: 0.3 }}>书签</div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(120px, 1fr))', gap: 10 }}>
            {bookmarks.slice(0, 8).map(b => (
              <button key={b.id} onClick={() => onNavigate(b.url)} className="edge-press"
                style={{
                  display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 8,
                  padding: '14px 8px', borderRadius: T.radius.md, border: `1px solid ${T.borderSoft}`,
                  background: T.surface, cursor: 'pointer',
                }}>
                <div style={{ width: 32, height: 32, borderRadius: T.radius.sm, background: T.blueSoft, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                  <Icon name="globe" size={16} style={{ color: T.blueDeep }} />
                </div>
                <span style={{ fontSize: 11.5, color: T.ink2, textAlign: 'center', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: '100%' }}>
                  {b.title || b.url}
                </span>
              </button>
            ))}
          </div>
        </div>
      )}

      {recent.length > 0 && (
        <div style={{ width: '100%', maxWidth: 560, marginTop: 28 }}>
          <div style={{ fontSize: 11.5, fontWeight: 600, color: T.ink3, marginBottom: 8, letterSpacing: 0.3 }}>最近访问</div>
          {recent.map((h, i) => (
            <button key={i} onClick={() => onNavigate(h.url)} className="edge-press edge-row-hover"
              style={{
                display: 'flex', alignItems: 'center', gap: 10, width: '100%',
                padding: '8px 10px', borderRadius: T.radius.sm, border: 'none',
                background: 'transparent', cursor: 'pointer', textAlign: 'left',
              }}>
              <Icon name="history" size={13} style={{ color: T.ink4, flexShrink: 0 }} />
              <div style={{ minWidth: 0, flex: 1 }}>
                <div style={{ fontSize: 12.5, color: T.ink, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{h.title || h.url}</div>
                <div style={{ fontSize: 11, color: T.ink4, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', ...MONO }}>{h.url}</div>
              </div>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// ─── 书签 / 历史 下拉面板 ───────────────────────────────────────

function BrowserPanel({ title, items, emptyHint, onOpen, onDelete, onClear, onClose }) {
  return (
    <>
      <div onClick={onClose} style={{ position: 'fixed', inset: 0, zIndex: 50 }} />
      <div style={{
        position: 'absolute', top: 76, right: 12, zIndex: 51,
        width: 320, maxHeight: 360, overflowY: 'auto',
        background: T.surface, border: `1px solid ${T.border}`, borderRadius: T.radius.md,
        boxShadow: T.shadow.lg, padding: 6,
      }}>
        <div style={{ display: 'flex', alignItems: 'center', padding: '6px 10px' }}>
          <span style={{ fontSize: 12, fontWeight: 600, color: T.ink }}>{title}</span>
          <div style={{ flex: 1 }} />
          {onClear && items.length > 0 && (
            <button onClick={onClear} className="edge-press" style={{
              fontSize: 11, color: T.red, background: 'transparent', border: 'none', cursor: 'pointer', padding: '2px 4px',
            }}>清空</button>
          )}
        </div>
        {items.length === 0 ? (
          <div style={{ padding: '16px 12px', fontSize: 11.5, color: T.ink4, textAlign: 'center' }}>{emptyHint}</div>
        ) : items.map(it => (
          <div key={it.id} className="edge-row-hover" style={{
            display: 'flex', alignItems: 'center', gap: 8, padding: '7px 10px', borderRadius: T.radius.sm, cursor: 'pointer',
          }} onClick={() => onOpen(it)}>
            <Icon name="globe" size={13} style={{ color: T.ink4, flexShrink: 0 }} />
            <div style={{ minWidth: 0, flex: 1 }}>
              <div style={{ fontSize: 12, color: T.ink, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{it.title}</div>
              <div style={{ fontSize: 10.5, color: T.ink4, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', ...MONO }}>{it.url}</div>
            </div>
            {onDelete && (
              <button onClick={(e) => { e.stopPropagation(); onDelete(it); }} className="edge-press" title="删除"
                style={{ width: 22, height: 22, borderRadius: T.radius.xs, border: 'none', background: 'transparent', cursor: 'pointer', color: T.ink4, display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 }}>
                <Icon name="trash" size={12} />
              </button>
            )}
          </div>
        ))}
      </div>
    </>
  );
}
