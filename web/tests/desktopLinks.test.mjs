import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

test('desktop links reach the scoped opener from anchors and window.open without navigating the app', async () => {
  const capability = JSON.parse(readFileSync(new URL('../../desktop/src-tauri/capabilities/default.json', import.meta.url), 'utf8'));
  const scopes = capability.permissions.filter(p => typeof p === 'object' && p.identifier === 'opener:allow-open-url').flatMap(p => p.allow ?? []);
  const permitted = url => scopes.some(scope => url.startsWith(scope.url.replace(/\*$/, '')));
  const listeners = new Map(), opened = [], errors = [];
  const keys = ['window', 'document', 'location', 'isTauri', 'MutationObserver'];
  const original = Object.fromEntries(keys.map(key => [key, globalThis[key]]));
  let reject = false;
  try {
    globalThis.isTauri = true;
    globalThis.location = new URL('http://tauri.localhost/index.html?desktop-tab=tab-main-a#/s/a');
    globalThis.document = { documentElement: { classList: { add() {} } }, addEventListener(type, fn) { listeners.set(type, fn); } };
    globalThis.window = {
      fetch: async () => new Response('{}'), alert: text => errors.push(text),
      __TAURI_INTERNALS__: { metadata: { currentWindow: { label: 'main' } }, invoke: async (cmd, args) => {
        if (cmd === 'plugin:opener|open_url') {
          if (reject || !permitted(args.url)) throw new Error('URL forbidden or no default browser');
          opened.push(args.url);
        }
      } },
    };
    await import('../src/platform/desktop.ts');
    const click = (href, options = {}) => {
      const anchor = { href, target: options.target ?? '', hasAttribute: name => name === 'download' && !!options.download };
      const { target: _target, ...eventOptions } = options;
      const event = { type: 'click', button: 0, target: { closest: () => anchor }, preventDefault() { this.defaultPrevented = true; }, ...eventOptions };
      listeners.get(event.type)(event);
      return event;
    };
    for (const options of [{}, { target: '_blank' }, { ctrlKey: true }, { metaKey: true }, { shiftKey: true }, { type: 'auxclick', button: 1 }]) {
      assert.equal(click('https://example.com/docs?q=中文#section', options).defaultPrevented, true);
    }
    click('http://127.0.0.1:8080/help');
    click('mailto:support@example.com');
    window.open('https://example.com/from-terminal');
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(opened.length, 9, 'scoped opener must accept all supported external links');
    assert.equal(errors.length, 0);
    assert.equal(location.hash, '#/s/a');
    assert.equal(click('http://tauri.localhost/index.html#/s/b').defaultPrevented, undefined);
    for (const href of ['blob:http://tauri.localhost/file', 'file:///C:/private.txt', 'javascript:alert(1)', 'custom:execute']) {
      assert.equal(click(href).defaultPrevented, undefined);
    }
    assert.equal(click('https://example.com/download', { download: true }).defaultPrevented, undefined);
    assert.equal(click('https://example.com/already-handled', { defaultPrevented: true }).defaultPrevented, true);
    assert.equal(opened.length, 9);
    reject = true;
    click('https://example.com/unavailable');
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(errors.length, 1, 'opener failures need visible feedback instead of disappearing');
    assert.match(errors[0], /browser|浏览器/);
  } finally {
    for (const [key, value] of Object.entries(original)) {
      if (value === undefined) delete globalThis[key]; else globalThis[key] = value;
    }
  }
});
