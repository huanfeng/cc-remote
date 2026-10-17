import { test } from 'node:test';
import assert from 'node:assert/strict';
import { argSummary, groupStats, groupTranscript, parseAnswers, questionViews, stabilize, summaryLabel } from '../src/toolGroups.ts';

let seq = 0;
const at = s => new Date(Date.UTC(2026, 0, 1, 0, 0, s)).toISOString();
const msg = (kind, text = 'x') => ({ seq: ++seq, kind, text, createdAt: at(0) });
const use = (id, name, input = {}, s = 0) => ({ seq: ++seq, kind: 'tool', payload: { type: 'tool_use', id, name, input }, createdAt: at(s) });
const res = (id, content = 'ok', isError, s = 0) => ({ seq: ++seq, kind: 'tool_result', text: typeof content === 'string' ? content : JSON.stringify(content), payload: { type: 'tool_result', tool_use_id: id, content, ...(isError === undefined ? {} : { is_error: isError }) }, createdAt: at(s) });
const shape = items => items.map(item => item.type === 'event' ? item.event.kind : item.type === 'tools' ? `tools:${item.calls.length}` : `special:${item.call.name}`);

test('consecutive tool events between messages fold into one group per run', () => {
  const items = groupTranscript([
    msg('user'), msg('assistant'),
    use('a', 'Read', { file_path: '/p/a.ts' }), res('a'), use('b', 'Bash', { command: 'ls' }), res('b'),
    msg('assistant'), use('c', 'Grep', { pattern: 'x' }), res('c'),
    msg('error'), use('d', 'Edit', { file_path: 'b.ts' }), res('d'), msg('user'),
  ]);
  assert.deepEqual(shape(items), ['user', 'assistant', 'tools:2', 'assistant', 'tools:1', 'error', 'tools:1', 'user']);
  assert.equal(items[2].key, items[2].calls[0].seq, 'group key is the first event seq');
});

test('results pair with their tool_use by id, even interleaved for parallel calls', () => {
  const [group] = groupTranscript([use('a', 'Read', {}, 0), use('b', 'Read', {}, 0), res('b', 'second', false, 1), res('a', 'first', false, 5)]);
  assert.equal(group.calls.length, 2);
  assert.equal(group.calls[0].resultText, 'first');
  assert.equal(group.calls[1].resultText, 'second');
  assert.equal(group.calls[0].durationMs, 5000);
  assert.equal(group.calls[1].durationMs, 1000);
});

test('block-array results are reduced to text and orphan results still show up', () => {
  const [group] = groupTranscript([use('a', 'Read', { file_path: 'x' }), res('a', [{ type: 'text', text: 'launched' }]), res('zz', 'lost')]);
  assert.equal(group.calls[0].resultText, 'launched');
  assert.equal(group.calls[1].name, 'Tool result');
  assert.equal(group.calls[1].resultText, 'lost');
});

test('calls without a result are running only in the last group of a live session', () => {
  const events = [use('a', 'Bash', { command: 'sleep 1' }), msg('assistant'), use('b', 'Bash', { command: 'npm test\nmore' }), use('c', 'Read', {}), res('c')];
  const live = groupTranscript(events, true);
  assert.equal(live[0].calls[0].status, 'missing');
  assert.equal(live[2].calls[0].status, 'running');
  assert.equal(live[2].calls[1].status, 'ok');
  const stats = groupStats(live[2].calls);
  assert.equal(stats.running, 1);
  assert.equal(stats.current.name, 'Bash');
  assert.equal(argSummary(stats.current.name, stats.current.input).text, 'npm test');
  assert.equal(groupTranscript(events, false)[2].calls[0].status, 'missing');
});

test('failed results are counted', () => {
  const [group] = groupTranscript([use('a', 'Bash'), res('a', 'Exit code 1', true), use('b', 'Bash'), res('b', 'fine', false), use('c', 'Read'), res('c', 'boom', true)]);
  assert.deepEqual(group.calls.map(call => call.status), ['error', 'ok', 'error']);
  assert.equal(groupStats(group.calls).failed, 2);
});

test('AskUserQuestion and ExitPlanMode break groups and carry their answer', () => {
  const question = { questions: [
    { question: '是否修复？', header: '下一步', multiSelect: false, options: [{ label: '全部 (Recommended)', description: 'd1' }, { label: '先不改' }] },
    { question: 'Which "files"?', multiSelect: true, options: [{ label: 'a' }, { label: 'b' }, { label: 'c' }] },
    { question: '备注', options: [{ label: 'x' }] },
  ] };
  const answer = 'Your questions have been answered: "是否修复？"="全部 (Recommended)", "Which \\"files\\"?"="a, c", "备注"="自定义回答". You can now continue with these answers in mind.';
  const items = groupTranscript([use('a', 'Read'), res('a'), use('q', 'AskUserQuestion', question), res('q', answer), use('b', 'Bash'), res('b'), use('p', 'ExitPlanMode', { plan: '# 计划' })], true);
  assert.deepEqual(shape(items), ['tools:1', 'special:AskUserQuestion', 'tools:1', 'special:ExitPlanMode']);
  assert.equal(items[3].call.status, 'running');
  const views = questionViews(items[1].call);
  assert.deepEqual(views[0].options.map(option => option.selected), [true, false]);
  assert.equal(views[0].header, '下一步');
  assert.deepEqual(views[1].options.map(option => option.selected), [true, false, true]);
  assert.equal(views[2].custom, '自定义回答');
  assert.deepEqual(parseAnswers('nothing here'), {});
  const unanswered = questionViews(groupTranscript([use('q', 'AskUserQuestion', question)])[0].call);
  assert.ok(unanswered.every(view => view.answer === undefined && view.options.every(option => !option.selected)));
});

test('summary label lists counts by tool, single calls show their argument', () => {
  const [group] = groupTranscript([
    ...['a', 'b', 'c', 'd', 'e'].map(id => use(id, 'Read', { file_path: `/x/${id}.ts` })),
    ...['f', 'g', 'h', 'i'].map(id => use(id, 'Bash', { command: 'ls' })),
    ...['j', 'k', 'l'].map(id => use(id, 'Edit', { file_path: 'y.ts' })),
  ]);
  assert.equal(summaryLabel(group.calls), '12 tool calls · Read ×5 · Bash ×4 · Edit ×3');
  const [single] = groupTranscript([use('a', 'Read', { file_path: 'D:\\src\\app.ts' })]);
  assert.equal(summaryLabel(single.calls), '1 tool call · Read app.ts');
  const [mixed] = groupTranscript([use('a', 'Read'), use('b', 'Bash'), use('c', 'Bash'), use('d', 'Grep'), use('e', 'Glob')]);
  assert.equal(summaryLabel(mixed.calls), '5 tool calls · Bash ×2 · Read · Grep · 1 more');
});

test('argument summaries per tool', () => {
  assert.deepEqual(argSummary('Read', { file_path: 'D:\\a\\b\\c.kt' }), { text: 'c.kt', title: 'D:\\a\\b\\c.kt', dir: 'D:\\a\\b' });
  assert.equal(argSummary('Bash', { command: 'git status\ngit diff' }).text, 'git status');
  assert.equal(argSummary('Grep', { pattern: 'fun x', path: 'src' }).text, 'fun x');
  assert.equal(argSummary('WebFetch', { url: 'https://e.com', prompt: 'p' }).text, 'https://e.com');
  assert.equal(argSummary('Agent', { description: 'Review diff', prompt: 'long' }).text, 'Review diff');
  assert.equal(argSummary('TodoWrite', { todos: [{}, {}, {}] }).text, '3 items');
  assert.equal(argSummary('Custom', { n: 1, name: 'first\nsecond' }).text, 'first');
});

test('question card and tool group render answered and collapsed states', async () => {
  const { build } = await import('esbuild');
  const { mkdirSync } = await import('node:fs');
  const { dirname, join } = await import('node:path');
  const { fileURLToPath, pathToFileURL } = await import('node:url');
  const web = join(dirname(fileURLToPath(import.meta.url)), '..');
  const outdir = join(web, 'node_modules', '.cache', 'tools-test');
  mkdirSync(outdir, { recursive: true });
  const outfile = join(outdir, 'entry.mjs');
  await build({ stdin: { contents: "export { QuestionCard } from './src/QuestionCard.tsx'; export { ToolGroup } from './src/ToolGroup.tsx';", resolveDir: web, loader: 'ts' }, outfile, bundle: true, format: 'esm', platform: 'node', jsx: 'automatic', packages: 'external', loader: { '.css': 'empty' }, logLevel: 'silent' });
  const { QuestionCard, ToolGroup } = await import(pathToFileURL(outfile).href);
  const { createElement } = await import('react');
  const { renderToStaticMarkup } = await import('react-dom/server');
  const question = { questions: [{ question: 'Q?', options: [{ label: 'A', description: 'da' }, { label: 'B' }] }] };
  const items = groupTranscript([use('q', 'AskUserQuestion', question), res('q', 'Your questions have been answered: "Q?"="B". ok'), use('a', 'Bash', { command: 'npm test' }), res('a', 'fail', true), use('b', 'Read', { file_path: '/r/x.ts' })], true);
  const card = renderToStaticMarkup(createElement(QuestionCard, { call: items[0].call, waitingHint: '' }));
  assert.match(card, /Answered/);
  assert.match(card, /<li class="selected"><div>.*<strong>B<\/strong>/);
  const waiting = renderToStaticMarkup(createElement(QuestionCard, { call: groupTranscript([use('w', 'AskUserQuestion', question)], true)[0].call, waitingHint: 'Waiting for an answer (answer in the terminal)' }));
  assert.match(waiting, /Waiting for an answer \(answer in the terminal\)/);
  const props = { groupKey: items[1].key, calls: items[1].calls, onToggle() {}, openCalls: [], onToggleCall() {} };
  const collapsed = renderToStaticMarkup(createElement(ToolGroup, { ...props, open: false }));
  assert.match(collapsed, /2 tool calls · Bash · Read/);
  assert.match(collapsed, /1 failed/);
  assert.match(collapsed, /Running: <strong>Read<\/strong>/);
  assert.ok(!collapsed.includes('tool-calls'), 'collapsed group renders no call rows');
  const open = renderToStaticMarkup(createElement(ToolGroup, { ...props, open: true, openCalls: [items[1].calls[0].seq] }));
  assert.equal((open.match(/class="tool-call /g) || []).length, 2);
  assert.match(open, /<pre>fail<\/pre>/);
});
test('stabilize keeps unchanged calls and groups, replaces changed ones', () => {
  const events = [use('a', 'Read', {}), res('a'), msg('assistant'), use('b', 'Bash', { command: 'ls' }), use('q', 'AskUserQuestion', { questions: [] })];
  const first = groupTranscript(events, true);
  // A poll appends a result; refetched events are new objects with the same seqs.
  const done = res('b');
  const next = stabilize(first, groupTranscript([...structuredClone(events), done], true));
  assert.equal(next[0], first[0], 'untouched group keeps its identity');
  assert.equal(next[0].calls[0], first[0].calls[0]);
  assert.notEqual(next[2], first[2], 'group with a new result changes');
  assert.equal(next[2].calls[0].status, 'ok');
  assert.equal(next[3], first[3], 'unchanged special card keeps its identity');
  assert.equal(stabilize(next, groupTranscript([...events, done], true))[2], next[2]);
});

test('compact_boundary renders a divider with trigger and token sizes', async () => {
  const { compactSummary } = await import('../src/toolGroups.ts');
  assert.equal(compactSummary({ compact_metadata: { trigger: 'manual', pre_tokens: 48026, post_tokens: 8969 } }), 'Context compacted (manual) · 48.0k → 9.0k tokens');
  assert.equal(compactSummary({ compact_metadata: { trigger: 'auto', pre_tokens: 900, post_tokens: 300 } }), 'Context compacted (auto) · 900 → 300 tokens');
  assert.equal(compactSummary(undefined), 'Context compacted');
});

test('isCompacting follows the latest system status until the boundary arrives', async () => {
  const { isCompacting } = await import('../src/toolGroups.ts');
  const status = value => ({ seq: ++seq, kind: 'status', text: 'status', payload: { type: 'system', subtype: 'status', status: value }, createdAt: at(0) });
  const hook = { seq: ++seq, kind: 'status', text: 'hook_started', payload: { type: 'system', subtype: 'hook_started' }, createdAt: at(0) };
  assert.equal(isCompacting([msg('user'), status('compacting'), hook]), true);
  assert.equal(isCompacting([msg('user'), status('compacting'), status(null)]), false);
  assert.equal(isCompacting([status('compacting'), { seq: ++seq, kind: 'compact', createdAt: at(0) }]), false);
  assert.equal(isCompacting([msg('user')]), false);
});

test('calls list the seqs of events the host truncated, and change when they load whole', () => {
  const tool = { seq: 1, kind: 'tool', createdAt: 't', truncated: true, payload: { id: 'a', name: 'Write', input: { file_path: 'x', content: 'short…' } } };
  const result = { seq: 2, kind: 'tool_result', createdAt: 't', truncated: true, payload: { tool_use_id: 'a', content: 'cut…' } };
  const [group] = groupTranscript([tool, result]);
  assert.deepEqual(group.calls[0].truncated, [1, 2]);
  const whole = groupTranscript([{ ...tool, truncated: false, payload: { ...tool.payload, input: { file_path: 'x', content: 'full' } } }, { ...result, truncated: false, payload: { tool_use_id: 'a', content: 'full' } }]);
  assert.equal(whole[0].calls[0].truncated, undefined);
  const next = stabilize([group], whole);
  assert.notEqual(next[0], group, 'loading the full events must re-render the group');
  const [orphan] = groupTranscript([{ ...result, payload: { tool_use_id: 'zz', content: 'cut…' } }]);
  assert.deepEqual(orphan.calls[0].truncated, [2]);
});

const child = (event, parent) => ({ ...event, payload: { ...(event.payload || {}), parent_tool_use_id: parent } });

test('subagent events nest under their Agent call card, outside the main tool groups', () => {
  const events = [use('ag', 'Agent', { description: 'Count files', subagent_type: 'general-purpose', prompt: 'count' }), child(msg('assistant', 'looking'), 'ag'), child(use('g1', 'Glob', { pattern: '*' }), 'ag'), child(res('g1', 'a.txt'), 'ag'), res('ag', [{ type: 'text', text: '2' }]), msg('assistant', 'done')];
  const items = groupTranscript(events, false);
  assert.deepEqual(items.map(item => item.type), ['agent', 'event']);
  const [agent] = items;
  assert.equal(agent.call.status, 'ok');
  assert.equal(agent.call.resultText, '2');
  assert.deepEqual(agent.items.map(item => item.type === 'tools' ? `tools:${item.calls[0].name}:${item.calls[0].status}` : item.event.text), ['looking', 'tools:Glob:ok']);
});

test('an agent call is running until its result, or while its background task runs', () => {
  const pending = groupTranscript([use('a1', 'Agent', { description: 'x' }), child(use('r1', 'Read', {}), 'a1')], true);
  assert.equal(pending[0].call.status, 'running');
  assert.equal(pending[0].items[0].calls[0].status, 'running', 'its unfinished calls run too');
  const launched = [use('b1', 'Agent', { description: 'bg', run_in_background: true }), res('b1', 'launched'), child(use('r2', 'Read', {}), 'b1')];
  assert.equal(groupTranscript(launched, false, id => id === 'b1')[0].items[0].calls[0].status, 'running');
  assert.equal(groupTranscript(launched, false, () => false)[0].items[0].calls[0].status, 'missing');
});

test('stabilize keeps an unchanged agent card and replaces it when its subagent moves on', () => {
  const base = [use('s1', 'Agent', { description: 'x' }), child(use('t1', 'Bash', { command: 'ls' }), 's1')];
  const first = groupTranscript(base, true);
  assert.equal(stabilize(first, groupTranscript(structuredClone(base), true))[0], first[0]);
  const next = stabilize(first, groupTranscript([...base, child(res('t1', 'ok'), 's1')], true));
  assert.notEqual(next[0], first[0]);
  assert.equal(next[0].items[0].calls[0].status, 'ok');
});

test('a subagent whose Agent call is on an older page still shows, as a placeholder card', () => {
  const items = groupTranscript([msg('user', 'go'), child(use('k1', 'Read', {}), 'older'), child(res('k1', 'ok'), 'older')], true, id => id === 'older');
  const agent = items.find(item => item.type === 'agent');
  assert.ok(agent, 'placeholder card');
  assert.equal(agent.call.id, 'older');
  assert.ok(agent.key < 0, 'its key stays apart from the first child event');
  assert.equal(agent.call.status, 'running');
  assert.equal(agent.items[0].calls[0].status, 'ok');
});

test('only agent calls after the last message run in a live session', () => {
  const items = groupTranscript([use('old', 'Agent', { description: 'lost' }), msg('assistant', 'later'), use('new', 'Agent', { description: 'now' })], true);
  const agents = items.filter(item => item.type === 'agent');
  assert.deepEqual(agents.map(item => item.call.status), ['missing', 'running']);
});

test('an agent whose id shows up in its own events nests once instead of looping', () => {
  const items = groupTranscript([use('loop', 'Agent', { description: 'x' }), child(use('loop', 'Agent', { description: 'again' }), 'loop')], false);
  assert.equal(items.length, 1);
  assert.equal(items[0].items.length, 1);
  assert.equal(items[0].items[0].items.length, 0);
});
