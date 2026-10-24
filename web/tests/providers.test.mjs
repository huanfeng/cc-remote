import { test, before } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

let P, M, S;
before(async () => {
  const { build } = await import('esbuild');
  const web = join(dirname(fileURLToPath(import.meta.url)), '..');
  const outdir = join(web, 'node_modules', '.cache', 'providers-test');
  mkdirSync(outdir, { recursive: true });
  const outfile = join(outdir, 'providers.mjs');
  await build({
    stdin: { contents: "export * as P from './providers'; export * as M from './ModelPicker'; export * as S from './SessionSettings';", resolveDir: join(web, 'src'), loader: 'ts' },
    outfile, bundle: true, format: 'esm', platform: 'node', packages: 'external', jsx: 'automatic',
    loader: { '.css': 'empty' }, define: { 'import.meta.env': '{}' }, logLevel: 'silent',
  });
  ({ P, M, S } = await import(pathToFileURL(outfile).href));
});

test('legacy sessions remain Claude; native identities separate identical thread IDs', () => {
  assert.equal(P.providerOf(), 'claude');
  assert.equal(P.providerOf({}), 'claude');
  assert.equal(P.providerOf({ provider: 'codex' }), 'codex');
  assert.equal(P.sessionKey('same', true), 'native:same');
  assert.equal(P.sessionKey('same', true, 'codex'), 'native-codex:same');
  assert.equal(P.sessionKey('same', false, 'codex'), 'session:same');
});

test('native list, import and transcript paging retain the Codex provider', () => {
  assert.equal(P.nativeListPath('claude'), '/api/native/sessions');
  assert.equal(P.nativeListPath('codex'), '/api/native/sessions?provider=codex');
  for (const path of ['/api/native/sessions/thread/import', '/api/native/sessions/thread?tail=200', '/api/native/sessions/thread/events?before=123&limit=200', '/api/native/sessions/thread/events/123']) {
    const url = new URL(P.providerQuery(path, 'codex'), 'https://host');
    assert.equal(url.searchParams.get('provider'), 'codex');
    assert.equal(url.pathname, path.split('?')[0]);
  }
});

test('Codex creation remains managed even with a saved Claude terminal preference', () => {
  const overrides = { permissionMode: 'danger-full-access', model: ' gpt-test ', effort: 'none' };
  const body = P.newSessionBody('codex', ' Title ', ' /project ', overrides, 'terminal', true);
  assert.deepEqual(body, { provider: 'codex', title: 'Title', cwd: '/project', permissionMode: 'danger-full-access', model: 'gpt-test', effort: 'none', mode: 'managed', confirmDangerous: true });
  assert.equal('trustFolder' in body, false);
  const claude = P.newSessionBody('claude', '', '/project', { permissionMode: '', model: '', effort: '' }, 'terminal', true);
  assert.equal(claude.mode, 'terminal');
  assert.equal(claude.trustFolder, true);
});

test('Claude creation still passes an older host strict JSON decoder', () => {
  const accepted = new Set(['title', 'cwd', 'mode', 'trustFolder', 'resume', 'claudeArgs', 'permissionMode', 'model', 'effort', 'confirmDangerous']);
  for (const mode of ['managed', 'terminal']) {
    const wire = JSON.parse(JSON.stringify(P.newSessionBody('claude', ' Task ', ' /project ', { permissionMode: 'default', model: ' sonnet ', effort: 'high' }, mode, true)));
    assert.equal(Object.hasOwn(wire, 'provider'), false);
    assert.deepEqual(Object.keys(wire).filter(key => !accepted.has(key)), [], 'DisallowUnknownFields must accept the request');
    assert.equal(wire.model, 'sonnet');
    assert.equal(wire.mode, mode === 'terminal' ? 'terminal' : undefined);
  }
});

test('an older failed model refresh cannot evict the newer provider cache', async () => {
  let rejectOld, resolveNew;
  let calls = 0;
  const api = () => ++calls === 1 ? new Promise((_, reject) => { rejectOld = reject; }) : new Promise(resolve => { resolveNew = resolve; });
  const old = M.loadModels(api, '', false, 'codex');
  const refreshed = M.loadModels(api, '', true, 'codex');
  rejectOld(new Error('old request failed'));
  await assert.rejects(old, /old request failed/);
  resolveNew({ models: [{ id: 'new', source: 'codex' }] });
  assert.equal(await M.loadModels(api, '', false, 'codex'), await refreshed);
  assert.equal(calls, 2);
});

test('permissions and effort choices stay within each provider contract', () => {
  assert.deepEqual(M.permissionOptions('Inherit', '', 'codex').map(x => x.value), ['', 'read-only', 'workspace-write', 'danger-full-access']);
  assert.deepEqual(M.effortOptions('Inherit', 'codex').map(x => x.value), ['', 'none', 'minimal', 'low', 'medium', 'high', 'xhigh']);
  assert.equal(M.effortOptions('Inherit').at(-1).value, 'max');
  assert.ok(M.permissionOptions('Inherit').some(x => x.value === 'acceptEdits'));
  assert.equal(M.dangerousMode('danger-full-access', 'codex'), true);
  assert.equal(M.dangerousMode('workspace-write', 'codex'), false);
  assert.equal(M.dangerousMode('bypassPermissions', 'codex'), false);
});

test('enabling Codex full access requires confirmation, retaining it does not', () => {
  const previousWindow = globalThis.window;
  let confirmations = 0;
  globalThis.window = { confirm: message => { confirmations++; assert.match(message, /Codex/); return false; } };
  try {
    assert.equal(M.confirmMode('danger-full-access', '', 'codex'), false);
    assert.equal(M.confirmMode('danger-full-access', 'danger-full-access', 'codex'), true);
    assert.equal(M.confirmMode('workspace-write', '', 'codex'), true);
    assert.equal(confirmations, 1);
  } finally { globalThis.window = previousWindow; }
});

test('model caches isolate provider, session and host API; refresh preserves query parameters', async () => {
  const calls = [];
  const api = async path => { calls.push(path); return { models: [{ id: path, label: path, source: 'codex' }] }; };
  const claude = await M.loadModels(api);
  const codex = await M.loadModels(api, '', false, 'codex');
  assert.notDeepEqual(claude, codex);
  assert.equal(await M.loadModels(api), claude);
  assert.equal(await M.loadModels(api, '', false, 'codex'), codex);
  await M.loadModels(api, 'thread id', true, 'codex');
  assert.deepEqual(calls, ['/api/models', '/api/models?provider=codex', '/api/models?sessionId=thread+id&refresh=1&provider=codex']);
  const otherHost = async () => ({ models: [] });
  assert.notDeepEqual(await M.loadModels(otherHost, '', false, 'codex'), codex);
  assert.ok(M.MODEL_GROUPS.some(([, sources]) => sources.includes('codex')));
});

test('failed model discovery can retry; manual model IDs remain independent', async () => {
  let attempts = 0;
  const api = async () => { if (++attempts === 1) throw new Error('Discovery unavailable'); return { models: [], warnings: ['Enter a model manually'] }; };
  await assert.rejects(M.loadModels(api, '', false, 'codex'), /Discovery unavailable/);
  assert.deepEqual((await M.loadModels(api, '', false, 'codex')).models, []);
  const html = renderToStaticMarkup(createElement(M.ModelPicker, { api, provider: 'codex', value: 'custom-model', onChange() {} }));
  assert.match(html, /value="custom-model"/);
});

test('Codex session settings and chips identify the thread and inherit Codex configuration', () => {
  const session = { id: 'remote', provider: 'codex', title: 'Task', cwd: '/project', mode: 'managed', claudeSessionId: 'thread-id' };
  const html = renderToStaticMarkup(createElement(S.SessionSettings, { session, api: async () => ({}), close() {}, onUpdated() {}, onDelete() {} }));
  assert.match(html, /Codex thread ID/);
  assert.match(html, /thread-id/);
  assert.match(html, /Codex settings/);
  assert.doesNotMatch(html, /Claude|acceptEdits|bypassPermissions/);
  const chips = renderToStaticMarkup(createElement(S.SessionChips, { session, open() {} }));
  assert.match(chips, /Codex settings/);
  assert.doesNotMatch(chips, /Claude/);
});


test('Codex routing requires explicit host capability, independent of CLI installation', () => {
  for (const info of [undefined, {}, { version: '0.21.1', claudeAvailable: true }, { providers: ['claude'] }]) {
    assert.deepEqual(P.hostProviders(info), ['claude']);
  }
  for (const info of [{ providers: ['claude', 'codex'] }, { codexAvailable: true }, { codexAvailable: false }]) {
    assert.deepEqual(P.hostProviders(info), ['claude', 'codex']);
  }
});
