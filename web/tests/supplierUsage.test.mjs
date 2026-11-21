import { test } from 'node:test';
import assert from 'node:assert/strict';
import { HttpBackend } from '../src/backend/http/httpBackend.ts';
import { supplierUsageDescription, supplierUsageFor } from '../src/features/pickers/supplierUsage.ts';
import { supplierOptions } from '../src/features/pickers/supplierSwitch.ts';

const now = Date.parse('2026-11-19T08:00:00Z');
const current = { available: true, identityKnown: true, fetchedAt: '2026-11-19T07:59:00Z', windows: [
  { name: 'session', windowMinutes: 300, percent: 24, resetsAt: '2026-11-19T10:00:00Z' },
  { name: 'week', windowMinutes: 10080, percent: 83 },
] };
const descriptionText = parts => parts.map(({ text }) => text).join(' · ');
const usageText = usage => descriptionText(supplierUsageDescription(usage, now));

test('supplier overview displays cached 5h/7d used quota, reset and collection age', () => {
  const text = usageText(current);
  assert.match(text, /Used.*5h 24%.*↺2h 0m.*7d 83%/);
  assert.match(text, /Fetched 1 min ago/);
  assert.doesNotMatch(text, /Cached|failed|unconfirmed/);
});

test('each quota window carries its own severity, while unknown readings stay neutral', () => {
  assert.deepEqual(supplierUsageDescription(current, now).filter(part => part.tone).map(part => part.tone), ['ok', 'warn']);
  const windows = [{ name: '5h', percent: 93 }, { name: '7d', percent: NaN }];
  const parts = supplierUsageDescription({ ...current, windows }, now);
  assert.equal(parts.find(part => part.text === '5h 93%').tone, 'high');
  assert.equal(parts.find(part => part.text === '7d —').tone, undefined);
});

test('stale, shared and failed readings are explicit; unknown percentages never become zero', () => {
  const text = usageText({ ...current, stale: true, shared: true, identityKnown: false, error: 'request-failed', windows: [{ name: '5h', percent: NaN }] });
  assert.match(text, /Update failed.*Cached reading.*5h —/);
  assert.match(text, /Shared quota/);
  assert.doesNotMatch(text, /Identity unconfirmed/);
  assert.match(text, /Fetched 1 min ago/, 'failure keeps the last successful reading time');
  assert.doesNotMatch(text, /0%/);
  assert.match(usageText({ available: false, windows: [], error: 'expired' }), /Login expired/);
  assert.match(usageText({ available: false, reason: 'pending', windows: [] }), /No reading yet/);
  assert.match(usageText({ available: false, reason: 'third-party', windows: [] }), /No subscription quota/);
  assert.match(usageText({ available: false, reason: 'no-login', windows: [] }), /Not signed in/);
  assert.doesNotThrow(() => supplierUsageDescription({ windows: [null] }));
  assert.match(usageText({ ...current, fetchedAt: undefined }), /Fetch time unknown/);
  assert.match(usageText({ ...current, fetchedAt: 'invalid' }), /Fetch time unknown/);
});

test('shared supplier options attach each OAuth reading by stable ID and preserve selection semantics', () => {
  const catalog = { profiles: [
    { id: 'oauth:one', app: 'claude', kind: 'oauth', name: 'Work' },
    { id: 'api', app: 'claude', name: 'Relay' },
  ], defaultProfiles: { claude: 'oauth:one' } };
  const usage = { profiles: { 'oauth:one': current, '@native': { windows: [], reason: 'no-login', available: false } } };
  const options = supplierOptions(catalog, 'claude', false, usage);
  assert.deepEqual(options.map(option => option.value), ['', '@native', 'oauth:one', 'api']);
  assert.deepEqual(options[0].description.map(part => part.tone), options[2].description.map(part => part.tone));
  assert.match(descriptionText(options[1].description), /Not signed in/);
  assert.equal(options[3].description, undefined);
  assert.equal(supplierOptions(catalog, 'claude', true, usage)[0].description, undefined, 'resume keep-current does not claim default quota');
  assert.equal(supplierOptions(catalog, 'codex', false, usage).some(option => option.description), false);
  assert.equal(supplierUsageFor(null, 'oauth:one'), undefined, 'old hosts do not gain a misleading zero quota');
  assert.match(descriptionText(supplierUsageFor({ profiles: {} }, 'oauth:one')), /unavailable/);
});

test('supplier usage reads the CLI cached endpoint once and gracefully handles older hosts', async () => {
  for (const status of [404, 405, 501]) {
    const paths = [];
    const backend = new HttpBackend({ baseUrl: '', token: 'test', lang: 'en', fetch: async path => {
      paths.push(path); return new Response('{}', { status });
    } });
    assert.equal(await backend.profileUsage('claude'), null);
    assert.deepEqual(paths, ['/api/profiles/usage?app=claude']);
    assert.equal(await backend.profileUsage('codex'), null);
    assert.equal(paths.length, 1);
    backend.dispose();
  }
  for (const body of ['<html>old host</html>', '{}', '{"profiles":[]}']) {
    const backend = new HttpBackend({ baseUrl: '', token: 'test', lang: 'en', fetch: async () => new Response(body) });
    assert.equal(await backend.profileUsage('claude'), null);
    backend.dispose();
  }
  const backend = new HttpBackend({ baseUrl: '', token: 'test', lang: 'en', fetch: async () => new Response(JSON.stringify({ profiles: { 'oauth:one': current } })) });
  assert.deepEqual(await backend.profileUsage('claude'), { profiles: { 'oauth:one': current } });
  backend.dispose();
});
