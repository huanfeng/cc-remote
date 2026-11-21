import { test } from 'node:test';
import assert from 'node:assert/strict';
import { currentSupplierValue, supplierOptions, supplierSwitchOptions, defaultSupplierLabel, profilesFor, switchSupplier } from '../src/features/pickers/supplierSwitch.ts';
import { BackendError } from '../src/backend/errors.ts';

const catalog = {
  capabilities: { switchSupplier: true },
  profiles: [{ id: 'c', name: 'Claude work', app: 'claude' }, { id: 'o', name: 'Personal', app: 'claude', kind: 'oauth' }, { id: 'x', name: 'Codex', app: 'codex' }],
  defaultProfiles: { claude: 'c', codex: '@native' },
};

test('supplier choices share defaults/native and remain scoped to the session provider', () => {
  assert.deepEqual(supplierOptions(catalog, 'claude').map(o => o.value), ['', '@native', 'c', 'o']);
  assert.deepEqual(supplierOptions(catalog, 'codex').map(o => o.value), ['', '@native', 'x']);
  assert.equal(supplierOptions(catalog, 'claude')[0].label, 'Default · Claude work');
  assert.equal(supplierOptions(catalog, 'claude')[3].label, 'Personal · OAuth');
  assert.equal(defaultSupplierLabel(catalog, 'codex'), 'Default · native configuration');
  assert.equal(supplierOptions(catalog, 'claude', true)[0].label, 'Keep the session configuration');
});

test('missing defaults/current suppliers remain explicit without leaking cross-provider choices', () => {
  assert.equal(defaultSupplierLabel({ ...catalog, defaultProfiles: { claude: 'missing' } }, 'claude'), 'Default · unavailable');
  assert.equal(defaultSupplierLabel(undefined, 'claude'), 'Default · native configuration');
  assert.deepEqual(profilesFor(undefined, 'claude'), []);
  assert.equal(profilesFor(catalog, 'claude').find(p => p.id === 'x'), undefined);
});

test('switching hides signed-out native Claude and any default that resolves to it', () => {
  const usage = { profiles: { '@native': { reason: 'no-login', available: false, windows: [] } } };
  assert.deepEqual(supplierSwitchOptions(catalog, 'claude', usage).map(o => o.value), ['', 'c', 'o']);
  for (const defaultProfiles of [{}, { claude: '@native' }]) {
    assert.deepEqual(supplierSwitchOptions({ ...catalog, defaultProfiles }, 'claude', usage).map(o => o.value), ['c', 'o']);
  }
  assert.deepEqual(supplierSwitchOptions({ profiles: [], defaultProfiles: {} }, 'claude', usage), []);
  assert.deepEqual(supplierSwitchOptions(catalog, 'codex', usage).map(o => o.value), ['', '@native', 'x']);
});

test('switching keeps native available with API credentials, cached failures or an older host', () => {
  const native = { reason: 'no-login', available: false, windows: [] };
  for (const usage of [null, undefined, { profiles: {} }, ...[
    { ...native, reason: 'api-key' }, { ...native, reason: 'third-party' },
    { ...native, reason: 'pending' }, { ...native, stale: true }, { ...native, error: 'request-failed' },
  ].map(reading => ({ profiles: { '@native': reading } }))]) {
    assert.deepEqual(supplierSwitchOptions(catalog, 'claude', usage).map(o => o.value), ['', '@native', 'c', 'o']);
  }
});

test('busy managed and terminal switches require a separate explicit forced request', async () => {
  for (const mode of ['managed', 'terminal']) {
    const calls = [];
    const session = { id: mode, mode };
    const backend = { switchProfile: async (...args) => {
      calls.push(args);
      if (!args[2]) throw new BackendError('work is running', 409);
      return { ...session, profileId: args[1] };
    } };
    assert.deepEqual(await switchSupplier(backend, session, '@native'), { status: 'confirmation' });
    assert.deepEqual(calls, [[mode, '@native', false]], 'no automatic forced retry');
    const result = await switchSupplier(backend, session, '@native', true);
    assert.equal(result.status, 'switched');
    assert.equal(result.session.profileId, '@native');
    assert.deepEqual(calls[1], [mode, '@native', true]);
  }
});

test('default selection stays empty and failed forced switches/errors are surfaced', async () => {
  const calls = [];
  const backend = { switchProfile: async (...args) => { calls.push(args); return { id: 's', profileId: 'c' }; } };
  assert.equal((await switchSupplier(backend, { id: 's' }, '')).status, 'switched');
  assert.deepEqual(calls, [['s', '', false]]);
  for (const [status, force] of [[400, false], [404, false], [409, true], [500, false]]) {
    const error = new BackendError('failed', status);
    await assert.rejects(switchSupplier({ switchProfile: async () => { throw error; } }, { id: 's' }, 'c', force), e => e === error);
  }
});

test('native session selection differs from requesting the host default supplier', async () => {
  for (const profileId of [undefined, '', '@native']) {
    const session = { id: 'native', profileId };
    const current = currentSupplierValue(session);
    assert.equal(current, '@native');
    const selected = supplierOptions(catalog, 'claude').filter(option => option.value === current);
    assert.deepEqual(selected.map(option => option.value), ['@native']);
    assert.notEqual('', current, 'the Default item must not be skipped as an unchanged native selection');
    const calls = [];
    const result = await switchSupplier({ switchProfile: async (...args) => { calls.push(args); return { id: 'native', profileId: 'c' }; } }, session, '');
    assert.deepEqual(calls, [['native', '', false]]);
    assert.equal(result.session.profileId, 'c', 'the host resolves its configured default');
  }
  assert.equal(currentSupplierValue({ profileId: 'c' }), 'c');
});
