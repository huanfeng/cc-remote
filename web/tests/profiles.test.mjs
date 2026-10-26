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

let createRenderer, SessionProfile, NewSessionProfile, SupplierBadge, P, B;
before(async () => {
  const { build } = await import('esbuild');
  const web = join(dirname(fileURLToPath(import.meta.url)), '..');
  const outdir = join(web, 'node_modules', '.cache', 'profiles-test');
  mkdirSync(outdir, { recursive: true });
  const outfile = join(outdir, 'profiles.mjs');
  await build({
    stdin: { contents: "export { createRenderer } from 'react'; export { SessionProfile, NewSessionProfile, SupplierBadge } from './SupplierProfile'; export * as P from './profiles'; export * as B from './providers';", resolveDir: join(web, 'src'), loader: 'ts' },
    outfile, bundle: true, platform: 'node', format: 'esm', packages: 'external', jsx: 'automatic', logLevel: 'silent',
    plugins: [{ name: 'effect-harness', setup(build) {
      build.onResolve({ filter: /^react$/ }, () => ({ path: 'hooks', namespace: 'test' }));
      build.onLoad({ filter: /.*/, namespace: 'test' }, () => ({ contents: hooks, loader: 'js' }));
    } }],
  });
  ({ createRenderer, SessionProfile, NewSessionProfile, SupplierBadge, P, B } = await import(pathToFileURL(outfile).href));
});

const catalog = { source: 'cc-switch', schemaVersion: 1, capabilities: {resumeSelection:true,switchSupplier:true}, profiles: [{ id: 'one', name: 'First relay', app: 'claude' }, { id: 'two', name: 'Backup relay', app: 'claude' }, { id: 'cx', name: 'Codex relay', app: 'codex' }], defaultProfiles: { claude: 'one' } };
const settle = async () => { await Promise.resolve(); await Promise.resolve(); await Promise.resolve(); };
const nodes = tree => Array.isArray(tree) ? tree.flatMap(nodes) : tree && typeof tree === 'object' ? [tree, ...nodes(tree.props?.children)] : [];
const content = tree => typeof tree === 'string' || typeof tree === 'number' ? String(tree) : Array.isArray(tree) ? tree.map(content).join(' ') : tree ? content(tree.props?.children) : '';
function find(tree, predicate) { const node = nodes(tree).find(predicate); assert.ok(node, 'Expected control exists'); return node; }
const button = (tree, text) => find(tree, node => node.type === 'button' && text.test(content(node)));
const selection = tree => find(tree, node => node.type?.name === 'Select');

test('supplier badge identifies idle and running sessions without fetching the catalog', () => {
  for (const status of ['idle', 'running', 'waiting']) {
    assert.match(content(SupplierBadge({session:{profileId:'one',profileName:'Saved supplier',status}})), /Saved supplier/);
  }
  assert.match(content(SupplierBadge({session:{}})), /Native host|宿主原生/);
  assert.match(content(SupplierBadge({session:{profileId:'ccswitch:missing'}})), /ccswitch:missing/);
});

test('session details show the saved supplier name after the catalog is renamed', async () => {
  const r=createRenderer(); const props={api:async()=>catalog,app:'claude',session:{id:'s',profileId:'one',profileName:'Saved supplier'},onBusy(){},onUpdated(){}};
  r.render(SessionProfile,props);r.effects();await settle();
  const tree=r.render(SessionProfile,props);
  assert.ok(nodes(tree).some(n=>n.type==='strong' && content(n)==='Saved supplier'));
  r.dispose();
});

test('supplier catalog stays separate from engine and defaults stay app-scoped', () => {
  assert.deepEqual(P.profilesFor(catalog, 'codex').map(p => p.id), ['cx']);
  assert.equal(P.defaultProfile(catalog, 'claude').id, 'one');
  assert.equal(P.defaultProfile(catalog, 'codex'), undefined);
  const overrides = { permissionMode: '', model: '', effort: '' };
  assert.equal(B.newSessionBody('codex', '', '/project', overrides, 'terminal', true, 'cx').profileId, 'cx');
  assert.equal(Object.hasOwn(B.newSessionBody('claude', '', '/project', overrides, 'terminal', true), 'profileId'), false);
});

test('new-session picker filters suppliers and shows configured default', async () => {
  const r = createRenderer();
  const props = { api: async () => catalog, app: 'claude', value: '', onChange() {} };
  r.render(NewSessionProfile, props); r.effects(); await settle();
  const tree = r.render(NewSessionProfile, props);
  assert.deepEqual(selection(tree).props.options.map(p => p.value), ['', '@native', 'one', 'two']);
  assert.match(selection(tree).props.options[0].label, /First relay/);
  r.dispose();
});

test('older hosts show upgrade guidance without unsupported controls', async () => {
  const r = createRenderer();
  const props = { api: async () => { throw Object.assign(new Error('Not found'), { status: 404 }); }, app: 'claude', value: '', onChange() {} };
  r.render(NewSessionProfile, props); r.effects(); await settle();
  const tree = r.render(NewSessionProfile, props);
  assert.match(content(tree), /Upgrade this host|请升级此宿主/);
  assert.equal(nodes(tree).some(node => node.type?.name === 'Select'), false);
  r.dispose();
});

test('switching hosts immediately hides the previous supplier catalog', async () => {
  const r = createRenderer();
  const props = { api: async () => catalog, app: 'claude', value: '', onChange() {} };
  r.render(NewSessionProfile, props); r.effects(); await settle();
  assert.ok(r.render(NewSessionProfile, props));
  const next = { ...props, api: async () => ({ ...catalog, profiles: [], defaultProfiles: {} }) };
  assert.equal(r.render(NewSessionProfile, next), null);
  r.effects(); await settle();
  assert.deepEqual(selection(r.render(NewSessionProfile, next)).props.options.map(p => p.value), ['', '@native']);
  r.dispose();
});

test('active session requires explicit interruption confirmation, never automatic force retry', async () => {
  const r = createRenderer(); const calls = []; const busy = []; let updated;
  const props = { app: 'claude', session: { id: 'session/x', profileId: 'one' }, onBusy: value => busy.push(value), onUpdated: value => { updated = value; }, api: async (path, options) => {
    if (path === '/api/profiles') return catalog;
    calls.push({ path, body: JSON.parse(options.body) });
    if (calls.length === 1) throw Object.assign(new Error('Session is busy'), { status: 409 });
    return { id: 'session/x', profileId: 'two' };
  } };
  r.render(SessionProfile, props); r.effects(); await settle();
  let tree = r.render(SessionProfile, props);
  selection(tree).props.onChange('two');
  tree = r.render(SessionProfile, props);
  button(tree, /Switch supplier|切换供应商/).props.onClick(); await settle();
  tree = r.render(SessionProfile, props);
  assert.equal(calls.length, 1);
  assert.deepEqual(calls[0], { path: '/api/sessions/session%2Fx/profile', body: { profileId: 'two' } });
  assert.ok(nodes(tree).some(node => node.props?.role === 'alertdialog'));
  assert.match(content(tree), /not replay|不会自动重发/);
  button(tree, /Interrupt and switch|中断并切换/).props.onClick(); await settle();
  assert.deepEqual(calls[1].body, { profileId: 'two', force: true });
  assert.equal(updated.profileId, 'two');
  assert.deepEqual(busy, [true, false, true, false]);
  r.dispose();
});

test('non-conflict switch errors do not offer forced interruption', async () => {
  const r = createRenderer();
  const props = { app: 'claude', session: { id: 's', profileId: 'one' }, onBusy() {}, onUpdated() {}, api: async path => {
    if (path === '/api/profiles') return catalog;
    throw Object.assign(new Error('Invalid supplier'), { status: 400 });
  } };
  r.render(SessionProfile, props); r.effects(); await settle();
  selection(r.render(SessionProfile, props)).props.onChange('two');
  button(r.render(SessionProfile, props), /Switch supplier|切换供应商/).props.onClick(); await settle();
  const tree = r.render(SessionProfile, props);
  assert.match(content(tree), /Invalid supplier/);
  assert.equal(nodes(tree).some(node => node.props?.role === 'alertdialog'), false);
  r.dispose();
});

 test('resume picker keeps saved supplier by default and offers explicit native configuration', async () => {
 const r = createRenderer(); const props = { api: async () => catalog, app:'claude', value:'', onChange(){}, resuming:true };
 r.render(NewSessionProfile, props); r.effects(); await settle();
 const options = selection(r.render(NewSessionProfile, props)).props.options;
 assert.match(options[0].label, /Keep session|沿用原/);
 assert.equal(options[1].value, '@native'); r.dispose();
 });

test('legacy export catalogs and future contracts never enable CC-Switch controls', async () => {
 for (const incompatible of [{profiles:catalog.profiles,defaultProfiles:{}}, {...catalog,schemaVersion:2}, '<html>old server</html>']) {
 const r = createRenderer(); const props={api:async()=>incompatible,app:'claude',value:'',onChange(){},resuming:true};
 r.render(NewSessionProfile,props);r.effects();await settle();const tree=r.render(NewSessionProfile,props);
 assert.match(content(tree), /Upgrade this host|请升级此宿主/);
 assert.equal(nodes(tree).some(n=>n.type?.name==='Select'),false);r.dispose();
 }
});
test('resume is hidden when the selected host lacks resume selection capability', async () => {
 const r=createRenderer();const props={api:async()=>({...catalog,capabilities:{}}),app:'claude',value:'',onChange(){},resuming:true};
 r.render(NewSessionProfile,props);r.effects();await settle();assert.match(content(r.render(NewSessionProfile,props)), /Upgrade this host|请升级此宿主/);r.dispose();
});
