import { useState, useEffect, useRef, useCallback } from 'react';

const API = '/api/v1';

// ─── Auth token management ──────────────────────────────────────

let _token = localStorage.getItem('edge_token') || '';

export function setAuthToken(t) {
  _token = t || '';
  if (t) localStorage.setItem('edge_token', t);
  else localStorage.removeItem('edge_token');
}

export function getAuthToken() { return _token; }

export function clearAuth() {
  _token = '';
  localStorage.removeItem('edge_token');
}

function authHeaders() {
  return _token ? { Authorization: 'Bearer ' + _token } : {};
}

let _onAuthExpired = null;
export function setOnAuthExpired(cb) { _onAuthExpired = cb; }

// 连续 401 计数 — 累计达到阈值才登出，避免单次偶发 401 (云端 token 校验抖动 /
// 缓存失效瞬间 / 网络 race) 把用户踢出登录。
// 任何 2xx 成功响应重置计数。
const MAX_CONSECUTIVE_401 = 3;
let _consecutive401 = 0;

// authFetch 通用 fetch wrapper —— 自动注入 Bearer token + 累计 401 触发清登录
// 凡是调 /api/v1/* 受保护接口的页面必须用这个，不要用 raw fetch
// （raw fetch 会被 middleware 拦 401 + 页面空白；见 2026-06-22 进程/磁盘/网络/告警/设置 5 页空白事件）
export async function authFetch(url, opts = {}) {
  // 未登录短路：App.jsx 里的 hooks (useMetrics/useApps/…) 写在 early-return 之前，
  // 登录前会先跑一遍。这时不发网络，返合成 401 让 usePoll 走 error 分支即可，
  // 避免登录页/初始加载在 devtool 里堆一片 401 噪音。
  if (!_token) {
    return new Response(null, { status: 401, statusText: 'no token (pre-login)' });
  }
  const resp = await fetch(url, { ...opts, headers: { ...authHeaders(), ...opts.headers } });
  if (resp.status === 401 && _token) {
    _consecutive401 += 1;
    if (_consecutive401 >= MAX_CONSECUTIVE_401) {
      _consecutive401 = 0;
      clearAuth();
      if (_onAuthExpired) _onAuthExpired();
    }
  } else if (resp.ok) {
    _consecutive401 = 0;
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
  const [loading, setLoading] = useState(true);
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

    if (!url) {
      setLoading(false);
      return () => { mountedRef.current = false; };
    }

    async function doFetch() {
      try {
        const r = await authFetch(API + url);
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        const json = await r.json();
        if (!mountedRef.current) return;
        setData(transform ? transform(json) : json);
        setError(null);
      } catch (err) {
        if (!mountedRef.current) return;
        setError(err);
        // keep previous data (or fallback) on error
      } finally {
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

export function useApps(interval = 10000) {
  return usePoll('/apps', {
    interval,
    fallback: [],
    transform: (apps) => {
      if (!Array.isArray(apps)) return [];
      return apps.map((a) => ({
        id: a.id,
        kind: 'app',
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
      }));
    },
  });
}

// ─── App detail (含 env + deployValues，AppMgmtDrawer 打开时拉一次) ───
//
// 跟 useApps 区别：单次拉取（不轮询），按 appID 变化触发，drawer 关闭传
// null/'' 跳过 fetch。后端 GET /api/v1/apps/{id} 比 ListApps 多调 N+1 K8s
// API（Secret/ConfigMap GET），故不进列表流程。
export function useAppDetail(appID) {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);

  useEffect(() => {
    if (!appID) { setData(null); return; }
    let cancelled = false;
    setLoading(true);
    setError(null);
    authFetch(`${API}/apps/${encodeURIComponent(appID)}`)
      .then(async r => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return r.json();
      })
      .then(d => { if (!cancelled) setData(d); })
      .catch(e => { if (!cancelled) setError(e.message || 'fetch failed'); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [appID]);

  return { data, loading, error };
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

export function useAppLogs(appId, interval = 3000, tail = 200) {
  const [lines, setLines] = useState([]);
  const [loading, setLoading] = useState(true);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    if (!appId) { setLines([]); setLoading(false); return; }

    async function doFetch() {
      try {
        const r = await authFetch(`${API}/apps/${appId}/logs?tail=${tail}`);
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
  }, [appId, interval, tail]);

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
