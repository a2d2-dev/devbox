import { useState, useEffect, useRef } from 'react'
import { T } from '../tokens'
import { Icon } from '../icons'
import { StatusDot, Chip, Sparkline } from '../components/ui'
import { useFiles, usePorts, useModels } from '../hooks/useApi'
import { PORTS as MOCK_PORTS, FILES_TREE, MODELS as MOCK_MODELS } from '../data/mock'

const btnSecondary = {
  display: 'inline-flex', alignItems: 'center', gap: 5,
  borderRadius: 7, border: `1px solid ${T.border}`,
  background: 'white', color: T.ink2, cursor: 'pointer',
  fontSize: 12.5, fontWeight: 500,
  height: 32, padding: '0 14px',
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

const iconBtnLight = {
  width: 26, height: 26, borderRadius: 5, border: `1px solid ${T.border}`,
  background: T.surface, color: T.ink3, cursor: 'pointer',
  display: 'flex', alignItems: 'center', justifyContent: 'center',
};

const Card = ({ title, action, padding = 16, children }) => (
  <div style={{ background: T.surface, border: `1px solid ${T.border}`, borderRadius: 10 }}>
    {title && (
      <div style={{ display: 'flex', alignItems: 'center', padding: '10px 16px',
        borderBottom: `1px solid ${T.borderSoft}` }}>
        <div style={{ fontSize: 12.5, fontWeight: 600, color: T.ink }}>{title}</div>
        <div style={{ flex: 1 }}/>
        {action}
      </div>
    )}
    <div style={{ padding }}>{children}</div>
  </div>
);

export default function ModelsFace() {
  const [filter, setFilter] = useState('all');
  const [liveModels, setLiveModels] = useState(MOCK_MODELS);
  useEffect(() => {
    fetch('/api/v1/models').then(r => r.ok ? r.json() : null).then(d => {
      if (d && Array.isArray(d) && d.length > 0) setLiveModels(d);
    }).catch(() => {});
  }, []);
  const filtered = liveModels.filter(m => filter === 'all' || (m.family || '').toLowerCase() === filter);

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', background: T.surfaceAlt, overflow: 'hidden' }}>
      <div style={{
        padding: '14px 24px', background: T.surface,
        borderBottom: `1px solid ${T.border}`, flexShrink: 0,
        display: 'flex', alignItems: 'center', gap: 14,
      }}>
        <div>
          <div style={{ fontSize: 17, fontWeight: 700, color: T.ink, letterSpacing: '-0.01em' }}>模型仓库</div>
          <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 3 }}>
            本地已下载 <strong className="mono" style={{ color: T.ink2 }}>{liveModels.length}</strong> 个 ·
            共计 <strong className="mono" style={{ color: T.ink2 }}>326 GB</strong> ·
            存储于 <span className="mono">/workspace/models</span>
          </div>
        </div>
        <div style={{ flex: 1 }}/>
        <button style={btnSecondary}>
          <Icon name="cloud" size={13} stroke={1.8}/>从 HuggingFace 同步
        </button>
        <button style={btnPrimary}>
          <Icon name="download" size={13} stroke={2}/>拉取新模型
        </button>
      </div>

      <div style={{ flex: 1, overflow: 'auto', padding: 24 }}>
        {/* Filter chips */}
        <div style={{ display: 'flex', gap: 6, marginBottom: 14, flexWrap: 'wrap' }}>
          {['all', 'Llama', 'Qwen', 'DeepSeek', 'GLM', 'BGE', 'SD', 'Flux', 'Whisper'].map(f => {
            const on = filter === (f === 'all' ? 'all' : f.toLowerCase());
            const c = f === 'all' ? MOCK_MODELS.length : MOCK_MODELS.filter(m => m.family === f).length;
            return (
              <button key={f} onClick={() => setFilter(f === 'all' ? 'all' : f.toLowerCase())}
                disabled={c === 0 && f !== 'all'}
                style={{
                  display: 'flex', alignItems: 'center', gap: 4,
                  padding: '5px 10px', borderRadius: 999,
                  background: on ? T.blue : T.surface,
                  color: on ? 'white' : T.ink2,
                  border: `1px solid ${on ? T.blueDeep : T.border}`,
                  fontSize: 11.5, fontWeight: on ? 600 : 500,
                  cursor: c === 0 && f !== 'all' ? 'not-allowed' : 'pointer',
                  opacity: c === 0 && f !== 'all' ? 0.5 : 1,
                }}>
                {f === 'all' ? '全部' : f} · {c}
              </button>
            );
          })}
        </div>

        <Card padding={0}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12.5 }}>
            <thead>
              <tr style={{ background: '#fafbfc', borderBottom: `1px solid ${T.borderSoft}` }}>
                <th style={{ ...th, textAlign: 'left' }}>模型</th>
                <th style={{ ...th, textAlign: 'left' }}>家族</th>
                <th style={{ ...th, textAlign: 'right' }}>体积</th>
                <th style={{ ...th, textAlign: 'left' }}>格式</th>
                <th style={{ ...th, textAlign: 'left' }}>可用引擎</th>
                <th style={{ ...th, textAlign: 'left' }}>标签</th>
                <th style={{ ...th, textAlign: 'right' }}>添加于</th>
                <th style={{ ...th, textAlign: 'right' }}></th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((m, i) => (
                <tr key={m.name} style={{ borderTop: `1px solid ${T.borderSoft}` }}>
                  <td style={{ padding: '10px 14px' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                      <Icon name="brain" size={14} stroke={1.8} style={{ color: T.violet }}/>
                      <span style={{ fontSize: 12.5, color: T.ink, fontWeight: 600 }} className="mono">{m.name}</span>
                      {m.pinned && <span title="常驻" style={{ color: T.amber }}>★</span>}
                    </div>
                  </td>
                  <td style={{ padding: '10px 14px', color: T.ink2 }}>{m.family}</td>
                  <td style={{ padding: '10px 14px', textAlign: 'right', color: T.ink2 }} className="mono tnum">{m.size}</td>
                  <td style={{ padding: '10px 14px' }} className="mono"><span style={{ background: T.surfaceAlt, padding: '1px 6px', borderRadius: 3, fontSize: 11, color: T.ink3 }}>{m.format}</span></td>
                  <td style={{ padding: '10px 14px', fontSize: 11.5, color: T.ink2 }}>{m.engine}</td>
                  <td style={{ padding: '10px 14px' }}>
                    <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
                      {m.tags.map(t => (
                        <span key={t} style={{
                          fontSize: 10, padding: '1px 5px', borderRadius: 3,
                          background: t === '新' ? '#fef2f2' : t === 'Chat' ? '#eff4ff' : t === 'Image' ? '#fdf2f8' : t === 'Embedding' ? '#f5f3ff' : T.surfaceAlt,
                          color: t === '新' ? T.red : t === 'Chat' ? T.blueDeep : t === 'Image' ? '#9d174d' : t === 'Embedding' ? '#5b21b6' : T.ink3,
                          fontWeight: 600,
                        }}>{t}</span>
                      ))}
                    </div>
                  </td>
                  <td style={{ padding: '10px 14px', textAlign: 'right', color: T.ink3, fontSize: 11.5 }} className="mono">{m.added}</td>
                  <td style={{ padding: '10px 14px', textAlign: 'right' }}>
                    <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 4 }}>
                      <button style={iconBtnLight} title="复制路径"><Icon name="copy" size={12} stroke={1.8}/></button>
                      <button style={iconBtnLight} title="加载到 vLLM"><Icon name="play" size={12} stroke={1.8}/></button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      </div>
    </div>
  );
}
