import { test } from 'node:test';
import assert from 'node:assert/strict';
import { HttpBackend } from '../src/backend/http/httpBackend.ts';

function harness(ticket = Response.json({ ticket: 'once' })) {
  const requests = [];
  const { promise: connected, resolve } = Promise.withResolvers();
  class Socket {
    readyState = 1;
    sent = [];
    constructor(url) { this.url = new URL(url); resolve(this); }
    send(data) { this.sent.push(data); }
    close() { this.readyState = 3; }
    frame(data) { this.onmessage?.({ data: JSON.stringify(data) }); }
    output(text, seq) {
      const payload = new TextEncoder().encode(text);
      const frame = new Uint8Array(9 + payload.length).subarray(1);
      new DataView(frame.buffer, frame.byteOffset).setBigUint64(0, BigInt(seq));
      frame.set(payload, 8);
      this.onmessage?.({ data: frame });
    }
  }
  const backend = new HttpBackend({
    baseUrl: 'https://host.test', token: 'secret', lang: 'en', WebSocket: Socket,
    fetch: async (url, init) => {
      requests.push({ url: new URL(url), init });
      if (url.endsWith('/ticket')) return ticket;
      if (init.method === 'GET') return Response.json([]);
      if (init.method === 'POST') return Response.json({ id: 'shell', runId: 'run' });
      return new Response(null, { status: 204 });
    },
  });
  return { backend, connected, requests };
}

function listener() {
  const events = [];
  return {
    events,
    handlers: {
      onState: (...args) => events.push(['state', ...args.filter(value => value !== undefined)]),
      onData: (bytes, seq) => events.push(['data', new TextDecoder().decode(bytes), seq]),
      onSize: (...args) => events.push(['size', ...args.filter(value => value !== undefined)]),
      onControl: held => events.push(['control', held]),
      onReady: seq => events.push(['ready', seq]),
      onReset: seq => events.push(['reset', seq]),
    },
  };
}

test('project Shell uses an authenticated run-specific ticket and applies ordered replay frames', { timeout: 3000 }, async t => {
  const { backend, connected, requests } = harness();
  t.after(() => backend.dispose());
  const { events, handlers } = listener();
  const stream = backend.openProjectShell('shell/id', 'run#2', 17, handlers);
  const socket = await connected;

  assert.equal(requests[0].url.pathname, '/api/project-shells/shell%2Fid/runs/run%232/ticket');
  assert.equal(requests[0].init.method, 'POST');
  assert.equal(requests[0].init.headers.Authorization, 'Bearer secret');
  assert.equal(socket.url.href, 'wss://host.test/api/project-shells/shell%2Fid/runs/run%232/stream?ticket=once&after=17&flow=1');

  socket.frame({ type: 'control', held: true });
  stream.send('ignored before replay');
  socket.frame({ type: 'reset', seq: 20 });
  socket.frame({ type: 'size', cols: 80, rows: 20 });
  socket.output('提示符 $ ', 20);
  socket.frame({ type: 'ready', seq: 20 });
  socket.frame({ type: 'size', cols: 81, rows: 21, seq: 21 });
  socket.output('live', 22);
  assert.deepEqual(events, [
    ['state', 'connecting'], ['control', true], ['reset', 20],
    ['size', { cols: 80, rows: 20 }], ['data', '提示符 $ ', 20], ['ready', 20],
    ['size', { cols: 81, rows: 21 }, 21], ['data', 'live', 22],
  ]);
  assert.deepEqual(socket.sent, []);

  stream.send('echo test\r');
  socket.frame({ type: 'control', held: false });
  stream.send('ignored as spectator');
  stream.resize(100, 30);
  stream.take();
  assert.deepEqual(socket.sent, [new TextEncoder().encode('echo test\r'), '{"type":"take"}']);

  socket.frame({ type: 'exit', detail: 'stopped' });
  assert.deepEqual(events.at(-1), ['state', 'exited', 'stopped']);
  assert.equal(socket.readyState, 3);
});

test('project Shell acknowledges applied output during replay only when the host negotiates flow control', { timeout: 3000 }, async t => {
  const { backend, connected } = harness();
  t.after(() => backend.dispose());
  const { handlers } = listener();
  const stream = backend.openProjectShell('shell', 'run', 0, handlers);
  const socket = await connected;
  stream.ack?.(1);
  assert.deepEqual(socket.sent, []);
  socket.frame({ type: 'flow' });
  socket.output('replay', 3);
  assert.deepEqual(socket.sent, []);
  stream.ack?.(3);
  stream.ack?.(3);
  stream.ack?.(2);
  await Promise.resolve();
  assert.deepEqual(socket.sent, ['{"type":"ack","seq":3}']);
});

test('project Shell metadata uses its own API and a lost run stops automatic transport retries', { timeout: 3000 }, async t => {
  const { backend, requests } = harness(Response.json({ error: 'Run has ended' }, { status: 410 }));
  t.after(() => backend.dispose());
  assert.deepEqual(await backend.projectShells(), []);
  assert.deepEqual(await backend.startProjectShell({ cwd: '/work/project' }), { id: 'shell', runId: 'run' });
  await backend.stopProjectShell('shell', 'run');
  assert.deepEqual(requests.map(({ url, init }) => [url.pathname, init.method, init.body]), [
    ['/api/project-shells', 'GET', undefined],
    ['/api/project-shells', 'POST', '{"cwd":"/work/project"}'],
    ['/api/project-shells/shell/runs/run', 'DELETE', undefined],
  ]);
  const { promise: exited, resolve } = Promise.withResolvers();
  const { handlers } = listener();
  backend.openProjectShell('shell', 'run', 4, {
    ...handlers,
    onState: (state, detail) => { if (state !== 'connecting') resolve({ state, detail }); },
  });
  assert.deepEqual(await exited, { state: 'exited', detail: 'Run has ended' });
});

test('the project Shell watcher delivers shells created by another client', { timeout: 3000 }, async t => {
  const shell = { id: 'cli-shell', cwd: '/work/cli', projectKey: '/work/cli', state: 'running', runId: 'run' };
  let shells = [];
  const backend = new HttpBackend({
    baseUrl: 'https://host.example', token: 'token', lang: 'en', intervals: { projectShells: 10 },
    fetch: async () => Response.json(shells),
  });
  t.after(() => backend.dispose());
  const seen = [];
  const { promise: discovered, resolve } = Promise.withResolvers();
  const stop = backend.watchProjectShells(value => {
    seen.push(value);
    if (seen.length === 1) shells = [shell];
    else resolve();
  });
  await discovered;
  stop();
  assert.deepEqual(seen, [[], [shell]]);
});

test('a quiet project Shell can become ready at sequence zero and closes silently on host switch', { timeout: 3000 }, async t => {
  const { backend, connected } = harness();
  t.after(() => backend.dispose());
  const { events, handlers } = listener();
  const stream = backend.openProjectShell('shell', 'run', 0, handlers);
  const socket = await connected;
  socket.frame({ type: 'size', cols: 120, rows: 40 });
  socket.frame({ type: 'ready', seq: 0 });
  socket.frame({ type: 'control', held: true });
  assert.deepEqual(events, [
    ['state', 'connecting'], ['size', { cols: 120, rows: 40 }], ['ready', 0], ['control', true],
  ]);
  backend.dispose();
  socket.output('stale output', 1);
  stream.send('stale input');
  assert.equal(events.length, 4);
  assert.equal(socket.readyState, 3);
  assert.deepEqual(socket.sent, []);
});
