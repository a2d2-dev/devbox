import { T } from '../tokens'
import { Icon } from '../icons'
import { useLinks } from '../hooks/useApi'

const MONO = { fontFamily: '"JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, monospace' }

// badgeKind → 颜色对
const BADGE_COLORS = {
  ok:    { bg: '#dcfce7', fg: '#166534', border: '#86efac' },
  warn:  { bg: '#fef3c7', fg: '#78350f', border: '#fcd34d' },
  err:   { bg: '#fee2e2', fg: '#991b1b', border: '#fca5a5' },
  muted: { bg: '#f1f5f9', fg: '#475569', border: '#cbd5e1' },
}

// supervisor 实时状态名字 → 显示文案 / kind
const STATE_LABEL = {
  RUNNING:  { text: 'RUNNING',  kind: 'ok' },
  STARTING: { text: 'STARTING', kind: 'warn' },
  BACKOFF:  { text: 'BACKOFF',  kind: 'warn' },
  STOPPED:  { text: 'STOPPED',  kind: 'muted' },
  EXITED:   { text: 'EXITED',   kind: 'err' },
  FATAL:    { text: 'FATAL',    kind: 'err' },
  UNKNOWN:  { text: 'UNKNOWN',  kind: 'muted' },
}

function Badge({ text, kind }) {
  if (!text) return null
  const c = BADGE_COLORS[kind] || BADGE_COLORS.muted
  return (
    <span style={{
      display: 'inline-block', padding: '2px 8px', borderRadius: 999,
      fontSize: 10.5, fontWeight: 700, letterSpacing: 0.3,
      color: c.fg, background: c.bg, border: `1px solid ${c.border}`,
      ...MONO,
    }}>{text}</span>
  )
}

function humanizeBadge(item) {
  // supervisor 关联段：badge 来自后端拉的 statename
  if (STATE_LABEL[item.badge]) return STATE_LABEL[item.badge]
  return { text: item.badge, kind: item.badgeKind || 'muted' }
}

function LinkRow({ item, isSupervisor }) {
  const b = humanizeBadge(item)
  const clickable = !!item.url
  return (
    <tr style={{ borderBottom: `1px dashed ${T.borderSoft || '#f1f5f9'}` }}>
      <td style={{ padding: '10px 8px', width: '30%', minWidth: 240 }}>
        <div style={{ fontSize: 13.5, fontWeight: 700, color: T.ink }}>{item.name}</div>
        {item.project && (
          <div style={{ fontSize: 11, color: T.ink3, marginTop: 2 }}>{item.project}</div>
        )}
      </td>
      <td style={{ padding: '10px 8px' }}>
        {clickable ? (
          <a href={item.url} target="_blank" rel="noreferrer" style={{
            ...MONO, fontSize: 12.5, color: T.blueDeep, textDecoration: 'none',
            wordBreak: 'break-all',
          }} className="edge-link-hover">
            {item.url}
          </a>
        ) : (
          <span style={{ ...MONO, fontSize: 12.5, color: T.ink3 }}>—</span>
        )}
        {item.note && (
          <div style={{ fontSize: 11, color: T.ink3, marginTop: 3 }}>{item.note}</div>
        )}
      </td>
      <td style={{ padding: '10px 8px', width: 130, ...MONO }}>
        {item.supervisor && (
          <span style={{ fontSize: 11.5, color: T.ink3 }}>{item.supervisor}</span>
        )}
      </td>
      <td style={{ padding: '10px 8px', width: 100, textAlign: 'right' }}>
        <Badge text={b.text} kind={b.kind} />
      </td>
    </tr>
  )
}

function SectionCard({ section }) {
  const isSupervisor = section.kind === 'supervisor'
  return (
    <div style={{
      background: 'white', border: `1px solid ${T.border}`, borderRadius: 10,
      marginBottom: 16, overflow: 'hidden',
    }}>
      <div style={{
        padding: '12px 16px',
        borderBottom: `1px solid ${T.borderSoft || '#f1f5f9'}`,
      }}>
        <div style={{ fontSize: 14.5, fontWeight: 700, color: T.ink }}>
          {section.title}
          {isSupervisor && (
            <span style={{
              marginLeft: 8, fontSize: 10.5, fontWeight: 600, color: T.blueDeep,
              padding: '1px 6px', borderRadius: 4, background: T.blueSoft,
            }}>实时状态</span>
          )}
        </div>
        {section.description && (
          <div style={{ fontSize: 12, color: T.ink3, marginTop: 4, lineHeight: 1.55 }}>
            {section.description}
          </div>
        )}
      </div>
      {section.items?.length > 0 ? (
        <div className="edge-table-scroll"><table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ background: '#f8fafc' }}>
              <th style={hdrStyle}>名称</th>
              <th style={hdrStyle}>地址</th>
              <th style={{ ...hdrStyle, width: 130 }}>{isSupervisor ? 'Supervisor 名' : ''}</th>
              <th style={{ ...hdrStyle, width: 100, textAlign: 'right' }}>状态</th>
            </tr>
          </thead>
          <tbody>
            {section.items.map((it, i) => (
              <LinkRow key={i} item={it} isSupervisor={isSupervisor} />
            ))}
          </tbody>
        </table></div>
      ) : (
        <div style={{ padding: 16, fontSize: 12, color: T.ink3 }}>（无条目）</div>
      )}
    </div>
  )
}

const hdrStyle = {
  padding: '8px 8px',
  fontSize: 10.5, fontWeight: 700, textTransform: 'uppercase',
  letterSpacing: '0.06em', color: T.ink3, textAlign: 'left',
  borderBottom: `1px solid ${T.border}`,
}

export default function Links() {
  const { data, loading } = useLinks()

  if (loading && !data) {
    return <div style={{ padding: 40, color: T.ink3 }}>加载中…</div>
  }
  const sections = data?.sections || []
  const err = data?.error

  return (
    <div className="edge-page edge-links-page" style={{
      display: 'flex', flexDirection: 'column', height: '100%',
      width: '100%', background: '#f8fafc', overflow: 'hidden',
    }}>
      <div style={{
        padding: '12px 20px', background: 'white',
        borderBottom: `1px solid ${T.border}`,
      }}>
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 10 }}>
          <div style={{ fontSize: 16, fontWeight: 700, color: T.ink }}>服务导航</div>
          <div style={{ fontSize: 11.5, color: T.ink3 }}>
            Edge 与 Global 两条主线的文档、控制台和 API 地址清单
            {data?.path && <> · 数据源 <code style={MONO}>{data.path}</code></>}
          </div>
        </div>
      </div>

      <div style={{ flex: 1, overflow: 'auto', padding: 20 }}>
        {err && (
          <div style={{
            padding: '10px 14px', marginBottom: 14, borderRadius: 8,
            background: '#fef3c7', border: '1px solid #fcd34d',
            color: '#78350f', fontSize: 12.5,
          }}>
            ⚠ 清单加载出问题：{err}
          </div>
        )}
        {sections.length === 0 && !err && (
          <div style={{
            padding: 40, textAlign: 'center', color: T.ink3, fontSize: 13,
          }}>
            清单为空。检查 {data?.path || '/etc/devbox/links.yaml'} 是否存在。
          </div>
        )}
        {sections.map((s, i) => (
          <SectionCard key={i} section={s} />
        ))}
      </div>
    </div>
  )
}
