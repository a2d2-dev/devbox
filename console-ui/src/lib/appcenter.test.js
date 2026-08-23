import { test, expect } from 'vitest';
import { normalizeCategory, deriveAppCenterStatus, sortNewest, sourceTrust } from './appcenter.js';

test('应用中心分类归一化覆盖平台与 1Panel 常见标签', () => {
  expect(normalizeCategory('ai-inference')).toBe('AI');
  expect(normalizeCategory('dev-environment')).toBe('开发工具');
  expect(normalizeCategory('Database')).toBe('数据存储');
  expect(normalizeCategory('download-tool')).toBe('下载工具');
  expect(normalizeCategory('Backup & Sync')).toBe('备份同步');
});

test('应用状态互斥且安装任务优先于静态状态', () => {
  expect(deriveAppCenterStatus({ installable: true })).toBe('not-installed');
  expect(deriveAppCenterStatus({ installable: false })).toBe('incompatible');
  expect(deriveAppCenterStatus({ installed: true, installable: false })).toBe('installed');
  expect(deriveAppCenterStatus({ installed: true, upgradable: true })).toBe('updatable');
  expect(deriveAppCenterStatus({ installed: true, upgradable: true, taskStatus: 'running' })).toBe('installing');
});

test('最新发布按时间倒序，无时间时按版本与名称稳定降级', () => {
  const sorted = sortNewest([
    { name: 'old', ver: '9.0.0', publishedAt: '2025-01-01T00:00:00Z' },
    { name: 'new', ver: '1.0.0', publishedAt: '2026-01-01T00:00:00Z' },
    { name: 'fallback-a', ver: '2.0.0' },
    { name: 'fallback-b', ver: '1.0.0' },
  ]);
  expect(sorted.map((app) => app.name)).toEqual(['new', 'old', 'fallback-a', 'fallback-b']);
});

test('来源信任明确区分官方与社区', () => {
  expect(sourceTrust({ origin: 'platform' }).trustLabel).toBe('官方审核');
  expect(sourceTrust({ origin: 'catalog', sourceName: '1Panel' }).trustLabel).toBe('社区未审核');
});
