// DockerInventory — Docker 应用「网络」「存储」tab（容器域 IA 重组 T3）。
//
// 只读清单，无任何写操作按钮（创建/删除/清理是后续票）：
//   - DockerNetworksTab：全局 docker network 表格（名称/driver/scope/internal/容器数）
//   - DockerStorageTab：上半部 data-root 信息卡（复用 useDockerOverview 数据，
//     迁移入口不搬代码——按钮切回「概览」tab，由概览的存储设置对话框承接）；
//     下半部全局 volume 表格（名称/driver/所属 project/容器数）
// 两 tab 均处理 available:false 空态：显示后端诊断字符串，不白屏。
import { T } from '../tokens';
import { Icon } from '../icons';
import { useDockerNetworks, useDockerVolumes, useDockerOverview } from '../hooks/useApi';

export function DockerNetworksTab() {
  const { data } = useDockerNetworks();
  return (
    <div style={{ padding: 24, height: '100%', overflow: 'auto', background: T.bg }}>
      <SectionHeader title="网络" subtitle="全局 docker network 只读清单 · 15 秒刷新"
        count={data?.available ? data.networks?.length || 0 : null}/>
      {!data && <LoadingBox label="正在读取网络清单…"/>}
      {data && !data.available && <UnavailableBox diagnostic={data.diagnostic}/>}
      {data?.available && (
        (data.networks?.length || 0) === 0 ? (
          <EmptyBox icon="network" text="暂无 docker network"/>
        ) : (
          <table style={table}>
            <thead>
              <tr>
                <th style={th}>名称</th><th style={th}>Driver</th><th style={th}>Scope</th>
                <th style={th}>Internal</th><th style={{ ...th, textAlign: 'right' }}>容器数</th>
              </tr>
            </thead>
            <tbody>
              {data.networks.map((n) => (
                <tr key={n.name}>
                  <td style={td}><span className="mono">{n.name}</span></td>
                  <td style={td}>{n.driver || '—'}</td>
                  <td style={td}>{n.scope || '—'}</td>
                  <td style={td}>{n.internal ? '是' : '否'}</td>
                  <td style={{ ...td, textAlign: 'right' }} className="mono tnum">{n.containers}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )
      )}
    </div>
  );
}

export function DockerStorageTab({ onOpenOverview }) {
  const { data: volumes } = useDockerVolumes();
  const { data: overview } = useDockerOverview(15000);
  const storage = overview?.storage;
  const diskUsed = Math.max(0, (storage?.disk?.totalBytes || 0) - (storage?.disk?.availableBytes || 0));
  const diskPct = storage?.disk?.totalBytes > 0 ? diskUsed / storage.disk.totalBytes * 100 : 0;

  return (
    <div style={{ padding: 24, height: '100%', overflow: 'auto', background: T.bg }}>
      {/* data-root 信息卡：数据复用 overview，迁移流程留在「概览」tab（不搬迁移代码） */}
      <SectionHeader title="数据存储" subtitle="Docker data-root 与全局 volume 只读清单"/>
      <div style={{ padding: 16, background: T.surface, border: `1px solid ${T.border}`, borderRadius: 8, marginTop: 10 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
          <Icon name="hardDrive" size={15}/>
          <span style={{ fontSize: 13, fontWeight: 700, color: T.ink }}>data-root</span>
          <span className="mono" style={{ fontSize: 12.5, color: storage?.valid ? T.ink : T.red }}>
            {storage?.path || '未配置'}
          </span>
          <span style={{ flex: 1 }}/>
          <button onClick={onOpenOverview} style={linkBtn}>
            <Icon name="gear" size={12}/>存储设置与迁移（概览页）
          </button>
        </div>
        {storage?.valid ? (
          <>
            <div style={{ height: 7, borderRadius: 4, background: T.borderSoft, marginTop: 12, overflow: 'hidden' }}>
              <div style={{ width: `${Math.min(100, diskPct)}%`, height: '100%', background: diskPct >= 85 ? T.red : diskPct >= 70 ? T.amber : T.teal }}/>
            </div>
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, marginTop: 7, fontSize: 11.5, color: T.ink3 }}>
              <span>已用 {formatBytes(diskUsed)}</span><span>可用 {formatBytes(storage.disk.availableBytes)}</span>
            </div>
          </>
        ) : (
          <div style={{ marginTop: 10, fontSize: 12, color: T.ink3 }}>
            {storage ? (storage.error || '存储位置尚未确认，请到概览页完成设置') : '正在读取存储信息…'}
          </div>
        )}
      </div>

      <SectionHeader title="Volume" subtitle="全局 docker volume 只读清单 · 15 秒刷新"
        count={volumes?.available ? volumes.volumes?.length || 0 : null} style={{ marginTop: 22 }}/>
      {!volumes && <LoadingBox label="正在读取 volume 清单…"/>}
      {volumes && !volumes.available && <UnavailableBox diagnostic={volumes.diagnostic}/>}
      {volumes?.available && (
        (volumes.volumes?.length || 0) === 0 ? (
          <EmptyBox icon="database" text="暂无 docker volume"/>
        ) : (
          <table style={table}>
            <thead>
              <tr>
                <th style={th}>名称</th><th style={th}>Driver</th><th style={th}>所属 Compose 项目</th>
                <th style={{ ...th, textAlign: 'right' }}>容器数</th>
              </tr>
            </thead>
            <tbody>
              {volumes.volumes.map((v) => (
                <tr key={v.name}>
                  <td style={td} title={v.mountpoint}><span className="mono">{v.name}</span></td>
                  <td style={td}>{v.driver || '—'}</td>
                  <td style={td}>{v.composeProject ? <span className="mono">{v.composeProject}</span> : <span style={{ color: T.ink4 }}>—</span>}</td>
                  <td style={{ ...td, textAlign: 'right' }} className="mono tnum">{v.containers}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )
      )}
    </div>
  );
}

function SectionHeader({ title, subtitle, count, style }) {
  return (
    <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, flexWrap: 'wrap', ...style }}>
      <h2 style={{ margin: 0, fontSize: 15, color: T.ink, letterSpacing: 0 }}>{title}</h2>
      <span style={{ fontSize: 11, color: T.ink4 }}>{subtitle}</span>
      {Number.isFinite(count) && <span className="mono tnum" style={{ fontSize: 11.5, color: T.ink3 }}>{count} 个</span>}
    </div>
  );
}

function LoadingBox({ label }) {
  return <div style={placeholderBox}>{label}</div>;
}

// daemon 不可用空态：显示后端诊断，不白屏。
function UnavailableBox({ diagnostic }) {
  return (
    <div role="status" style={{ ...placeholderBox, display: 'flex', alignItems: 'center', gap: 9, textAlign: 'left' }}>
      <Icon name="alertTri" size={16} style={{ flexShrink: 0 }}/>
      <div style={{ minWidth: 0 }}>
        <div style={{ fontWeight: 700, color: T.ink2 }}>Docker daemon 不可用</div>
        <div style={{ marginTop: 3, overflowWrap: 'anywhere' }}>{diagnostic || '清单暂不可用'}</div>
      </div>
    </div>
  );
}

function EmptyBox({ icon, text }) {
  return (
    <div style={placeholderBox}>
      <Icon name={icon} size={22}/><div style={{ marginTop: 6 }}>{text}</div>
    </div>
  );
}

function formatBytes(value) {
  const n = Number(value) || 0;
  if (n >= 1024 ** 4) return `${(n / 1024 ** 4).toFixed(1)} TiB`;
  if (n >= 1024 ** 3) return `${(n / 1024 ** 3).toFixed(1)} GiB`;
  if (n >= 1024 ** 2) return `${(n / 1024 ** 2).toFixed(1)} MiB`;
  if (n >= 1024) return `${(n / 1024).toFixed(1)} KiB`;
  return `${Math.round(n)} B`;
}

const table = {
  width: '100%', marginTop: 10, borderCollapse: 'collapse', background: T.surface,
  border: `1px solid ${T.border}`, borderRadius: 8, fontSize: 12.5,
};
const th = {
  textAlign: 'left', padding: '9px 12px', fontSize: 11.5, fontWeight: 700, color: T.ink3,
  borderBottom: `1px solid ${T.border}`, whiteSpace: 'nowrap',
};
const td = { padding: '8px 12px', color: T.ink2, borderBottom: `1px solid ${T.borderSoft}` };
const placeholderBox = {
  marginTop: 10, padding: 28, textAlign: 'center', color: T.ink3, fontSize: 12.5,
  border: `1px dashed ${T.border}`, borderRadius: 8, background: T.surface,
};
const linkBtn = {
  display: 'inline-flex', alignItems: 'center', gap: 5, padding: '5px 11px',
  borderRadius: 6, border: `1px solid ${T.border}`, background: '#fff', color: T.ink2,
  fontSize: 12, fontWeight: 600, cursor: 'pointer',
};
