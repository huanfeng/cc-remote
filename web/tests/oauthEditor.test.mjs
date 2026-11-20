import { test } from 'node:test';
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';
import { readFileSync } from 'node:fs';
import { transpileModule, ModuleKind, JsxEmit } from 'typescript';

test('OAuth editor preserves server errors and edits names without replacing credentials', async t => {
  let values = [], cursor = 0;
  globalThis.__oauthEditorState = initial => {
    const at = cursor++;
    if (!(at in values)) values[at] = initial;
    return [values[at], next => { values[at] = typeof next === 'function' ? next(values[at]) : next; }];
  };
  const data = source => 'data:text/javascript,' + encodeURIComponent(source);
  const mocks = {
    '@/lib/supplierOrder': 'export const useSupplierOrder=()=>({order:[]});',
    '@/domain/supplierOrder': 'export const orderSuppliers=entries=>entries;',
    '@/components/ui/button': 'export function Button() {}',
    '@/components/ui/input': 'export function Input() {}',
    '@/components/ui/spinner': 'export function Spinner() {}',
    '@/features/dialogs/SurfaceShell': 'export function SurfaceShell() {} export function GroupCard() {}',
    '@/features/dialogs/common': 'export function ConfirmDialog() {} export function CopyField() {} export function ErrorText() {} export const errorMessage=e=>e instanceof Error?e.message:String(e); export const errorStatus=e=>e?.status;',
    '@/backend/backend': 'export const useBackend=()=>globalThis.__oauthEditorBackend;',
    '@/lib/i18n': 'export const t=(_en,zh)=>zh;',
    './parts': 'export const useReportDirty=()=>{}; export function GroupTitle() {} export function SectionHeader() {}',
  };
  const hook = registerHooks({
    resolve(specifier, context, next) {
      if (/\/OAuth(?:TokenEditor|Section)\.tsx$/.test(context.parentURL ?? '') && specifier === 'react') return { url: data('export const useState=initial=>globalThis.__oauthEditorState(initial); export const useEffect=()=>{};'), shortCircuit: true };
      if (specifier === './OAuthTokenEditor') return { url: new URL('../src/features/settings/OAuthTokenEditor.tsx', import.meta.url).href, shortCircuit: true };
      if (mocks[specifier]) return { url: data(mocks[specifier]), shortCircuit: true };
      return next(specifier, context);
    },
    load(url, context, next) {
      if (/\/OAuth(?:TokenEditor|Section)\.tsx$/.test(url)) return { format: 'module', shortCircuit: true, source: transpileModule(readFileSync(new URL(url), 'utf8'), { compilerOptions: { module: ModuleKind.ESNext, jsx: JsxEmit.ReactJSX } }).outputText };
      return next(url, context);
    },
  });
  try {
    const { OAuthTokenEditor } = await import('../src/features/settings/OAuthTokenEditor.tsx');
    const nodes = element => {
      if (!element || typeof element !== 'object') return [];
      if (Array.isArray(element)) return element.flatMap(nodes);
      return [element, ...nodes(element.props?.children), ...nodes(element.props?.footer)];
    };
    const render = props => { cursor = 0; return nodes(OAuthTokenEditor({ mobile: false, ...props })); };
    const input = (tree, password = false) => tree.find(n => n.type?.name === 'Input' && (n.props.type === 'password') === password);
    const save = async tree => { tree.find(n => n.type === 'form').props.onSubmit({ preventDefault() {} }); await new Promise(resolve => setImmediate(resolve)); };

    await t.test('specific server validation is shown and the editor remains open', async () => {
      values = [];
      let closed = false;
      const detail = 'OAuth 令牌不能包含空格或换行，请检查粘贴的内容。';
      const props = { onClose: () => { closed = true; }, onSave: async () => { throw new Error(detail); } };
      let tree = render(props);
      input(tree).props.onChange({ target: { value: 'Work' } });
      tree = render(props);
      input(tree, true).props.onChange({ target: { value: 'sk-ant-oat invalid' } });
      tree = render(props);
      await save(tree);
      tree = render(props);
      assert.equal(tree.find(n => n.type?.name === 'ErrorText')?.props.children, detail);
      assert.equal(closed, false);
      assert.equal(input(tree, true).props.value, 'sk-ant-oat invalid', 'allow correcting the rejected value');
    });

    await t.test('editing a display name saves with an empty token and keeps the existing ID context', async () => {
      values = [];
      const calls = [];
      let closed = false;
      const props = { replacing: { id: 'oauth:stable-id', name: 'Old' }, onClose: () => { closed = true; }, onSave: async (...args) => { calls.push(args); } };
      let tree = render(props);
      assert.equal(input(tree).props.disabled, false, 'existing display name must be editable');
      assert.equal(input(tree, true).props.value, '', 'existing credentials are never filled in');
      input(tree).props.onChange({ target: { value: 'Renamed' } });
      tree = render(props);
      assert.equal(tree.find(n => n.props?.type === 'submit')?.props.disabled, false);
      assert.equal(tree.find(n => n.type?.name === 'SurfaceShell')?.props.title, '编辑 OAuth 账号');
      await save(tree);
      assert.deepEqual(calls, [['Renamed', '']]);
      assert.equal(closed, true);
    });

    await t.test('adding still requires a token while editing can replace one', async () => {
      values = [];
      const calls = [];
      let props = { onClose() {}, onSave: async (...args) => { calls.push(args); } };
      let tree = render(props);
      input(tree).props.onChange({ target: { value: 'New' } });
      tree = render(props);
      await save(tree);
      assert.deepEqual(calls, []);
      assert.match(render(props).find(n => n.type?.name === 'ErrorText')?.props.children, /令牌/);
      values = [];
      props = { ...props, replacing: { id: 'oauth:stable-id', name: 'Existing' } };
      tree = render(props);
      input(tree, true).props.onChange({ target: { value: '  sk-ant-oat-replacement  ' } });
      await save(render(props));
      assert.deepEqual(calls, [['Existing', 'sk-ant-oat-replacement']]);
    });
    await t.test('the account list saves edits by stable ID and omits an empty credential', async () => {
      const { OAuthSection } = await import('../src/features/settings/OAuthSection.tsx');
      const entry = { id: 'oauth:stable-id', name: 'Old' };
      values = [[entry], false, { replacing: entry }];
      cursor = 0;
      const calls = [];
      globalThis.__oauthEditorBackend = { oauthProfiles: async () => [entry], editOAuthProfile: async (...args) => { calls.push(args); } };
      const tree = nodes(OAuthSection({ mobile: false }));
      assert.ok(tree.some(n => n.type?.name === 'Button' && n.props.children === '编辑'));
      const editor = tree.find(n => n.type === OAuthTokenEditor);
      await editor.props.onSave('Renamed', '');
      await editor.props.onSave('Renamed', 'sk-ant-oat-new');
      assert.deepEqual(calls, [['oauth:stable-id', { name: 'Renamed' }], ['oauth:stable-id', { name: 'Renamed', token: 'sk-ant-oat-new' }]]);
    });
  } finally { hook.deregister(); delete globalThis.__oauthEditorState; delete globalThis.__oauthEditorBackend; }
});
