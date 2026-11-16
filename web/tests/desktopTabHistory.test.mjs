import { test } from 'node:test';
import assert from 'node:assert/strict';
import { DesktopTabHistory } from '../src/domain/desktopTabHistory.ts';

test('each desktop tab has independent navigation and new routes discard forward history', () => {
  const a = new DesktopTabHistory('#/');
  const b = new DesktopTabHistory('#/s/server-b');
  a.record('#/s/one'); a.record('#/s/two');
  assert.equal(a.move(-1), '#/s/one');
  a.record('#/s/one'); // the ensuing hashchange must not add a route
  assert.equal(a.canForward, true);
  a.record('#/native/three');
  assert.equal(a.canForward, false);
  assert.equal(a.move(1), undefined);
  assert.equal(b.canBack, false);
  assert.deepEqual(b.entries, ['#/s/server-b']);
});

test('refresh restores matching route history and rejects corrupt or stale saved state', () => {
  const history = new DesktopTabHistory('#/');
  history.record('#/s/one'); history.record('#/s/two'); history.move(-1);
  const restored = new DesktopTabHistory('#/s/one', JSON.parse(JSON.stringify(history)));
  assert.equal(restored.move(1), '#/s/two');
  for (const saved of [null, { entries: [], index: 0 }, { entries: ['https://evil.test'], index: 0 }, { entries: ['#/s/one'], index: -1 }, JSON.parse(JSON.stringify(history))]) {
    const reset = new DesktopTabHistory('#/new', saved);
    assert.equal(reset.canBack, false); assert.equal(reset.canForward, false);
  }
});

test('navigation is bounded and boundary buttons cannot move past available routes', () => {
  const history = new DesktopTabHistory('');
  for (let i = 0; i < 200; i++) history.record(`#/s/${i}`);
  assert.equal(history.entries.length, 100);
  assert.equal(history.move(1), undefined);
  for (let i = 0; i < 99; i++) history.move(-1);
  assert.equal(history.move(-1), undefined);
  assert.equal(history.entries[history.index], '#/s/100');
});
