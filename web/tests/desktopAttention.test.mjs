import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { aggregateDesktopAttention, createDesktopAttention } from '../src/platform/desktopAttention.ts';

test('desktop attention includes inactive hosts once and drops closed or signed-out tabs', () => {
  const a = { hostId: 'a', waiting: 2, done: 1, running: 9, phase: 'ready' };
  const b = { hostId: 'b', waiting: 0, done: 3, running: 7, phase: 'ready' };
  assert.deepEqual(aggregateDesktopAttention([a, b, { ...a }]), { waiting: 2, done: 4, total: 6 });
  assert.deepEqual(aggregateDesktopAttention([a]), { waiting: 2, done: 1, total: 3 });
  assert.deepEqual(aggregateDesktopAttention([{ ...a, phase: 'signed-out' }]), { waiting: 0, done: 0, total: 0 });
  assert.deepEqual(aggregateDesktopAttention([{ hostId: 'old', waiting: 1 }]), { waiting: 1, done: 0, total: 1 });
});

test('native overlay writes are serialized, counts clear at zero and resources close', async () => {
  const calls = [], closed = [];
  let release;
  const pending = new Promise(resolve => { release = resolve; });
  const controller = createDesktopAttention('windows', {
    async image(counts) { calls.push(['image', counts.total]); await pending; return { close: async () => closed.push('image') }; },
    async overlay(image) { calls.push(['overlay', !!image]); },
    async badge(count) { calls.push(['badge', count]); },
  });
  const one = controller.update({ waiting: 1, done: 0, total: 1 });
  await new Promise(resolve => setImmediate(resolve));
  const two = controller.update({ waiting: 0, done: 0, total: 0 });
  release();
  await Promise.all([one, two]);
  assert.deepEqual(calls.map(call => call[0]), ['image', 'overlay', 'overlay']);
  assert.equal(calls.at(-1)[1], false, 'an earlier slow image must not restore a cleared badge');
  assert.equal(closed.length, 1);
  await controller.update({ waiting: 0, done: 0, total: 0 });
  assert.equal(calls.length, 3, 'unchanged counts do not repeat native calls');
  await controller.dispose();
});

test('Dock badges use the numeric API and desktop capabilities allow both badge APIs', async () => {
  const calls = [];
  const controller = createDesktopAttention('mac', { image: async () => { throw Error('unexpected image'); }, overlay: async () => {}, badge: async count => { calls.push(count); } });
  await controller.update({ waiting: 0, done: 8, total: 8 });
  await controller.dispose();
  assert.deepEqual(calls, [8, undefined]);
  const permissions = JSON.parse(readFileSync(new URL('../../desktop/src-tauri/capabilities/default.json', import.meta.url))).permissions;
  assert.ok(permissions.includes('core:window:allow-set-overlay-icon'));
  assert.ok(permissions.includes('core:window:allow-set-badge-count'));
});

test('recreating a controller cannot let an old cleanup clear the new badge', async () => {
  const calls = [];
  let release;
  const pending = new Promise(resolve => { release = resolve; });
  const bridge = {
    image: async () => { await pending; return { close: async () => {} }; },
    overlay: async image => { calls.push(!!image); }, badge: async () => {},
  };
  const old = createDesktopAttention('windows', bridge);
  const first = old.update({ waiting: 1, done: 0, total: 1 });
  await new Promise(resolve => setImmediate(resolve));
  const disposed = old.dispose();
  const current = createDesktopAttention('windows', bridge);
  const next = current.update({ waiting: 0, done: 2, total: 2 });
  release();
  await Promise.all([first, disposed, next]);
  assert.deepEqual(calls, [true, false, true]);
  await current.dispose();
});

test('the Tauri bridge forwards DPI-scaled RGBA and releases the image after overlay IPC', async t => {
  const calls = [];
  const context = {
    scale() {}, roundRect() {}, fill() {}, stroke() {}, fillText() {},
    measureText: () => ({ actualBoundingBoxAscent: 7, actualBoundingBoxDescent: 0 }),
    getImageData: (_x, _y, width, height) => ({ data: new Uint8ClampedArray(width * height * 4) }),
  };
  const globals = {
    document: { createElement: () => ({ getContext: () => context }) },
    devicePixelRatio: 1.5,
  };
  for (const [name, value] of Object.entries(globals)) {
    const descriptor = Object.getOwnPropertyDescriptor(globalThis, name);
    Object.defineProperty(globalThis, name, { configurable: true, value });
    t.after(() => {
      if (descriptor) Object.defineProperty(globalThis, name, descriptor);
      else delete globalThis[name];
    });
  }
  const saved = globalThis.window;
  globalThis.window = { __TAURI_INTERNALS__: { metadata: { currentWindow: { label: 'main' } }, invoke: async (command, args) => {
    calls.push([command, args]);
    if (command === 'plugin:image|new') return 7;
  } } };
  try {
    const current = createDesktopAttention('windows');
    await current.update({ waiting: 1, done: 2, total: 3 });
    assert.deepEqual(calls.map(call => call[0]), ['plugin:image|new', 'plugin:window|set_overlay_icon', 'plugin:resources|close']);
    assert.equal(calls[0][1].width, 24);
    assert.equal(calls[0][1].height, 24);
    assert.equal(calls[0][1].rgba.length, 24 * 24 * 4);
    assert.equal(calls[1][1].label, 'main');
    assert.equal(calls[1][1].value, 7);
    await current.dispose();
    assert.equal(calls.at(-1)[1].value, undefined);
  } finally { if (saved === undefined) delete globalThis.window; else globalThis.window = saved; }
});
