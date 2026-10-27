import test from 'node:test';
import assert from 'node:assert/strict';
import { build } from 'esbuild';
import { pathToFileURL, fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
const web = join(dirname(fileURLToPath(import.meta.url)), '..');
const out = join(web, 'node_modules/.cache/context-usage-test.mjs');
await build({ entryPoints: [join(web, 'src/contextReading.ts')], outfile: out, bundle: true, platform: 'node', format: 'esm', logLevel: 'silent' });
const { contextReading } = await import(pathToFileURL(out).href);

const event = (seq, tokenUsage) => ({ seq, kind: 'status', payload: { tokenUsage } });
test('current Codex context uses the latest call rather than cumulative session tokens', () => {
  const reading = contextReading('codex', undefined, [event(1, { total: { totalTokens: 400000 }, last: { totalTokens: 25000 }, modelContextWindow: 100000 })]);
  assert.equal(reading.percent, 25); assert.equal(reading.tokens, 25000); assert.equal(reading.total.totalTokens, 400000);
});
test('missing last call, malformed newer snapshots and absent context stay unknown', () => {
  const live = { contextTokens: 10, contextSize: 100, contextPercent: 10 };
  assert.equal(contextReading('codex', live, [event(1, { total: { totalTokens: 100 }, modelContextWindow: 1000 })]).percent, undefined);
  assert.equal(contextReading('codex', live, [event(1, { last: { totalTokens: 10 }, modelContextWindow: 100 }), event(2, null)]).percent, undefined);
  assert.deepEqual(contextReading('claude'), {});
  assert.deepEqual(contextReading('claude', { contextTokens: 0, contextSize: 0, contextPercent: 0 }), {});
});
test('reported Claude values retain zero, clamp ring and reject invalid capacity', () => {
  assert.equal(contextReading('claude', { contextTokens: 0, contextSize: 100, contextPercent: 0 }).percent, 0);
  assert.equal(contextReading('claude', { contextTokens: 200, contextSize: 100, contextPercent: 200 }).percent, 100);
  assert.equal(contextReading('claude', { contextTokens: 20, contextSize: NaN, contextPercent: 20 }).percent, undefined);
});
