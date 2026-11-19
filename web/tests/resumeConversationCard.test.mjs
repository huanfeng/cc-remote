import { test } from 'node:test';
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';
import { readFileSync } from 'node:fs';
import { transpileModule, ModuleKind, JsxEmit } from 'typescript';

test('resume card preserves the cost warning, submits a selected action and disables settled or stale menus', async () => {
  let state = { draft: {}, busy: false, settled: false, error: '' };
  const answers = [];
  globalThis.__resumeRequest = () => ({ ...state,
    update(fn) { state.draft = fn(state.draft); },
    answer(value) { answers.push(value); state.settled = true; },
  });
  const data = source => 'data:text/javascript,' + encodeURIComponent(source);
  const translate = data('export const t=(_en,zh)=>zh;');
  const mocks = {
    '@/components/ui/button': data('export function Button() {}'),
    '@/components/lib/utils': data('export const cn=(...v)=>v.filter(Boolean).join(" ");'),
    '@/lib/i18n': translate,
    './RequestShell': data('export function RequestShell() {}'),
    './drafts': data('export const useRequest=()=>globalThis.__resumeRequest();'),
  };
  const hook = registerHooks({
    resolve(specifier, context, next) {
      if (mocks[specifier]) return { url: mocks[specifier], shortCircuit: true };
      if (specifier === '../lib/i18n.ts' && context.parentURL?.endsWith('/resumeConversation.ts')) return { url: translate, shortCircuit: true };
      if (specifier === '@/domain/resumeConversation') return { url: new URL('../src/domain/resumeConversation.ts', import.meta.url).href, shortCircuit: true };
      return next(specifier, context);
    },
    load(url, context, next) {
      if (url.endsWith('/ResumeConversationCard.tsx')) return { format: 'module', shortCircuit: true, source: transpileModule(readFileSync(new URL(url), 'utf8'), { compilerOptions: { module: ModuleKind.ESNext, jsx: JsxEmit.ReactJSX } }).outputText };
      return next(url, context);
    },
  });
  try {
    const { ResumeConversationCard } = await import('../src/features/requests/ResumeConversationCard.tsx');
    const input = { title: 'Resume this conversation?', message: 'Conversation is large. Resuming will use 142,000 tokens.', options: [
      { id: 'resume', label: 'Resume' }, { id: 'new_conversation', label: 'Start a new conversation' }, { id: 'compact', label: 'Resume from summary' },
    ] };
    const call = { id: 'r1', name: 'ResumeConversation', input, status: 'running', resultText: '' };
    const props = { call, request: { id: 'r1', status: 'pending' }, session: { id: 's1' }, mobile: false };
    const render = (overrides = {}) => ResumeConversationCard.type({ ...props, ...overrides });
    const nodes = element => !element || typeof element !== 'object' ? [] : Array.isArray(element) ? element.flatMap(nodes) : [element, ...nodes(element.props?.children)];
    let tree = render();
    assert.equal(tree.props.eyebrow, '恢复此对话？');
    assert.equal(tree.props.tone, 'pending');
    assert.equal(tree.props.actions.props.disabled, true);
    assert.ok(nodes(tree).some(node => node.type === 'p' && node.props.children === input.message));
    const radios = () => nodes(tree).filter(node => node.props?.role === 'radio');
    assert.deepEqual(radios().map(node => node.props.children.at(-1)), ['恢复对话', '新开对话', '从摘要恢复']);
    radios()[2].props.onClick(); tree = render();
    assert.equal(tree.props.actions.props.disabled, false);
    tree.props.actions.props.onClick(); tree = render();
    assert.deepEqual(answers, [{ decision: 'allow', answers: { option: 'compact' } }]);
    assert.equal(tree.props.actions, undefined);
    assert.equal(tree.props.state, '已提交');
    assert.equal(radios()[2].props['aria-checked'], true);
    assert.ok(radios().every(node => node.props.disabled));
    state = { draft: {}, busy: false, settled: false, error: 'Menu changed, please try again.' };
    tree = render(); assert.equal(tree.props.error, state.error);
    tree = render({ request: { id: 'other', status: 'pending' } });
    assert.equal(tree.props.actions, undefined); assert.ok(radios().every(node => node.props.disabled));
    tree = render({ call: { ...call, status: 'ok', resultText: JSON.stringify({ outcome: 'terminal', option: 'new_conversation' }) }, request: undefined });
    assert.equal(tree.props.state, '终端已处理'); assert.equal(radios()[1].props['aria-checked'], true);
    tree = render({ call: { ...call, resultText: '{"outcome":"expired"}' } });
    assert.equal(tree.props.state, '已过期'); assert.equal(tree.props.tone, 'declined');
    assert.equal(tree.props.actions, undefined); assert.ok(radios().every(node => node.props.disabled));
  } finally { hook.deregister(); delete globalThis.__resumeRequest; }
});
