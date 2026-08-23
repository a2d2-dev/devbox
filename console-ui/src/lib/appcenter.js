import { compareVersions } from './compose.js';

const CATEGORY_RULES = [
  ['AI', ['ai', 'artificial intelligence', 'machine learning', 'llm', '大模型', '人工智能']],
  ['影音娱乐', ['media', 'multimedia', 'video', 'audio', 'music', 'photo', '影音', '影视', '音乐', '相册']],
  ['下载工具', ['download', 'torrent', 'usenet', '下载']],
  ['备份同步', ['backup', 'sync', 'cloud', '备份', '同步', '网盘']],
  ['开发工具', ['dev', 'development', 'code', 'ide', 'git', 'ci', '开发', '编程']],
  ['数据存储', ['database', 'storage', 'db', 'mysql', 'postgres', 'redis', '数据库', '存储']],
  ['网络服务', ['network', 'proxy', 'dns', 'vpn', 'web server', '网络', '代理']],
  ['实用效率', ['tool', 'utility', 'office', 'productivity', 'middleware', '工具', '效率', '办公']],
];

export function normalizeCategory(raw) {
  const value = String(raw || '').trim();
  if (!value) return '其他';
  const lower = value.toLowerCase();
  const tokens = lower.split(/[^a-z0-9\u4e00-\u9fff]+/).filter(Boolean);
  for (const [label, needles] of CATEGORY_RULES) {
    if (needles.some((needle) => lower === needle || (needle.length <= 2 ? tokens.includes(needle) : lower.includes(needle)))) return label;
  }
  return value;
}

export function deriveAppCenterStatus({ installed = false, upgradable = false, installable = true, taskStatus = '', taskType = '' } = {}) {
  if ((!taskType || taskType === 'apply' || taskType === 'restore') && (taskStatus === 'queued' || taskStatus === 'running')) return 'installing';
  if (installed && upgradable) return 'updatable';
  if (installed) return 'installed';
  if (!installable) return 'incompatible';
  return 'not-installed';
}

export const APP_STATUS = {
  'not-installed': { label: '未安装', tone: 'gray' },
  installing: { label: '安装中', tone: 'blue' },
  installed: { label: '已安装', tone: 'green' },
  updatable: { label: '可更新', tone: 'amber' },
  incompatible: { label: '不兼容', tone: 'red' },
};

function timestamp(value) {
  if (!value) return null;
  const parsed = Date.parse(value);
  return Number.isNaN(parsed) ? null : parsed;
}

// 有 catalog 时间时优先倒序；任一方无时间时降级为版本号、名称稳定排序。
export function sortNewest(apps) {
  return [...(apps || [])].sort((a, b) => {
    const at = timestamp(a.publishedAt);
    const bt = timestamp(b.publishedAt);
    if (at != null && bt != null && at !== bt) return bt - at;
    if (at != null && bt == null) return -1;
    if (at == null && bt != null) return 1;
    const versionOrder = compareVersions(a.ver || '', b.ver || '');
    if (versionOrder !== 0) return -versionOrder;
    return String(a.name || '').localeCompare(String(b.name || ''), 'zh-CN');
  });
}

export function sourceTrust(app) {
  if (app?.sourceType === 'official' || app?.origin === 'platform') {
    return { sourceLabel: 'DevBox 官方', trustLabel: '官方审核', tone: 'blue' };
  }
  if (app?.origin === 'manual') {
    return { sourceLabel: '手动安装', trustLabel: '本机自管', tone: 'gray' };
  }
  return { sourceLabel: app?.sourceName || '社区 Catalog', trustLabel: '社区未审核', tone: 'violet' };
}

export function matchesAppCenterFilters(app, { view = 'all', source = 'all', category = 'all', query = '' } = {}) {
  if (view === 'installed' && !app.installed) return false;
  // 手动 Compose 没有 catalog 发布时间，只属于“已安装”管理视图。
  if (app.origin === 'manual' && view !== 'installed') return false;
  if (source === 'platform' && app.origin !== 'platform') return false;
  if (source === 'manual' && app.origin !== 'manual') return false;
  if (source !== 'all' && source !== 'platform' && source !== 'manual' && app.sourceId !== source) return false;
  if (category !== 'all' && app.cat !== category) return false;
  const needle = String(query || '').trim().toLocaleLowerCase('zh-CN');
  if (!needle) return true;
  return [app.name, app.desc].some((value) => String(value || '').toLocaleLowerCase('zh-CN').includes(needle));
}
