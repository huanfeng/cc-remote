import { test } from 'node:test';
import assert from 'node:assert/strict';
import { fileDockStore } from '../src/features/files/fileDockState.ts';

const file = (path, base) => ({ kind: 'file', path, base });

test('the file panel groups positions in the same file and retains distinct base paths', () => {
  const store = fileDockStore({});
  const key = 'session';
  const { open } = store.getState();
  open(key, file('README.md:3', ['/source']));
  const first = store.getState().docks.get(key).active;
  open(key, file('README.md:9', ['/source']));
  assert.deepEqual(store.getState().docks.get(key).tabs, [{ id: first, entry: file('README.md:9', ['/source']) }]);
  open(key, file('README.md:3', ['/other']));
  assert.equal(store.getState().docks.get(key).tabs.length, 2);
  assert.notEqual(store.getState().docks.get(key).active, first);
});

test('tabs survive collapsing, select a neighbor on close, and stay scoped to host and session', () => {
  const backend = {};
  const store = fileDockStore(backend);
  const { open, close, collapse, select } = store.getState();
  const key = 'managed';
  open(key, file('README.md'));
  const first = store.getState().docks.get(key).active;
  open(key, { kind: 'attachment', attachment: { id: 'abc', name: 'a.png', url: '/asset/abc' } });
  const image = store.getState().docks.get(key).active;
  open(key, file('index.ts'));
  const last = store.getState().docks.get(key).active;
  collapse(key, true);
  assert.equal(fileDockStore(backend).getState().docks.get(key).tabs.length, 3);
  assert.equal(store.getState().docks.get(key).collapsed, true);
  select(key, image);
  assert.equal(store.getState().docks.get(key).collapsed, false);
  close(key, image);
  assert.equal(store.getState().docks.get(key).active, last);
  close(key, last);
  assert.equal(store.getState().docks.get(key).active, first);
  assert.equal(fileDockStore({}).getState().docks.has(key), false);
  assert.equal(store.getState().docks.has('unrelated'), false);
  close(key, first);
  assert.equal(store.getState().docks.has(key), false);
});
