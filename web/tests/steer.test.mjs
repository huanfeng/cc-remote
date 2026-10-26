import { test, before } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

let S;
before(async () => {
  const { build } = await import('esbuild');
  const web = join(dirname(fileURLToPath(import.meta.url)), '..');
  const outdir = join(web, 'node_modules', '.cache', 'steer-test');
  mkdirSync(outdir, { recursive: true });
  const outfile = join(outdir, 'steer.mjs');
  await build({ entryPoints: [join(web, 'src/SteerButton.tsx')], outfile, bundle: true, format: 'esm', platform: 'node', packages: 'external', jsx: 'automatic', define: { 'import.meta.env': '{}' }, logLevel: 'silent' });
  S = await import(pathToFileURL(outfile).href);
});

test('steering is opt-in and absent on old hosts, Claude, history and queued work', () => {
  const info = { features: ['codexSteer'] };
  const active = { provider: 'codex', mode: 'managed', status: 'running' };
  assert.equal(S.canSteer(undefined, active), false);
  assert.equal(S.canSteer({}, active), false);
  assert.equal(S.canSteer({ features: ['codex', 'sendId'] }, active), false);
  assert.equal(S.canSteer(info, active), true);
  assert.equal(S.canSteer(info, active, true), false);
  for (const override of [{ provider: 'claude' }, { mode: 'terminal' }, { status: 'waiting' }, { status: 'completed' }, { queued: [{}] }]) {
    assert.equal(S.canSteer(info, { ...active, ...override }), false);
  }
  assert.equal(renderToStaticMarkup(createElement(S.SteerButton, { supported: false, disabled: false, send() {} })), '');
  const html = renderToStaticMarkup(createElement(S.SteerButton, { supported: true, disabled: true, send() {} }));
  assert.match(html, /type="button"/);
  assert.match(html, /disabled/);
});

test('an unconfirmed submission stays a confirmation operation after completion', () => {
  const held = { content: 'focus on tests', id: 'original-send-id' };
  assert.equal(S.canSteer({ features: ['codexSteer'] }, { provider: 'codex', status: 'completed' }), false);
  assert.equal(S.needsSteerConfirmation(held, ' focus on tests '), true);
  assert.equal(S.needsSteerConfirmation(held, 'different instruction'), false);
  assert.equal(S.needsSteerConfirmation(undefined, 'focus on tests'), false);
  const html = renderToStaticMarkup(createElement(S.SteerButton, { supported: true, confirming: true, disabled: false, send() {} }));
  assert.match(html, /Check additional instruction/);
  assert.match(html, /original submission/);
});
