// UninstallDialog — 统一卸载确认（Issue #2 要求 10）。
//
// 运行时分流：
//   - compose：先 GET /remove-preview?purge=false|true，明确展示 willDelete / willKeep；
//     purge 是高风险二次确认且必须输入应用名；external/bind/socket 永不删除（后端保证，
//     UI 用 willKeep 明示）。DELETE 的 purge 值显式传给后端。
//   - kubernetes：保留旧行为（无受管卷概念），简单「输入应用名确认」+ deleteApp(purge=false)。
//
// 安全约束：
//   - 默认保留受管数据（purge=false）；purge 需勾选 + 输入应用名双重确认。
//   - 永不展示/回传 secret；remove-preview 仅含资源路径字符串。
//   - 颜色不是唯一状态：purge 用红底 + 文案「不可恢复」+ 名称匹配三重表达。
import { useState, useEffect, useRef } from 'react';
import { T } from '../tokens';
import { Icon } from '../icons';
import { useToast } from './toastContext';
import { getRemovePreview, removeAppEx, useTask } from '../hooks/useApi';

export default function UninstallDialog({ app, onClose, onDone, trackTask = false }) {
  const isCompose = (app?.runtime || 'kubernetes') === 'compose';
  const [purge, setPurge] = useState(false);
  const [preview, setPreview] = useState(null); // RemovePreview | null
  const [loading, setLoading] = useState(isCompose);
  const [submitting, setSubmitting] = useState(false);
  const [typed, setTyped] = useState('');
  const [error, setError] = useState(null);
  const [taskId, setTaskId] = useState(null);
  const { task } = useTask(taskId);
  const toast = useToast();
  const mountedRef = useRef(true);
  const handledTaskRef = useRef(null);
  useEffect(() => () => { mountedRef.current = false; }, []);
  useEffect(() => {
    if (!trackTask || !taskId || !task) return;
    if (task.status === 'succeeded' && handledTaskRef.current !== taskId) {
      handledTaskRef.current = taskId;
      toast.ok(`已卸载「${app.name}」`);
      onDone?.(taskId, purge);
    }
  }, [trackTask, taskId, task, app.name, onDone, purge, toast]);

  // compose：按 purge 取 remove-preview（purge 切换时重取）。
  useEffect(() => {
    if (!isCompose) return; // k8s 无 remove-preview
    let cancelled = false;
    getRemovePreview(app.id, purge)
      .then((p) => { if (!cancelled && mountedRef.current) { setPreview(p); setLoading(false); } })
      .catch((e) => {
        if (cancelled || !mountedRef.current) return;
        // remove-preview 失败不阻断卸载：降级为不带明细的确认（后端仍保证 external 不删）。
        setError(e?.message || '无法获取删除预览');
        setPreview(null);
        setLoading(false);
      });
    return () => { cancelled = true; };
  }, [app.id, purge, isCompose]);

  const taskFailure = trackTask && task && ['failed', 'canceled', 'superseded'].includes(task.status)
    ? (task.message || `卸载任务 ${task.status}`) : null;
  const busy = submitting && !taskFailure;
  const nameMatches = typed.trim() === app.name;
  // purge 必须输入应用名；非 purge 不需要。K8s 一律要求输入应用名（与旧行为一致）。
  const canConfirm = !busy && (isCompose ? (purge ? nameMatches : true) : nameMatches);

  async function onConfirm() {
    if (!canConfirm) return;
    handledTaskRef.current = null;
    setTaskId(null);
    setSubmitting(true);
    setError(null);
    try {
      const res = await removeAppEx(app.id, isCompose ? purge : false);
      const submittedTaskId = res?.taskId || res?.id || null;
      toast.ok(`已提交卸载「${app.name}」`);
      if (trackTask && submittedTaskId) {
        if (mountedRef.current) setTaskId(submittedTaskId);
      } else {
        if (mountedRef.current) setSubmitting(false);
        onDone?.(submittedTaskId, purge);
      }
    } catch (e) {
      if (mountedRef.current) {
        setSubmitting(false);
        setError(e?.message || '卸载失败');
      }
    }
  }

  const displayError = error || taskFailure;
  const willDelete = Array.isArray(preview?.willDelete) ? preview.willDelete : [];
  const willKeep = Array.isArray(preview?.willKeep) ? preview.willKeep : [];

  return (
    <div className="edge-backdrop-in" onClick={busy ? undefined : onClose} style={{
      position: 'fixed', inset: 0, zIndex: 250,
      background: 'rgba(15,23,41,0.5)',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      backdropFilter: 'blur(4px)', WebkitBackdropFilter: 'blur(4px)',
    }}>
      <div onClick={(e) => e.stopPropagation()} className="edge-fade-in" style={{
        width: 480, maxWidth: '92vw', maxHeight: '88vh', overflow: 'auto',
        background: 'white', borderRadius: 12,
        boxShadow: '0 28px 64px -12px rgba(15,23,42,0.45)',
      }}>
        {/* Header */}
        <div style={{
          padding: '14px 20px', display: 'flex', alignItems: 'center', gap: 10,
          background: purge ? '#fef2f2' : '#fff7ed',
          borderBottom: `1px solid ${purge ? '#fecaca' : '#fed7aa'}`,
        }}>
          <div style={{
            width: 30, height: 30, borderRadius: 8,
            background: T.red, color: 'white',
            display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0,
          }}>
            <Icon name="trash" size={15} stroke={2}/>
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: 14, fontWeight: 700, color: T.ink }}>
              {purge ? '卸载并删除受管数据' : '卸载应用'}
            </div>
            <div style={{ fontSize: 11.5, color: purge ? '#b91c1c' : '#9a3412', marginTop: 2 }}>
              {purge ? '受管数据将被永久删除 · 此操作不可恢复' : '默认保留受管数据'}
            </div>
          </div>
          {!busy && (
            <button onClick={onClose} aria-label="关闭" style={{
              width: 28, height: 28, borderRadius: 7, cursor: 'pointer', border: 'none', background: 'transparent',
              display: 'flex', alignItems: 'center', justifyContent: 'center', color: T.ink3,
            }}>
              <Icon name="x" size={15} stroke={2}/>
            </button>
          )}
        </div>

        <div style={{ padding: '16px 20px 20px' }}>
          {/* App identity */}
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 14 }}>
            <div style={{
              width: 44, height: 44, borderRadius: 11,
              background: app.bg || T.slate, color: 'white',
              display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0,
              boxShadow: 'inset 0 1px 0 rgba(255,255,255,0.3)',
            }}>
              <Icon name={app.icon || 'apps'} size={22} stroke={1.6}/>
            </div>
            <div style={{ minWidth: 0 }}>
              <div style={{ ...T.type.heading, color: T.ink }}>{app.name}</div>
              <div style={{ fontSize: 11.5, color: T.ink3, marginTop: 2 }} className="mono">
                {app.version || '—'} · {(app.runtime || 'kubernetes') === 'compose' ? 'Docker Compose' : 'Kubernetes'}
              </div>
            </div>
          </div>

          {/* Compose: remove-preview 明细 */}
          {isCompose && (
            <>
              <div style={{
                padding: 12, borderRadius: 8, marginBottom: 12,
                background: T.surfaceAlt, border: `1px solid ${T.borderSoft}`,
              }}>
                <div style={{ fontSize: 12, fontWeight: 700, color: T.ink, marginBottom: 8 }}>
                  {purge ? '将被删除' : '将被删除（仅容器/网络）'}
                </div>
                {loading ? (
                  <div style={{ fontSize: 11.5, color: T.ink3 }}>正在加载删除预览…</div>
                ) : willDelete.length > 0 ? (
                  <ul style={{ margin: 0, paddingLeft: 18, fontSize: 11.5, color: T.ink2, lineHeight: 1.7 }}>
                    {willDelete.map((d, i) => <li key={i} className="mono" style={{ wordBreak: 'break-all' }}>{d}</li>)}
                  </ul>
                ) : (
                  <div style={{ fontSize: 11.5, color: T.ink3 }}>容器与网络资源</div>
                )}

                <div style={{ fontSize: 12, fontWeight: 700, color: T.ink, margin: '10px 0 6px' }}>
                  将被保留（卸载不影响）
                </div>
                {willKeep.length > 0 ? (
                  <ul style={{ margin: 0, paddingLeft: 18, fontSize: 11.5, color: T.ink3, lineHeight: 1.7 }}>
                    {willKeep.map((d, i) => <li key={i} className="mono" style={{ wordBreak: 'break-all' }}>{d}</li>)}
                  </ul>
                ) : (
                  <div style={{ fontSize: 11.5, color: T.ink3 }}>无</div>
                )}
                {preview?.note && (
                  <div style={{ fontSize: 11, color: T.ink4, marginTop: 8, lineHeight: 1.5 }}>{preview.note}</div>
                )}
              </div>

              {/* Purge toggle */}
              <label style={{
                display: 'flex', alignItems: 'flex-start', gap: 10, marginBottom: 12, cursor: 'pointer',
                padding: 10, borderRadius: 8,
                background: purge ? '#fef2f2' : T.surfaceAlt,
                border: `1px solid ${purge ? '#fecaca' : T.borderSoft}`,
              }}>
                <input type="checkbox" checked={purge} disabled={busy}
                  onChange={(e) => { setLoading(true); setError(null); setPurge(e.target.checked); }}
                  style={{ marginTop: 2, width: 15, height: 15 }}/>
                <div>
                  <div style={{ fontSize: 12.5, fontWeight: 600, color: purge ? '#b91c1c' : T.ink }}>
                    同时删除受管数据（purge）
                  </div>
                  <div style={{ fontSize: 11, color: T.ink3, marginTop: 3, lineHeight: 1.5 }}>
                    删除受管命名卷与受管数据目录。<strong>外部卷(external)、宿主挂载(bind)、socket 永远不会被删除。</strong>
                  </div>
                </div>
              </label>
            </>
          )}

          {/* K8s: static bullets */}
          {!isCompose && (
            <div style={{
              padding: 12, borderRadius: 8, marginBottom: 12,
              background: T.surfaceAlt, border: `1px solid ${T.borderSoft}`,
              fontSize: 12, color: T.ink2, lineHeight: 1.65,
            }}>
              <strong style={{ color: T.ink }}>将会发生什么</strong>
              <ul style={{ margin: '6px 0 0', paddingLeft: 20, fontSize: 11.5, color: T.ink3 }}>
                <li>立即终止该 Pod，释放占用的 GPU / 内存 / 端口</li>
                <li>删除容器与配置</li>
              </ul>
            </div>
          )}

          {/* Name-typing confirm (k8s always; compose only when purge) */}
          {(isCompose ? purge : true) && (
            <div style={{ marginBottom: 12 }}>
              <div style={{ fontSize: 12, color: T.ink2, marginBottom: 6 }}>
                请输入应用名称 <span className="mono" style={{
                  background: T.surfaceAlt, padding: '1px 6px', borderRadius: 3,
                  color: T.ink, fontWeight: 600, border: `1px solid ${T.border}`,
                }}>{app.name}</span> 确认：
              </div>
              <input value={typed} disabled={busy} autoFocus onChange={(e) => setTyped(e.target.value)}
                placeholder={app.name} aria-label="输入应用名称以确认"
                style={{
                  width: '100%', height: 38, padding: '0 12px',
                  border: `1px solid ${nameMatches ? T.red : T.border}`, borderRadius: 7,
                  fontSize: 13, color: T.ink, background: 'white', outline: 'none',
                  boxShadow: nameMatches ? `0 0 0 3px ${T.red}22` : 'none',
                  boxSizing: 'border-box',
                }} className="mono"/>
            </div>
          )}

          {/* Restore-data caveat for purge */}
          {isCompose && purge && (
            <div style={{
              padding: '8px 10px', marginBottom: 12, borderRadius: 7,
              background: '#fffbeb', border: '1px solid #fde68a',
              fontSize: 11.5, color: '#92400e', lineHeight: 1.5,
              display: 'flex', gap: 8, alignItems: 'flex-start',
            }}>
              <Icon name="alertTri" size={13} stroke={1.8} style={{ color: '#b45309', marginTop: 1, flexShrink: 0 }}/>
              <div>删除受管数据不可恢复。若该应用持有数据库等内容，删除后无法找回。</div>
            </div>
          )}

          {displayError && (
            <div style={{
              padding: '8px 10px', marginBottom: 12, borderRadius: 7,
              background: '#fef2f2', border: '1px solid #fecaca',
              fontSize: 11.5, color: '#b91c1c',
            }}>{displayError}</div>
          )}
          {trackTask && taskId && !displayError && (
            <div style={{ padding: '8px 10px', marginBottom: 12, borderRadius: 7, background: '#e6f4ff', border: '1px solid #99c7ff', fontSize: 11.5, color: '#0043b8' }}>
              卸载任务 {task?.status || 'queued'} · {task?.phase || 'queued'}{task?.message ? ` · ${task.message}` : ''}
            </div>
          )}

          {/* Actions */}
          <div style={{ display: 'flex', gap: 8 }}>
            <button onClick={onClose} disabled={busy} style={{
              flex: 1, height: 38, borderRadius: 8,
              background: 'white', border: `1px solid ${T.border}`,
              fontSize: 13, fontWeight: 500, color: T.ink2, cursor: busy ? 'not-allowed' : 'pointer',
            }}>取消</button>
            <button onClick={onConfirm} disabled={!canConfirm} style={{
              flex: 1.4, height: 38, borderRadius: 8,
              background: canConfirm ? T.red : '#fca5a5', color: 'white', border: 'none',
              fontSize: 13, fontWeight: 600, cursor: canConfirm ? 'pointer' : 'not-allowed',
              display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 6,
              opacity: canConfirm ? 1 : 0.7,
            }}>
              <Icon name="trash" size={13} stroke={2}/>
              {busy ? '卸载中…' : purge ? '确认删除数据并卸载' : '我已了解，卸载'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
