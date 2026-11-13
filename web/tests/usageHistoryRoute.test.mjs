import { test } from 'node:test';
import assert from 'node:assert/strict';
import { isUsageHistory, usageHistoryHash } from '../src/domain/usageHistoryRoute.ts';

test('history links encode opaque account ids without changing the route', () => {
  const id = 'claude:account/a?b#c & 中文';
  const hash = usageHistoryHash(id);
  assert.ok(isUsageHistory(hash));
  assert.equal(new URLSearchParams(hash.split('?')[1]).get('account'), id);
  assert.equal(usageHistoryHash(), '#/usage-history');
  assert.ok(!isUsageHistory('#/usage-history-extra'));
  assert.ok(!isUsageHistory('#/s/usage-history'));
});
