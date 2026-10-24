import { before, test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

// Exercise effect cleanup and out-of-order replies without a DOM dependency.
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
  if (!slots[i]) slots[i] = { value: typeof initial === 'function' ? initial() : initial };
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

let createRenderer, ModelPicker, QuickSwitch;
before(async () => {
  const { build } = await import('esbuild');
  const web = join(dirname(fileURLToPath(import.meta.url)), '..');
  const outdir = join(web, 'node_modules', '.cache', 'model-state-test');
  mkdirSync(outdir, { recursive: true });
  const outfile = join(outdir, 'state.mjs');
  await build({
    stdin: { contents: "export { createRenderer } from 'react'; export { ModelPicker } from './ModelPicker'; export { QuickSwitch } from './QuickSwitch';", resolveDir: join(web, 'src'), loader: 'ts' },
    outfile, bundle: true, platform: 'node', format: 'esm', packages: 'external', jsx: 'automatic', logLevel: 'silent',
    plugins: [{ name: 'effect-harness', setup(build) {
      build.onResolve({ filter: /^react$/ }, () => ({ path: 'hooks', namespace: 'test' }));
      build.onLoad({ filter: /.*/, namespace: 'test' }, () => ({ contents: hooks, loader: 'js' }));
    } }],
  });
  ({ createRenderer, ModelPicker, QuickSwitch } = await import(pathToFileURL(outfile).href));
});

const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
const reply = label => ({ models: [{ id: label, label, source: 'codex' }], warnings: [], fetchedAt: '2026-10-24T00:00:00Z' });
const settle = async () => { await Promise.resolve(); await Promise.resolve(); };
function nodes(tree) { return Array.isArray(tree) ? tree.flatMap(nodes) : tree && typeof tree === 'object' ? [tree, ...nodes(tree.props?.children)] : []; }
const text = tree => JSON.stringify(tree, (key, value) => key === 'ref' ? undefined : value);

test('model picker hides the previous provider before effects and ignores its late reply', async () => {
  const renderer = createRenderer(), old = deferred(), next = deferred();
  const api = path => path.includes('provider=codex') ? next.promise : old.promise;
  const props = { api, provider: 'claude', value: '', onChange() {} };
  let tree = renderer.render(ModelPicker, props); renderer.effects();
  nodes(tree).find(node => node.props?.['aria-label'] === 'Show model list').props.onClick();
  const codexProps = { ...props, provider: 'codex' };
  renderer.render(ModelPicker, codexProps);
  old.resolve(reply('old-claude-model')); await settle();
  assert.doesNotMatch(text(renderer.render(ModelPicker, codexProps)), /old-claude-model/);
  renderer.effects();
  next.resolve(reply('new-codex-model')); await settle();
  tree = renderer.render(ModelPicker, codexProps);
  assert.match(text(tree), /new-codex-model/);
  assert.doesNotMatch(text(tree), /old-claude-model/);
  renderer.dispose();
});

test('quick switch ignores previous-provider replies and reloads when the API changes', async () => {
  const previousDocument = globalThis.document;
  // i18n has already initialized outside browser mode; listeners need only cleanup.
  globalThis.document = { addEventListener() {}, removeEventListener() {} };
  const renderer = createRenderer(), old = deferred(), next = deferred();
  const api = path => path.includes('provider=codex') ? next.promise : old.promise;
  const props = { api, provider: 'claude', sessionId: 'same', target: { kind: 'model', anchor: { left: 0, top: 0, bottom: 0 } }, current: '', close() {}, pick() {} };
  try {
    renderer.render(QuickSwitch, props); renderer.effects();
    const codexProps = { ...props, provider: 'codex' };
    renderer.render(QuickSwitch, codexProps); renderer.effects();
    next.resolve(reply('codex-current')); await settle();
    old.resolve(reply('claude-late')); await settle();
    let tree = renderer.render(QuickSwitch, codexProps);
    assert.match(text(tree), /codex-current/);
    assert.doesNotMatch(text(tree), /claude-late/);
    const otherHost = { ...codexProps, api: async () => reply('other-host') };
    assert.doesNotMatch(text(renderer.render(QuickSwitch, otherHost)), /codex-current/);
    renderer.effects(); await settle();
    tree = renderer.render(QuickSwitch, otherHost);
    assert.match(text(tree), /other-host/);
  } finally { renderer.dispose(); globalThis.document = previousDocument; }
});
