// AppManagement — 「应用管理」独立页（容器域 IA 重组 T2；LF 拍板命名：应用管理，
// appId: app-management）。
//
// 展示 runtime=kubernetes 的已部署应用（云端下发，前端无新建入口）。
// Compose 应用不在此页——由「Docker」应用的 Compose tab 管理（DockerApp.jsx），
// 页内提供提示跳转避免歧义。与应用商店的边界保持现状：安装/升级是商店职责，
// 本页只做已部署应用的状态与生命周期。
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

export default function AppManagement({ authed, onRequireAuth, onOpenApp }) {
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
        <div style={{ fontSize: 20, fontWeight: 700, color: T.ink }}>应用管理</div>
        <div style={{ fontSize: 12, color: T.ink3 }}>Kubernetes · 由云端下发</div>
        <span className="mono tnum" style={{ fontSize: 12, color: T.ink3, padding: '2px 9px', borderRadius: 999, border: `1px solid ${T.border}`, background: '#fff' }}>
          {list.length} 个应用
        </span>
      </div>

      {/* 命名歧义提示：Compose 应用不在此页，经 onOpenApp 跳转 Docker 应用的 Compose tab
          （复用 T1 的 launchApp → resolveAppLaunch/appLaunchTabs 机制）。 */}
      <div style={{ marginTop: 12, padding: '8px 12px', borderRadius: 8, background: T.blueSoft,
        border: '1px solid #99c7ff', fontSize: 12, color: T.ink2,
        display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
        <Icon name="info" size={13} style={{ color: T.blue, flexShrink: 0 }}/>
        <span>本地 Compose 应用在「Docker」应用中管理。</span>
        <button onClick={() => onOpenApp?.({ id: 'docker', tab: 'compose' })}
          className="edge-press"
          style={{ display: 'inline-flex', alignItems: 'center', gap: 4, padding: '3px 10px',
            borderRadius: 6, border: '1px solid #99c7ff', background: '#fff', color: T.blue,
            fontSize: 12, fontWeight: 600, cursor: 'pointer' }}>
          <Icon name="apps" size={11} stroke={2}/>打开 Compose 应用
        </button>
      </div>

      {activeTask && task && <TaskBanner task={task} label={activeTask.label} />}

      <div style={{ display: 'grid', gap: 10, marginTop: 14 }}>
        {list.length === 0 && (
          <div style={{ padding: 36, textAlign: 'center', color: T.ink3, fontSize: 13,
            border: `1px dashed ${T.border}`, borderRadius: 12, background: '#fff' }}>
            <Icon name="cloud" size={30} stroke={1.5} style={{ color: T.ink4, marginBottom: 8 }}/>
            <div style={{ fontSize: 14, fontWeight: 600, color: T.ink2 }}>暂无应用</div>
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
