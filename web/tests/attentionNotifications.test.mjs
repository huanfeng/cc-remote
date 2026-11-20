import { test } from 'node:test';
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';

test('notification bridge updates unread attention on viewing without another session poll or popup permission', async () => {
  const app = { sessionsLoaded: false, sessions: [], selection: null };
  let records = {}, latest, cursor = 0;
  const refs = [], effects = [];
  const originalNavigator = Object.getOwnPropertyDescriptor(globalThis, 'navigator');
  Object.defineProperty(globalThis, 'navigator', { configurable: true, value: {} });
  globalThis.__attentionTest = {
    app: selector => selector(app), records: selector => selector({ records }), badge: counts => { latest = counts; },
    ref: initial => { const at = cursor++; return refs[at] ??= { current: initial }; },
    effect: (run, deps) => {
      const at = cursor++, old = effects[at];
      if (old && old.deps.length === deps.length && old.deps.every((v, i) => Object.is(v, deps[i]))) return;
      old?.cleanup?.(); effects[at] = { deps, cleanup: run() };
    },
  };
  const data = text => 'data:text/javascript,' + encodeURIComponent(text);
  const mocks = {
    react: 'export const useRef=v=>globalThis.__attentionTest.ref(v); export const useEffect=(f,d)=>globalThis.__attentionTest.effect(f,d);',
    '@/store/app': 'export const useApp=f=>globalThis.__attentionTest.app(f);useApp.getState=()=>globalThis.__attentionTest.app(x=>x);export const parseHash=()=>null,select=()=>{},selectionHash=()=>"";',
    '@/lib/prefs': 'export const usePrefs={getState:()=>({notify:{on:false}})};',
    '@/domain/notify': 'export const notifySupported=()=>false, sessionChanges=()=>[], showNotification=()=>{};',
    '@/lib/identity': 'export const setTitleAttention=v=>globalThis.__attentionTest.badge(v);',
    '@/store/completionRead': 'export const useCompletionRead=f=>globalThis.__attentionTest.records(f);',
    '@/lib/hosts': 'export const hostKey=id=>"host:"+id;',
  };
  const hook = registerHooks({ resolve(specifier, context, next) {
    if (context.parentURL?.endsWith('/notifications.ts') && mocks[specifier]) return { url: data(mocks[specifier]), shortCircuit: true };
    if (specifier === '@/domain/attention') return { url: new URL('../src/domain/attention.ts', import.meta.url).href, shortCircuit: true };
    return next(specifier, context);
  } });
  try {
    const { useNotifications } = await import('../src/features/dialogs/notifications.ts');
    const render = () => { cursor = 0; useNotifications(); };
    render(); assert.equal(latest.total, 0);
    const at = '2026-11-20T01:00:00Z';
    app.sessionsLoaded = true;
    app.sessions = [{ id: 'wait', status: 'waiting' }, { id: 'done', status: 'completed', completedAt: at }];
    records = { 'host:done': { completedAt: at, viewedAt: at } };
    render(); assert.deepEqual(latest, { waiting: 1, done: 0, total: 1 });
    records = { 'host:done': { completedAt: at, viewedAt: '' } };
    render(); assert.equal(latest.total, 2);
    records = { 'host:done': { completedAt: at, viewedAt: at } };
    render(); assert.deepEqual(latest, { waiting: 1, done: 0, total: 1 });
    app.sessions = []; render(); assert.equal(latest.total, 0);
  } finally {
    effects.forEach(effect => effect?.cleanup?.()); hook.deregister(); delete globalThis.__attentionTest;
    if (originalNavigator) Object.defineProperty(globalThis, 'navigator', originalNavigator); else delete globalThis.navigator;
  }
});
