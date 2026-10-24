import { before, test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

// Exercise polling and API changes with controlled effects and promises, without a DOM dependency.
const hooks = `
let active;
export function createRenderer() {
  const slots = []; let cursor, pending = [];
  return {
    render(Component, props) { cursor = 0; active = { slots, next: () => cursor++, pending }; try { return Component(props); } finally { active = undefined; } },
    effects() { for (const run of pending.splice(0)) run(); },
    dispose() { for (const slot of slots) slot?.cleanup?.(); },
  };
}
export function useState(initial) {
  const { slots, next } = active, i = next();
  if (!slots[i]) slots[i] = { value: initial };
  return [slots[i].value, value => { slots[i].value = typeof value === 'function' ? value(slots[i].value) : value; }];
}
export function useRef(value) { const { slots, next } = active, i = next(); return slots[i] ||= { current: value }; }
export function useEffect(effect, deps) {
  const { slots, next, pending } = active, i = next(), old = slots[i];
  if (!old || deps.some((value, n) => !Object.is(value, old.deps[n]))) {
    slots[i] = { deps, cleanup: old?.cleanup };
    pending.push(() => { slots[i].cleanup?.(); slots[i].cleanup = effect(); });
  }
}
export const useLayoutEffect = useEffect;
`;

let createRenderer, UsageBar, useUsage;
before(async () => {
  const { build } = await import('esbuild');
  const web = join(dirname(fileURLToPath(import.meta.url)), '..');
  const outdir = join(web, 'node_modules', '.cache', 'usage-test');
  mkdirSync(outdir, { recursive: true });
  const outfile = join(outdir, 'usage.mjs');
  await build({
    stdin: { contents: "export { createRenderer } from 'react'; export { UsageBar, useUsage } from './Usage';", resolveDir: join(web, 'src'), loader: 'ts' },
    outfile, bundle: true, platform: 'node', format: 'esm', packages: 'external', jsx: 'automatic', logLevel: 'silent',
    plugins: [{ name: 'effect-harness', setup(build) {
      build.onResolve({ filter: /^react$/ }, () => ({ path: 'hooks', namespace: 'test' }));
      build.onLoad({ filter: /.*/, namespace: 'test' }, () => ({ contents: hooks, loader: 'js' }));
      build.onLoad({ filter: /\.css$/ }, () => ({ contents: '', loader: 'js' }));
    } }],
  });
  ({ createRenderer, UsageBar, useUsage } = await import(pathToFileURL(outfile).href));
});

const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
const settle = async () => { await Promise.resolve(); await Promise.resolve(); };
const reply = (fiveHour = 0, weekly = 100) => ({ available: true, usage: { fiveHour: { percent: fiveHour }, weekly: { percent: weekly } }, fetchedAt: '2026-10-24T00:00:00Z' });
function nodes(tree) { return Array.isArray(tree) ? tree.flatMap(nodes) : tree && typeof tree === 'object' ? [tree, ...nodes(tree.props?.children)] : []; }
const byClass = (tree, name) => nodes(tree).filter(node => node.props?.className?.split(' ').includes(name));

function environment() {
  const oldDocument = globalThis.document, oldWindow = globalThis.window;
  const listeners = new Set(), timers = new Set();
  globalThis.document = { hidden: false, addEventListener(event, fn) { assert.equal(event, 'visibilitychange'); listeners.add(fn); }, removeEventListener(event, fn) { listeners.delete(fn); } };
  globalThis.window = { setInterval(fn, ms) { assert.equal(ms, 60000); const id = setInterval(() => {}, 60000); timers.add(id); this.tick = fn; return id; } };
  return { visibility() { for (const fn of listeners) fn(); }, restore() { assert.equal(listeners.size, 0); for (const id of timers) clearInterval(id); globalThis.document = oldDocument; globalThis.window = oldWindow; } };
}

test('rings show used endpoints, severity, Claude ownership and the existing details action', async () => {
  const env = environment(), renderer = createRenderer(); let opened = 0;
  const props = { api: async () => reply(), open: () => opened++ };
  try {
    renderer.render(UsageBar, props); renderer.effects(); await settle();
    const tree = renderer.render(UsageBar, props);
    assert.deepEqual(byClass(tree, 'usage-value').map(node => node.props.children), ['0%', '100%']);
    assert.deepEqual(byClass(tree, 'usage-ring-fill').map(node => node.props.strokeDasharray), ['0 100', '100 100']);
    assert.equal(byClass(tree, 'usage-ring-fill')[1].props.className, 'usage-ring-fill high');
    assert.match(tree.props['aria-label'], /Claude subscription usage/);
    assert.match(tree.props.title, /100% used/);
    assert.match(tree.props.title, /Updated/);
    tree.props.onClick(); assert.equal(opened, 1);
  } finally { renderer.dispose(); env.restore(); }
});

test('loading, unavailable, stale, errors and invalid windows stay neutral', async () => {
  const env = environment(), renderer = createRenderer();
  const neutral = tree => {
    assert.equal(byClass(tree, 'usage-ring').length, 2);
    assert.equal(byClass(tree, 'usage-ring-fill').length, 0);
    assert.deepEqual(byClass(tree, 'usage-value').map(node => node.props.children), ['—', '—']);
  };
  try {
    const initial = renderer.render(UsageBar, { api: () => new Promise(() => {}), open() {} });
    neutral(initial); assert.match(initial.props.title, /Loading/);
    for (const data of [{ available: false, reason: 'api-key' }, { ...reply(), stale: true }, { ...reply(), error: 'rate-limited' }, reply(NaN, 101), { available: true }]) {
      const props = { api: async () => data, open() {} };
      renderer.render(UsageBar, props); renderer.effects(); await settle();
      neutral(renderer.render(UsageBar, props));
    }
  } finally { renderer.dispose(); env.restore(); }
});

test('partial readings keep the known window and include full reset and update times', async () => {
  const env = environment(), renderer = createRenderer();
  const props = { api: async () => ({ ...reply(), usage: { fiveHour: { percent: 78, resetsAt: '2026-10-25T00:00:00Z' } } }), open() {} };
  try {
    renderer.render(UsageBar, props); renderer.effects(); await settle();
    const tree = renderer.render(UsageBar, props);
    assert.deepEqual(byClass(tree, 'usage-value').map(node => node.props.children), ['78%', '—']);
    assert.equal(byClass(tree, 'usage-ring-fill')[0].props.className, 'usage-ring-fill warn');
    assert.match(tree.props.title, /resets in/);
    assert.match(tree.props.title, /Weekly limit: unknown/);
    assert.match(tree.props.title, /Usage unknown/);
  } finally { renderer.dispose(); env.restore(); }
});

test('API switches hide old data before effects and ignore late old-host replies', async () => {
  const env = environment(), renderer = createRenderer(), late = deferred(), next = deferred();
  let calls = 0;
  const oldApi = () => ++calls === 1 ? Promise.resolve(reply(25)) : late.promise;
  const nextApi = () => next.promise;
  try {
    renderer.render(useUsage, oldApi); renderer.effects(); await settle();
    assert.equal(renderer.render(useUsage, oldApi).usage.fiveHour.percent, 25);
    env.visibility();
    assert.equal(renderer.render(useUsage, nextApi), undefined);
    late.resolve(reply(99)); await settle();
    assert.equal(renderer.render(useUsage, nextApi), undefined);
    renderer.effects(); next.resolve(reply(12)); await settle();
    assert.equal(renderer.render(useUsage, nextApi).usage.fiveHour.percent, 12);
  } finally { renderer.dispose(); env.restore(); }
});

test('visible-page polling ignores out-of-order replies and marks rejected refreshes stale', async () => {
  const env = environment(), renderer = createRenderer(), requests = [];
  const api = path => { assert.equal(path, '/api/usage'); const request = deferred(); requests.push(request); return request.promise; };
  try {
    document.hidden = true;
    renderer.render(useUsage, api); renderer.effects(); assert.equal(requests.length, 0);
    document.hidden = false; env.visibility(); window.tick();
    requests[1].resolve(reply(75)); await settle();
    requests[0].resolve(reply(10)); await settle();
    assert.equal(renderer.render(useUsage, api).usage.fiveHour.percent, 75);
    window.tick(); requests[2].reject(new Error('offline')); await settle();
    const failed = renderer.render(useUsage, api);
    assert.equal(failed.stale, true); assert.equal(failed.error, 'request-failed');
    assert.equal(failed.usage.fiveHour.percent, 75);
    window.tick(); requests[3].resolve(reply(80)); await settle();
    assert.equal(renderer.render(useUsage, api).stale, undefined);
    window.tick(); renderer.dispose(); requests[4].resolve(reply(90)); await settle();
    assert.equal(renderer.render(useUsage, api).usage.fiveHour.percent, 80);
  } finally { renderer.dispose(); env.restore(); }
});
