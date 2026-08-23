import { useState, useEffect, useRef, useCallback } from 'react';

const API = '/api/v1';

// ─── Auth token management ──────────────────────────────────────

const TOKEN_KEY = 'edge_token';
let _token = localStorage.getItem(TOKEN_KEY) || sessionStorage.getItem(TOKEN_KEY) || '';
let _authRequired = true;
let _expiryNotified = false;

export function setAuthToken(t, persistence = 'persistent') {
	_token = t || '';
	localStorage.removeItem(TOKEN_KEY);
	sessionStorage.removeItem(TOKEN_KEY);
	if (t) {
		const storage = persistence === 'session' ? sessionStorage : localStorage;
		storage.setItem(TOKEN_KEY, t);
	}
	_expiryNotified = false;
}

export function getAuthToken() { return _token; }

export function setAuthRequired(required) {
	_authRequired = required;
}

export function clearAuth() {
	_token = '';
	localStorage.removeItem(TOKEN_KEY);
	sessionStorage.removeItem(TOKEN_KEY);
}

function authHeaders() {
  return _token ? { Authorization: 'Bearer ' + _token } : {};
}

let _onAuthExpired = null;
export function setOnAuthExpired(cb) { _onAuthExpired = cb; }

// authFetch 通用 fetch wrapper —— 自动注入 Bearer token；认证中间件返回 401 时
// 立即清理会话。通知只触发一次，避免多个轮询请求同时过期造成登录页回跳循环。
// 凡是调 /api/v1/* 受保护接口的页面必须用这个，不要用 raw fetch
// （raw fetch 会被 middleware 拦 401 + 页面空白；见 2026-06-22 进程/磁盘/网络/告警/设置 5 页空白事件）
export async function authFetch(url, opts = {}) {
  // 未登录短路：App.jsx 里的 hooks (useMetrics/useApps/…) 写在 early-return 之前，
  // 登录前会先跑一遍。这时不发网络，返合成 401 让 usePoll 走 error 分支即可，
  // 避免登录页/初始加载在 devtool 里堆一片 401 噪音。
	if (!_token && _authRequired) {
		return new Response(null, { status: 401, statusText: 'no token (pre-login)' });
	}
	const resp = await fetch(url, { ...opts, headers: { ...authHeaders(), ...opts.headers } });
	if (resp.status === 401 && _token && !_expiryNotified) {
		_expiryNotified = true;
		clearAuth();
		if (_onAuthExpired) _onAuthExpired();
	}
  return resp;
}

// ─── Internal helpers ────────────────────────────────────────────

function guessIcon(name) {
  const n = name.toLowerCase();
  if (n.includes('code') || n.includes('vscode')) return 'code';
  if (n.includes('jupyter')) return 'jupyter';
  if (n.includes('ollama')) return 'ollama';
  if (n.includes('vllm')) return 'vllm';
  if (n.includes('comfy')) return 'comfy';
  if (n.includes('webui') || n.includes('sd')) return 'palette';
  if (n.includes('open-webui') || n.includes('openwebui')) return 'openwebui';
  if (n.includes('train')) return 'flame';
  return 'apps';
}

function formatSize(bytes) {
  if (bytes >= 1e12) return (bytes / 1e12).toFixed(1) + ' TB';
  if (bytes >= 1e9) return (bytes / 1e9).toFixed(1) + ' GB';
  if (bytes >= 1e6) return (bytes / 1e6).toFixed(1) + ' MB';
  if (bytes >= 1e3) return (bytes / 1e3).toFixed(1) + ' KB';
  return bytes + ' B';
}

// ─── Generic polling hook ────────────────────────────────────────

function usePoll(url, { interval = 0, fallback = null, transform } = {}) {
  const [data, setData] = useState(fallback);
  const [loading, setLoading] = useState(() => !!url);
  const [error, setError] = useState(null);
  const [tick, setTick] = useState(0);
  const mountedRef = useRef(true);
  const refresh = useCallback(() => {
    setLoading(true);
    setTick(t => t + 1);
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    let timer = null;
    let inFlight = false;

    if (!url) {
      return () => { mountedRef.current = false; };
    }

    async function doFetch() {
      if (inFlight) return;
      inFlight = true;
      try {
        const r = await authFetch(API + url);
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        const json = await r.json();
        if (!mountedRef.current) return;
        setData((previous) => transform ? transform(json, previous) : json);
        setError(null);
      } catch (err) {
        if (!mountedRef.current) return;
        setError(err);
        // keep previous data (or fallback) on error
      } finally {
        inFlight = false;
        if (mountedRef.current) setLoading(false);
      }
    }

    doFetch();

    if (interval > 0) {
      timer = setInterval(doFetch, interval);
    }

    return () => {
      mountedRef.current = false;
      if (timer) clearInterval(timer);
    };
  }, [url, interval, tick]); // eslint-disable-line react-hooks/exhaustive-deps

  return { data, loading, error, refresh };
}

// ─── Device ──────────────────────────────────────────────────────

export function useDevice() {
  return usePoll('/device', {
    fallback: null,
    transform: (d) => ({
      // deviceName 是 K8s node 注册名 (cfg.Device.Name=edge-004)
      // hostname 是 OS hostname (edge-probe-node)，可能不同
      deviceName: d.deviceName,
      hostname: d.hostname,
      // name 兼容旧调用方，优先 deviceName
      name: d.deviceName || d.hostname,
      alias: d.deviceName || d.hostname,
      os: d.platform || d.os,
      model: d.cpuModel,
      uptime: d.uptimeHuman,
      agent: 'Edge Platform ' + (d.agentVersion || ''),
      sn: d.hostname,
      site: '',
      dept: '',
      username: '',
    }),
  });
}

// ─── Metrics (current) ──────────────────────────────────────────

export function useMetrics(interval = 5000) {
  const [data, setData] = useState({ metrics: null, gpus: [] });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;

    async function doFetch() {
      try {
        const r = await authFetch(API + '/metrics');
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        const m = await r.json();
        if (!mountedRef.current) return;

        const prev = data.metrics || {};
        const metrics = {
          cpu: { used: Math.round(m.cpuUsedPercent), total: 100, series: prev.cpu?.series || [] },
          gpu: {
            used: m.gpuData && m.gpuData.length > 0 ? Math.round(m.gpuData[0].gpuUtil) : 0,
            total: 100, series: prev.gpu?.series || [],
          },
          mem: {
            used: Math.round(m.memoryUsed / (1024 * 1024 * 1024)),
            total: Math.round(m.memoryTotal / (1024 * 1024 * 1024)),
            pct: Math.round(m.memoryUsedPercent), series: prev.mem?.series || [],
          },
          dsk: prev.dsk || { used: 0, total: 0, pct: 0, series: [] },
        };
        if (m.diskData && m.diskData.length > 0) {
          const d = m.diskData[0];
          metrics.dsk = {
            used: Math.round(d.used / (1024 * 1024 * 1024)),
            total: Math.round(d.total / (1024 * 1024 * 1024)),
            pct: Math.round(d.usedPercent), series: prev.dsk?.series || [],
          };
        }

        let gpus = data.gpus;
        if (m.gpuData && m.gpuData.length > 0) {
          gpus = m.gpuData.map((g) => ({
            id: g.index,
            model: g.productName,
            util: Math.round(g.gpuUtil),
            memUsed: g.memUsed,
            memTotal: g.memTotal,
            temp: g.temperature,
            power: g.powerDraw,
            max: g.powerLimit,
            tasks: [],
          }));
        }

        setData({ metrics, gpus });
        setError(null);
      } catch (err) {
        if (!mountedRef.current) return;
        setError(err);
      } finally {
        if (mountedRef.current) setLoading(false);
      }
    }

    doFetch();
    const timer = setInterval(doFetch, interval);

    return () => {
      mountedRef.current = false;
      clearInterval(timer);
    };
  }, [interval]); // eslint-disable-line react-hooks/exhaustive-deps

  return { data, loading, error };
}

// ─── Metrics history (series) ────────────────────────────────────

export function useMetricsHistory(interval = 5000) {
  const [data, setData] = useState({
    cpuPercent: [],
    memoryPercent: [],
    diskPercent: [],
    gpuUtil: [],
  });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;

    async function doFetch() {
      try {
        const r = await authFetch(API + '/metrics/history');
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        const h = await r.json();
        if (!mountedRef.current) return;
        setData((prev) => ({
          cpuPercent: h.cpuPercent || prev.cpuPercent,
          memoryPercent: h.memoryPercent || prev.memoryPercent,
          diskPercent: h.diskPercent || prev.diskPercent,
          gpuUtil: h.gpuUtil || prev.gpuUtil,
          gpuMemPercent: h.gpuMemPercent || prev.gpuMemPercent,
          netBytesSent: h.netBytesSent || prev.netBytesSent,
          netBytesRecv: h.netBytesRecv || prev.netBytesRecv,
          timestamps: h.timestamps || prev.timestamps,
        }));
        setError(null);
      } catch (err) {
        if (!mountedRef.current) return;
        setError(err);
      } finally {
        if (mountedRef.current) setLoading(false);
      }
    }

    doFetch();
    const timer = setInterval(doFetch, interval);

    return () => {
      mountedRef.current = false;
      clearInterval(timer);
    };
  }, [interval]);

  return { data, loading, error };
}

// ─── Apps ────────────────────────────────────────────────────────

export function useApps(interval = 10000, enabled = true) {
	return usePoll(enabled ? '/apps' : null, {
    interval,
    fallback: [],
    transform: (apps) => {
      if (!Array.isArray(apps)) return [];
      return apps.map((a) => ({
        id: a.id,
        kind: 'app',
        // runtime 由后端提供（compose | kubernetes）；旧 K8s app 默认 kubernetes。
        runtime: a.runtime || 'kubernetes',
        name: a.name,
        icon: guessIcon(a.name),
        color: '#3b82f6',
        bg: 'linear-gradient(160deg,#3b82f6,#1d4ed8)',
        state: a.state,
        version: a.version || 'latest',
        category: '',
        gpu: 'CPU',
        gpuPct: 0,
        desc: a.image,
        ports: a.ports || [],
        deployedAt: a.createdAt ? new Date(a.createdAt).toLocaleString('zh-CN') : '',
        image: a.image,
        namespace: a.namespace,
        replicas: a.replicas,
        ready: a.ready,
        createdAt: a.createdAt,
        containerID: a.containerID || '',
        restartCount: a.restartCount || 0,
        restartPolicy: a.restartPolicy || '',
        volumeMounts: a.volumeMounts || [],
        podName: a.podName || '',
        nodeName: a.nodeName || '',
        startedAt: a.startedAt,
        cpuRequest: a.cpuRequest || '',
        cpuLimit: a.cpuLimit || '',
        memRequest: a.memRequest || '',
        memLimit: a.memLimit || '',
        // 后端 phase / runtime 透传（权威值，前端不自行推断）。
        //   - observed.phase        应用聚合状态（running/degraded/...）
        //   - observed.services[]   服务级运行态（compose=container / k8s=replica）
        //   - observed.endpoints[]  对外入口（url）
        //   - observed.message      诊断/错误摘要
        //   - source{kind,storeId,version,catalogId}  来源与版本（升级/来源筛选）
        //   - revision              desired generation（乐观并发 / 版本 Tab）
        //   - lastTask              最近一次操作（列表「最近 operation」提示）
        observed: a.observed || null,
        source: a.source || null,
        revision: a.revision || 0,
        lastTask: a.lastTask || null,
        // 系统 Compose project 自动发现与接管：ownership(managed/discovered) + discovered 诊断。
        // discovered 为只读，ComposeManager 据此渲染 DiscoveredCard（隐藏全部写操作）。
        ownership: a.ownership || '',
        discovered: a.discovered || null,
      }));
    },
  });
}

// ─── Docker overview / host runtime settings ───────────────────

export function useDockerOverview(interval = 5000) {
  return usePoll('/docker/overview', { interval, fallback: null });
}

export function useDockerStats(interval = 3000) {
  const empty = { available: false, cpuPercent: 0, memoryUsageBytes: 0, memoryLimitBytes: 0,
    networkRxBytes: 0, networkTxBytes: 0, containers: 0 };
  return usePoll('/docker/stats', {
    interval,
    fallback: { current: empty, history: { cpu: [], memory: [], rx: [], tx: [], times: [] } },
    transform: (sample, previous) => {
      const prior = previous?.history || { cpu: [], memory: [], rx: [], tx: [], times: [] };
      if (!sample?.available || !sample.sampledAt || prior.times.at(-1) === sample.sampledAt) {
        return { current: sample || empty, history: prior };
      }
      const trim = (values, value) => [...values, value].slice(-24);
      const memory = sample.memoryLimitBytes > 0 ? sample.memoryUsageBytes / sample.memoryLimitBytes * 100 : 0;
      return { current: sample, history: {
        cpu: trim(prior.cpu, sample.cpuPercent || 0), memory: trim(prior.memory, memory),
        rx: trim(prior.rx, sample.networkRxBytes || 0), tx: trim(prior.tx, sample.networkTxBytes || 0),
        times: trim(prior.times, sample.sampledAt),
      } };
    },
  });
}

export async function dockerServiceAction(action) {
  const r = await authFetch(`${API}/docker/service`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action }),
  });
  if (!r.ok) throw await readErr(r);
  return r.json();
}

export async function setDockerAutostart(enabled) {
  const r = await authFetch(`${API}/docker/autostart`, {
    method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ enabled: !!enabled }),
  });
  if (!r.ok) throw await readErr(r);
  return r.json();
}

export async function planDockerMigration(targetPath) {
  const r = await authFetch(`${API}/docker/storage/plan`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ targetPath }),
  });
  if (!r.ok) throw await readErr(r);
  return r.json();
}

export async function executeDockerMigration(targetPath, planId) {
  const r = await authFetch(`${API}/docker/storage/execute`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ targetPath, planId, confirm: true }),
  });
  if (!r.ok) throw await readErr(r);
  return r.json();
}

// ─── App detail (含 env + deployValues，AppMgmtDrawer 打开时拉一次) ───
//
// 跟 useApps 区别：单次拉取（不轮询），按 appID 变化触发，drawer 关闭传
// null/'' 跳过 fetch。后端 GET /api/v1/apps/{id} 比 ListApps 多调 N+1 K8s
// API（Secret/ConfigMap GET），故不进列表流程。
export function useAppDetail(appID) {
  const url = appID ? `/apps/${encodeURIComponent(appID)}` : null;
  return usePoll(url, { interval: 0, fallback: null });
}

// ─── Alerts ──────────────────────────────────────────────────────

export function useAlerts(interval = 30000) {
  return usePoll('/alerts', {
    interval,
    fallback: [],
    transform: (a) => (Array.isArray(a) ? a : []),
  });
}

// ─── Ports ───────────────────────────────────────────────────────

export function usePorts() {
  return usePoll('/ports', {
    fallback: [],
    transform: (p) => {
      if (!Array.isArray(p)) return [];
      return p.map((x) => ({
        id: x.name || x.app,
        app: x.app,
        port: x.port,
        proto: x.protocol || 'TCP',
        state: x.state === 'running' ? 'lan' : 'offline',
        url: 'http://localhost:' + x.port,
        traffic: 0,
        since: '',
        auth: 'none',
      }));
    },
  });
}

// ─── Models ──────────────────────────────────────────────────────

export function useModels() {
  return usePoll('/models', {
    fallback: [],
    transform: (m) => (Array.isArray(m) ? m : []),
  });
}

// ─── Files ───────────────────────────────────────────────────────

export function useFiles(path = '/workspace') {
  const url = '/files?path=' + encodeURIComponent(path);

  return usePoll(url, {
    fallback: [],
    transform: (f) => {
      if (!Array.isArray(f)) return [];
      return f.map((e) => ({
        name: e.name,
        type: e.isDir ? 'dir' : e.type,
        size: e.isDir ? '' : formatSize(e.size),
        count: e.count || 0,
        modified: e.modified ? new Date(e.modified).toLocaleDateString('zh-CN') : '',
      }));
    },
  });
}

// ─── Network ─────────────────────────────────────────────────────

export function useNetwork() {
  return usePoll('/network', {
    fallback: null,
  });
}

// ─── App logs (polling) ─────────────────────────────────────────

export function useAppLogs(appId, interval = 3000, tail = 200, service = '') {
  const [lines, setLines] = useState([]);
  const [loading, setLoading] = useState(() => !!appId);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    if (!appId) return () => { mountedRef.current = false; };

    async function doFetch() {
      try {
        const q = new URLSearchParams({ tail: String(tail) });
        if (service) q.set('service', service);
        const r = await authFetch(`${API}/apps/${encodeURIComponent(appId)}/logs?${q}`);
        if (!r.ok) return;
        const d = await r.json();
        if (!mountedRef.current) return;
        const text = d ? (d.logs || '') : '';
        setLines(text ? text.split('\n').filter(Boolean) : []);
      } catch { /* keep previous */ }
      finally { if (mountedRef.current) setLoading(false); }
    }

    doFetch();
    const timer = setInterval(doFetch, interval);
    return () => { mountedRef.current = false; clearInterval(timer); };
  }, [appId, interval, tail, service]);

  return { lines, loading };
}

// ─── App versions ──────────────────────────────────────────────

export function useAppVersions(appName) {
  return usePoll(appName ? `/apps/${appName}/versions` : null, {
    fallback: null,
  });
}

export async function switchAppVersion(appName, version) {
  const r = await authFetch(`${API}/apps/${appName}/versions/switch`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ version }),
  });
  if (!r.ok) {
    const text = await r.text();
    throw new Error(text || `HTTP ${r.status}`);
  }
  return r.json().catch(() => null);
}

// ─── Imperative API helpers ──────────────────────────────────────

export async function appOp(appId, operation) {
  const r = await authFetch(`${API}/apps/${appId}/${operation}`, { method: 'POST' });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return r.json().catch(() => null);
}

export async function deleteApp(appId) {
  const r = await authFetch(`${API}/apps/${appId}`, { method: 'DELETE' });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return r.json().catch(() => null);
}

// devbox-only: 本地进程管理入口，edge-ota 侧无此能力
export function useSupervisor(interval = 5000) {
  return usePoll('/supervisor/status', { interval, fallback: null });
}

// devbox-only: libvirt 虚拟机清单与状态。
export function useVirtualMachines(interval = 5000) {
  return usePoll('/vms', { interval, fallback: [] });
}

// devbox-only: 硬件清单快照。后端 60s cache，前端 30s 轻量轮询就够了。
export function useHardware(interval = 30000) {
  return usePoll('/hardware', { interval, fallback: null });
}

// 传感器实时数据 (温度/风扇/RAPL 功耗)。后端每次调用采样 ~200ms，
// 前端 3s 轮询：既让读数活起来又不至于把 CPU 压力放上去。
export function useSensors(interval = 3000) {
  return usePoll('/hardware/sensors', { interval, fallback: null });
}

// 服务导航清单。后端 5s 会回读 supervisor 状态覆盖 badge，
// 前端 5s 轮询就够了 —— 静态段基本不变。
export function useLinks(interval = 5000) {
  return usePoll('/links', { interval, fallback: null });
}

// 本机 AI 活动快照：Claude Code / Codex 进程、Claude daemon worker、近期会话与限流事件。
export function useAIActivity(interval = 5000) {
  return usePoll('/ai/activity', { interval, fallback: null });
}

// 只读 transcript tail。后端做路径白名单，前端只负责展示。
export function useAITranscript(path, tail = 200, interval = 3000) {
  const n = Math.max(1, Math.min(Number(tail) || 200, 1000));
  const url = path ? `/ai/transcript?path=${encodeURIComponent(path)}&tail=${n}` : null;
  return usePoll(url, { interval, fallback: null });
}

export async function cleanupStaleCodex() {
  const r = await authFetch(`${API}/ai/codex/cleanup-stale`, { method: 'POST' });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return r.json().catch(() => null);
}

export async function supervisorControl(name, action) {
  const r = await authFetch(`${API}/supervisor/services/${name}/control`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ action }),
  });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return r.json().catch(() => null);
}

export async function vmControl(name, action) {
  const r = await authFetch(`${API}/vms/${encodeURIComponent(name)}/control`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ action }),
  });
  if (!r.ok) {
    const text = await r.text();
    throw new Error(text || `HTTP ${r.status}`);
  }
  return r.json().catch(() => null);
}

export async function vmConfigure(name, config) {
  const r = await authFetch(`${API}/vms/${encodeURIComponent(name)}/config`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  });
  if (!r.ok) {
    const text = await r.text();
    throw new Error(text || `HTTP ${r.status}`);
  }
  return r.json().catch(() => null);
}

export async function supervisorLogs(name) {
  try {
    const r = await authFetch(`${API}/supervisor/services/${name}/logs`);
    if (!r.ok) return { name, log: '' };
    return await r.json();
  } catch {
    return { name, log: '' };
  }
}

export async function getLogs(appId, tail = 100) {
  try {
    const r = await authFetch(`${API}/apps/${appId}/logs?tail=${tail}`);
    if (!r.ok) return '';
    const d = await r.json();
    return d ? d.logs : '';
  } catch {
    return '';
  }
}

// ─── Browser 应用：书签 / 历史 ───────────────────────────────────
// 后端持久化在 /etc/devbox/browser.json（单机单用户一份）。增删后手动调
// 返回的 refresh() 重拉（不轮询——浏览器交互是用户驱动的，不需要定时刷）。

export function useBookmarks() {
  return usePoll('/browser/bookmarks', { fallback: [] });
}

export function useHistory() {
  return usePoll('/browser/history', { fallback: [] });
}

export async function addBookmark(title, url) {
  const r = await authFetch(`${API}/browser/bookmarks`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title, url }),
  });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return r.json().catch(() => null);
}

export async function removeBookmark(id) {
  const r = await authFetch(`${API}/browser/bookmarks/${encodeURIComponent(id)}`, { method: 'DELETE' });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return r.json().catch(() => null);
}

// 记录访问：fire-and-forget，失败不影响导航
export async function addHistory(url, title) {
  try {
    await authFetch(`${API}/browser/history`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url, title }),
    });
  } catch { /* ignore */ }
}

export async function clearHistory() {
  const r = await authFetch(`${API}/browser/history`, { method: 'DELETE' });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return r.json().catch(() => null);
}

// 探测目标 URL 能否被 iframe 直连（后端 HEAD 检测 X-Frame-Options / CSP frame-ancestors）。
// 导航前调：能直连就 iframe 直连（快、无副作用），检测到拦截头才走代理。
// 任何探测失败/异常默认 direct=true（交回前端直连，由浏览器自然报错）。
export async function probeDirectEmbed(url) {
  try {
    const r = await authFetch(`${API}/browser/probe?url=${encodeURIComponent(url)}`);
    if (!r.ok) return { direct: true, reason: 'probe-failed' };
    const d = await r.json();
    return d && typeof d.direct === 'boolean' ? d : { direct: true, reason: 'unknown' };
  } catch {
    return { direct: true, reason: 'error' };
  }
}

// ─── Docker Compose 应用管理（Issue #2） ──────────────────────────
//
// 写操作统一返回 Task（202）。前端提交后用 useTask 轮询进度。
// 兼容旧 action（appOp）与旧 delete（deleteApp）保留；以下为新异步 API。

export function useAppCapability(interval = 15000) {
  return usePoll('/apps/capability', { interval, fallback: null });
}

// useTask 轮询单个任务到终态后停止。
export function useTask(taskId, interval = 1500) {
  const [task, setTask] = useState(null);
  const [loading, setLoading] = useState(true);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    if (!taskId) return () => { mountedRef.current = false; };
    let timer = null;
    async function poll() {
      let terminal = false;
      try {
        const r = await authFetch(`${API}/tasks/${encodeURIComponent(taskId)}`);
        if (!r.ok) return;
        const t = await r.json();
        if (!mountedRef.current) return;
        setTask(t);
        if (t.status && ['succeeded', 'failed', 'canceled', 'superseded'].includes(t.status)) {
          terminal = true;
          if (timer) clearInterval(timer);
          setLoading(false);
          return;
        }
      } catch { /* keep */ }
      finally { if (mountedRef.current) setLoading(false); }
      return terminal;
    }
    poll().then((terminal) => {
      if (!terminal && mountedRef.current) timer = setInterval(poll, interval);
    });
    return () => { mountedRef.current = false; if (timer) clearInterval(timer); };
  }, [taskId, interval]);

  const currentTask = taskId && task?.id === taskId ? task : null;
  return { task: currentTask, loading: taskId ? loading : false };
}

// useAppOperations 轮询某应用的最近操作历史。
export function useAppOperations(appId, interval = 4000) {
  const url = appId ? `/apps/${encodeURIComponent(appId)}/operations` : null;
  return usePoll(url, { interval, fallback: [] });
}

export function useAppRevisions(appId) {
  const url = appId ? `/apps/${encodeURIComponent(appId)}/revisions` : null;
  return usePoll(url, { interval: 0, fallback: [] });
}

// readErr 把后端统一错误信封 {"error","reason","findings"} 解析成带结构的 Error。
// 返回的 Error 携带：
//   .status   HTTP 状态码（409 冲突 / 422 风险阻断 / 502 catalog 不可达 / ...）
//   .reason   机器可读原因码（revision_mismatch / idempotency_conflict /
//             risk_blocked / catalog_unreachable / not_installable / validation_failed / ...）
//   .findings 风险项数组（仅 risk_blocked；脱敏，无 secret/compose 正文）
// 调用方 `throw await readErr(r)`；UI 据此分流（如 409 提示重新加载而非静默覆盖）。
async function readErr(r) {
  const t = await r.text().catch(() => '');
  const err = new Error(`HTTP ${r.status}`);
  err.status = r.status;
  err.reason = '';
  err.findings = null;
  if (t) {
    try {
      const j = JSON.parse(t);
      if (j && typeof j.error === 'string') {
        err.message = j.error;
        if (typeof j.reason === 'string') err.reason = j.reason;
        if (j.detail) err.detail = j.detail;
        if (Array.isArray(j.findings)) err.findings = j.findings;
        return err;
      }
    } catch { /* 非 JSON，按纯文本返回 */ }
    err.message = t.length > 500 ? t.slice(0, 500) + '…' : t;
    return err;
  }
  return err;
}

// 预检（不落盘）。
export async function validateCompose(req) {
  const r = await authFetch(`${API}/apps/validate`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(req),
  });
  if (!r.ok) throw await readErr(r);
  return r.json();
}

// 创建/更新 inline Compose（202 + Task）。
export async function applyComposeApp(desired, idempotencyKey) {
  const headers = { 'Content-Type': 'application/json' };
  if (idempotencyKey) headers['Idempotency-Key'] = idempotencyKey;
  const isUpdate = !!desired.id;
  const r = await authFetch(`${API}/apps${isUpdate ? '/' + encodeURIComponent(desired.id) : ''}`, {
    method: isUpdate ? 'PUT' : 'POST', headers, body: JSON.stringify(desired),
  });
  if (!r.ok) throw await readErr(r);
  return r.json();
}

// 异步生命周期（202 + Task）。
export async function appActionAsync(appId, action) {
  const r = await authFetch(`${API}/apps/${encodeURIComponent(appId)}/actions/${action}`, { method: 'POST' });
  if (!r.ok) throw await readErr(r);
  return r.json();
}

// 卸载（兼容同步；purge=true 删除受管数据，external 永不删）。
export async function removeAppEx(appId, purge = false) {
  const r = await authFetch(`${API}/apps/${encodeURIComponent(appId)}${purge ? '?purge=true' : ''}`, { method: 'DELETE' });
  if (!r.ok) throw await readErr(r);
  return r.json();
}

// 回滚到历史 revision（202 + Task）。
export async function restoreAppRevision(appId, rev) {
  const r = await authFetch(`${API}/apps/${encodeURIComponent(appId)}/revisions/${rev}/restore`, { method: 'POST' });
  if (!r.ok) throw await readErr(r);
  return r.json();
}

// 接管 discovered compose project（同步返回受管 Application；confirmRisky 用于 confirmation 级风险）。
export async function takeoverApp(appId, confirmRisky = false, idempotencyKey) {
  const headers = { 'Content-Type': 'application/json' };
  if (idempotencyKey) headers['Idempotency-Key'] = idempotencyKey;
  const r = await authFetch(`${API}/apps/${encodeURIComponent(appId)}/takeover`, {
    method: 'POST', headers, body: JSON.stringify({ confirmRisky: !!confirmRisky }),
  });
  if (!r.ok) throw await readErr(r);
  return r.json();
}

// ─── Compose 详情：compose 正文 / env 元信息 / storage ─────────────
//
// 均为「事实源静态推导」读路径，单次拉取（drawer 打开时），不轮询。
// usePoll 在 url 为 null 时跳过 fetch（id 未就绪 / drawer 关闭）。

// useCompose：GET /apps/{id}/compose → {appId, source, compose, revision}。
// compose 正文仅含非敏感渲染结果（secret 以 ${KEY} 引用），可安全展示/编辑。
export function useCompose(appId) {
  const url = appId ? `/apps/${encodeURIComponent(appId)}/compose` : null;
  return usePoll(url, { interval: 0, fallback: null });
}

// useEnv：GET /apps/{id}/env → {appId, vars[{key,configured,type,required}]}。
// 仅元信息，绝不回值（后端 EnvVarInfo 无 value 字段）。
export function useEnv(appId) {
  const url = appId ? `/apps/${encodeURIComponent(appId)}/env` : null;
  return usePoll(url, { interval: 0, fallback: null });
}

// useStorage：GET /apps/{id}/storage → {appId, volumes[{kind,source,target,external,managed,deletable}], managedDataDir, note}。
export function useStorage(appId) {
  const url = appId ? `/apps/${encodeURIComponent(appId)}/storage` : null;
  return usePoll(url, { interval: 0, fallback: null });
}

// updateComposeApp：PUT /apps/{id} 更新期望状态（乐观并发 via expectedRevision）。
// 409 (revision_mismatch / idempotency_conflict) 由 readErr 带出 .status/.reason，
// 调用方据此提示「重新加载」而非静默覆盖。
// compose 正文 + parameters + secrets；secrets 仅写不入 revision/audit。
export async function updateComposeApp(desired, idempotencyKey) {
  const headers = { 'Content-Type': 'application/json' };
  if (idempotencyKey) headers['Idempotency-Key'] = idempotencyKey;
  const r = await authFetch(`${API}/apps/${encodeURIComponent(desired.id)}`, {
    method: 'PUT', headers, body: JSON.stringify(desired),
  });
  if (!r.ok) throw await readErr(r);
  return r.json();
}

// getRemovePreview：GET /apps/{id}/remove-preview?purge= 明确列出 willDelete/willKeep。
// 卸载对话框打开时按需调用（非 hook，避免无谓轮询）。
export async function getRemovePreview(appId, purge = false) {
  const q = purge ? '?purge=true' : '?purge=false';
  const r = await authFetch(`${API}/apps/${encodeURIComponent(appId)}/remove-preview${q}`);
  if (!r.ok) throw await readErr(r);
  return r.json();
}

// ─── 平台商店（edge-apiserver storeapps）──────────────────────────
//
// 走 /store/*。compose 模板由后端持有，前端永不接收/发送原文（CEO 裁决第5条）。

// useStoreApps：GET /store/apps（含 runtime/installable/installed/pinned）。
// 单次拉取 + 手动刷新（列表成本较高，依赖 deployed apps 轮询做 installed/upgradable）。
export function useStoreApps() {
  return usePoll('/store/apps', { interval: 0, fallback: null });
}

// getStoreVersion：GET /store/version?appId=&v= → StoreAppVersion（valuesSchema/defaultValues）。
// compose 模板 json:"-" 裁剪，永不回前端。
export async function getStoreVersion(appId, version = '') {
  const q = `?appId=${encodeURIComponent(appId)}&v=${encodeURIComponent(version || '')}`;
  const r = await authFetch(`${API}/store/version${q}`);
  if (!r.ok) throw await readErr(r);
  return r.json();
}

// installStoreApp：POST /store/install → 202 + {taskId, appId, name, revision}。
// values 含 password 字段；compose 原文由后端从可信 catalog 重取渲染。
export async function installStoreApp({ appId, version, values, idempotencyKey, confirmRisky = false }) {
  const headers = { 'Content-Type': 'application/json' };
  if (idempotencyKey) headers['Idempotency-Key'] = idempotencyKey;
  const r = await authFetch(`${API}/store/install`, {
    method: 'POST', headers, body: JSON.stringify({ appId, version, values, idempotencyKey, confirmRisky }),
  });
  if (!r.ok) throw await readErr(r);
  return r.json();
}

// ─── 第三方 catalog（HTTP/Git 文件原生 source）──────────────────────
//
// 走 /catalogs/*。多 source 聚合，单 source 不可用仅影响该 source（用上次缓存）。

// useCatalogSources：GET /catalogs → []CatalogSnapshot（来源健康状态）。
export function useCatalogSources(interval = 30000) {
  return usePoll('/catalogs', { interval, fallback: [] });
}

// 动态市场来源管理（token 只写，响应仅含 tokenConfigured）。
export function useCatalogSourceConfigs(interval = 30000) {
  return usePoll('/catalogs/sources', { interval, fallback: [] });
}

async function catalogSourceMutation(path, method, body) {
  const r = await authFetch(`${API}/catalogs/sources${path}`, {
    method, headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!r.ok) throw await readErr(r);
  if (r.status === 204) return null;
  return r.json();
}

export function testCatalogSource(input) { return catalogSourceMutation('/test', 'POST', input); }
export function createCatalogSource(input) { return catalogSourceMutation('', 'POST', input); }
export function updateCatalogSource(id, input) { return catalogSourceMutation(`/${encodeURIComponent(id)}`, 'PUT', input); }
export function deleteCatalogSource(id) { return catalogSourceMutation(`/${encodeURIComponent(id)}`, 'DELETE'); }
export function refreshCatalogSource(id) { return catalogSourceMutation(`/${encodeURIComponent(id)}/refresh`, 'POST'); }

// useCatalogApps：GET /catalogs/apps → []StoreApp（带 catalogId/catalogName/installable/installed）。
export function useCatalogApps(interval = 30000) {
  return usePoll('/catalogs/apps', { interval, fallback: [] });
}

// refreshCatalogs：POST /catalogs 显式刷新所有已配置 source（不接受 URL 入参）。
export async function refreshCatalogs() {
  const r = await authFetch(`${API}/catalogs`, { method: 'POST' });
  if (!r.ok) throw await readErr(r);
  return r.json();
}

// getCatalogVersion：GET /catalogs/version?sourceId=&appId=&v= → StoreAppVersion。
export async function getCatalogVersion(sourceId, appId, version = '') {
  const q = `?sourceId=${encodeURIComponent(sourceId)}&appId=${encodeURIComponent(appId)}&v=${encodeURIComponent(version || '')}`;
  const r = await authFetch(`${API}/catalogs/version${q}`);
  if (!r.ok) throw await readErr(r);
  return r.json();
}

// installCatalogApp：POST /catalogs/install → 202 + StoreInstallResult。
// 后端按 sourceId+appId+version 从可信 source 重取渲染（前端不传 compose 原文）。
export async function installCatalogApp({ sourceId, appId, version, values, idempotencyKey, confirmRisky = false }) {
  const headers = { 'Content-Type': 'application/json' };
  if (idempotencyKey) headers['Idempotency-Key'] = idempotencyKey;
  const r = await authFetch(`${API}/catalogs/install`, {
    method: 'POST', headers,
    body: JSON.stringify({ sourceId, appId, version, values, idempotencyKey, confirmRisky }),
  });
  if (!r.ok) throw await readErr(r);
  return r.json();
}
