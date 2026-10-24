import { before, test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

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
export const useId = () => useRef('test-id').current;
`;

let createRenderer, SessionSettings, SessionStatus, ClaudeSettings, bundleURL;
before(async () => {
  const { build } = await import('esbuild');
  const web = join(dirname(fileURLToPath(import.meta.url)), '..');
  const outdir = join(web, 'node_modules', '.cache', 'session-information-test');
  mkdirSync(outdir, { recursive: true });
  const outfile = join(outdir, 'state.mjs');
  await build({
    stdin: { contents: "export { createRenderer } from 'react'; export { SessionSettings } from './SessionSettings'; export { SessionStatus } from './SessionStatus'; export { ClaudeSettings } from './ClaudeSettings';", resolveDir: join(web, 'src'), loader: 'ts' },
    outfile, bundle: true, platform: 'node', format: 'esm', packages: 'external', jsx: 'automatic', logLevel: 'silent', loader: { '.css': 'empty' },
    plugins: [{ name: 'effect-harness', setup(build) {
      build.onResolve({ filter: /^react$/ }, () => ({ path: 'hooks', namespace: 'test' }));
      build.onLoad({ filter: /.*/, namespace: 'test' }, () => ({ contents: hooks, loader: 'js' }));
    } }],
  });
  bundleURL = pathToFileURL(outfile).href;
  ({ createRenderer, SessionSettings, SessionStatus, ClaudeSettings } = await import(bundleURL));
  globalThis.document = { addEventListener() {}, removeEventListener() {}, activeElement: null, getElementById() {} };
  globalThis.window = { addEventListener() {}, removeEventListener() {}, confirm: () => true };
});
const settle = async () => { await Promise.resolve(); await Promise.resolve(); };
function nodes(tree) { return Array.isArray(tree) ? tree.flatMap(nodes) : tree && typeof tree === 'object' ? [tree, ...nodes(tree.props?.children)] : []; }
const content = tree => typeof tree === 'string' || typeof tree === 'number' ? String(tree) : Array.isArray(tree) ? tree.map(content).join(' ') : tree ? content(tree.props?.children) : '';
const find = (tree, predicate) => { const found = nodes(tree).find(predicate); assert.ok(found, 'Expected control exists'); return found; };
const button = (tree, label) => find(tree, node => node.type === 'button' && content(node) === label);
const control = (tree, label) => find(tree, node => node.props?.label === label);
const submit = async tree => { await find(tree, node => node.type === 'form').props.onSubmit({ preventDefault() {} }); await settle(); };
const session = { id: 'session/1', provider: 'codex', title: 'A session', cwd: '/work', mode: 'managed', status: 'running', model: 'configured-old', live: { model: 'reported-current', effort: 'high', costUSD: 0, durationMs: 0, contextTokens: 0, contextSize: 0, contextPercent: 0 }, lastConversationAt: '2026-10-21T09:15:00Z', conversationTimeEstimated: true, queued: [{ text: 'Next' }] };

test('details are read-only by default, disclose estimated times and keep token availability independent of cost', async () => {
  const renderer = createRenderer();
  const props = { api: async () => ({ turns: 2, inputTokens: 10, outputTokens: 5, cacheReadTokens: 0, cacheCreationTokens: 0, costKnown: false }), session, close() {}, onUpdated() {}, onDelete() {} };
  let tree = renderer.render(SessionSettings, props); renderer.effects(); await settle();
  tree = renderer.render(SessionSettings, props);
  assert.equal(nodes(tree).some(node => node.type === 'form'), false);
  assert.match(content(tree), /Estimated from history metadata/);
  assert.match(content(tree), /Current ·  reported-current/);
  assert.match(content(tree), /Configured \/ next turn ·  configured-old/);
  const rows = nodes(tree).filter(node => node.type === 'div');
  assert.match(content(rows.find(node => content(node).startsWith('Tokens '))), /15/);
  assert.match(content(rows.find(node => content(node).startsWith('Cost \(estimated\) '))), /Not provided/);
  assert.match(content(rows.find(node => content(node).startsWith('Queued messages '))), /1/);
  renderer.dispose();
});

test('running-session save sends only overrides and leaves reported values unchanged', async () => {
  const renderer = createRenderer(); let patched, updated, closed = 0;
  const props = { api: async (path, options) => { if (!options) return {}; patched = JSON.parse(options.body); return { ...session, ...patched }; }, session, close() { closed++; }, onUpdated(value) { updated = value; }, onDelete() {} };
  let tree = renderer.render(SessionSettings, props); renderer.effects(); await settle();
  button(tree, 'Edit session overrides').props.onClick();
  tree = renderer.render(SessionSettings, props);
  control(tree, 'Session model').props.onChange('next-model');
  tree = renderer.render(SessionSettings, props); await submit(tree);
  assert.equal(patched.model, 'next-model'); assert.equal(patched.live, undefined); assert.equal(closed, 0);
  tree = renderer.render(SessionSettings, { ...props, session: updated });
  assert.match(content(tree), /reported-current/); assert.match(content(tree), /next-model/);
  assert.equal(nodes(tree).some(node => node.type === 'form'), false);
  renderer.dispose();
});

test('dirty details close and cancel require discard confirmation; native history has no editing or management controls', () => {
  const renderer = createRenderer(); let closed = 0;
  const props = { api: async () => ({}), session, close() { closed++; }, onUpdated() {}, onDelete() {} };
  let tree = renderer.render(SessionSettings, props);
  button(tree, 'Edit session overrides').props.onClick(); tree = renderer.render(SessionSettings, props);
  control(tree, 'Session model').props.onChange('draft'); tree = renderer.render(SessionSettings, props);
  window.confirm = () => false;
  find(tree, node => node.props?.['aria-label'] === 'Close session details').props.onClick();
  button(tree, 'Cancel').props.onClick();
  assert.equal(closed, 0); assert.ok(nodes(renderer.render(SessionSettings, props)).some(node => node.type === 'form'));
  window.confirm = () => true; button(tree, 'Cancel').props.onClick();
  assert.equal(nodes(renderer.render(SessionSettings, props)).some(node => node.type === 'form'), false);
  const native = createRenderer(); const nativeTree = native.render(SessionSettings, { ...props, session: { ...session, readOnly: true } });
  assert.doesNotMatch(content(nativeTree), /Edit session overrides|Delete session/);
});

test('global sections are fixed, provider selection stays in defaults and preserves host state', async () => {
  const renderer = createRenderer();
  const identity = { name: 'My host', color: '', icon: '', shownName: 'My host', shownColor: '#293c2e', defaultName: 'host', defaultColor: 'green', palette: [{ key: 'green', hex: '#293c2e' }] };
  const props = { api: async path => path === '/api/identity' ? identity : path === '/api/info' ? { codexAvailable: true, codexVersion: '1' } : { provider: path.includes('codex') ? 'codex' : 'claude', model: '', effort: '', permissionMode: '', revision: 'r1' }, close() {} };
  let tree = renderer.render(ClaudeSettings, props); renderer.effects(); await settle();
  tree = renderer.render(ClaudeSettings, props);
  assert.deepEqual(nodes(tree).filter(node => node.props?.role === 'tab').map(node => content(node).split(' ')[0]), ['Session', 'This', 'This']);
  control(tree, 'Settings provider').props.onChange('codex'); tree = renderer.render(ClaudeSettings, props); renderer.effects(); await settle();
  tree = renderer.render(ClaudeSettings, props);
  assert.equal(nodes(tree).filter(node => node.props?.role === 'tab').length, 3);
  assert.doesNotMatch(content(tree), /Native configuration/);
  find(tree, node => node.props?.id === 'settings-tab-host').props.onClick(); tree = renderer.render(ClaudeSettings, props); renderer.effects(); await settle();
  tree = renderer.render(ClaudeSettings, props);
  assert.equal(nodes(tree).some(node => node.props?.label === 'Settings provider'), false);
  assert.ok(nodes(tree).some(node => node.type === 'input' && node.props.value === 'My host'));
  find(tree, node => node.type === 'input' && node.props.value === 'My host').props.onChange({ target: { value: 'Unsaved host' } });
  tree = renderer.render(ClaudeSettings, props); window.confirm = () => false;
  find(tree, node => node.props?.id === 'settings-tab-remote').props.onClick();
  assert.ok(nodes(renderer.render(ClaudeSettings, props)).some(node => node.type === 'input' && node.props.value === 'Unsaved host'));
  window.confirm = () => true;
  renderer.dispose();
});

test('defaults retain revision on save and keep edits after version conflict', async () => {
  const renderer = createRenderer(); let sent, closed = 0;
  const props = { api: async (path, options) => { if (options) { sent = JSON.parse(options.body); throw new Error('Settings changed elsewhere (revision conflict)'); } return { provider: 'claude', model: 'old', effort: '', permissionMode: '', revision: 'revision-original' }; }, close() { closed++; } };
  let tree = renderer.render(ClaudeSettings, props); renderer.effects(); await settle(); tree = renderer.render(ClaudeSettings, props);
  control(tree, 'CC Remote default model').props.onChange('unsaved-model'); tree = renderer.render(ClaudeSettings, props);
  await submit(tree); tree = renderer.render(ClaudeSettings, props);
  assert.equal(sent.revision, 'revision-original'); assert.equal(sent.model, 'unsaved-model');
  assert.match(content(tree), /revision conflict/); assert.equal(control(tree, 'CC Remote default model').props.value, 'unsaved-model');
  window.confirm = () => false; button(tree, 'Reload').props.onClick();
  find(tree, node => node.props?.['aria-label'] === 'Close settings').props.onClick();
  assert.equal(closed, 0); assert.equal(control(renderer.render(ClaudeSettings, props), 'CC Remote default model').props.value, 'unsaved-model');
  window.confirm = () => true; renderer.dispose();
});


test('late defaults replies cannot overwrite the selected provider', async () => {
  const renderer = createRenderer(); let finishClaude, finishCodex;
  const claude = new Promise(resolve => { finishClaude = resolve; });
  const codex = new Promise(resolve => { finishCodex = resolve; });
  const props = { api: path => path.includes('codex') ? codex : claude, close() {} };
  let tree = renderer.render(ClaudeSettings, props); renderer.effects();
  control(tree, 'Settings provider').props.onChange('codex');
  renderer.render(ClaudeSettings, props); renderer.effects();
  finishCodex({ provider: 'codex', model: 'codex-new', permissionMode: '', effort: '', revision: 'c2' }); await settle();
  finishClaude({ provider: 'claude', model: 'claude-late', permissionMode: '', effort: '', revision: 'c1' }); await settle();
  tree = renderer.render(ClaudeSettings, props);
  assert.equal(control(tree, 'CC Remote default model').props.value, 'codex-new');
  assert.equal(control(tree, 'Settings provider').props.value, 'codex');
  renderer.dispose();
});

test('native Claude advanced form retains all fields and revision, and protects provider switching', async () => {
  const renderer = createRenderer(); let sent;
  const native = { model: 'native-model', effortLevel: 'high', permissionMode: 'default', revision: 'native-revision', path: '/home/.claude/settings.json' };
  const props = { api: async (path, options) => { if (options) { sent = JSON.parse(options.body); return sent; } return path === '/api/claude/settings' ? native : { provider: 'claude', model: '', effort: '', permissionMode: '', revision: 'defaults-revision' }; }, close() {} };
  let tree = renderer.render(ClaudeSettings, props); renderer.effects(); await settle();
  tree = renderer.render(ClaudeSettings, props);
  find(tree, node => node.type === 'details').props.onToggle({ currentTarget: { open: true } });
  renderer.render(ClaudeSettings, props); renderer.effects(); await settle();
  tree = renderer.render(ClaudeSettings, props);
  control(tree, 'Claude default model').props.onChange('native-next'); tree = renderer.render(ClaudeSettings, props);
  window.confirm = () => false; control(tree, 'Settings provider').props.onChange('codex');
  assert.equal(control(renderer.render(ClaudeSettings, props), 'Settings provider').props.value, 'claude');
  const nativeForm = nodes(tree).filter(node => node.type === 'form')[1];
  await nativeForm.props.onSubmit({ preventDefault() {} }); await settle();
  assert.equal(sent.model, 'native-next'); assert.equal(sent.effortLevel, 'high'); assert.equal(sent.permissionMode, 'default'); assert.equal(sent.revision, 'native-revision'); assert.equal(sent.path, native.path);
  window.confirm = () => true; renderer.dispose();
});

test('known zero costs do not imply token reporting; explicitly reported zero tokens remain known', async () => {
  for (const tokensKnown of [undefined, false, true]) {
    const renderer = createRenderer();
    const props = { api: async () => ({ turns: 0, costKnown: true, costUSD: 0, tokensKnown, inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheCreationTokens: 0 }), session, close() {}, onUpdated() {}, onDelete() {} };
    renderer.render(SessionSettings, props); renderer.effects(); await settle();
    const tree = renderer.render(SessionSettings, props);
    const rows = nodes(tree).filter(node => node.type === 'div');
    const tokenRow = rows.find(node => content(node).startsWith('Tokens '));
    assert.equal(content(tokenRow).includes('Not provided'), tokensKnown !== true);
    assert.match(content(rows.find(node => content(node).startsWith('Cost (estimated) '))), /0\.0000/);
    renderer.dispose();
  }
});

test('quick summary keeps model/effort switch, attach and details callback contracts', () => {
  const renderer = createRenderer(); const calls = [];
  const props = { session, label: 'Running', anchor: { left: 0, bottom: 0 }, close() { calls.push('close'); }, quick(kind) { calls.push(kind); }, settings() { calls.push('details'); }, attach() { calls.push('attach'); } };
  const tree = renderer.render(SessionStatus, props);
  const switches = nodes(tree).filter(node => node.type === 'button' && content(node) === 'Switch');
  switches[0].props.onClick({ currentTarget: { getBoundingClientRect: () => ({ left: 1 }) } });
  switches[1].props.onClick({ currentTarget: { getBoundingClientRect: () => ({ left: 1 }) } });
  find(tree, node => node.type === 'button' && content(node).includes('Session details')).props.onClick();
  find(tree, node => node.type === 'button' && content(node).includes('Attach from a local terminal')).props.onClick();
  assert.deepEqual(calls, ['close', 'model', 'close', 'effort', 'close', 'details', 'close', 'attach']);
  assert.doesNotMatch(content(tree), /Directory|Cost|Permission mode/);
});


test('native history never requests managed stats, keeps missing metrics unknown and exposes import only when supplied', async () => {
  const renderer = createRenderer(); const paths = []; let imported = 0;
  const props = { api: async path => { paths.push(path); return { turns: 42 }; }, session: { ...session, id: 'native-thread-own-id', claudeSessionId: undefined, readOnly: true }, close() {}, onUpdated() {}, onDelete() {}, onImport() { imported++; } };
  renderer.render(SessionSettings, props); renderer.effects(); await settle();
  const tree = renderer.render(SessionSettings, props);
  assert.deepEqual(paths, []);
  assert.match(content(tree), /Codex thread ID native-thread-own-id/);
  assert.doesNotMatch(content(tree), /Current execution|Queued messages|Background tasks|Edit session overrides|Delete session/);
  assert.match(content(tree), /Turns Not provided/);
  button(tree, 'Import session…').props.onClick(); assert.equal(imported, 1);
  renderer.dispose();
});

test('decorated managed sessions omit empty queue/task counts and display zero; unreported actual configuration has one explanation', async () => {
  const renderer = createRenderer();
  const props = { api: async () => ({}), session: { ...session, queued: undefined, activeTasks: undefined, live: undefined }, close() {}, onUpdated() {}, onDelete() {} };
  renderer.render(SessionSettings, props); renderer.effects(); await settle();
  const tree = renderer.render(SessionSettings, props);
  assert.match(content(tree), /Queued messages 0/); assert.match(content(tree), /Background tasks 0/);
  assert.equal((content(tree).match(/Current configuration has not been reported/g) || []).length, 1);
  assert.doesNotMatch(content(tree), /Current ·  Not reported/);
  assert.match(content(tree), /Status Running/);
  renderer.dispose();
});

test('session status labels render in Chinese when the host language is Chinese', async () => {
  const previousDocument = globalThis.document;
  const previousDispatch = window.dispatchEvent;
  window.dispatchEvent = () => {};
  globalThis.document = { ...previousDocument, querySelector: () => null, documentElement: { lang: 'zh-CN', dataset: {}, style: { setProperty() {} } } };
  try {
    const translated = await import(bundleURL + '?lang=zh');
    const renderer = translated.createRenderer();
    const props = { api: async () => ({}), session: { ...session, status: 'waiting' }, close() {}, onUpdated() {}, onDelete() {} };
    const tree = renderer.render(translated.SessionSettings, props);
    assert.match(content(tree), /等待回答/);
    assert.doesNotMatch(content(tree), /Status waiting/);
  } finally { globalThis.document = previousDocument; window.dispatchEvent = previousDispatch; }
});
