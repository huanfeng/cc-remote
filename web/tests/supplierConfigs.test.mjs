import { test } from 'node:test';
import assert from 'node:assert/strict';
import { HttpBackend } from '../src/backend/http/httpBackend.ts';

const make = fetch => new HttpBackend({ baseUrl: 'http://host.test', token: 'token', lang: 'en', fetch });
test('supplier management is optional while old OAuth and catalog remain usable', async () => {
  for (const status of [404, 405, 501]) {
    const b = make(async url => url.includes('supplier-configs') ? new Response('{}', { status }) : new Response(JSON.stringify(url.includes('/oauth') ? { profiles: [{ id: 'official', name: 'Official' }] } : { profiles: [{ id: 'ccswitch:one', name: 'Imported' }], defaultProfiles: {} })));
    assert.equal(await b.supplierConfigs(), null);
    assert.equal((await b.oauthProfiles())[0].name, 'Official');
    assert.equal((await b.profiles()).profiles[0].name, 'Imported');
    b.dispose();
  }
  const b = make(async () => new Response('<html>legacy</html>'));
  assert.equal(await b.supplierConfigs(), null);
  b.dispose();
});
test('supplier edit sends only supported fields, preserves blank credential and collection revision', async () => {
  const calls = [];
  const b = make(async (url, init) => { calls.push([url, init.method, JSON.parse(init.body)]); return new Response('{"profiles":[],"revision":"next"}'); });
  const body = { name: 'Relay', app: 'claude', baseUrl: 'https://relay.example/v1', model: '', authType: 'apiKey', credential: '', revision: 'current', hasCredential: true, unexpected: 'omit' };
  await b.saveSupplierConfig('local:a/b', body);
  await b.removeSupplierConfig('local:a/b', 'next');
  assert.deepEqual(calls[0], ['http://host.test/api/supplier-configs/local%3Aa%2Fb', 'PUT', { name: 'Relay', app: 'claude', baseUrl: body.baseUrl, model: '', authType: 'apiKey', credential: '', revision: 'current' }]);
  assert.deepEqual(calls[1], ['http://host.test/api/supplier-configs/local%3Aa%2Fb', 'DELETE', { revision: 'next' }]);
  b.dispose();
});

test('advanced supplier fields are sent without runtime snapshots or unrelated credentials', async () => {
  let sent;
  const b = make(async (_url, init) => { sent = JSON.parse(init.body); return new Response('{"profiles":[],"revision":"next","capabilities":{"advanced":true}}'); });
  await b.saveSupplierConfig('local:one', { name:'Relay', app:'claude', baseUrl:'https://relay.example', model:'', authType:'apiKey', credential:'', revision:'current', website:'https://example.com', notes:'Work', modelMappings:{sonnet:'relay-sonnet'}, timeoutMs:30000, env:{ANTHROPIC_API_KEY:'never-send'} });
  assert.equal(sent.modelMappings.sonnet, 'relay-sonnet');
  assert.equal(sent.timeoutMs, 30000);
  assert.equal(sent.notes, 'Work');
  assert.ok(!JSON.stringify(sent).includes('never-send'));
  b.dispose();
});
