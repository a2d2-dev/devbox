import test from 'node:test';
import assert from 'node:assert/strict';
import {
  compareVersions, slugify, parseEnv, groupRisks, canDeployGivenRisks,
  volumeMeta, envDisplay, computeUpgrade, observedPhase,
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
