import { test } from 'node:test';
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';

test('page attention changes title and favicon, survives identity updates, and clears without notification permission', async () => {
  const previous = globalThis.document;
  const icon = { href: '' };
  globalThis.document = { title: '', documentElement: { dataset: {}, style: { setProperty() {} } }, querySelector: () => icon };
  const data = text => 'data:text/javascript,' + encodeURIComponent(text);
  const mocks = {
    './hosts': data('export const currentHost=undefined; export const updateCurrentHost=()=>{};'),
    './hostIcon': data('export const iconSvg=(color,icon)=>`<svg><path data-color="${color}" data-icon="${icon}"/></svg>`;'),
    './i18n': data('export const lang="zh";'),
    '@/domain/attention': new URL('../src/domain/attention.ts', import.meta.url).href,
  };
  const hook = registerHooks({ resolve(specifier, context, next) {
    if (context.parentURL?.endsWith('/identity.ts') && mocks[specifier]) return { url: mocks[specifier], shortCircuit: true };
    return next(specifier, context);
  } });
  try {
    const { applyIdentity, setTitleAttention } = await import('../src/lib/identity.ts');
    applyIdentity({ name: 'Work', color: '#293c2e', icon: 'logo' });
    const original = icon.href;
    setTitleAttention({ waiting: 2, done: 1, total: 3 });
    assert.equal(document.title, '(2 待处理 · 1 待查看) Work · CC Remote');
    assert.match(decodeURIComponent(icon.href), /#d97706/);
    applyIdentity({ name: 'Renamed', color: '#334455', icon: 'server' });
    assert.match(document.title, /待查看.*Renamed/);
    assert.match(decodeURIComponent(icon.href), /data-icon="server"/);
    setTitleAttention({ waiting: 0, done: 1, total: 1 });
    assert.equal(document.title, '(1 待查看) Renamed · CC Remote');
    assert.match(decodeURIComponent(icon.href), /#16a34a/);
    setTitleAttention({ waiting: 0, done: 0, total: 0 });
    assert.equal(document.title, 'Renamed · CC Remote');
    assert.doesNotMatch(decodeURIComponent(icon.href), /<circle/);
    applyIdentity({ name: 'Work', color: '#293c2e', icon: 'logo' });
    assert.equal(icon.href, original);
  } finally {
    hook.deregister();
    if (previous === undefined) delete globalThis.document; else globalThis.document = previous;
  }
});
