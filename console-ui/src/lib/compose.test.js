import test from 'node:test';
import assert from 'node:assert/strict';
import {
  compareVersions, slugify, parseEnv, groupRisks, canDeployGivenRisks,
  volumeMeta, envDisplay, computeUpgrade, observedPhase, appActionSet, formatServicePort, serviceInventory,
} from './compose.js';

test('版本比较和应用 ID 规范化', () => {
  assert.equal(compareVersions('1.10.0', '1.9.0'), 1);
  assert.equal(compareVersions('v2.0.0', '2.0.0'), 0);
  assert.equal(slugify(' My App / Web '), 'my-app-web');
});

test('环境变量解析保留等号且忽略注释', () => {
  assert.deepEqual(parseEnv('# note\nTOKEN=a=b\nPORT=8080'), { TOKEN: 'a=b', PORT: '8080' });
});

test('blocked 永不可部署，confirmation 必须显式确认', () => {
  const blocked = groupRisks([{ level: 'blocked', message: 'docker socket' }]);
  assert.deepEqual(canDeployGivenRisks(blocked, true), { deployable: false, reason: 'blocked' });
  const confirmation = groupRisks([{ level: 'confirmation', message: 'capability' }]);
  assert.equal(canDeployGivenRisks(confirmation, false).deployable, false);
  assert.equal(canDeployGivenRisks(confirmation, true).deployable, true);
});

test('存储和 secret 展示不推断可删除外部数据', () => {
  assert.match(volumeMeta({ kind: 'external' }).hint, /永不删除/);
  assert.equal(envDisplay({ type: 'password' }).secret, true);
});

test('phase 使用后端 observed 值，catalog 升级按来源匹配', () => {
  const app = { observed: { phase: 'degraded' }, source: { kind: 'catalog', catalogId: 'c1', storeId: 'web', version: '1.0.0' } };
  assert.equal(observedPhase(app), 'degraded');
  const result = computeUpgrade(app, [], [{ catalogId: 'c1', id: 'web', version: '1.1.0', installable: true }]);
  assert.equal(result.upgradable, true);
});

test('discovered project 卡片只读：禁止 lifecycle/uninstall/detail，仅 takeover', () => {
  const disc = { ownership: 'discovered', discovered: { takeoverAvailable: true } };
  const a = appActionSet(disc);
  assert.equal(a.lifecycle, false, 'discovered 不得有 start/stop/restart/redeploy');
  assert.equal(a.uninstall, false, 'discovered 不得有 uninstall');
  assert.equal(a.detail, false, 'discovered 不得开 AppMgmtDrawer（避免暴露写入口）');
  assert.equal(a.takeover, true, '可接管 discovered 应有 takeover CTA');

  // 不可接管的 discovered（标签冲突/非法 project name）连 takeover 也没有。
  const notAvail = appActionSet({ ownership: 'discovered', discovered: { takeoverAvailable: false } });
  assert.equal(notAvail.takeover, false);

  // 受管应用（含已接管）恢复完整动作。
  const managed = appActionSet({ ownership: 'managed' });
  assert.equal(managed.lifecycle, true);
  assert.equal(managed.uninstall, true);
  assert.equal(managed.detail, true);
  assert.equal(managed.takeover, false);
});

test('discovered 服务清单格式化端口并截断长列表', () => {
  // 端口：host:container/proto 与仅 container/proto。
  assert.equal(formatServicePort({ hostPort: 8080, containerPort: 80, protocol: 'TCP' }), '8080:80/tcp');
  assert.equal(formatServicePort({ containerPort: 5432, protocol: 'tcp' }), '5432/tcp');

  // 清单：汇总 name/state/health/ports，按 limit 截断。
  const app = { observed: { services: [
    { name: 'web', state: 'running', health: 'healthy', ports: [{ hostPort: 8080, containerPort: 80, protocol: 'tcp' }] },
    { name: 'db', state: 'running', health: 'none', ports: [{ containerPort: 5432 }] },
    { name: 'cache', state: 'exited' },
    { name: 'worker', state: 'running' },
    { name: 'beat', state: 'running' },
  ] } };
  const inv = serviceInventory(app, 3);
  assert.equal(inv.total, 5);
  assert.equal(inv.truncated, true);
  assert.equal(inv.rows.length, 3);
  assert.equal(inv.rows[0].name, 'web');
  assert.deepEqual(inv.rows[0].ports, ['8080:80/tcp']);
  assert.deepEqual(inv.rows[1].ports, ['5432/tcp']);
  assert.equal(inv.rows[2].name, 'cache');

  // 空服务安全返回。
  assert.equal(serviceInventory({ observed: { services: [] } }).total, 0);
  assert.equal(serviceInventory({}).total, 0);
});
