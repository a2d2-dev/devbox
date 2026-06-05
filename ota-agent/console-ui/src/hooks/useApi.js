import { useState, useEffect, useRef } from 'react';
import {
  DEVICE as MOCK_DEVICE,
  APPS as MOCK_APPS,
  APP_BY_ID as MOCK_APP_BY_ID,
  METRICS as MOCK_METRICS,
  GPUS as MOCK_GPUS,
  ALERTS as MOCK_ALERTS,
  PORTS as MOCK_PORTS,
  FILES_TREE as MOCK_FILES_TREE,
  MODELS as MOCK_MODELS,
  genSeries,
} from '../data/mock';

const API = '/api/v1';

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
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    let timer = null;

    async function doFetch() {
      try {
        const r = await fetch(API + url);
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
  }, [url, interval]); // eslint-disable-line react-hooks/exhaustive-deps

  return { data, loading, error };
}

// ─── Device ──────────────────────────────────────────────────────

export function useDevice() {
  return usePoll('/device', {
    fallback: MOCK_DEVICE,
    transform: (d) => ({
      name: d.hostname,
      alias: d.hostname,
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
  const [data, setData] = useState({ metrics: { ...MOCK_METRICS }, gpus: [...MOCK_GPUS] });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;

    async function doFetch() {
      try {
        const r = await fetch(API + '/metrics');
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        const m = await r.json();
        if (!mountedRef.current) return;

        const metrics = { ...data.metrics };

        metrics.cpu = { ...metrics.cpu, used: Math.round(m.cpuUsedPercent), total: 100 };
        metrics.gpu = {
          ...metrics.gpu,
          used: m.gpuData && m.gpuData.length > 0 ? Math.round(m.gpuData[0].gpuUtil) : 0,
          total: 100,
        };
        metrics.mem = {
          ...metrics.mem,
          used: Math.round(m.memoryUsed / (1024 * 1024 * 1024)),
          total: Math.round(m.memoryTotal / (1024 * 1024 * 1024)),
          pct: Math.round(m.memoryUsedPercent),
        };
        if (m.diskData && m.diskData.length > 0) {
          const d = m.diskData[0];
          metrics.dsk = {
            ...metrics.dsk,
            used: Math.round(d.used / (1024 * 1024 * 1024)),
            total: Math.round(d.total / (1024 * 1024 * 1024)),
            pct: Math.round(d.usedPercent),
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
    cpuPercent: MOCK_METRICS.cpu.series,
    memoryPercent: MOCK_METRICS.mem.series,
    diskPercent: MOCK_METRICS.dsk.series,
    gpuUtil: MOCK_METRICS.gpu.series,
  });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;

    async function doFetch() {
      try {
        const r = await fetch(API + '/metrics/history');
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        const h = await r.json();
        if (!mountedRef.current) return;
        setData((prev) => ({
          cpuPercent: h.cpuPercent || prev.cpuPercent,
          memoryPercent: h.memoryPercent || prev.memoryPercent,
          diskPercent: h.diskPercent || prev.diskPercent,
          gpuUtil: h.gpuUtil || prev.gpuUtil,
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
  const sysApps = MOCK_APPS.filter((a) => a.kind === 'system');

  return usePoll('/apps', {
    interval,
    fallback: MOCK_APPS,
    transform: (apps) => {
      if (!Array.isArray(apps)) return MOCK_APPS;
      const mapped = apps.map((a) => ({
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
        deployedAt: a.createdAt ? new Date(a.createdAt).toLocaleString('zh-CN') : '',
      }));
      return [...sysApps, ...mapped];
    },
  });
}

// ─── Alerts ──────────────────────────────────────────────────────

export function useAlerts(interval = 30000) {
  return usePoll('/alerts', {
    interval,
    fallback: MOCK_ALERTS,
    transform: (a) => (Array.isArray(a) && a.length > 0 ? a : MOCK_ALERTS),
  });
}

// ─── Ports ───────────────────────────────────────────────────────

export function usePorts() {
  return usePoll('/ports', {
    fallback: MOCK_PORTS,
    transform: (p) => {
      if (!Array.isArray(p)) return MOCK_PORTS;
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
    fallback: MOCK_MODELS,
    transform: (m) => (Array.isArray(m) ? m : MOCK_MODELS),
  });
}

// ─── Files ───────────────────────────────────────────────────────

export function useFiles(path = '/workspace') {
  const url = '/files?path=' + encodeURIComponent(path);

  return usePoll(url, {
    fallback: MOCK_FILES_TREE[path] || MOCK_FILES_TREE['/workspace'] || [],
    transform: (f) => {
      if (!Array.isArray(f)) return MOCK_FILES_TREE[path] || [];
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

// ─── Supervisor ─────────────────────────────────────────────

export function useSupervisor(interval = 5000) {
  return usePoll('/supervisor/status', {
    interval,
    fallback: null,
  });
}

export async function supervisorControl(name, action) {
  const r = await fetch(`${API}/supervisor/services/${name}/control`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ action }),
  });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return r.json().catch(() => null);
}

export async function supervisorLogs(name) {
  try {
    const r = await fetch(`${API}/supervisor/services/${name}/logs`);
    if (!r.ok) return { name, log: '' };
    return await r.json();
  } catch {
    return { name, log: '' };
  }
}

// ─── Imperative API helpers ──────────────────────────────────────

export async function appOp(appId, operation) {
  const r = await fetch(`${API}/apps/${appId}/${operation}`, { method: 'POST' });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return r.json().catch(() => null);
}

export async function deleteApp(appId) {
  const r = await fetch(`${API}/apps/${appId}`, { method: 'DELETE' });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return r.json().catch(() => null);
}

export async function getLogs(appId, tail = 100) {
  try {
    const r = await fetch(`${API}/apps/${appId}/logs?tail=${tail}`);
    if (!r.ok) return '';
    const d = await r.json();
    return d ? d.logs : '';
  } catch {
    return '';
  }
}

export async function authVerify(password) {
  try {
    const r = await fetch(`${API}/auth/verify`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password }),
    });
    return await r.json();
  } catch {
    return { authenticated: false };
  }
}
