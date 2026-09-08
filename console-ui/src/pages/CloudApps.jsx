// CloudApps — 「云端应用」独立页（容器域 IA 重组 T2，LF 裁决：K8s 应用独立入口）。
//
// 展示 runtime=kubernetes 的已部署应用（云端下发，前端无新建入口）。
// 操作语义严格对齐 T1 前 ComposeManager 对 K8s 应用实际提供过的能力
// （git show main:console-ui/src/pages/ComposeManager.jsx AppCard 分支考证）：
//   - 生命周期：启动 / 停止 / 重启（appActionAsync；「重部署」是 compose 专属，K8s 无）
//   - 卸载：UninstallDialog 的 kubernetes 分支（输入应用名确认 + purge=false）
//   - 详情（onOpenApp）、endpoints 打开入口、catalog 升级徽标、最近 operation
// 不新造写路径：新建 / 接管 / 预检向导均为 compose 专属，此页没有。
import { useState, useEffect, useMemo } from 'react';
import { T } from '../tokens';
import { Icon } from '../icons';
import { useToast } from '../components/toastContext';
import UninstallDialog from '../components/UninstallDialog';
import { AppCard, TaskBanner } from './ComposeManager';
import { useApps, useTask, appActionAsync, useStoreApps, useCatalogApps } from '../hooks/useApi';

export default function CloudApps({ authed, onRequireAuth, onOpenApp }) {
  const { data: apps, refresh } = useApps(5000);
  const { data: storeApps } = useStoreApps();
  const { data: catalogApps } = useCatalogApps();
  const [activeTask, setActiveTask] = useState(null); // {id, label, appId}
  const [uninstall, setUninstall] = useState(null);   // app | null
  const toast = useToast();

  const { task } = useTask(activeTask?.id);
  useEffect(() => {
    if (!task || !activeTask) return;
    if (['succeeded', 'failed', 'canceled', 'superseded'].includes(task.status)) {
      if (task.status === 'succeeded') toast.ok(`${activeTask.label}完成`);
      else toast.err(`${activeTask.label}失败：${task.message || task.status}`);
      const t = setTimeout(() => setActiveTask(null), 1500);
      refresh();
      return () => clearTimeout(t);
    }
  }, [task, activeTask, toast, refresh]);

  // runtime 缺省按 kubernetes 处理（与后端 pkg/apps 缺省一致，口径同原 ComposeManager）。
  const arr = useMemo(() => Array.isArray(apps) ? apps : [], [apps]);
  const list = useMemo(() => arr.filter((a) => (a.runtime || 'kubernetes') === 'kubernetes'), [arr]);

  function guard(fn) {
    return (...args) => {
      if (!authed) { onRequireAuth?.(); return; }
      return fn(...args);
    };
  }

  async function doAction(appId, action, label) {
    try {
      const t = await appActionAsync(appId, action);
      setActiveTask({ id: t.id, label, appId });
      toast.ok(`${label}已提交`);
    } catch (e) { toast.err(`${label}失败：${e.message}`); }
  }

  return (
    <div style={{ padding: 24, height: '100%', overflow: 'auto', background: T.bg }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
        <div style={{ fontSize: 20, fontWeight: 700, color: T.ink }}>云端应用</div>
        <div style={{ fontSize: 12, color: T.ink3 }}>Kubernetes · 由云端下发</div>
        <span className="mono tnum" style={{ fontSize: 12, color: T.ink3, padding: '2px 9px', borderRadius: 999, border: `1px solid ${T.border}`, background: '#fff' }}>
          {list.length} 个应用
        </span>
      </div>

      {activeTask && task && <TaskBanner task={task} label={activeTask.label} />}

      <div style={{ display: 'grid', gap: 10, marginTop: 14 }}>
        {list.length === 0 && (
          <div style={{ padding: 36, textAlign: 'center', color: T.ink3, fontSize: 13,
            border: `1px dashed ${T.border}`, borderRadius: 12, background: '#fff' }}>
            <Icon name="cloud" size={30} stroke={1.5} style={{ color: T.ink4, marginBottom: 8 }}/>
            <div style={{ fontSize: 14, fontWeight: 600, color: T.ink2 }}>暂无云端应用</div>
            <div style={{ fontSize: 12, color: T.ink3, marginTop: 6 }}>
              Kubernetes 应用由云端下发后显示在此，本地不提供新建入口。
            </div>
          </div>
        )}

        {list.map((app) => (
          <AppCard
            key={app.id}
            app={app}
            storeApps={storeApps}
            catalogApps={catalogApps}
            disabled={!!activeTask}
            onAction={(action, label) => guard(doAction)(app.id, action, label)}
            onUninstall={() => setUninstall(app)}
            onOpenApp={onOpenApp}
          />
        ))}
      </div>

      {uninstall && (
        <UninstallDialog
          app={uninstall}
          onClose={() => setUninstall(null)}
          onDone={(taskId) => {
            setUninstall(null);
            if (taskId) setActiveTask({ id: taskId, label: '卸载', appId: uninstall.id });
            else { toast.ok('已卸载'); refresh(); }
          }}
        />
      )}
    </div>
  );
}
