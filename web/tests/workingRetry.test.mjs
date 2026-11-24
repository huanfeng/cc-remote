import { test } from 'node:test';
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';
import { readFileSync } from 'node:fs';
import { transpileModule, ModuleKind, JsxEmit } from 'typescript';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

test('the working indicator renders a retry warning with the original cause and restores normal work', async () => {
  const data = text => 'data:text/javascript,' + encodeURIComponent(text);
  const mocks = {
    '@/lib/i18n': data('export const lang="zh"; export const t=(_en,zh)=>zh;'),
    '@/domain/toolGroups': data('export const isCompacting=()=>false;'),
    '@/domain/providers': data('export const providerName=()=>"Claude"; export const providerOf=()=>"claude";'),
    '@/store/app': data('export const useApp={getState:()=>({open(){}})};'),
  };
  const hook = registerHooks({
    resolve(specifier, context, next) {
      if (mocks[specifier]) return { url: mocks[specifier], shortCircuit: true };
      if (specifier === '@/domain/sessionRetry') return { url: new URL('../src/domain/sessionRetry.ts', import.meta.url).href, shortCircuit: true };
      return next(specifier, context);
    },
    load(url, context, next) {
      if (url.endsWith('/WorkingIndicator.tsx')) return { format: 'module', shortCircuit: true, source: transpileModule(readFileSync(new URL(url), 'utf8'), { compilerOptions: { module: ModuleKind.ESNext, jsx: JsxEmit.ReactJSX } }).outputText };
      return next(url, context);
    },
  });
  try {
    const { WorkingIndicator } = await import('../src/features/conversation/WorkingIndicator.tsx');
    const render = session => renderToStaticMarkup(React.createElement(WorkingIndicator, { session, events: [] }));
    const retry = { message: 'Connection dropped (ECONNRESET) · retrying… · attempt 6/10', attempt: 6, maxAttempts: 10 };
    const html = render({ status: 'running', live: { retry } });
    assert.match(html, /连接重试中 · 6\/10/);
    assert.match(html, /ECONNRESET/);
    assert.match(html, /whitespace-pre-wrap break-words/);
    assert.doesNotMatch(html, /正在工作|animated-gradient-text|animate-ping/);
    assert.match(render({ status: 'running', live: {} }), /正在工作/);
    const compacting = render({ status: 'running', live: { compacting: true } });
    assert.match(compacting, /正在压缩上下文/);
    assert.doesNotMatch(compacting, /正在工作/);
    assert.equal(render({ status: 'interrupted', live: { compacting: true } }), '');
    assert.equal(render({ status: 'completed', live: { retry } }), '');
    const background = render({ status: 'completed', activeTasks: 2 });
    assert.match(background, /2 个后台任务仍在运行/);
    assert.match(background, /查看/);
    assert.doesNotMatch(background, /正在工作|连接重试中/);
    assert.equal(render({ status: 'completed', activeTasks: 0 }), '');
  } finally { hook.deregister(); }
});
