import { before, test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

let CodexPermissionDetails;
before(async () => {
  const { build } = await import('esbuild');
  const web = join(dirname(fileURLToPath(import.meta.url)), '..');
  const outdir = join(web, 'node_modules', '.cache', 'codex-permission-details-test');
  mkdirSync(outdir, { recursive: true });
  const outfile = join(outdir, 'details.mjs');
  await build({ entryPoints: [join(web, 'src/CodexPermissionDetails.tsx')], outfile, bundle: true, platform: 'node', format: 'esm', packages: 'external', jsx: 'automatic', loader: { '.css': 'empty' }, logLevel: 'silent' });
  ({ CodexPermissionDetails } = await import(pathToFileURL(outfile).href));
});
const render = input => renderToStaticMarkup(createElement(CodexPermissionDetails, { input }));
const request = permissions => ({ reason: 'Run project checks', cwd: 'D:\\work\\my project', threadId: 'secret-thread', turnId: 'secret-turn', itemId: 'secret-item', environmentId: 'secret-env', startedAtMs: 123, permissions });

test('shows reason, exact cwd, network and read/write paths with turn-only scope, hiding metadata', () => {
  const html = render(request({ network: { enabled: true }, fileSystem: { read: ['/work/a b', '~/literal/**'], write: ['D:\\build\\output'] } }));
  for (const value of ['Run project checks', 'D:\\work\\my project', '/work/a b', '~/literal/**', 'D:\\build\\output', 'Read paths', 'Write paths']) assert.ok(html.includes(value), value);
  assert.match(html, /Network access/);
  assert.match(html, /Requested/);
  assert.match(html, /this turn only/);
  assert.doesNotMatch(html, /secret-|threadId|turnId|itemId|environmentId|startedAtMs|<button|<pre/);
});

test('typed entries show read, write, deny, glob patterns and exact special directory values', () => {
  const entries = [
    { access: 'read', path: { type: 'path', path: '/work/exact path' } },
    { access: 'write', path: { type: 'glob_pattern', pattern: '**/*.{ts,tsx}' } },
    { access: 'deny', path: { type: 'path', path: '/private' } },
    ...['root', 'minimal', 'tmpdir', 'slash_tmp', 'project_roots', 'unknown'].map(kind => ({ access: 'read', path: { type: 'special', value: { kind, ...(kind === 'project_roots' ? { subpath: 'src/**' } : {}), ...(kind === 'unknown' ? { path: '/unresolved', subpath: '../exact' } : {}) } } })),
  ];
  const html = render(request({ fileSystem: { entries, globScanMaxDepth: 3 } }));
  for (const value of ['Read', 'Write', 'Deny', 'Path', '/work/exact path', 'Glob pattern', '**/*.{ts,tsx}', '/private', 'Filesystem root', 'Minimal default directories', 'System temporary directory', '/tmp directory', 'Project roots', 'Unrecognized special directory', 'root', 'minimal', 'tmpdir', 'slash_tmp', 'project_roots', 'unknown', 'src/**', '/unresolved', '../exact', 'Glob scan depth', '>3<']) assert.ok(html.includes(value), value);
  assert.doesNotMatch(html, /\[object Object\]|&quot;access&quot;|&quot;permissions&quot;/);
});

test('explicit false is distinct from missing network settings; empty lists do not imply no permissions', () => {
  assert.match(render(request({ network: { enabled: false } })), /Not requested/);
  for (const network of [undefined, null, {}, { enabled: null }]) {
    const html = render(request({ network, fileSystem: { read: [], write: [], entries: [] } }));
    assert.match(html, /Not reported/);
    assert.doesNotMatch(html, /No permissions|No access|Not requested/);
    assert.match(html, /No additional paths listed/);
  }
});

test('missing, malformed, future and cyclic structures degrade without raw JSON or false assurances', () => {
  const cyclic = {}; cyclic.self = cyclic;
  for (const input of [undefined, null, 'opaque request', [], {}, { permissions: null }, { permissions: cyclic }, request({ network: 'all', fileSystem: [] }), request({ fileSystem: { read: 'all', write: [null, 42, '/valid'], entries: [null, { access: 'future', path: { type: 'path', path: '/visible' } }, { access: 'read', path: { type: 'future', value: cyclic } }], globScanMaxDepth: -1 } }), request({ network: { enabled: true, futureGrant: true } }), request({ futurePermission: 'all' })]) {
    const html = render(input);
    assert.match(html, /unavailable|unrecognized|Not reported/i);
    assert.match(html, /this turn only/);
    assert.doesNotMatch(html, /No permissions|No access|\[object Object\]|opaque request|futureGrant|futurePermission|"permissions"/);
  }
  const mixed = render(request({ fileSystem: { read: [null, '/valid'], entries: [{ access: 'future', path: { type: 'path', path: '/visible' } }] } }));
  assert.match(mixed, /\/valid/);
  assert.match(mixed, /\/visible/);
  assert.match(mixed, /Unknown access/);
  assert.match(mixed, /Some permission details are unavailable or unrecognized/);
});

test('source text is escaped and exact whitespace and wildcard values are retained', () => {
  const html = render({ reason: '<script>alert(1)</script>', cwd: ' /work/../exact ', permissions: { fileSystem: { read: [' <img src=x>/** '], entries: [{ access: 'read', path: { type: 'special', value: { kind: 'project_roots', subpath: ' ../a/** ' } } }] } } });
  assert.match(html, /&lt;script&gt;/);
  assert.match(html, / &lt;img src=x&gt;\/\*\* /);
  assert.ok(html.includes(' /work/../exact '));
  assert.ok(html.includes(' ../a/** '));
  assert.doesNotMatch(html, /<script|<img|<a\b/);
});
