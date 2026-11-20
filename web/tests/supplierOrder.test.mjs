import { test } from 'node:test';
import assert from 'node:assert/strict';
import { decodeSupplierOrder, mergeSupplierOrder, mergeSupplierProfiles, moveSupplier, orderSuppliers, supplierOrderKey } from '../src/domain/supplierOrder.ts';
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

test('dragging mixes OAuth, local and CC-Switch while preserving other apps and hidden preferences', () => {
  const visible = [{app:'claude',id:'local:1'}, {app:'claude',id:'oauth:1'}, {app:'claude',id:'ccswitch:1'}];
  const old = ['codex:one', 'claude:hidden'];
  const moved = mergeSupplierOrder(['claude:oauth:1', 'claude:ccswitch:1', 'claude:local:1'], old);
  assert.deepEqual(orderSuppliers(visible,moved).map(supplierOrderKey), ['claude:oauth:1','claude:ccswitch:1','claude:local:1']);
  assert.ok(moved.includes('codex:one') && moved.includes('claude:hidden'));
});

test('unified list deduplicates sources and respects CC-Switch visibility on old and new hosts', () => {
  const catalog = [{app:'claude',id:'ccswitch:1',name:'External'}, {app:'claude',id:'oauth:1',name:'Old'}, {app:'codex',id:'local:1',name:'Codex'}];
  const local = [{app:'claude',id:'local:2',name:'Local'}];
  const oauth = [{id:'oauth:1',name:'Updated'}];
  const merged = mergeSupplierProfiles(catalog,local,oauth,false);
  assert.deepEqual(merged.map(p=>p.id), ['ccswitch:1','oauth:1','local:1','local:2']);
  assert.equal(merged[1].name,'Updated');
  assert.equal(merged[1].kind,'oauth');
  assert.deepEqual(mergeSupplierProfiles(catalog,local,oauth,true).map(p=>p.id), ['oauth:1','local:1','local:2']);
  assert.deepEqual(mergeSupplierProfiles(catalog,[],[],false),catalog);
});
