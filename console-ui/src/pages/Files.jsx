import { useState, useEffect, useRef, useCallback } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, Chip, Sparkline } from '../components/ui'
import { useFiles, useMetrics, authFetch } from '../hooks/useApi'
import { FileIcon } from '../components/AppShell'

const btnSecondary = {
  display: 'inline-flex', alignItems: 'center', gap: 5,
  borderRadius: 7, border: `1px solid ${T.border}`,
  background: 'white', color: T.ink2, cursor: 'pointer',
  fontSize: 12.5, fontWeight: 500,
};

const btnPrimary = {
  display: 'inline-flex', alignItems: 'center', gap: 5,
  borderRadius: 7, border: 'none',
  background: T.blue, color: 'white', cursor: 'pointer',
  fontSize: 12.5, fontWeight: 600,
  height: 32, padding: '0 14px',
};

const th = { padding: '8px 14px', fontSize: 10.5, fontWeight: 600, color: T.ink3,
  letterSpacing: '0.04em', textTransform: 'uppercase' };

const IMAGE_EXTS = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'ico', 'avif']);
function isImageType(t) { return t && IMAGE_EXTS.has(t.toLowerCase()); }

// 复制到剪贴板：优先走 Clipboard API，非安全上下文/无权限时用 textarea 兜底。
async function copyText(text) {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch (_) {}
  try {
    const ta = document.createElement('textarea');
    ta.value = text; ta.style.position = 'fixed'; ta.style.opacity = '0';
    document.body.appendChild(ta); ta.select();
    const ok = document.execCommand('copy');
    document.body.removeChild(ta);
    return ok;
  } catch (_) { return false; }
}

function _fmtSize(b) {
  if (b >= 1e12) return (b/1e12).toFixed(1)+' TB';
  if (b >= 1e9) return (b/1e9).toFixed(1)+' GB';
  if (b >= 1e6) return (b/1e6).toFixed(1)+' MB';
  if (b >= 1e3) return (b/1e3).toFixed(1)+' KB';
  return b+' B';
}

// 详情面板用的完整时间戳，例：2026-07-05 14:32:18
function _fmtDateTime(d) {
  if (!d) return '';
  const p = n => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth()+1)}-${p(d.getDate())} ` +
         `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

// 文件种类的可读名称，参考 Finder 的 “Kind” 字段
const KIND_MAP = {
  dir: '文件夹',
  png: 'PNG 图像', jpg: 'JPEG 图像', jpeg: 'JPEG 图像', gif: 'GIF 图像',
  webp: 'WebP 图像', svg: 'SVG 矢量图', bmp: 'BMP 图像', ico: '图标',
  avif: 'AVIF 图像',
  txt: '纯文本', md: 'Markdown 文档', json: 'JSON 数据', yaml: 'YAML 配置',
  yml: 'YAML 配置', toml: 'TOML 配置', xml: 'XML 文档', csv: 'CSV 表格',
  log: '日志文件',
  pdf: 'PDF 文档', zip: 'ZIP 压缩包', tar: 'TAR 归档', gz: 'GZIP 压缩',
  go: 'Go 源码', py: 'Python 源码', js: 'JavaScript 源码',
  jsx: 'React 源码', ts: 'TypeScript 源码', tsx: 'TSX 源码',
  sh: 'Shell 脚本', ipynb: 'Jupyter Notebook',
};
function fileKind(type) {
  if (!type) return '文件';
  const k = KIND_MAP[type.toLowerCase()];
  return k || (type.toUpperCase() + ' 文件');
}

// 详情面板里 label / value 一行的通用样式
function DetailRow({ label, value }) {
  return (
    <div style={{
      display: 'flex', gap: 10, padding: '6px 0', fontSize: 11.5,
      borderBottom: `1px dashed ${T.borderSoft}`,
    }}>
      <div style={{ width: 68, flexShrink: 0, color: T.ink4, fontWeight: 500 }}>{label}</div>
      <div style={{ flex: 1, color: T.ink2, minWidth: 0 }}>{value}</div>
    </div>
  );
}

export default function FilesFace() {
  const [items, setItems] = useState([]);
  // curPath 是相对工作目录的路径（空 = 工作区根）。后端用 console.workDir
  // chroot，前端绝不发起绝对路径（防 leak 宿主目录结构）。
  const [curPath, setCurPath] = useState('');
  const [selected, setSelected] = useState(null);
  const [error, setError] = useState(null); // { code, message }
  const [toast, setToast] = useState(null); // { kind: 'ok'|'warn'|'err', text }
  const [preview, setPreview] = useState(null); // { name, url, status: 'loading'|'ok'|'err' }
  const [showDetails, setShowDetails] = useState(true); // 右侧详情面板开关，类似 Finder 的显示简介
  const [uploading, setUploading] = useState(false);
  // 让 paste 事件稳定触发：容器可 focus + 挂载后自动拿焦点
  const rootRef = useRef(null);
  const fileInputRef = useRef(null);
  const { data: metricsData } = useMetrics(10000);
  const dsk = metricsData?.metrics?.dsk;
  const diskPct = dsk?.pct || 0;
  const diskUsed = dsk?.used || 0;
  const diskTotal = dsk?.total || 0;

  // 面包屑：根 = 「工作区」，中段以相对路径分段，每段 clickable。
  const breadcrumbSegments = curPath ? curPath.split('/').filter(Boolean) : [];

  // 加载指定目录的条目。返回一个 cancel 函数，供 useEffect 用。
  const loadDir = useCallback((path) => {
    setError(null);
    let cancelled = false;
    let resetTimer = null;

    authFetch('/api/v1/files?path=' + encodeURIComponent(path))
      .then(async r => {
        if (cancelled) return null;
        if (r.ok) return r.json();
        let body = null;
        try { body = await r.json(); } catch (_) {}
        if (r.status === 403 && body?.code === 'PATH_FORBIDDEN') {
          setError({ code: 'PATH_FORBIDDEN', message: '无权访问该路径（工作区外）' });
          resetTimer = setTimeout(() => { if (!cancelled) setCurPath(''); }, 1500);
        } else if (r.status === 404 && body?.code === 'PATH_NOT_FOUND') {
          setError({ code: 'PATH_NOT_FOUND', message: '路径不存在' });
        }
        return null;
      })
      .then(d => {
        if (cancelled) return;
        if (d && Array.isArray(d)) setItems(d.map(e => {
          const md = e.modified ? new Date(e.modified) : null;
          return {
            name: e.name,
            type: e.isDir ? 'dir' : e.type,
            size: e.isDir ? '' : _fmtSize(e.size),
            sizeBytes: e.isDir ? null : e.size, // 原始字节数，详情面板显示 “12,345 字节”
            count: e.count,
            absPath: e.absPath,
            modified: md ? md.toLocaleDateString('zh-CN') : '',
            modifiedFull: md ? _fmtDateTime(md) : '', // 详情面板用的完整时间戳
          };
        }));
      })
      .catch(() => {});

    return () => {
      cancelled = true;
      if (resetTimer !== null) clearTimeout(resetTimer);
    };
  }, []);

  useEffect(() => {
    const cancel = loadDir(curPath);
    return cancel;
  }, [curPath, loadDir]);

  // 挂载后主动聚焦容器，避免 body 无焦点时 window paste 事件不触发
  useEffect(() => { rootRef.current?.focus(); }, []);

  const uploadFiles = useCallback(async (files) => {
    const selectedFiles = Array.from(files || []).filter(Boolean);
    if (selectedFiles.length === 0 || uploading) return;

    setUploading(true);
    let okCount = 0;
    let lastName = '';
    try {
      for (const file of selectedFiles) {
        const fd = new FormData();
        fd.append('path', curPath);
        fd.append('name', file.name);
        fd.append('file', file, file.name);

        const r = await authFetch('/api/v1/files/upload', { method: 'POST', body: fd });
        if (!r.ok) {
          let msg = `上传失败 (${r.status})`;
          if (r.status === 403) msg = '目标目录无写入权限';
          else if (r.status === 413) msg = '文件超过 20MB 上限';
          else if (r.status === 400) msg = '文件名或目标路径无效';
          setToast({ kind: 'err', text: msg });
          return;
        }
        const body = await r.json().catch(() => ({}));
        lastName = body.name || file.name;
        okCount += 1;
      }

      setToast({
        kind: 'ok',
        text: okCount === 1 ? `已上传 ${lastName}` : `已上传 ${okCount} 个文件`,
      });
      if (lastName) setSelected(lastName);
      loadDir(curPath);
    } catch (_) {
      setToast({ kind: 'err', text: '上传失败' });
    } finally {
      setUploading(false);
      if (fileInputRef.current) fileInputRef.current.value = '';
    }
  }, [curPath, loadDir, uploading]);

  // toast 3s 自动清
  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(null), 3000);
    return () => clearTimeout(t);
  }, [toast]);

  // selected 变化时拉图片 blob 做预览（authFetch 走 Authorization header，
  // 所以不能直接 <img src=/api/...>，得先拉成 blob URL）。非图片文件不预览。
  // 直接看文件名扩展名，不依赖 items 状态（避免粘贴瞬间 items 未刷新的 race）。
  useEffect(() => {
    if (!selected) { setPreview(null); return; }
    const dot = selected.lastIndexOf('.');
    const ext = dot > 0 ? selected.slice(dot + 1) : '';
    if (!isImageType(ext)) { setPreview(null); return; }

    let cancelled = false;
    let objectURL = null;
    setPreview({ name: selected, url: null, status: 'loading' });
    authFetch('/api/v1/files/content?path=' + encodeURIComponent(curPath) +
              '&name=' + encodeURIComponent(selected))
      .then(async r => {
        if (cancelled) return;
        if (!r.ok) {
          console.error('[Files] preview fetch HTTP', r.status, 'name=', selected, 'path=', curPath);
          setPreview({ name: selected, url: null, status: 'err' });
          return;
        }
        const blob = await r.blob();
        if (cancelled) return;
        objectURL = URL.createObjectURL(blob);
        setPreview({ name: selected, url: objectURL, status: 'ok' });
      })
      .catch(err => {
        console.error('[Files] preview fetch failed', err);
        if (!cancelled) setPreview({ name: selected, url: null, status: 'err' });
      });

    return () => {
      cancelled = true;
      if (objectURL) URL.revokeObjectURL(objectURL);
    };
  }, [selected, curPath]);

  // 剪贴板粘贴图片：Ctrl+V 时若剪贴板里有图，POST /api/v1/files/upload 建文件。
  // 输入框/可编辑元素上不拦，让原生粘贴走。
  useEffect(() => {
    async function onPaste(e) {
      const t = e.target;
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return;

      const items = e.clipboardData?.items || [];
      const imgItem = Array.from(items).find(it => it.type && it.type.startsWith('image/'));
      if (!imgItem) return;
      e.preventDefault();

      const blob = imgItem.getAsFile();
      if (!blob) return;

      const ext = (blob.type.split('/')[1] || 'png').split(';')[0];
      const d = new Date();
      const pad = n => String(n).padStart(2, '0');
      const stamp = `${d.getFullYear()}${pad(d.getMonth()+1)}${pad(d.getDate())}-${pad(d.getHours())}${pad(d.getMinutes())}${pad(d.getSeconds())}`;
      const name = `screenshot-${stamp}.${ext}`;

      const fd = new FormData();
      fd.append('path', curPath);
      fd.append('name', name);
      fd.append('file', blob, name);

      try {
        const r = await authFetch('/api/v1/files/upload', { method: 'POST', body: fd });
        if (r.ok) {
          // 后端冲突时会自动加序号，最终名以响应为准
          const body = await r.json().catch(() => ({}));
          const finalName = body.name || name;
          setToast({ kind: 'ok', text: `已粘贴 ${finalName}` });
          setSelected(finalName);
          loadDir(curPath);
        } else if (r.status === 409) {
          setToast({ kind: 'warn', text: '同名文件已存在' });
        } else if (r.status === 403) {
          setToast({ kind: 'err', text: '目标目录无写入权限' });
        } else if (r.status === 413) {
          setToast({ kind: 'err', text: '图片超过 20MB 上限' });
        } else {
          setToast({ kind: 'err', text: `粘贴失败 (${r.status})` });
        }
      } catch (_) {
        setToast({ kind: 'err', text: '粘贴失败' });
      }
    }
    window.addEventListener('paste', onPaste);
    return () => window.removeEventListener('paste', onPaste);
  }, [curPath, loadDir]);

  return (
    <div
      ref={rootRef}
      tabIndex={0}
      style={{ flex: 1, display: 'flex', flexDirection: 'column', background: T.surface, overflow: 'hidden', outline: 'none' }}>
      {/* Toolbar */}
      <div style={{
        padding: '10px 18px', borderBottom: `1px solid ${T.border}`,
        display: 'flex', alignItems: 'center', gap: 10, flexShrink: 0,
      }}>
        <button
          disabled={breadcrumbSegments.length === 0}
          onClick={() => {
            const parts = curPath.split('/').filter(Boolean);
            parts.pop();
            setCurPath(parts.join('/'));
          }}
          style={{ ...btnSecondary, height: 30, padding: '0 10px',
            opacity: breadcrumbSegments.length === 0 ? 0.4 : 1,
            cursor: breadcrumbSegments.length === 0 ? 'not-allowed' : 'pointer' }}>
          <Icon name="chevLeft" size={12} stroke={2}/>
        </button>
        <button style={{ ...btnSecondary, height: 30, padding: '0 10px' }}>
          <Icon name="chevRight" size={12} stroke={2}/>
        </button>
        <button onClick={() => setCurPath(p => p)} style={{ ...btnSecondary, height: 30, padding: '0 10px' }}>
          <Icon name="refresh" size={12} stroke={1.8}/>
        </button>
        {/* Breadcrumb: 工作区 > seg1 > seg2 ... 每段 clickable，根节点 = 「工作区」 */}
        <div style={{
          flex: 1, display: 'flex', alignItems: 'center', gap: 4,
          padding: '0 12px', height: 30, borderRadius: 6,
          background: T.surfaceAlt, border: `1px solid ${T.border}`,
          fontFamily: 'ui-monospace, monospace', fontSize: 12, color: T.ink,
        }}>
          <Icon name="folder" size={12} stroke={1.8} style={{ color: T.ink4 }}/>
          <span onClick={() => setCurPath('')} style={{
            cursor: 'pointer',
            color: breadcrumbSegments.length === 0 ? T.blueDeep : T.ink,
            fontWeight: breadcrumbSegments.length === 0 ? 600 : 500,
          }}>工作区</span>
          {breadcrumbSegments.map((seg, i) => {
            const isLast = i === breadcrumbSegments.length - 1;
            const targetPath = breadcrumbSegments.slice(0, i + 1).join('/');
            return (
              <span key={i} style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
                <span style={{ color: T.ink4 }}>/</span>
                <span
                  onClick={() => setCurPath(targetPath)}
                  style={{
                    cursor: 'pointer',
                    color: isLast ? T.blueDeep : T.ink,
                    fontWeight: isLast ? 600 : 500,
                  }}>{seg}</span>
              </span>
            );
          })}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6,
          padding: '0 10px', height: 30, borderRadius: 6,
          background: T.surfaceAlt, border: `1px solid ${T.border}`,
          fontSize: 12, color: T.ink, width: 240 }}>
          <Icon name="search" size={12} stroke={1.8} style={{ color: T.ink4 }}/>
          <input placeholder="搜索文件…" style={{
            flex: 1, border: 'none', outline: 'none', fontSize: 12, background: 'transparent',
          }}/>
        </div>
        <input
          ref={fileInputRef}
          type="file"
          multiple
          onChange={(e) => uploadFiles(e.target.files)}
          style={{ display: 'none' }}
        />
        <button
          disabled={uploading}
          onClick={() => fileInputRef.current?.click()}
          style={{ ...btnPrimary, height: 30, padding: '0 12px',
            opacity: uploading ? 0.65 : 1,
            cursor: uploading ? 'wait' : 'pointer' }}>
          <Icon name="download" size={12} stroke={2}/>{uploading ? '上传中' : '上传'}
        </button>
        <button
          title={showDetails ? '隐藏详情' : '显示详情'}
          onClick={() => setShowDetails(v => !v)}
          style={{
            ...btnSecondary, height: 30, padding: '0 10px',
            background: showDetails ? T.blueSoft : 'white',
            color: showDetails ? T.blueDeep : T.ink2,
            borderColor: showDetails ? T.blueDeep : T.border,
          }}>
          <Icon name="sidebar" size={12} stroke={1.8}/>
        </button>
      </div>

      {/* Disk usage strip */}
      <div style={{
        padding: '10px 18px', background: T.surfaceAlt,
        borderBottom: `1px solid ${T.borderSoft}`, flexShrink: 0,
        display: 'flex', alignItems: 'center', gap: 14, fontSize: 11.5,
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
          <Icon name="hardDrive" size={13} stroke={1.8} style={{ color: T.amber }}/>
          <span style={{ color: T.ink2, fontWeight: 600 }}>
            {curPath ? `工作区 / ${curPath}` : '工作区'}
          </span>
        </div>
        <div style={{ flex: 1, height: 6, background: '#fef3c7', borderRadius: 3,
          overflow: 'hidden', position: 'relative' }}>
          <div style={{ width: diskPct + '%', height: '100%', background: T.amber }}/>
        </div>
        <span className="mono tnum" style={{ color: T.ink, fontWeight: 600 }}>{diskUsed} GB / {diskTotal} GB</span>
        <span style={{ color: T.ink3 }}>({diskPct}% 已用)</span>
        <button style={{ ...btnSecondary, height: 24, padding: '0 8px', fontSize: 11 }}>
          <Icon name="sparkle" size={11} stroke={1.8}/>分析占用
        </button>
      </div>

      <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* Sidebar shortcuts */}
        <div style={{ width: 200, flexShrink: 0, borderRight: `1px solid ${T.borderSoft}`,
          background: T.surfaceAlt, padding: '12px 8px' }}>
          <div style={{ fontSize: 10.5, color: T.ink3, fontWeight: 600,
            letterSpacing: '0.06em', textTransform: 'uppercase', padding: '4px 10px 6px' }}>快速访问</div>
          {/* 受控开发界面方向（2026-06-20）：侧栏只显示「工作区」单根节点。
              宿主路径快捷入口（/, /data, /home, /tmp, /var/log, /opt）已移除，
              防止用户认为可以访问宿主任意目录。后端 chroot 校验同步生效，
              即使 URL 注入也会返 403 PATH_FORBIDDEN。
              「云端」段（HuggingFace / MinIO）未实现后端，先一并隐藏。 */}
          <div onClick={() => setCurPath('')} style={{
            display: 'flex', alignItems: 'center', gap: 8,
            padding: '6px 10px', borderRadius: 6, fontSize: 12.5,
            color: curPath === '' ? T.blueDeep : T.ink, cursor: 'pointer',
            background: curPath === '' ? T.blueSoft : 'transparent',
            fontWeight: curPath === '' ? 600 : 500,
          }}
          onMouseEnter={(e) => { if (curPath !== '') e.currentTarget.style.background = T.surface; }}
          onMouseLeave={(e) => { if (curPath !== '') e.currentTarget.style.background = 'transparent'; }}>
            <Icon name="folder" size={13} stroke={1.8} style={{ color: curPath === '' ? T.blueDeep : T.ink3 }}/>
            工作区
          </div>
        </div>

        {/* Toast (paste 反馈) */}
        {toast && (
          <div style={{
            position: 'absolute', top: error ? 54 : 10, right: 16, zIndex: 11,
            padding: '8px 14px', borderRadius: 6,
            background: toast.kind === 'ok' ? '#f0fdf4' : toast.kind === 'warn' ? '#fffbeb' : '#fef2f2',
            border: `1px solid ${toast.kind === 'ok' ? '#bbf7d0' : toast.kind === 'warn' ? '#fde68a' : '#fecaca'}`,
            color: toast.kind === 'ok' ? '#166534' : toast.kind === 'warn' ? '#92400e' : '#991b1b',
            fontSize: 12, fontWeight: 500,
            boxShadow: '0 4px 12px rgba(0,0,0,0.08)',
          }}>
            {toast.text}
          </div>
        )}
        {/* Error banner (chroot violation / not found) */}
        {error && (
          <div style={{
            position: 'absolute', top: 10, right: 16, zIndex: 10,
            padding: '8px 14px', borderRadius: 6,
            background: error.code === 'PATH_FORBIDDEN' ? '#fef2f2' : '#fffbeb',
            border: `1px solid ${error.code === 'PATH_FORBIDDEN' ? '#fecaca' : '#fde68a'}`,
            color: error.code === 'PATH_FORBIDDEN' ? '#991b1b' : '#92400e',
            fontSize: 12, fontWeight: 500,
            boxShadow: '0 4px 12px rgba(0,0,0,0.08)',
          }}>
            {error.message}
            {error.code === 'PATH_FORBIDDEN' && (
              <span style={{ marginLeft: 6, fontWeight: 400, opacity: 0.7 }}>(自动回到工作区)</span>
            )}
          </div>
        )}
        {/* File list */}
        <div style={{ flex: 1, overflow: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12.5 }}>
            <thead>
              <tr style={{ background: T.surfaceAlt, borderBottom: `1px solid ${T.border}` }}>
                <th style={{ ...th, width: 36 }}><input type="checkbox" disabled/></th>
                <th style={{ ...th, textAlign: 'left' }}>名称</th>
                <th style={{ ...th, textAlign: 'right' }}>大小</th>
                <th style={{ ...th, textAlign: 'right' }}>修改时间</th>
                <th style={{ ...th, textAlign: 'right' }}></th>
              </tr>
            </thead>
            <tbody>
              {items.map((f, i) => {
                const on = selected === f.name;
                return (
                  <tr key={f.name} onClick={() => setSelected(f.name)}
                    onDoubleClick={() => { if (f.type === 'dir') setCurPath(curPath ? `${curPath}/${f.name}` : f.name); }}
                    style={{
                    background: on ? '#eff4ff' : 'transparent',
                    borderTop: `1px solid ${T.borderSoft}`, cursor: 'pointer',
                  }}>
                    <td style={{ padding: '8px 14px' }}><input type="checkbox" disabled checked={on}/></td>
                    <td style={{ padding: '8px 14px' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        <FileIcon type={f.type} size={14}/>
                        <span style={{ color: T.ink, fontWeight: f.type === 'dir' ? 600 : 500 }}>{f.name}</span>
                        {f.type === 'dir' && <span style={{ fontSize: 11, color: T.ink4 }}>· {f.count} 项</span>}
                      </div>
                    </td>
                    <td style={{ padding: '8px 14px', textAlign: 'right', color: T.ink3 }} className="mono">{f.size}</td>
                    <td style={{ padding: '8px 14px', textAlign: 'right', color: T.ink3 }} className="mono">{f.modified || ''}</td>
                    <td style={{ padding: '8px 14px', textAlign: 'right' }}>
                      {f.absPath && (
                        <button
                          title={`复制路径: ${f.absPath}`}
                          onClick={async (e) => {
                            e.stopPropagation();
                            const ok = await copyText(f.absPath);
                            setToast(ok
                              ? { kind: 'ok', text: `已复制路径 ${f.absPath}` }
                              : { kind: 'err', text: '复制失败' });
                          }}
                          style={{
                            border: 'none', background: 'transparent', cursor: 'pointer',
                            color: T.ink4, padding: 2, display: 'inline-flex',
                          }}>
                          <Icon name="copy" size={13} stroke={1.8}/>
                        </button>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>

        {/* Details panel：仿 Mac Finder “显示简介”。选中任意文件时展开右侧栏，
            图片显示预览，其余显示大号类型图标，下方统一给出元数据字段。 */}
        {showDetails && selected && (() => {
          const item = items.find(f => f.name === selected);
          if (!item) return null;
          const isImg = item.type !== 'dir' && isImageType(item.type);
          return (
            <div style={{
              width: 320, flexShrink: 0, borderLeft: `1px solid ${T.borderSoft}`,
              background: T.surfaceAlt, display: 'flex', flexDirection: 'column',
              overflow: 'hidden',
            }}>
              {/* 标题栏 */}
              <div style={{
                padding: '10px 14px', borderBottom: `1px solid ${T.borderSoft}`,
                fontSize: 11.5, fontWeight: 600, color: T.ink2,
                display: 'flex', alignItems: 'center', gap: 6,
              }}>
                <Icon name="info" size={13} stroke={1.8} style={{ color: T.ink3 }}/>
                <span style={{ flex: 1 }}>简介</span>
                <button
                  title="关闭详情"
                  onClick={() => setShowDetails(false)}
                  style={{
                    border: 'none', background: 'transparent', cursor: 'pointer',
                    color: T.ink3, padding: 2, display: 'inline-flex',
                  }}>
                  <Icon name="x" size={13} stroke={1.8}/>
                </button>
              </div>

              {/* 预览区：图片显示真图，其它显示大号类型图标 */}
              <div style={{
                flexShrink: 0, height: 200,
                display: 'flex', alignItems: 'center', justifyContent: 'center',
                padding: 14, overflow: 'hidden',
                background: isImg
                  ? 'repeating-conic-gradient(#f1f5f9 0% 25%, #ffffff 0% 50%) 50% / 16px 16px'
                  : T.surface,
                borderBottom: `1px solid ${T.borderSoft}`,
              }}>
                {isImg ? (
                  <>
                    {(!preview || preview.name !== selected || preview.status === 'loading') && (
                      <div style={{ fontSize: 12, color: T.ink3 }}>加载中…</div>
                    )}
                    {preview?.name === selected && preview.status === 'err' && (
                      <div style={{ fontSize: 12, color: '#991b1b' }}>预览加载失败</div>
                    )}
                    {preview?.name === selected && preview.status === 'ok' && preview.url && (
                      <img src={preview.url} alt={selected} style={{
                        maxWidth: '100%', maxHeight: '100%', objectFit: 'contain',
                        boxShadow: '0 2px 8px rgba(0,0,0,0.08)', borderRadius: 4,
                      }}/>
                    )}
                  </>
                ) : (
                  <div style={{
                    display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 8,
                  }}>
                    <FileIcon type={item.type} size={72}/>
                    <div style={{ fontSize: 11, color: T.ink4 }}>无预览</div>
                  </div>
                )}
              </div>

              {/* 文件名 + 类型摘要 */}
              <div style={{
                padding: '12px 14px 8px', borderBottom: `1px solid ${T.borderSoft}`,
              }}>
                <div style={{
                  fontSize: 13, fontWeight: 600, color: T.ink,
                  overflowWrap: 'anywhere', lineHeight: 1.35,
                }} title={item.name}>{item.name}</div>
                <div style={{ marginTop: 4, fontSize: 11.5, color: T.ink3 }}>
                  {fileKind(item.type)}
                  {item.sizeBytes != null && <> · <span className="mono">{item.size}</span></>}
                  {item.type === 'dir' && item.count != null && <> · {item.count} 项</>}
                </div>
              </div>

              {/* 详细字段：仿 Finder 的信息面板 */}
              <div style={{ flex: 1, overflow: 'auto', padding: '10px 14px' }}>
                <DetailRow label="种类" value={fileKind(item.type)}/>
                {item.sizeBytes != null && (
                  <DetailRow
                    label="大小"
                    value={
                      <span className="mono">
                        {item.size}
                        {item.sizeBytes >= 1000 && (
                          <span style={{ color: T.ink4 }}> ({item.sizeBytes.toLocaleString()} 字节)</span>
                        )}
                      </span>
                    }/>
                )}
                {item.type === 'dir' && item.count != null && (
                  <DetailRow label="项目数" value={<span className="mono">{item.count}</span>}/>
                )}
                {item.modifiedFull && (
                  <DetailRow label="修改时间" value={<span className="mono">{item.modifiedFull}</span>}/>
                )}
                {item.absPath && (
                  <DetailRow
                    label="位置"
                    value={
                      <div style={{
                        display: 'flex', alignItems: 'flex-start', gap: 4,
                      }}>
                        <span className="mono" style={{
                          flex: 1, overflowWrap: 'anywhere', lineHeight: 1.4,
                        }}>{item.absPath}</span>
                        <button
                          title={`复制路径: ${item.absPath}`}
                          onClick={async () => {
                            const ok = await copyText(item.absPath);
                            setToast(ok
                              ? { kind: 'ok', text: `已复制路径 ${item.absPath}` }
                              : { kind: 'err', text: '复制失败' });
                          }}
                          style={{
                            border: 'none', background: 'transparent', cursor: 'pointer',
                            color: T.ink3, padding: 2, display: 'inline-flex', flexShrink: 0,
                          }}>
                          <Icon name="copy" size={12} stroke={1.8}/>
                        </button>
                      </div>
                    }/>
                )}
              </div>
            </div>
          );
        })()}
        {/* 未选中 / 详情关闭时占位提示，仅在详情开着但没选文件时显示 */}
        {showDetails && !selected && (
          <div style={{
            width: 320, flexShrink: 0, borderLeft: `1px solid ${T.borderSoft}`,
            background: T.surfaceAlt, display: 'flex', alignItems: 'center',
            justifyContent: 'center', padding: 20, textAlign: 'center',
          }}>
            <div style={{ fontSize: 12, color: T.ink4 }}>
              <Icon name="info" size={24} stroke={1.5} style={{ color: T.ink4, display: 'block', margin: '0 auto 8px' }}/>
              选中文件后<br/>此处显示详细信息
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
