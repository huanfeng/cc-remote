import { test } from 'node:test';
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';

test('supplier quota reads only while visible, ignores late closed results, and never reuses another host reading', async () => {
  let state, deps, cleanup, effect;
  globalThis.__supplierUsageHooks = {
    useState: () => [state, next => { state = next; }],
    useEffect: (callback, nextDeps) => {
      if (!deps || nextDeps.some((value, i) => value !== deps[i])) {
        cleanup?.(); deps = nextDeps; effect = callback;
      }
    },
  };
  const calls = [];
  const backend = { profileUsage: () => new Promise(resolve => calls.push(resolve)) };
  globalThis.__supplierUsageBackend = backend;
  const data = source => 'data:text/javascript,' + encodeURIComponent(source);
  const hook = registerHooks({ resolve(specifier, context, next) {
    if (context.parentURL?.endsWith('/useSupplierUsage.ts') && specifier === 'react') return { url: data('export const useState=(...v)=>globalThis.__supplierUsageHooks.useState(...v);export const useEffect=(...v)=>globalThis.__supplierUsageHooks.useEffect(...v);'), shortCircuit: true };
    if (specifier === '@/backend/backend') return { url: data('export const useBackend=()=>globalThis.__supplierUsageBackend;'), shortCircuit: true };
    return next(specifier, context);
  } });
  try {
    const { useSupplierUsage } = await import('../src/features/pickers/useSupplierUsage.ts');
    const render = (provider = 'claude', enabled = true) => {
      const result = useSupplierUsage(provider, enabled);
      if (effect) { const run = effect; effect = undefined; cleanup = run(); }
      return result;
    };
    render('claude', false); assert.equal(calls.length, 0);
    render(); render(); assert.equal(calls.length, 1);
    const reading = { profiles: { 'oauth:one': { available: true, windows: [] } } };
    calls[0](reading); await Promise.resolve();
    assert.equal(render(), reading);
    render('claude', false); render(); assert.equal(calls.length, 2);
    render('claude', false);
    calls[1]({ profiles: { old: {} } }); await Promise.resolve();
    assert.notDeepEqual(render('claude', false), { profiles: { old: {} } });
    globalThis.__supplierUsageBackend = { profileUsage: async () => ({ profiles: { other: {} } }) };
    assert.equal(render(), undefined, 'new host never displays the previous host data');
    await Promise.resolve();
    assert.deepEqual(render(), { profiles: { other: {} } });
    assert.equal(render('codex'), null);
    globalThis.__supplierUsageBackend = {};
    assert.equal(render(), null, 'backends without the optional API keep basic supplier choices');
  } finally {
    cleanup?.(); hook.deregister(); delete globalThis.__supplierUsageHooks; delete globalThis.__supplierUsageBackend;
  }
});
