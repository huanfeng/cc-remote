import { test } from 'node:test';
import assert from 'node:assert/strict';
import { OverlayHistory } from '../src/lib/overlayHistory.ts';

function browser() {
  const stack = [{ state: { route: 'previous' }, url: '#/s/previous' }, { state: { route: 'current' }, url: '#/s/current' }];
  let index = 1;
  const pending = [];
  const listeners = [];
  const b = { history: {
    get state() { return stack[index].state; },
    pushState(state) { stack.splice(index + 1); stack.push({ state, url: stack[index].url }); index++; },
    replaceState(state) { stack[index].state = state; },
    back() { pending.push(-1); },
    forward() { pending.push(1); },
  }, addEventListener(_, cb) { listeners.push(cb); },
  flush() { let guard = 20; while (pending.length && guard--) { index += pending.shift(); assert.ok(index >= 0 && index < stack.length); listeners.forEach(cb => cb({ state: stack[index].state })); } assert.ok(guard > 0); },
  get index() { return index; }, get url() { return stack[index].url; }, get length() { return stack.length; } };
  return b;
}

test('Back closes nested dialogs before changing the route; forward does not resurrect them', () => {
  const b = browser(), c = new OverlayHistory(b);
  const calls = [];
  let parent, child;
  // React mounts child layout effects first: depth, not registration order, wins.
  child = c.add(() => { calls.push('child'); child(); }, 1);
  parent = c.add(() => { calls.push('parent'); parent(); }, 0);
  b.history.back(); b.flush();
  assert.deepEqual(calls, ['child']); assert.equal(b.url, '#/s/current');
  b.history.back(); b.flush();
  assert.deepEqual(calls, ['child', 'parent']); assert.equal(b.index, 1);
  b.history.back(); b.flush(); assert.equal(b.url, '#/s/previous');
  b.history.forward(); b.flush(); assert.equal(c.dismiss(), false);
  b.history.forward(); b.flush(); assert.equal(b.index, 1, 'stale Forward entry is skipped');
});

test('a cancelled unsaved-changes confirmation cancels pending navigation', async () => {
  const b = browser(), c = new OverlayHistory(b);
  let confirmation;
  const settings = c.add(() => { if (!confirmation) confirmation = c.add(() => confirmation()); });
  let navigated = false;
  c.afterClose(() => { navigated = true; });
  confirmation();
  await Promise.resolve();
  settings(); b.flush();
  assert.equal(navigated, false);
});

test('navigation can be requested again after cancelling a dirty confirmation', async () => {
  const b = browser(), c = new OverlayHistory(b);
  let confirmation, requests = 0;
  const settings = c.add(() => { requests++; confirmation = c.add(() => confirmation()); });
  let first = false, second = false;
  c.afterClose(() => { first = true; });
  confirmation(); await Promise.resolve();
  c.afterClose(() => { second = true; });
  assert.equal(requests, 2);
  settings(); confirmation(); b.flush(); await Promise.resolve(); b.flush();
  assert.equal(first, false); assert.equal(second, true);
});

test('accepted unsaved-changes confirmation continues the requested navigation', async () => {
  const b = browser(), c = new OverlayHistory(b);
  let confirmation;
  const settings = c.add(() => { if (!confirmation) confirmation = c.add(() => confirmation()); });
  let navigated = false;
  c.afterClose(() => { navigated = true; });
  settings(); confirmation(); b.flush();
  await Promise.resolve(); b.flush();
  assert.equal(navigated, true); assert.equal(b.index, 1);
});

test('manual close and immediate reopen consume only the old overlay entry', () => {
  const b = browser(), c = new OverlayHistory(b);
  const first = c.add(() => {}); first();
  let second; second = c.add(() => second());
  b.flush(); assert.equal(b.index, 2); assert.equal(b.length, 3);
  assert.equal(c.dismiss(), true); b.flush(); assert.equal(b.index, 1);
  assert.equal(c.dismiss(), false);
});

test('reload discards a stale guard before accepting newly opened dialogs', () => {
  const b = browser();
  b.history.pushState({ ccrOverlay: 'old-document' });
  const c = new OverlayHistory(b);
  let remove; remove = c.add(() => remove());
  b.flush(); assert.equal(b.index, 2); assert.equal(b.length, 3);
  c.dismiss(); b.flush(); assert.equal(b.index, 1);
  b.history.back(); b.flush(); assert.equal(b.url, '#/s/previous');
});

test('rejected close keeps the route protected and navigation waits for history cleanup', () => {
  const b = browser(), c = new OverlayHistory(b);
  const remove = c.add(() => {});
  b.history.back(); b.flush(); assert.equal(b.index, 2);
  let navigated = false;
  c.afterClose(() => { navigated = true; }); assert.equal(navigated, false);
  remove(); assert.equal(navigated, false); b.flush(); assert.equal(navigated, true); assert.equal(b.index, 1);
});
