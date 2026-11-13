import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
import { setImmediate } from 'node:timers/promises';
import { transpileModule, ModuleKind, JsxEmit } from 'typescript';
import * as jsx from 'react/jsx-runtime';

const source = readFileSync(new URL('../src/features/standalone/HostForm.tsx', import.meta.url), 'utf8');
const { outputText } = transpileModule(source, { compilerOptions: { module: ModuleKind.CommonJS, jsx: JsxEmit.ReactJSX } });
const existing = { id: 'saved-host', url: 'https://old.example.com', token: 'saved-test-token', name: 'My host' };

// Exercise the real form and its event handlers with isolated hook state and host I/O.
function formHarness({ editing = existing, ack = false, reachable = true } = {}) {
  const states = [], calls = [];
  let cursor = 0;
  const modules = {
    react: { useState(initial) {
      const index = cursor++;
      if (!(index in states)) states[index] = typeof initial === 'function' ? initial() : initial;
      return [states[index], value => { states[index] = value; }];
    } },
    'react/jsx-runtime': jsx,
    '@/components/ui/button': { Button: 'button' },
    '@/components/ui/input': { Input: 'input' },
    '@/components/ui/spinner': { Spinner: 'spinner' },
    '@/components/lib/utils': { cn: () => '' },
    '@/lib/hosts': {
      PALETTE: ['#123456'], blockedHttp: () => false,
      normalizeHostUrl: value => { try { return new URL(value).origin; } catch { return ''; } },
      checkHost: async (url, token) => { calls.push(['check', url, token]); return { ok: reachable, status: reachable ? 200 : 0, name: 'My host' }; },
      connectError: () => 'Host is unreachable', findHost: () => undefined,
      loadHosts: () => ({ hosts: [existing] }), randomColor: () => '#123456',
      rememberHost: record => { calls.push(['remember', structuredClone(record)]); return { ...record, id: 'new-host' }; },
      reopen: id => calls.push(['reopen', id]), resolveEntry: async () => { throw new Error('Unexpected entry lookup'); },
      shownColor: () => '#123456', shownName: host => host.name,
      updateHost: (id, change) => calls.push(['update', id, structuredClone(change)]),
    },
    '@/lib/i18n': { t: en => en },
    '@/features/dialogs/SurfaceShell': { SurfaceShell: 'dialog', TOUCH: '' },
    '@/features/dialogs/common': { ErrorText: 'error-text' },
    '@/features/auth/SafetyNotice': { SafetyNotice: 'safety-notice', useSafetyAck: () => ({ ack, show: !ack, setAck() {} }) },
    './parts': { AllowHint: 'allow-hint', ColorChoices: 'colors', IconChoices: 'icons' },
    '@/platform/desktop': { DESKTOP: false },
  };
  const exports = {};
  runInNewContext(outputText, { exports, require: name => {
    assert.ok(name in modules, `Unexpected form dependency: ${name}`);
    return modules[name];
  }, location: new URL('https://frontend.example.com/') });
  const dialog = exports.HostFormDialog({ open: true, editing, mobile: false, onOpenChange: () => calls.push(['done']) });
  const component = dialog.props.children;
  const render = () => { cursor = 0; return component.type(component.props); };
  function find(node, predicate) {
    if (!node || typeof node !== 'object') return undefined;
    if (predicate(node)) return node;
    for (const child of [node.props?.children].flat(Infinity)) {
      const match = find(child, predicate);
      if (match) return match;
    }
  }
  return {
    calls, render,
    save: () => find(render(), node => node.type === 'button' && node.props.type === 'submit'),
    notice: () => find(render(), node => node.type === 'safety-notice'),
    error: () => find(render(), node => node.type === 'error-text')?.props.children,
    input(placeholder, value) {
      const input = find(render(), node => node.type === 'input' && node.props.placeholder === placeholder);
      assert.ok(input, `Missing input: ${placeholder}`);
      input.props.onChange({ target: { value } });
    },
    async submit() {
      render().props.onSubmit({ preventDefault() {} });
      await setImmediate();
    },
  };
}

test('editing an existing host enables Save even when the first-use acknowledgment is absent', () => {
  const form = formHarness();
  assert.equal(form.notice(), undefined);
  assert.equal(form.save().props.disabled, false);
});

test('changing an unreachable host address checks the new address and preserves its ID and token', async () => {
  const form = formHarness();
  form.input('https://ccr.example.com', 'https://new.example.com');
  await form.submit();
  assert.deepEqual(form.calls[0], ['check', 'https://new.example.com', existing.token]);
  const update = form.calls.find(call => call[0] === 'update');
  assert.equal(update?.[1], existing.id);
  assert.equal(update?.[2].url, 'https://new.example.com');
  assert.equal(update?.[2].token, existing.token);
  assert.ok(form.calls.some(call => call[0] === 'done'));
});

test('an unsuccessful new connection shows its error and leaves the saved host unchanged', async () => {
  const form = formHarness({ reachable: false });
  form.input('https://ccr.example.com', 'https://new.example.com');
  await form.submit();
  assert.deepEqual(form.calls, [['check', 'https://new.example.com', existing.token]]);
  assert.equal(form.error(), 'Host is unreachable');
  assert.equal(form.save().props.disabled, false);
});

test('editing a local display name still saves while the host is offline', async () => {
  const form = formHarness({ reachable: false });
  form.input(existing.name, 'New display name');
  await form.submit();
  assert.equal(form.calls[0]?.[0], 'update');
  assert.equal(form.calls[0]?.[2].alias, 'New display name');
  assert.ok(!form.calls.some(call => call[0] === 'check'));
});

test('adding a host still requires the visible first-use acknowledgment', async () => {
  const form = formHarness({ editing: null });
  form.input('https://ccr.example.com', 'https://new.example.com');
  form.input('The access token of that host', 'new-test-token');
  assert.ok(form.notice());
  assert.equal(form.save().props.disabled, true);
  await form.submit();
  assert.deepEqual(form.calls, []);
});

test('adding a host after acknowledging still checks and opens it', async () => {
  const form = formHarness({ editing: null, ack: true });
  form.input('https://ccr.example.com', 'https://new.example.com');
  form.input('The access token of that host', 'new-test-token');
  assert.equal(form.save().props.disabled, false);
  await form.submit();
  assert.deepEqual(form.calls[0], ['check', 'https://new.example.com', 'new-test-token']);
  assert.ok(form.calls.some(call => call[0] === 'remember'));
  assert.ok(form.calls.some(call => call[0] === 'reopen'));
});
