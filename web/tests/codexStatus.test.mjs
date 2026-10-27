import { before, test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { renderToStaticMarkup } from 'react-dom/server';
import { createElement } from 'react';

let parseCodexStatus, readCodexUsage, foldCodexActivityEvents, CodexStatusEvent, CodexSessionUsage;
before(async () => {
  const { build } = await import('esbuild');
  const web = join(dirname(fileURLToPath(import.meta.url)), '..');
  const outdir = join(web, 'node_modules', '.cache', 'codex-status-test');
  mkdirSync(outdir, { recursive: true });
  const outfile = join(outdir, 'status.mjs');
  await build({
    stdin: { contents: "export * from './codexStatus'; export * from './CodexStatusEvent';", resolveDir: join(web, 'src'), loader: 'ts' },
    outfile, bundle: true, platform: 'node', format: 'esm', packages: 'external', jsx: 'automatic', loader: { '.css': 'empty' }, logLevel: 'silent',
  });
  ({ parseCodexStatus, readCodexUsage, foldCodexActivityEvents, CodexStatusEvent, CodexSessionUsage } = await import(pathToFileURL(outfile).href));
});

const event = (payload, seq = 1, kind = 'status') => ({ seq, kind, payload, createdAt: '2026-10-25T00:00:00Z' });
const usage = (totalTokens, extra = {}) => ({ tokenUsage: { total: { totalTokens, inputTokens: 12, outputTokens: 8 }, last: { totalTokens: 10 }, modelContextWindow: 200, ...extra } });
const html = (Component, props) => renderToStaticMarkup(createElement(Component, props));

const activity = (subtype, delta, seq, extra = {}) => event({ subtype, threadId: 'thread-a', turnId: 'turn-a', itemId: 'item-a', delta, ...extra }, seq);

test('streamed activity folds per item and turn without mutating source or duplicating repeated pages', () => {
  const first = activity('item/commandExecution/outputDelta', 'one\n', 1);
  const events = [first, activity('item/commandExecution/outputDelta', 'two', 2), first,
    activity('item/commandExecution/outputDelta', 'another tool', 3, { itemId: 'item-b' }),
    activity('item/commandExecution/outputDelta', 'another turn', 4, { turnId: 'turn-b' }),
  ];
  const folded = foldCodexActivityEvents(events);
  assert.equal(folded.length, 3);
  assert.deepEqual(folded.map(e => parseCodexStatus(e).text), ['one\ntwo', 'another tool', 'another turn']);
  assert.equal(first.payload.delta, 'one\n');
  assert.equal(first.payload.subtype, 'item/commandExecution/outputDelta');
});

test('public reasoning preserves ordered summary parts; MCP keeps latest human-readable progress', () => {
  const folded = foldCodexActivityEvents([
    activity('item/reasoning/summaryTextDelta', 'First ', 1, { summaryIndex: 0 }),
    activity('item/reasoning/summaryPartAdded', undefined, 2, { summaryIndex: 1 }),
    activity('item/reasoning/summaryTextDelta', 'read code', 3, { summaryIndex: 0 }),
    activity('item/reasoning/summaryTextDelta', 'Then test', 4, { summaryIndex: 1 }),
    activity('item/mcpToolCall/progress', undefined, 5, { message: 'Connecting' }),
    activity('item/mcpToolCall/progress', undefined, 6, { message: 'Searching' }),
  ]);
  assert.equal(folded.length, 2);
  assert.equal(parseCodexStatus(folded[0]).text, 'First read code\n\nThen test');
  assert.equal(parseCodexStatus(folded[1]).text, 'Searching');
  const rendered = html(CodexStatusEvent, { event: folded[0] });
  assert.match(rendered, /Thinking summary/);
  assert.doesNotMatch(rendered, /<details[^>]*\bopen(?:=|>)/);
});

test('legacy output events remain readable, bound preview size and escape supplied markup', () => {
  const folded = foldCodexActivityEvents([
    event({ subtype: 'item/commandExecution/outputDelta', item_id: 'legacy', delta: 'x'.repeat(25000) + '<script>bad</script>' }, 1),
    { ...activity('item/fileChange/outputDelta', 'file output', 2), truncated: true },
    { ...event({ itemId: 'mcp', message: 'legacy progress' }, 3), text: 'item/mcpToolCall/progress' },
  ]);
  assert.equal(parseCodexStatus(folded[0]).text.length, 24000);
  assert.equal(parseCodexStatus(folded[0]).clipped, true);
  const rendered = html(CodexStatusEvent, { event: folded[0] });
  assert.match(rendered, /earlier content was omitted/);
  assert.match(rendered, /&lt;script&gt;/);
  assert.doesNotMatch(rendered, /<script>/);
  assert.match(html(CodexStatusEvent, { event: folded[1] }), /truncated/);
  assert.equal(parseCodexStatus(folded[2]).text, 'legacy progress');
});

test('recognizes raw plan payloads without subtype and retains valid steps in mixed data', () => {
  const parsed = parseCodexStatus(event({ threadId: 't', turnId: 'r', explanation: 'First inspect the code', plan: [
    { step: 'Read code', status: 'completed' }, null, { step: {}, status: 'pending' },
    { step: 'Implement', status: 'inProgress' }, { step: 'Verify', status: 'pending' }, { step: 'Future', status: 'newState' },
  ] }));
  assert.equal(parsed.type, 'plan');
  assert.equal(parsed.explanation, 'First inspect the code');
  assert.deepEqual(parsed.steps.map(step => step.status), ['completed', 'inProgress', 'pending', 'unknown']);
  assert.deepEqual(parsed.steps.map(step => step.text), ['Read code', 'Implement', 'Verify', 'Future']);
});

test('unknown events and non-status events are ignored; malformed known events degrade without JSON', () => {
  for (const payload of [undefined, null, [], 'raw JSON', 42, {}, { subtype: 'future', secret: 'do not render' }]) {
    assert.equal(parseCodexStatus(event(payload)), null);
    assert.equal(html(CodexStatusEvent, { event: event(payload) }), '');
  }
  assert.equal(parseCodexStatus(event({ plan: [] }, 1, 'assistant')), null);
  for (const payload of [{ plan: {} }, { diff: {} }, { plan: [null, { step: 42 }] }]) {
    const rendered = html(CodexStatusEvent, { event: event(payload) });
    assert.match(rendered, /unavailable/i);
    assert.doesNotMatch(rendered, /\{\}|"step"|\[object Object\]/);
  }
});

test('plan uses native collapsed disclosure with progress, readable states and escaped text', () => {
  const rendered = html(CodexStatusEvent, { event: event({ explanation: '<script>alert(1)</script>', plan: [
    { step: '<img src=x onerror=alert(1)>', status: 'completed' }, { step: 'Test', status: 'inProgress' }, { step: 'Review', status: 'newState' },
  ] }) });
  assert.match(rendered, /<details[^>]*>/);
  assert.doesNotMatch(rendered, /<details[^>]*\bopen(?:=|>)/);
  assert.match(rendered, /<summary[^>]*>.*Plan.*1\/3/s);
  assert.match(rendered, /Completed/);
  assert.match(rendered, /In progress/);
  assert.match(rendered, /Status unknown/);
  assert.match(rendered, /&lt;img/);
  assert.doesNotMatch(rendered, /<script|<img/);
  assert.match(html(CodexStatusEvent, { event: event({ plan: [] }) }), /No steps reported/);
});

test('diff is collapsible, counts only changed content lines and escapes source markup', () => {
  const diff = 'diff --git a/a.ts b/a.ts\r\n--- a/a.ts\r\n+++ b/a.ts\r\n@@ -1 +1 @@\r\n-old\r\n+<script>new</script>\r\n context\r\n';
  const parsed = parseCodexStatus(event({ diff }));
  assert.equal(parsed.type, 'diff');
  assert.equal(parsed.added, 1);
  assert.equal(parsed.removed, 1);
  const rendered = html(CodexStatusEvent, { event: event({ diff }) });
  assert.match(rendered, /<summary[^>]*>.*Changes.*\+1.*−1/s);
  assert.doesNotMatch(rendered, /<details[^>]*\bopen(?:=|>)|<script/);
  assert.match(rendered, /&lt;script&gt;new/);
  assert.match(rendered, /codex-diff-added/);
  assert.match(html(CodexStatusEvent, { event: event({ diff: '' }) }), /No changes reported/);
});

test('large diffs bound rendered lines and disclose omissions and server truncation', () => {
  const rendered = html(CodexStatusEvent, { event: { ...event({ diff: Array.from({ length: 3000 }, (_, i) => `+line${i}`).join('\n') }), truncated: true } });
  assert.ok(rendered.length < 200000);
  assert.match(rendered, /1000 more lines/);
  assert.match(rendered, /truncated/i);
  assert.match(rendered, /\+3000/);
});

test('usage selects highest sequence snapshot, filters thread and never sums cumulative updates', () => {
  const events = [event({ threadId: 'a', ...usage(30) }, 2), event({ threadId: 'b', ...usage(99) }, 9), event({ threadId: 'a', ...usage(20) }, 1)];
  assert.equal(readCodexUsage(events, 'a').total.totalTokens, 30);
  assert.equal(readCodexUsage(events, 'b').total.totalTokens, 99);
  assert.equal(readCodexUsage(events, 'missing'), undefined);
  assert.equal(readCodexUsage([]), undefined);
  assert.equal(readCodexUsage([event({ subtype: 'status' })]), undefined);
  assert.equal(readCodexUsage([event(usage(10), 1, 'assistant')]), undefined);
  assert.equal(readCodexUsage([event(usage(30), 2), event({ tokenUsage: null }, 3)]), undefined);
});

test('usage preserves reported zero and partial readings without inventing missing metrics', () => {
  const value = readCodexUsage([event({ tokenUsage: { total: { totalTokens: 0, inputTokens: -1, outputTokens: '8', cachedInputTokens: Infinity }, last: {}, modelContextWindow: 0 } })]);
  assert.deepEqual(value, { total: { totalTokens: 0 } });
  for (const tokenUsage of [null, {}, [], { total: { totalTokens: NaN } }, { total: { totalTokens: 1.5 } }]) {
    assert.equal(readCodexUsage([event({ tokenUsage })]), undefined);
    assert.equal(html(CodexSessionUsage, { events: [event({ tokenUsage })] }), '');
  }
  assert.equal(html(CodexSessionUsage, { events: [] }), '');
  const rendered = html(CodexSessionUsage, { events: [event(usage(20, { total: { totalTokens: 0 } }))] });
  assert.match(rendered, /Session tokens/);
  assert.match(rendered, />0</);
  assert.doesNotMatch(rendered, /Input tokens|Output tokens|Cost|\$/);
});

test('session usage labels total, last turn and context capacity independently', () => {
  const rendered = html(CodexSessionUsage, { events: [event(usage(20))] });
  assert.match(rendered, /Session tokens/);
  assert.match(rendered, /Latest model call tokens/);
  assert.match(rendered, /Context window/);
  assert.doesNotMatch(rendered, /%|cost|USD|tokenUsage/);
  assert.equal(html(CodexStatusEvent, { event: event(usage(20)) }), '', 'usage belongs in the session summary, not each transcript update');
});
