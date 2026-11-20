import { test } from 'node:test';
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';

test('supplier order isolates hosts, persists reloads, follows storage events and tolerates denied storage', async () => {
  let host = 'first';
  const values = new Map();
  const events = new Map();
  globalThis.__supplierHostKey = key => `${key}@${host}`;
  globalThis.window = { addEventListener: (name, callback) => events.set(name, callback) };
  globalThis.localStorage = { getItem: key => values.get(key) ?? null, setItem: (key, value) => values.set(key, value) };
  const data = source => 'data:text/javascript,' + encodeURIComponent(source);
  const hook = registerHooks({
    resolve(specifier, context, next) {
      if (context.parentURL?.endsWith('/lib/supplierOrder.ts')) {
        if (specifier === 'react') return { url: data('export const useSyncExternalStore=(_subscribe,get)=>get();'), shortCircuit: true };
        if (specifier === './hosts') return { url: data('export const hostKey=key=>globalThis.__supplierHostKey(key);'), shortCircuit: true };
        if (specifier === '../domain/supplierOrder') return { url: new URL('../src/domain/supplierOrder.ts', import.meta.url).href, shortCircuit: true };
      }
      return next(specifier, context);
    },
  });
  try {
    const { useSupplierOrder } = await import('../src/lib/supplierOrder.ts');
    useSupplierOrder().setOrder(['claude:oauth', 'claude:api']);
    assert.deepEqual(JSON.parse(values.get('ccn-supplier-order@first')), ['claude:oauth', 'claude:api']);
    host = 'second';
    assert.deepEqual(useSupplierOrder().order, []);
    useSupplierOrder().setOrder(['codex:api']);
    host = 'first';
    assert.deepEqual(useSupplierOrder().order, ['claude:oauth', 'claude:api']);
    values.set('ccn-supplier-order@first', '["claude:api"]');
    events.get('storage')({ key: 'ccn-supplier-order@first' });
    assert.deepEqual(useSupplierOrder().order, ['claude:api']);
    events.get('storage')({ key: null });
    assert.deepEqual(useSupplierOrder().order, ['claude:api']);
    host = 'denied';
    globalThis.localStorage = { getItem() { throw Error('denied'); }, setItem() { throw Error('denied'); } };
    assert.deepEqual(useSupplierOrder().order, []);
    useSupplierOrder().setOrder(['claude:oauth']);
    assert.deepEqual(useSupplierOrder().order, ['claude:oauth']);
  } finally {
    hook.deregister();
    delete globalThis.__supplierHostKey;
    delete globalThis.localStorage;
    delete globalThis.window;
  }
});
