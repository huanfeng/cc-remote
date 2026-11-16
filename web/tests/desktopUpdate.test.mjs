import { test } from 'node:test';
import assert from 'node:assert/strict';
import { isCurrentUpdate } from '../src/domain/desktopUpdate.ts';

test('a late initial snapshot or check reply cannot overwrite download progress from another window', () => {
  assert.equal(isCurrentUpdate(null, { revision: 0 }), true);
  assert.equal(isCurrentUpdate({ revision: 9 }, { revision: 0 }), false);
  assert.equal(isCurrentUpdate({ revision: 9 }, { revision: 8 }), false);
  assert.equal(isCurrentUpdate({ revision: 9 }, { revision: 9 }), true);
  assert.equal(isCurrentUpdate({ revision: 9 }, { revision: 10 }), true);
});
