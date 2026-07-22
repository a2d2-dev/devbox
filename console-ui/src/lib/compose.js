// 纯函数集合（Issue #2 应用管理 / Compose / 商店 / Catalog 前端）。
//
// 刻意抽到独立模块：无 React / DOM / 网络依赖，可被 node:test 直接覆盖。
// 页面与 hooks 从这里 import，避免在组件里散落重复逻辑。
//
// 所有函数对未知输入应返回安全的占位值，绝不抛错（UI 容错优先）。

// ─── 版本比较 ────────────────────────────────────────────────────
// 简单语义版本比较，返 -1/0/1。非数字段 fallback 到字符串比较。
// 与后端 compareVersionStrings 对齐：10.0.0 > 9.0.0，1.10.0 > 1.9.0。
export function compareVersions(a, b) {
  if (!a && !b) return 0;
  if (!a) return -1;
  if (!b) return 1;
  const norm = (s) => String(s).replace(/^v/i, '').split(/[.\-+]/);
  const pa = norm(a);
  const pb = norm(b);
  const len = Math.max(pa.length, pb.length);
  for (let i = 0; i < len; i++) {
    const na = parseInt(pa[i] || '0', 10);
    const nb = parseInt(pb[i] || '0', 10);
    if (!Number.isNaN(na) && !Number.isNaN(nb)) {
      if (na !== nb) return na < nb ? -1 : 1;
    } else {
      const sa = pa[i] || '';
      const sb = pb[i] || '';
      if (sa !== sb) return sa < sb ? -1 : 1;
    }
  }
  return 0;
}

// ─── 名称 / 环境变量解析 ─────────────────────────────────────────
// slugify：应用名 → devbox app ID（小写 / 数字 / 连字符）。
export function slugify(s) {
  return String(s || '')
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}

// parseEnv：KEY=VALUE 每行 → map。忽略空行 / 注释行。
// 用于「粘贴/上传 Compose」向导里用户额外填的 secret / 环境变量（仅写，不回传）。
export function parseEnv(text) {
  const out = {};
  for (const line of String(text || '').split('\n')) {
    const t = line.trim();
    if (!t || t.startsWith('#')) continue;
    const i = t.indexOf('=');
    if (i > 0) out[t.slice(0, i).trim()] = t.slice(i + 1);
  }
  return out;
}

// ─── valuesSchema 字段 ───────────────────────────────────────────
// edge-apiserver / catalog 的 defaultValues 是 map[string]json.RawMessage，
// 值形如 {"raw":"8080"} 或裸字符串。parseRawValue 统一取字符串。
export function parseRawValue(v) {
  if (v && typeof v === 'object' && 'raw' in v) return String(v.raw);
  return v == null ? '' : String(v);
}

// fieldLabel：优先中文 label，其次英文，最后 key。
export function fieldLabel(f) {
  if (!f) return '';
  if (f.label) {
    if (f.label.zh) return f.label.zh;
    if (f.label.en) return f.label.en;
  }
  return f.key || '';
}

// coerceValueForSubmit：把表单字符串按字段类型转成后端期望的 JSON 类型。
// number → Number（空串/非法保持原样由后端校验）；boolean → true/false；其余字符串。
export function coerceValueForSubmit(field, value) {
  if (!field) return value;
  if (field.type === 'number' && value !== '' && value != null) {
    const n = Number(value);
    return Number.isNaN(n) ? value : n;
  }
  if (field.type === 'boolean') return value === true || value === 'true';
  return value;
}

// validateValuesFields：本地必填预检，返回缺失字段的 label 列表（空 = 通过）。
// 最终权威校验由后端 splitValues 完成；此函数仅用于即时 UI 反馈。
export function missingRequiredFields(fields, values) {
  if (!Array.isArray(fields)) return [];
  return fields
    .filter((f) => f.required && (values[f.key] == null || values[f.key] === ''))
    .map((f) => fieldLabel(f));
}

// ─── 风险分级（预检 findings）─────────────────────────────────────
// 后端 ValidateResult.risks: [{level:'blocked|confirmation|warning',service,field,message}]
// groupRisks 按 level 分桶，safe 不进结果（后端也不返回 safe）。
export function groupRisks(risks) {
  const out = { blocked: [], confirmation: [], warning: [] };
  if (!Array.isArray(risks)) return out;
  for (const r of risks) {
    const lvl = r && r.level;
    if (lvl === 'blocked' || lvl === 'confirmation' || lvl === 'warning') {
      out[lvl].push(r);
    } else {
      // 未知等级按 warning 兜底展示，确保不丢失。
      out.warning.push(r);
    }
  }
  return out;
}

// canDeployGivenRisks：blocked 永远禁止；confirmation 在用户显式确认后允许；
// warning/safe 不阻断。返回 {deployable, reason}。
export function canDeployGivenRisks(grouped, confirmed) {
  if (grouped.blocked.length > 0) {
    return { deployable: false, reason: 'blocked' };
  }
  if (grouped.confirmation.length > 0 && !confirmed) {
    return { deployable: false, reason: 'confirmation' };
  }
  return { deployable: true, reason: 'ok' };
}

// ─── 存储 / 卷分类（详情 Tab）─────────────────────────────────────
// 后端 VolumeInfo: {kind:'managed|external|bind|socket',source,target,external,managed,deletable}
// volumeMeta 返回 {label, tone, hint} 供 UI 稳定展示（颜色不是唯一状态：label + hint 同步表达）。
export function volumeMeta(v) {
  if (!v) return { label: '未知', tone: 'gray', hint: '' };
  switch (v.kind) {
    case 'managed':
      return {
        label: '受管卷',
        tone: 'blue',
        hint: v.deletable ? '卸载并清除数据(purge)时会删除' : '由 devbox 托管',
      };
    case 'external':
      return {
        label: '外部卷',
        tone: 'amber',
        hint: 'external:true 外部数据，卸载时永不删除',
      };
    case 'bind':
      return {
        label: '宿主挂载',
        tone: 'amber',
        hint: '绑定宿主路径，生命周期由宿主管，卸载时不删除',
      };
    case 'socket':
      return {
        label: 'Socket',
        tone: 'red',
        hint: '特权 socket 挂载（安装期已阻断，仅防御性标识）',
      };
    default:
      return { label: v.kind || '卷', tone: 'gray', hint: '' };
  }
}

// ─── 环境变量元信息（详情 Tab，绝不回显值）──────────────────────
// 后端 EnvVarInfo: {key,configured,type:'password|text',required}
// envDisplay 返回展示用的 type label 与是否敏感标记。
export function envDisplay(v) {
  if (!v) return { typeLabel: '—', secret: false };
  const secret = v.type === 'password';
  return {
    typeLabel: secret ? 'Secret' : v.type === 'text' ? '文本' : (v.type || '—'),
    secret,
  };
}

// ─── 时间格式化 ──────────────────────────────────────────────────
// formatRelativeTime：ISO → 「3 分钟前 / 2 小时前 / 昨天 / 2026-07-01 12:00」。
// 输入非法返回 '—'。now 可注入（测试）。
export function formatRelativeTime(iso, now = Date.now()) {
  if (!iso) return '—';
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return '—';
  const diff = Math.max(0, now - t);
  const min = Math.floor(diff / 60000);
  if (min < 1) return '刚刚';
  if (min < 60) return `${min} 分钟前`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr} 小时前`;
  const day = Math.floor(hr / 24);
  if (day === 1) return '昨天';
  if (day < 7) return `${day} 天前`;
  try {
    return new Date(iso).toLocaleString('zh-CN', {
      year: 'numeric', month: '2-digit', day: '2-digit',
      hour: '2-digit', minute: '2-digit',
    });
  } catch {
    return '—';
  }
}

// formatDateTime：ISO → 「2026-07-01 12:00」，非法返回 '—'。
export function formatDateTime(iso) {
  if (!iso) return '—';
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return '—';
  try {
    return new Date(iso).toLocaleString('zh-CN', {
      year: 'numeric', month: '2-digit', day: '2-digit',
      hour: '2-digit', minute: '2-digit',
    });
  } catch {
    return '—';
  }
}

// ─── Phase / runtime 文案（后端权威，前端只展示）──────────────────
export const PHASE_LABEL = {
  running: '运行中', stopped: '已停止', degraded: '降级', deploying: '部署中',
  failed: '失败', pending: '等待', removing: '卸载中', unknown: '未知',
};
export const PHASE_TONE = {
  running: 'green', stopped: 'gray', degraded: 'amber', deploying: 'blue',
  failed: 'red', pending: 'blue', removing: 'violet', unknown: 'gray',
};

// observedPhase：后端 observed.phase 优先，其次 state（旧兼容），兜底 unknown。
// 注意：不在此处「推断」业务 phase（如根据 replicas 推 running），phase 一律以后端为准。
export function observedPhase(app) {
  return app?.observed?.phase || app?.state || 'unknown';
}

// runtimeLabel：runtime → 人读。
export function runtimeLabel(rt) {
  return rt === 'compose' ? 'Docker Compose' : rt === 'kubernetes' ? 'Kubernetes' : '未知';
}

// ─── RemovePreview 辅助 ──────────────────────────────────────────
// 把 willDelete / willKeep 字符串数组分类成「容器/网络」「受管数据」「保留(外部/挂载)」。
export function classifyPreview(preview) {
  const del = Array.isArray(preview?.willDelete) ? preview.willDelete : [];
  const keep = Array.isArray(preview?.willKeep) ? preview.willKeep : [];
  return {
    willDelete: del,
    willKeep: keep,
    purge: !!preview?.purge,
  };
}

// ─── 来源 / 升级提示（列表卡片 × 商店 cross-reference）─────────────
// appOrigin：据 app.source 返回 {label, tone, kind}。
//   store   → 平台商店 / catalog → 第三方 Catalog / inline → 自定义 / 其余 → 本地。
export function appOrigin(app) {
  const k = app?.source?.kind;
  if (k === 'store') return { label: '平台商店', tone: 'blue', kind: 'store' };
  if (k === 'catalog') return { label: '第三方 Catalog', tone: 'violet', kind: 'catalog' };
  if (k === 'inline') return { label: '自定义', tone: 'gray', kind: 'inline' };
  return { label: app?.runtime === 'compose' ? '本地' : 'K8s', tone: 'gray', kind: 'local' };
}

// findCatalogEntry：在 storeApps/catalogApps 中按 app.source 找到对应条目。
export function findCatalogEntry(app, storeApps, catalogApps) {
  const k = app?.source?.kind;
  const storeId = app?.source?.storeId;
  if (!storeId) return null;
  if (k === 'store' && Array.isArray(storeApps)) {
    return storeApps.find((s) => s && s.id === storeId) || null;
  }
  if (k === 'catalog' && Array.isArray(catalogApps)) {
    return catalogApps.find(
      (s) => s && s.catalogId === app.source.catalogId && s.id === storeId,
    ) || null;
  }
  return null;
}

// computeUpgrade：列表卡片「catalog 更新提示」。
// 返回 {upgradable, latestVersion, installedVersion, installable}。
//   - 仅当能定位到商店/catalog 条目且其 version 高于已装版本时 upgradable=true。
//   - installable：条目当前是否可本机安装（compose + Docker 可用）。
export function computeUpgrade(app, storeApps, catalogApps) {
  const entry = findCatalogEntry(app, storeApps, catalogApps);
  const installedVersion = app?.source?.version || app?.version || '';
  if (!entry) {
    return { upgradable: false, latestVersion: '', installedVersion, installable: false };
  }
  const latestVersion = entry.version || '';
  const upgradable = !!installedVersion && !!latestVersion
    && compareVersions(installedVersion, latestVersion) < 0;
  return { upgradable, latestVersion, installedVersion, installable: !!entry.installable };
}
