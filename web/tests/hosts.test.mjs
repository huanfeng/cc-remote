import { test } from 'node:test';
import assert from 'node:assert/strict';

globalThis.location = { protocol: 'https:' };
const { normalizeHostUrl, STANDALONE } = await import('../src/hosts.ts');

test('host address: scheme, host and path kept, trailing slash dropped', () => {
  assert.equal(STANDALONE, false);
  assert.equal(normalizeHostUrl(' https://CCR.example.com:8443/ '), 'https://ccr.example.com:8443');
  assert.equal(normalizeHostUrl('https://proxy.example.com/ccr/'), 'https://proxy.example.com/ccr');
  assert.equal(normalizeHostUrl('http://127.0.0.1:8787'), 'http://127.0.0.1:8787');
});

test('host address: a bare host gets the page scheme; others are refused', () => {
  assert.equal(normalizeHostUrl('ccr.example.com'), 'https://ccr.example.com');
  globalThis.location = { protocol: 'http:' };
  assert.equal(normalizeHostUrl('192.168.1.5:8787'), 'http://192.168.1.5:8787');
  assert.equal(normalizeHostUrl('ftp://x.example'), '');
  assert.equal(normalizeHostUrl(''), '');
});

const { resolveEntry } = await import('../src/hosts.ts');
const answer = (fields) => async () => ({ ok: true, status: 200, text: async () => '', ...fields });
const rejects = (promise, pattern) => assert.rejects(promise, pattern);

test('entry: a text entry answers with the address; anything else is refused', async () => {
  globalThis.location = { protocol: 'https:', origin: 'https://ccr.example.com' };
  assert.equal(await resolveEntry('https://raw.example/ccr.txt', answer({ text: async () => '  https://home.example.com:45678/\n# updated 12:00\n' })), 'https://home.example.com:45678');
  await rejects(resolveEntry('https://raw.example/ccr.txt', answer({ text: async () => '<html>' })), /gave no host address/);
  await rejects(resolveEntry('https://raw.example/ccr.txt', answer({ ok: false, status: 404 })), /error \(404\)/);
  // No CORS, or not reachable: the browser rejects the fetch.
  await rejects(resolveEntry('https://raw.example/ccr.txt', async () => { throw new TypeError('Failed to fetch'); }), /Access-Control-Allow-Origin: https:\/\/ccr\.example\.com/);
});

test('entry: an https page refuses a plain http host, except on this machine', async () => {
  globalThis.location = { protocol: 'https:', origin: 'https://ccr.example.com' };
  await rejects(resolveEntry('https://raw.example/ccr.txt', answer({ text: async () => 'http://1.2.3.4:45678' })), /cannot reach a plain http/);
  assert.equal(await resolveEntry('https://raw.example/ccr.txt', answer({ text: async () => 'http://127.0.0.1:8787' })), 'http://127.0.0.1:8787');
  globalThis.location = { protocol: 'http:', origin: 'http://192.168.1.2:5173' };
  assert.equal(await resolveEntry('http://raw.example/ccr.txt', answer({ text: async () => 'http://1.2.3.4:45678' })), 'http://1.2.3.4:45678');
});

test('entry: a host moves by itself only to a new port', async () => {
  const { samePlace } = await import('../src/hosts.ts');
  assert.equal(samePlace('https://home.example.com:45678', 'https://home.example.com:51234'), true);
  assert.equal(samePlace('https://home.example.com:45678', 'https://evil.example:45678'), false);
  assert.equal(samePlace('https://home.example.com:45678', 'http://home.example.com:45678'), false);
  assert.equal(samePlace('https://home.example.com', 'not a url'), false);
});

test('hosts: editing changes one record; an address another host has is found', async () => {
  const data = new Map();
  globalThis.localStorage = { getItem: k => data.get(k) ?? null, setItem: (k, v) => data.set(k, String(v)), removeItem: k => data.delete(k) };
  const { findHost, updateHost, loadHosts } = await import('../src/hosts.ts');
  localStorage.setItem('cc-hosts:v1', JSON.stringify({ current: 'a', hosts: [
    { id: 'a', url: 'https://home.example.com:45678', token: 'ta', entry: 'https://addr.example.dev/', via: 'text' },
    { id: 'b', url: 'https://ccr.example.com', token: 'tb' },
  ] }));
  assert.equal(findHost('https://ccr.example.com', undefined, 'a')?.id, 'b');
  assert.equal(findHost('https://ccr.example.com', undefined, 'b'), undefined);
  assert.equal(findHost('https://other.example', 'https://addr.example.dev/', 'b')?.id, 'a');
  // Made direct: the entry goes; the token and the other host stay.
  updateHost('a', { url: 'https://home.example.com', entry: undefined, via: undefined });
  const [a, b] = loadHosts().hosts;
  assert.deepEqual(a, { id: 'a', url: 'https://home.example.com', token: 'ta' });
  assert.deepEqual(b, { id: 'b', url: 'https://ccr.example.com', token: 'tb' });
  assert.equal(loadHosts().current, 'a');
});

test('hosts: one added through a 302 entry stays at the address it was found at', async () => {
  const { loadHosts } = await import('../src/hosts.ts');
  localStorage.setItem('cc-hosts:v1', JSON.stringify({ current: 'a', hosts: [
    { id: 'a', url: 'https://home.example.com:45678', token: 'ta', entry: 'https://jump.example/ccr', via: 'redirect' },
  ] }));
  assert.deepEqual(JSON.parse(JSON.stringify(loadHosts().hosts)), [{ id: 'a', url: 'https://home.example.com:45678', token: 'ta' }]);
});

test('home network: checked with the token, refused on no answer or an error', async () => {
  const { lanAnswers, blockedHttp } = await import('../src/hosts.ts');
  const host = { id: 'a', url: 'https://home.example.com:45678', token: 'ta', lan: 'https://lan.example.com' };
  let asked;
  assert.equal(await lanAnswers(host, async (url, init) => { asked = [url, init.headers.Authorization]; return { ok: true }; }), true);
  assert.deepEqual(asked, ['https://lan.example.com/api/info', 'Bearer ta']);
  assert.equal(await lanAnswers(host, async () => ({ ok: false, status: 401 })), false);
  assert.equal(await lanAnswers(host, async () => { throw new TypeError('Failed to fetch'); }), false);
  assert.equal(await lanAnswers({ ...host, lan: undefined }, async () => ({ ok: true })), false);
  globalThis.location = { protocol: 'https:' };
  assert.equal(blockedHttp('http://192.168.1.5:8787'), true);
  assert.equal(blockedHttp('http://127.0.0.1:8787'), false);
  assert.equal(blockedHttp('https://lan.example.com'), false);
});

test('home network: a new address is checked again before it is used', async () => {
  const { updateHost } = await import('../src/hosts.ts');
  localStorage.setItem('cc-hosts:v1', JSON.stringify({ current: 'a', hosts: [{ id: 'a', url: 'https://ccr.example.com', token: 'ta', lan: 'https://lan.example.com' }] }));
  localStorage.setItem('cc-lan@a', '1');
  updateHost('a', { alias: 'Home' });
  assert.equal(localStorage.getItem('cc-lan@a'), '1');
  updateHost('a', { lan: 'https://nas.example.com' });
  assert.equal(localStorage.getItem('cc-lan@a'), null);
});

test('colour: random picks a free palette colour other than the current one', async () => {
  const { PALETTE, randomColor } = await import('../src/hosts.ts');
  const last = PALETTE.length - 1;
  const taken = PALETTE.slice(0, last - 1);
  for (const r of [0, 0.5, 0.99]) assert.equal(randomColor(taken, PALETTE[last], () => r), PALETTE[last - 1]);
  // Every colour taken: any but the current one.
  for (let i = 0; i < 20; i++) assert.notEqual(randomColor(PALETTE, PALETTE[3]), PALETTE[3]);
});
