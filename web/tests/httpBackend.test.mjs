import { test } from 'node:test';
import assert from 'node:assert/strict';
import { HttpBackend } from '../src/backend/http/httpBackend.ts';
import { HttpClient } from '../src/backend/http/client.ts';
import { BackendError } from '../src/backend/errors.ts';

const wait = ms => new Promise(r => setTimeout(r, ms));
async function until(check, ms = 1000) {
  const end = Date.now() + ms;
  while (Date.now() < end) { if (check()) return; await wait(5); }
  assert.fail('condition not reached in time');
}

/**
 * A fake host: routes(method, path, body, headers) returns { status, body, etag } or a plain
 * value (200 JSON). Every request is logged.
 */
function fakeHost(routes) {
  const log = [];
  const fetch = async (url, init = {}) => {
    const u = new URL(url, 'http://host.test');
    const path = u.pathname + u.search;
    const method = (init.method || 'GET').toUpperCase();
    const headers = init.headers || {};
    const body = typeof init.body === 'string' ? JSON.parse(init.body) : init.body;
    log.push({ method, path, body, headers, url });
    let r = await routes(method, path, body, headers);
    if (r === undefined) r = { status: 404, body: { error: `no route ${method} ${path}` } };
    if (!r || typeof r !== 'object' || typeof r.status !== 'number') r = { status: 200, body: r };
    if (r.status === 304) return new Response(null, { status: 304 });
    const h = { 'Content-Type': 'application/json' };
    if (r.etag) h.ETag = r.etag;
    return new Response(r.body === undefined ? null : JSON.stringify(r.body), { status: r.status, headers: h });
  };
  return { fetch, log };
}

const FEATURES = ['sendId', 'mentions', 'codex', 'codexSteer', 'queueControls'];
const info = (features = FEATURES) => ({ version: '1', cwd: '/', claudeAvailable: true, features });

test('OAuth editing keeps the original id and omits an unchanged token on existing hosts', async () => {
  const host = fakeHost((method, path) => {
    if (method === 'PATCH' && path === '/api/profiles/oauth/stable-id') return { id: 'oauth:stable-id', name: 'Renamed' };
  });
  const backend = make(host);
  await backend.editOAuthProfile('oauth:stable-id', { name: 'Renamed' });
  assert.deepEqual(host.log.map(({ method, path, body }) => ({ method, path, body })), [
    { method: 'PATCH', path: '/api/profiles/oauth/stable-id', body: { name: 'Renamed' } },
  ]);
  backend.dispose();
});

test('OAuth saves preserve detailed validation messages from the host', async () => {
  const detail = 'OAuth token must not contain spaces or control characters.';
  const host = fakeHost(() => ({ status: 400, body: { error: detail } }));
  const backend = make(host);
  await assert.rejects(backend.addOAuthProfile('Work', 'sk-ant-oat invalid'), error => error instanceof BackendError && error.status === 400 && error.message === detail);
  await assert.rejects(backend.editOAuthProfile('oauth:stable-id', { name: 'Work', token: 'sk-ant-oat invalid' }), error => error instanceof BackendError && error.message === detail);
  backend.dispose();
});
const session = (id, extra = {}) => ({ id, title: id, cwd: '/w', updatedAt: '2026-11-07T00:00:00Z', status: 'idle', ...extra });
const ev = (seq, kind = 'assistant') => ({ seq, kind, text: `e${seq}`, createdAt: '2026-11-07T00:00:00Z' });
const fast = { sessions: 15, transcript: 15, usage: 15 };
const make = (host, extra = {}) => new HttpBackend({ baseUrl: '', token: 'tok', lang: 'en', fetch: host.fetch, intervals: fast, ...extra });

test('statusline: older hosts return unsupported without blocking settings or sessions', async () => {
  for (const reply of [
    { status: 404, body: { error: 'not found' } },
    { status: 405, body: { error: 'method not allowed' } },
    { status: 200, body: { legacy: true } },
  ]) {
    const host = fakeHost((m, p) => p === '/api/statusline' ? reply : p === '/api/claude/settings' ? { model: 'opus', revision: 'old' } : []);
    const b = make(host);
    assert.equal(await b.statusline(), null);
    assert.equal((await b.claudeSettings()).model, 'opus');
    assert.deepEqual(await b.preferences('claude'), []);
    b.dispose();
  }
  const b = new HttpBackend({ baseUrl: '', token: 'tok', lang: 'en', fetch: async () => new Response('<html>old host</html>') });
  assert.equal(await b.statusline(), null);
  b.dispose();
});

test('statusline: saves style and uninstalls using the host revision', async () => {
  const state = { installed: true, style: 'color', revision: 'r2' };
  const host = fakeHost(() => state);
  const b = make(host);
  assert.deepEqual(await b.statusline(), state);
  assert.deepEqual(await b.saveStatusline({ style: 'color', revision: 'r1', extra: 'omit' }), state);
  await b.removeStatusline({ revision: 'r2' });
  assert.deepEqual(host.log.map(x => [x.method, x.path, x.body]), [
    ['GET', '/api/statusline', undefined],
    ['PUT', '/api/statusline', { style: 'color', revision: 'r1' }],
    ['DELETE', '/api/statusline', { revision: 'r2' }],
  ]);
  b.dispose();
});

test('client: headers, and a cursorless GET answered 304 returns the cached object itself', async () => {
  const data = { a: [1, 2] };
  const host = fakeHost((m, p, b, h) => (h['If-None-Match'] === '"v1"' ? { status: 304 } : { status: 200, body: data, etag: '"v1"' }));
  const c = new HttpClient({ baseUrl: 'https://h.example:8787/', token: 'tok', lang: 'zh', fetch: host.fetch });
  const first = await c.json('/api/sessions');
  const second = await c.json('/api/sessions');
  assert.equal(second, first);
  assert.equal(host.log[0].url, 'https://h.example:8787/api/sessions');
  assert.equal(host.log[0].headers.Authorization, 'Bearer tok');
  assert.equal(host.log[0].headers['X-CC-Lang'], 'zh');
  assert.equal(host.log[0].headers['If-None-Match'], undefined);
  assert.equal(host.log[1].headers['If-None-Match'], '"v1"');
  // Cursor queries are never revalidated.
  await c.json('/api/sessions/s1/events?after=3');
  await c.json('/api/sessions/s1/events?after=3');
  assert.equal(host.log[3].headers['If-None-Match'], undefined);
});

test('client: errors carry the host message, status, code and delivery', async () => {
  const host = fakeHost((m, p) => {
    if (p === '/a') return { status: 409, body: { error: 'still delivering', delivery: 'pending' } };
    if (p === '/b') return { status: 428, body: { error: 'trust it', code: 'trust-required' } };
    return { status: 500 };
  });
  const c = new HttpClient({ baseUrl: '', token: 't', lang: 'en', fetch: host.fetch });
  await assert.rejects(c.json('/a'), e => e instanceof BackendError && e.status === 409 && e.message === 'still delivering' && e.delivery === 'pending');
  await assert.rejects(c.json('/b'), e => e instanceof BackendError && e.status === 428 && e.code === 'trust-required');
  await assert.rejects(c.json('/c'), e => e instanceof BackendError && e.status === 500 && e.message === 'Request failed (500)');
  const down = new HttpClient({ baseUrl: '', token: 't', lang: 'en', fetch: async () => { throw new TypeError('fetch failed'); } });
  await assert.rejects(down.json('/x'), e => e instanceof BackendError && e.status === 0 && /Could not reach the host/.test(e.message));
});

test('client: 401 calls onUnauthorized once and rejects every time', async () => {
  let calls = 0;
  const host = fakeHost(() => ({ status: 401, body: { error: 'Invalid access token' } }));
  const c = new HttpClient({ baseUrl: '', token: 't', lang: 'en', fetch: host.fetch, onUnauthorized: () => calls++ });
  await assert.rejects(c.json('/api/info'), e => e.status === 401);
  await assert.rejects(c.json('/api/sessions'), e => e.status === 401);
  assert.equal(calls, 1);
});

test('watchTranscript: tail with reset, after-cursor advances, requests filtered, 409 reloads with reset', async () => {
  let events = [ev(1, 'user'), ev(2)];
  let renumbered = false;
  const requests = [{ id: 'r1', kind: 'approval', toolName: 'Bash', input: {}, status: 'pending' }, { id: 'r0', kind: 'approval', toolName: 'Bash', input: {}, status: 'resolved' }];
  const host = fakeHost((m, p) => {
    if (p === '/api/sessions') return { status: 200, body: [session('s1')], etag: '"l"' };
    if (p.startsWith('/api/requests?sessionId=s1')) return { status: 200, body: requests };
    if (p.startsWith('/api/sessions/s1?')) return { status: 200, body: { session: session('s1'), events, hasMore: true } };
    if (p.startsWith('/api/sessions/s1/events?after=')) {
      const after = Number(new URLSearchParams(p.split('?')[1]).get('after'));
      if (renumbered && after > events.length) return { status: 409, body: { error: 'history changed' } };
      return events.filter(e => e.seq > after);
    }
  });
  const b = make(host);
  const updates = [];
  const stop = b.watchTranscript('s1', u => updates.push(u));
  await until(() => updates.length >= 1);
  assert.equal(updates[0].reset, true);
  assert.equal(updates[0].hasMore, true);
  assert.deepEqual(updates[0].events.map(e => e.seq), [1, 2]);
  assert.deepEqual(updates[0].requests.map(r => r.id), ['r1']);
  const tail = host.log.find(l => l.path.startsWith('/api/sessions/s1?'));
  assert.match(tail.path, /tail=200&compact=1&viewing=1/);

  events = [...events, ev(3), ev(4)];
  await until(() => updates.some(u => u.events.some(e => e.seq === 4)));
  const grown = updates.find(u => u.events.some(e => e.seq === 4));
  assert.ok(!grown.reset);
  assert.deepEqual(grown.events.map(e => e.seq), [3, 4]);
  await until(() => host.log.some(l => l.path.startsWith('/api/sessions/s1/events?after=4&compact=1')));

  // The transcript is rewritten shorter: the host answers 409 and the watch starts over.
  events = [ev(1, 'user')];
  renumbered = true;
  const before = updates.length;
  await until(() => updates.slice(before).some(u => u.reset));
  const reset = updates.slice(before).find(u => u.reset);
  assert.deepEqual(reset.events.map(e => e.seq), [1]);
  stop();
  b.dispose();
});

test('watchTranscript: native history uses /api/native/sessions with provider=codex and no requests', async () => {
  const host = fakeHost((m, p) => {
    if (p.startsWith('/api/native/sessions/n1?')) return { session: { id: 'n1', title: 'n', cwd: '/', updatedAt: 'x' }, events: [ev(1)], hasMore: false };
    if (p.startsWith('/api/native/sessions/n1/events?')) return [];
  });
  const b = make(host);
  const updates = [];
  const stop = b.watchTranscript('n1', u => updates.push(u), { native: 'codex' });
  await until(() => host.log.some(l => l.path.includes('/events?after=1')));
  stop();
  assert.equal(updates[0].session.provider, 'codex');
  assert.ok(host.log.every(l => !l.path.startsWith('/api/requests')));
  assert.ok(host.log.every(l => l.path.includes('provider=codex')));
  await b.loadOlder('n1', 5, { native: 'codex' }).catch(() => {});
  assert.match(host.log.at(-1).path, /^\/api\/native\/sessions\/n1\/events\?before=5&limit=200&compact=1&provider=codex$/);
});

test('watchTranscript refreshes fork anchors for messages first received during a turn', async () => {
  const original = ev(1, 'user');
  let current = { ...session('s1'), status: 'running', canFork: true };
  const host = fakeHost((method, path) => {
    if (path === '/api/sessions') return [current];
    if (path.startsWith('/api/requests?')) return [];
    if (path.startsWith('/api/sessions/s1?')) {
      const events = current.status === 'running' ? [original] : [{ ...original, branchable: true, branchAnchor: 'anchor' }];
      return { session: current, events, hasMore: true };
    }
    if (path.startsWith('/api/sessions/s1/events?')) return [];
  });
  const backend = make(host);
  const updates = [];
  const stop = backend.watchTranscript('s1', update => updates.push(update));
  try {
    await until(() => updates.some(u => u.reset && !u.events[0]?.branchable));
    current = { ...current, status: 'completed' };
    await until(() => updates.some(u => u.events[0]?.branchAnchor === 'anchor'));
    assert.equal(updates.filter(u => u.reset).length, 1);
    assert.equal(updates.find(u => u.events[0]?.branchAnchor === 'anchor').hasMore, true);
  } finally { stop(); backend.dispose(); }
});

test('watchTranscript refreshes the prompt even when a long turn fills the tail', async () => {
  const prompt = ev(1, 'user');
  const annotated = { ...prompt, branchable: true, editableInBranch: true, branchAnchor: 'older-anchor' };
  let current = { ...session('s1'), status: 'running', canFork: true };
  const host = fakeHost((method, path) => {
    if (path === '/api/sessions') return [current];
    if (path.startsWith('/api/requests?')) return [];
    if (path.startsWith('/api/sessions/s1?')) return {
      session: current, events: current.status === 'running' ? [prompt] : [ev(202)], hasMore: false,
    };
    if (path.startsWith('/api/sessions/s1/events?')) return [];
    if (path === '/api/sessions/s1/events/1') return annotated;
  });
  const backend = make(host);
  const updates = [];
  const stop = backend.watchTranscript('s1', update => updates.push(update));
  try {
    await until(() => updates.some(u => u.reset && u.events[0]?.seq === 1));
    current = { ...current, status: 'completed' };
    await until(() => updates.some(u => u.events[0]?.branchAnchor === 'older-anchor'));
    assert.equal(updates.filter(u => u.reset).length, 1);
  } finally { stop(); backend.dispose(); }
});

test('forkSession sends the verified boundary and attachments to the appropriate source route', async () => {
  const reply = { session: session('child'), attachmentIds: ['upload'] };
  const host = fakeHost(() => reply);
  const backend = make(host);
  const body = { requestId: 'request-1', anchorSeq: 2, anchorToken: 'anchor', before: true, attachmentIds: ['upload'] };
  assert.deepEqual(await backend.forkSession('thread-1', body, 'codex'), reply);
  assert.deepEqual(await backend.forkSession('source-1', { requestId: 'request-2' }), reply);
  assert.deepEqual(host.log.map(({ method, path, body: payload }) => ({ method, path, payload })), [
    { method: 'POST', path: '/api/native/sessions/thread-1/fork?provider=codex', payload: body },
    { method: 'POST', path: '/api/sessions/source-1/fork', payload: { requestId: 'request-2' } },
  ]);
  backend.dispose();
});

test('unsubscribe stops polling and ignores replies still in flight', async () => {
  let release;
  const host = fakeHost(async (m, p) => {
    if (p === '/api/sessions') return [session('s1')];
    if (p.startsWith('/api/requests')) return [];
    if (p.startsWith('/api/sessions/s1?')) { await new Promise(r => { release = r; }); return { session: session('s1'), events: [ev(1)] }; }
  });
  const b = make(host);
  const updates = [];
  const stop = b.watchTranscript('s1', u => updates.push(u));
  await until(() => !!release);
  stop();
  release();
  await wait(60);
  assert.equal(updates.length, 0);
  const count = host.log.length;
  await wait(80);
  assert.equal(host.log.length, count, 'no request after unsubscribe');
});

test('watchSessions: delivers changes only; one shared poll; stops with the last watcher', async () => {
  let version = 1;
  const host = fakeHost((m, p, b, h) => {
    if (p !== '/api/sessions') return;
    if (h['If-None-Match'] === `"v${version}"`) return { status: 304 };
    return { status: 200, body: [session(`s${version}`)], etag: `"v${version}"` };
  });
  const b = make(host);
  const a = [], c = [];
  const stopA = b.watchSessions(s => a.push(s));
  const stopC = b.watchSessions(s => c.push(s));
  await until(() => host.log.length >= 4);
  assert.equal(a.length, 1);
  assert.equal(c.length, 1);
  version = 2;
  await until(() => a.length === 2);
  assert.equal(a[1][0].id, 's2');
  stopA(); stopC();
  const count = host.log.length;
  await wait(60);
  assert.ok(host.log.length <= count + 1);
});

test('send gates sendId and defer on host features; steerStatus turns 409 into a delivery', async () => {
  for (const [features, expected] of [
    [FEATURES, { text: 'hi', attachmentIds: ['a1'], defer: true, sendId: 'abcdefgh12' }],
    [['mentions'], { text: 'hi', attachmentIds: ['a1'] }],
  ]) {
    const host = fakeHost((m, p) => {
      if (p === '/api/info') return info(features);
      if (p === '/api/sessions/s1/messages') return { status: 202, body: { status: 'queued' } };
      if (p === '/api/sessions/s1/steer/abcdefgh12') return { status: 409, body: { error: 'unknown', delivery: 'pending' } };
      if (p.startsWith('/api/sessions/s1/mentions')) return { paths: ['a.ts'] };
    });
    const b = make(host);
    const r = await b.send('s1', { text: 'hi', attachmentIds: ['a1'], defer: true, sendId: 'abcdefgh12' });
    assert.equal(r.status, 'queued');
    assert.deepEqual(host.log.find(l => l.path === '/api/sessions/s1/messages').body, expected);
    b.dispose();
  }
  const host = fakeHost((m, p) => {
    if (p === '/api/info') return info(['sendId']);
    if (p === '/api/sessions/s1/steer/abcdefgh12') return { status: 409, body: { error: 'unknown', delivery: 'pending' } };
  });
  const b = make(host);
  assert.deepEqual(await b.mentions('s1', 'a'), [], 'no mentions feature: no request');
  assert.ok(host.log.every(l => !l.path.includes('/mentions')));
  const s = await b.steerStatus('s1', 'abcdefgh12');
  assert.equal(s.delivery, 'pending');
  await assert.rejects(b.steer('s1', 'more', 'abcdefgh12'), e => e.delivery === 'rejected');
});

test('request bodies carry only host fields', async () => {
  const host = fakeHost((m, p) => {
    if (p === '/api/sessions' && m === 'POST') return { status: 201, body: session('n') };
    if (p === '/api/sessions/s1' && m === 'PATCH') return session('s1');
    if (p.startsWith('/api/requests/')) return { status: 'resolved' };
    if (p.startsWith('/api/native/sessions/x/import')) return { status: 201, body: session('x') };
    if (p.startsWith('/api/profiles/oauth/')) return { status: 'ok' };
    if (p.startsWith('/api/preferences')) return { permissionMode: '', model: '', effort: '', revision: 'r2' };
    if (p === '/api/sessions') return [];
  });
  const b = make(host);
  await b.createSession({ provider: 'claude', title: 't', cwd: '/w', mode: 'terminal', trustFolder: true, permissionMode: 'default', model: '', effort: '', confirmDangerous: false });
  assert.deepEqual(host.log.at(-1).body, { title: 't', cwd: '/w', mode: 'terminal', trustFolder: true, permissionMode: 'default', model: '', effort: '', confirmDangerous: false });
  await b.patchSession('s1', { permissionMode: 'bypassPermissions', confirmDangerous: true });
  assert.deepEqual(host.log.find(l => l.method === 'PATCH').body, { permissionMode: 'bypassPermissions', confirmDangerous: true });
  await b.answer('r1', { decision: 'allow', answers: { q: 'a' } });
  assert.deepEqual(host.log.find(l => l.path === '/api/requests/r1').body, { decision: 'allow', answers: { q: 'a' } });
  await b.importNative('x', 'codex');
  assert.deepEqual(host.log.find(l => l.path.startsWith('/api/native/sessions/x/import')), { ...host.log.find(l => l.path.startsWith('/api/native/sessions/x/import')), path: '/api/native/sessions/x/import?provider=codex', body: { confirmStopped: true } });
  await b.editOAuthProfile('oauth:ab12', { name: 'Renamed', token: 'sk-ant-oat-x' });
  assert.equal(host.log.find(l => l.method === 'PATCH' && l.path.startsWith('/api/profiles')).path, '/api/profiles/oauth/ab12');
  assert.deepEqual(host.log.at(-1).body, { name: 'Renamed', token: 'sk-ant-oat-x' });
  await b.savePreferences('codex', { provider: 'codex', permissionMode: 'read-only', model: '', effort: '', revision: 'r1' });
  const put = host.log.find(l => l.method === 'PUT');
  assert.equal(put.path, '/api/preferences?provider=codex');
  assert.deepEqual(put.body, { permissionMode: 'read-only', model: '', effort: '', revision: 'r1' });
  b.dispose();
});

test('watchUsage: a failed poll keeps the last reading, marked stale', async () => {
  let fail = false;
  const host = fakeHost((m, p) => {
    if (!p.startsWith('/api/usage')) return;
    return fail ? { status: 502, body: { error: 'down' } } : { available: true, usage: { fiveHour: { percent: 12 } } };
  });
  const b = make(host);
  const seen = [];
  const stop = b.watchUsage({ provider: 'codex', sessionId: 's1' }, u => seen.push(u));
  await until(() => seen.length >= 1);
  assert.equal(host.log[0].path, '/api/usage?sessionId=s1&provider=codex');
  fail = true;
  await until(() => seen.some(u => u.stale));
  const stale = seen.find(u => u.stale);
  assert.equal(stale.usage.fiveHour.percent, 12);
  assert.equal(stale.error, 'request-failed');
  stop();
});

class FakeSocket {
  static last;
  constructor(url) { this.url = url; this.readyState = 0; this.sent = []; FakeSocket.last = this; }
  send(d) { this.sent.push(d); }
  close() { this.readyState = 3; }
  open() { this.readyState = 1; this.onopen?.({}); }
  message(data) { this.onmessage?.({ data }); }
  closeWith(code, reason = '') { this.readyState = 3; this.onclose?.({ code, reason }); }
}

test('terminal: ticket, wss URL, binary frames, size messages, 4001 exits', async () => {
  const host = fakeHost((m, p) => (p === '/api/terminals/t1/ticket' && m === 'POST' ? { ticket: 'tk' } : undefined));
  const b = make(host, { baseUrl: 'https://h.example:8787', WebSocket: FakeSocket });
  const states = [], data = [], sizes = [];
  const s = b.openTerminal('t1', { onData: c => data.push(c), onSize: z => sizes.push(z), onState: (st, d, o) => states.push([st, d, o]) });
  await until(() => !!FakeSocket.last && FakeSocket.last.url.includes('tk'));
  const ws = FakeSocket.last;
  assert.equal(ws.url, 'wss://h.example:8787/api/terminals/t1/stream?ticket=tk');
  ws.open();
  s.send('ls\r');
  s.resize(100, 30);
  s.redraw();
  assert.deepEqual([...ws.sent[0]], [...new TextEncoder().encode('ls\r')]);
  assert.deepEqual(JSON.parse(ws.sent[1]), { type: 'resize', cols: 100, rows: 30 });
  assert.deepEqual(JSON.parse(ws.sent[2]), { type: 'redraw' });
  ws.message(new Uint8Array([104, 105]).buffer);
  ws.message(JSON.stringify({ type: 'size', cols: 120, rows: 40, held: true }));
  assert.deepEqual([...data[0]], [104, 105]);
  assert.deepEqual(sizes, [{ cols: 120, rows: 40, held: true }]);
  ws.closeWith(4001, 'Claude Code exited');
  assert.deepEqual(states.map(x => x[0]), ['connecting', 'open', 'exited']);
  assert.equal(states[2][1], 'Claude Code exited');
});

test('terminal: other closes fail (retryable); a 404 ticket fails for good; close() is silent', async () => {
  const host = fakeHost((m, p) => (p === '/api/terminals/ok/ticket' ? { ticket: 'tk' } : { status: 404, body: { error: 'Terminal session not found' } }));
  const b = make(host, { WebSocket: FakeSocket });
  const states = [];
  FakeSocket.last = undefined;
  b.openTerminal('ok', { onData() {}, onSize() {}, onState: (st, d, o) => states.push([st, d, o]) });
  await until(() => FakeSocket.last?.url.endsWith('ticket=tk'));
  FakeSocket.last.open();
  FakeSocket.last.closeWith(1006);
  assert.equal(states.at(-1)[0], 'failed');
  assert.equal(states.at(-1)[2], undefined);

  const gone = [];
  b.openTerminal('gone', { onData() {}, onSize() {}, onState: (st, d, o) => gone.push([st, d, o]) });
  await until(() => gone.length === 2);
  assert.deepEqual(gone[1], ['failed', 'Terminal session not found', { retry: false }]);

  const quiet = [];
  FakeSocket.last = undefined;
  const s = b.openTerminal('ok', { onData() {}, onSize() {}, onState: st => quiet.push(st) });
  await until(() => !!FakeSocket.last);
  s.close();
  FakeSocket.last.closeWith(1000);
  assert.deepEqual(quiet, ['connecting']);
});

class FakeXHR {
  constructor() { this.headers = {}; this.upload = {}; FakeXHR.last = this; }
  open(method, url) { this.method = method; this.url = url; }
  setRequestHeader(k, v) { this.headers[k] = v; }
  send(form) {
    this.form = form;
    setTimeout(() => {
      this.upload.onprogress?.({ lengthComputable: true, loaded: 50, total: 100 });
      this.upload.onprogress?.({ lengthComputable: true, loaded: 100, total: 100 });
      this.status = FakeXHR.status;
      this.responseText = JSON.stringify(FakeXHR.reply);
      this.onload?.();
    }, 5);
  }
  abort() { this.onabort?.(); }
}

test('upload: multipart with the token, progress, and mapped errors', async () => {
  const b = make(fakeHost(() => undefined), { XMLHttpRequest: FakeXHR });
  FakeXHR.status = 201;
  FakeXHR.reply = { id: 'u1', kind: 'image', name: 'a.png', mime: 'image/png', size: 3, url: '/api/sessions/s1/uploads/u1' };
  const progress = [];
  const a = await b.upload('s1', new File([new Uint8Array([1, 2, 3])], 'a.png', { type: 'image/png' }), { onProgress: f => progress.push(f) });
  assert.equal(a.id, 'u1');
  assert.deepEqual(progress, [0.5, 1]);
  assert.equal(FakeXHR.last.url, '/api/sessions/s1/uploads');
  assert.equal(FakeXHR.last.headers.Authorization, 'Bearer tok');
  assert.ok(FakeXHR.last.form.get('file'));
  FakeXHR.status = 400;
  FakeXHR.reply = { error: 'A file cannot be larger than 20 MiB' };
  await assert.rejects(b.upload('s1', new File(['x'], 'x.bin')), e => e instanceof BackendError && e.status === 400 && /20 MiB/.test(e.message));
});

test('readFile: 413 is tooLarge; text and the host path come back', async () => {
  const fetch = async url => {
    if (url.includes('big')) return new Response(JSON.stringify({ error: 'too big' }), { status: 413 });
    return new Response('hello', { status: 200, headers: { 'Content-Type': 'text/plain; charset=utf-8', 'X-CC-Path': encodeURIComponent('/w/a.txt') } });
  };
  const b = new HttpBackend({ baseUrl: '', token: 't', lang: 'en', fetch });
  assert.deepEqual(await b.readFile('s1', 'big.bin'), { path: 'big.bin', mime: '', size: 0, tooLarge: true });
  const f = await b.readFile('s1', 'a.txt', ['/w']);
  assert.equal(f.text, 'hello');
  assert.equal(f.path, '/w/a.txt');
  assert.equal(f.mime, 'text/plain');
});

test('new transcript events poll the list at once; the refreshed session record follows', async () => {
  let events = [ev(1)];
  let status = 'running';
  const host = fakeHost((m, p) => {
    if (p === '/api/sessions') return [session('s1', { status })];
    if (p.startsWith('/api/requests')) return [];
    if (p.startsWith('/api/sessions/s1?')) return { session: session('s1', { status }), events };
    if (p.startsWith('/api/sessions/s1/events?after=')) {
      const after = Number(new URLSearchParams(p.split('?')[1]).get('after'));
      return events.filter(e => e.seq > after);
    }
  });
  const b = make(host, { intervals: { sessions: 60_000, transcript: 15, usage: 60_000 } });
  const updates = [];
  const stop = b.watchTranscript('s1', u => updates.push(u));
  await until(() => updates.length >= 1);
  await wait(30);
  const lists = () => host.log.filter(l => l.path === '/api/sessions').length;
  const before = lists();
  status = 'completed';
  events = [...events, ev(2, 'result')];
  await until(() => updates.some(u => u.session.status === 'completed'), 500);
  assert.ok(lists() > before, 'list polled right after new events');
  stop();
  b.dispose();
});
