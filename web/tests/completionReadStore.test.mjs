import { test } from 'node:test';
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';
import { isCompletionPending } from '../src/domain/completionRead.ts';

test('read tracking persists per host, follows other views and tolerates unavailable storage', async () => {
  const values = new Map(), listeners = [];
  let deny = false;
  globalThis.__completionHost = 'one';
  globalThis.localStorage = {
    getItem: key => { if (deny) throw new Error('denied'); return values.get(key) ?? null; },
    setItem: (key, value) => { if (deny) throw new Error('denied'); values.set(key, value); },
  };
  globalThis.window = { addEventListener: (type, callback) => { if (type === 'storage') listeners.push(callback); } };
  const hook = registerHooks({ resolve(specifier, context, next) {
    if (specifier === '@/lib/hosts') return {
      url: 'data:text/javascript,' + encodeURIComponent('export const hostKey = key => `${key}@${globalThis.__completionHost}`;'),
      shortCircuit: true,
    };
    return next(specifier, context);
  } });
  try {
    const { useCompletionRead: store } = await import('../src/store/completionRead.ts');
    const running = { id: 'same-id', status: 'running' };
    const done = { ...running, status: 'completed', completedAt: 'turn-1', completionEventSeq: 2 };
    store.getState().observe([running]);
    store.getState().observe([done]);
    assert.equal(isCompletionPending(done, store.getState().records['same-id@one']), true);

    const { useCompletionRead: reloaded } = await import('../src/store/completionRead.ts?reload');
    assert.equal(isCompletionPending(done, reloaded.getState().records['same-id@one']), true);
    globalThis.__completionHost = 'two';
    reloaded.getState().observe([done]);
    assert.equal(isCompletionPending(done, reloaded.getState().records['same-id@two']), false);
    assert.equal(reloaded.getState().records['same-id@two'].confirmedAt, undefined, 'the initial local baseline is not a confirmed view');
    assert.equal(isCompletionPending(done, reloaded.getState().records['same-id@one']), true);

    globalThis.__completionHost = 'one';
    reloaded.getState().markViewed(done);
    for (const callback of listeners) callback({ key: 'ccr-completion-read-v1' });
    assert.equal(isCompletionPending(done, store.getState().records['same-id@one']), false);
    assert.equal(store.getState().records['same-id@one'].confirmedAt, done.completedAt);

    const staleDisk = JSON.parse(values.get('ccr-completion-read-v1'));
    staleDisk.records['same-id@one'].viewedAt = '';
    values.set('ccr-completion-read-v1', JSON.stringify(staleDisk));
    for (const callback of listeners) callback({ key: 'ccr-completion-read-v1' });
    assert.equal(JSON.parse(values.get('ccr-completion-read-v1')).records['same-id@one'].viewedAt, 'turn-1', 'stale concurrent disk writes are repaired for future reloads');

    deny = true;
    const second = { ...done, completedAt: 'turn-2' };
    store.getState().observe([second]);
    assert.equal(isCompletionPending(second, store.getState().records['same-id@one']), true);
    store.getState().markViewed(done);
    assert.equal(isCompletionPending(second, store.getState().records['same-id@one']), true);
    store.getState().markViewed(second);
    assert.equal(isCompletionPending(second, store.getState().records['same-id@one']), false);

    const external = { ...done, id: 'externally-created' };
    store.getState().observe([external]);
    assert.equal(isCompletionPending(external, store.getState().records['externally-created@one']), true, 'new external tasks after initialization must not be silently read');
  } finally {
    hook.deregister();
    delete globalThis.__completionHost;
    delete globalThis.localStorage;
    delete globalThis.window;
  }
});
