import { test } from 'node:test';
import assert from 'node:assert/strict';
import { decodeSupplierOrder, moveSupplier, orderSuppliers, supplierOrderKey } from '../src/domain/supplierOrder.ts';
import { supplierOptions } from '../src/features/pickers/supplierSwitch.ts';

const entries = [
  { app: 'claude', id: 'api', name: 'API' },
  { app: 'claude', id: 'oauth', name: 'OAuth', kind: 'oauth' },
  { app: 'codex', id: 'api', name: 'Codex' },
];
test('OAuth/API mixed ordering reaches legacy catalog session choices without changing defaults', () => {
  const order = moveSupplier(entries, [], 'claude:oauth', -1);
  const catalog = { profiles: orderSuppliers(entries, order), defaultProfiles: { claude: 'api' } };
  assert.deepEqual(supplierOptions(catalog, 'claude').map(p => p.value), ['', '@native', 'oauth', 'api']);
  assert.equal(supplierOptions(catalog, 'claude')[0].label, 'Default · API');
  assert.deepEqual(supplierOptions(catalog, 'codex').map(p => p.value), ['', '@native', 'api']);
  assert.deepEqual(entries.map(supplierOrderKey), ['claude:api', 'claude:oauth', 'codex:api']);
  const legacy = { profiles: [entries[0]], defaultProfiles: {} };
  assert.deepEqual(supplierOptions({ ...legacy, profiles: orderSuppliers(legacy.profiles, order) }, 'claude').map(p => p.value), ['', '@native', 'api']);
});
test('new, deleted and temporarily missing suppliers do not break ordering', () => {
  const order = ['claude:gone', 'claude:oauth'];
  assert.deepEqual(orderSuppliers(entries, order).map(supplierOrderKey), ['claude:oauth', 'claude:api', 'codex:api']);
  assert.deepEqual(moveSupplier(entries, order, 'claude:oauth', 1), ['claude:api', 'claude:oauth', 'codex:api', 'claude:gone']);
  assert.equal(moveSupplier(entries, order, 'missing', -1), order);
  assert.equal(moveSupplier(entries, order, 'claude:oauth', -1), order);
  assert.deepEqual(orderSuppliers([], order), []);
});
test('corrupt preferences recover to server order; duplicate and non-string values are ignored', () => {
  for (const raw of [null, 'bad', '{}', 'null']) assert.deepEqual(decodeSupplierOrder(raw), []);
  assert.deepEqual(decodeSupplierOrder('["a",2,"a","b",null]'), ['a', 'b']);
});
