import { test } from 'node:test';
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';
import { readFileSync } from 'node:fs';
import { transpileModule, ModuleKind, JsxEmit } from 'typescript';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

test('desktop and mobile task lists render active work before historical categories without duplicates', async () => {
  const data = source => 'data:text/javascript,' + encodeURIComponent(source);
  const mocks = {
    '@/components/ui/dialog': 'export const Dialog=()=>null;',
    '@/components/ui/button': 'export const Button=()=>null;',
    '@/components/ui/spinner': 'export const Spinner=()=>null;',
    '@/components/mobile-ui/bottom-sheet': 'export const BottomSheet=()=>null, BottomSheetContent=()=>null, BottomSheetTitle=()=>null;',
    '@/components/lib/utils': 'export const cn=(...v)=>v.filter(Boolean).join(" ");',
    '@/backend/backend': 'export const useBackend=()=>null;',
    '@/store/app': 'export const useApp=()=>null;',
    '@/lib/hooks': 'export const useNow=()=>{};',
    '@/lib/i18n': 'export const t=(_en,zh)=>zh;',
    '@/features/dialogs/common': 'export const ErrorText=()=>null, errorMessage=String;',
    '@/features/dialogs/SurfaceShell': 'export const TOUCH="";',
  };
  const hook = registerHooks({
    resolve(specifier, context, next) {
      if (mocks[specifier]) return { url: data(mocks[specifier]), shortCircuit: true };
      if (specifier === '@/domain/taskGroups') return { url: new URL('../src/domain/taskGroups.ts', import.meta.url).href, shortCircuit: true };
      if (specifier === './usageMath') return { url: new URL('../src/features/status/usageMath.ts', import.meta.url).href, shortCircuit: true };
      return next(specifier, context);
    },
    load(url, context, next) {
      if (url.endsWith('/TaskPanel.tsx')) return { format: 'module', shortCircuit: true, source: transpileModule(readFileSync(new URL(url), 'utf8'), { compilerOptions: { module: ModuleKind.ESNext, jsx: JsxEmit.ReactJSX } }).outputText };
      return next(url, context);
    },
  });
  try {
    const { TaskList } = await import('../src/features/status/TaskPanel.tsx');
    const task = (id, taskType, status) => ({ id, description: id, taskType, status, toolUseId: id, startedAt: '2026-11-19T00:00:00Z' });
    const tasks = [task('historical-agent', 'local_agent', 'completed'), task('active-shell', 'local_bash', 'running'), task('active-agent', 'local_agent', 'running')];
    for (const mobile of [false, true]) {
      const render = list => renderToStaticMarkup(React.createElement(TaskList, { tasks: list, error: '', onPick() {}, mobile }));
      const html = render(tasks);
      assert.match(html, /运行中/);
      assert.ok(html.indexOf('active-shell') < html.indexOf('historical-agent'));
      assert.ok(html.indexOf('active-agent') < html.indexOf('historical-agent'));
      assert.equal(html.match(/>active-shell</g)?.length, 1);
      assert.match(html, /后台命令/);
      const finished = render(tasks.map(task => ({ ...task, status: 'completed' })));
      assert.doesNotMatch(finished, /运行中/);
      assert.ok(finished.indexOf('historical-agent') < finished.indexOf('active-shell'));
    }
  } finally { hook.deregister(); }
});
