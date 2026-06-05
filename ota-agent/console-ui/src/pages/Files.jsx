import { useState, useEffect, useRef } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, Chip, Sparkline } from '../components/ui'
import { useFiles, useMetrics } from '../hooks/useApi'
import { PORTS as MOCK_PORTS, FILES_TREE, MODELS as MOCK_MODELS } from '../data/mock'
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

function _fmtSize(b) {
  if (b >= 1e12) return (b/1e12).toFixed(1)+' TB';
  if (b >= 1e9) return (b/1e9).toFixed(1)+' GB';
  if (b >= 1e6) return (b/1e6).toFixed(1)+' MB';
  if (b >= 1e3) return (b/1e3).toFixed(1)+' KB';
  return b+' B';
}

export default function FilesFace() {
  const [items, setItems] = useState(FILES_TREE['/workspace'] || []);
  const [curPath, setCurPath] = useState('/');
  const [selected, setSelected] = useState(null);
  const { data: metricsData } = useMetrics(10000);
  const dsk = metricsData?.metrics?.dsk;
  const diskPct = dsk?.pct || 0;
  const diskUsed = dsk?.used || 0;
  const diskTotal = dsk?.total || 0;
  useEffect(() => {
    fetch('/api/v1/files?path=' + encodeURIComponent(curPath))
      .then(r => r.ok ? r.json() : null)
      .then(d => { if (d && Array.isArray(d)) setItems(d.map(e => ({name: e.name, type: e.isDir ? 'dir' : e.type, size: e.isDir ? '' : _fmtSize(e.size), count: e.count, modified: e.modified ? new Date(e.modified).toLocaleDateString('zh-CN') : ''}))); })
      .catch(() => {});
  }, [curPath]);

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', background: T.surface, overflow: 'hidden' }}>
      {/* Toolbar */}
      <div style={{
        padding: '10px 18px', borderBottom: `1px solid ${T.border}`,
        display: 'flex', alignItems: 'center', gap: 10, flexShrink: 0,
      }}>
        <button onClick={() => { var parts = curPath.split('/').filter(Boolean); parts.pop(); setCurPath('/' + parts.join('/')); }} style={{ ...btnSecondary, height: 30, padding: '0 10px' }}>
          <Icon name="chevLeft" size={12} stroke={2}/>
        </button>
        <button style={{ ...btnSecondary, height: 30, padding: '0 10px' }}>
          <Icon name="chevRight" size={12} stroke={2}/>
        </button>
        <button onClick={() => setCurPath(p => p)} style={{ ...btnSecondary, height: 30, padding: '0 10px' }}>
          <Icon name="refresh" size={12} stroke={1.8}/>
        </button>
        <div style={{
          flex: 1, display: 'flex', alignItems: 'center', gap: 6,
          padding: '0 12px', height: 30, borderRadius: 6,
          background: T.surfaceAlt, border: `1px solid ${T.border}`,
          fontFamily: 'ui-monospace, monospace', fontSize: 12, color: T.ink,
        }}>
          <Icon name="folder" size={12} stroke={1.8} style={{ color: T.ink4 }}/>
          {curPath || '/'}
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
        <button style={{ ...btnPrimary, height: 30, padding: '0 12px' }}>
          <Icon name="download" size={12} stroke={2}/>上传
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
          <span style={{ color: T.ink2, fontWeight: 600 }}>{curPath || '/'}</span>
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
          {[
            { icon: 'folder',  name: '/',             path: '/' },
            { icon: 'folder',  name: '/data',         path: '/data' },
            { icon: 'folder',  name: '/home',         path: '/home' },
            { icon: 'folder',  name: '/tmp',          path: '/tmp' },
            { icon: 'folder',  name: '/var/log',      path: '/var/log' },
            { icon: 'folder',  name: '/opt',          path: '/opt' },
          ].map(s => (
            <div key={s.name} onClick={() => setCurPath(s.path)} style={{
              display: 'flex', alignItems: 'center', gap: 8,
              padding: '6px 10px', borderRadius: 6, fontSize: 12.5,
              color: curPath === s.path ? T.blueDeep : T.ink, cursor: 'pointer',
              background: curPath === s.path ? T.blueSoft : 'transparent',
              fontWeight: curPath === s.path ? 600 : 500,
            }}
            onMouseEnter={(e) => { if (curPath !== s.path) e.currentTarget.style.background = T.surface; }}
            onMouseLeave={(e) => { if (curPath !== s.path) e.currentTarget.style.background = 'transparent'; }}>
              <Icon name={s.icon} size={13} stroke={1.8} style={{ color: curPath === s.path ? T.blueDeep : T.ink3 }}/>
              {s.name}
            </div>
          ))}
          <div style={{ height: 1, background: T.borderSoft, margin: '12px 8px' }}/>
          <div style={{ fontSize: 10.5, color: T.ink3, fontWeight: 600,
            letterSpacing: '0.06em', textTransform: 'uppercase', padding: '4px 10px 6px' }}>云端</div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8,
            padding: '6px 10px', fontSize: 12.5, color: T.ink2 }}>
            <Icon name="cloud" size={13} stroke={1.8} style={{ color: T.blue }}/>
            HuggingFace 缓存
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8,
            padding: '6px 10px', fontSize: 12.5, color: T.ink2 }}>
            <Icon name="database" size={13} stroke={1.8} style={{ color: T.indigo }}/>
            MinIO 桶
          </div>
        </div>

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
                    onDoubleClick={() => { if (f.type === 'dir') setCurPath((curPath === '/' ? '' : curPath) + '/' + f.name); }}
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
                      <Icon name="chevRight" size={12} stroke={2} style={{ color: T.ink4 }}/>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
