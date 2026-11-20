import { test } from 'node:test';
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';

test('supplier quota reads only while visible, ignores late closed results, and never reuses another host reading', async () => {
  let states = [], effects = [], stateIndex = 0, effectIndex = 0;
  globalThis.window = { setInterval: () => 1, clearInterval: () => {} };
  globalThis.__supplierUsageHooks = {
    useState: initial => { const index = stateIndex++; if (!(index in states)) states[index] = initial; return [states[index], next => { states[index] = typeof next === 'function' ? next(states[index]) : next; }]; },
    useEffect: (callback, nextDeps) => {
      const index = effectIndex++, old = effects[index];
      if (!old || nextDeps.some((value, i) => value !== old.deps[i])) {
        old?.cleanup?.(); effects[index] = { deps: nextDeps, callback };
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
      stateIndex = 0; effectIndex = 0;
      const result = useSupplierUsage(provider, enabled);
      for (const effect of effects) if (effect.callback) { const run = effect.callback; effect.callback = undefined; effect.cleanup = run(); }
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
    effects.forEach(effect => effect.cleanup?.()); hook.deregister(); delete globalThis.window; delete globalThis.__supplierUsageHooks; delete globalThis.__supplierUsageBackend;
  }
});
