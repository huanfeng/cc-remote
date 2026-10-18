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
