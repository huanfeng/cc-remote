import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

test('desktop links reach the scoped opener from anchors and window.open without navigating the app', async () => {
  const capability = JSON.parse(readFileSync(new URL('../../desktop/src-tauri/capabilities/default.json', import.meta.url), 'utf8'));
  const scopes = capability.permissions.filter(p => typeof p === 'object' && p.identifier === 'opener:allow-open-url').flatMap(p => p.allow ?? []);
  const permitted = url => scopes.some(scope => url.startsWith(scope.url.replace(/\*$/, '')));
  const listeners = new Map(), opened = [], windows = [], errors = [];
  const keys = ['window', 'document', 'location', 'isTauri', 'MutationObserver'];
  const original = Object.fromEntries(keys.map(key => [key, globalThis[key]]));
  let reject = false, confirmLink = true;
  try {
    globalThis.isTauri = true;
    globalThis.location = new URL('http://tauri.localhost/index.html?desktop-tab=tab-main-a#/s/a');
    globalThis.document = { documentElement: { classList: { add() {} } }, addEventListener(type, fn) { listeners.set(type, fn); } };
    globalThis.window = {
      fetch: async () => new Response('{}'), alert: text => errors.push(text), confirm: () => confirmLink,
      __TAURI_INTERNALS__: { metadata: { currentWindow: { label: 'main' } }, invoke: async (cmd, args) => {
        if (cmd === 'plugin:opener|open_url') {
          if (reject || !permitted(args.url)) throw new Error('URL forbidden or no default browser');
          opened.push(args.url);
        } else if (cmd === 'desktop_window_open') windows.push(args.url);
      } },
    };
    const { desktopTerminalLinks, openDesktopWindow } = await import('../src/platform/desktop.ts');
    const click = (href, options = {}) => {
      const anchor = { href, target: options.target ?? '', hasAttribute: name => name === 'download' && !!options.download,
        getAttribute: name => name === 'href' ? (options.rawHref ?? href) : null };
      const { target: _target, rawHref: _rawHref, ...eventOptions } = options;
      const event = { type: 'click', button: 0, target: { closest: () => anchor }, preventDefault() { this.defaultPrevented = true; }, ...eventOptions };
      listeners.get(event.type)(event);
      return event;
    };
    for (const options of [{}, { target: '_blank' }, { ctrlKey: true }, { metaKey: true }, { shiftKey: true }, { type: 'auxclick', button: 1 }]) {
      assert.equal(click('https://example.com/docs?q=中文#section', options).defaultPrevented, true);
    }
    click('http://127.0.0.1:8080/help');
    click('mailto:support@example.com');
    window.open('https://example.com/scripted');
    desktopTerminalLinks.open({}, 'https://example.com/terminal-autolink');
    desktopTerminalLinks.osc.activate({}, 'https://example.com/terminal-osc8');
    confirmLink = false;
    desktopTerminalLinks.osc.activate({}, 'https://example.com/cancelled-osc8');
    desktopTerminalLinks.open({}, 'file:///C:/private.txt');
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(opened.length, 11, 'scoped opener must accept all supported external links');
    assert.deepEqual(opened.slice(-3), ['https://example.com/scripted', 'https://example.com/terminal-autolink', 'https://example.com/terminal-osc8']);
    assert.deepEqual(windows, []);
    assert.equal(errors.length, 0);
    assert.equal(location.hash, '#/s/a');
    assert.equal(click('http://tauri.localhost/index.html#/s/b').defaultPrevented, undefined);
    for (const href of ['blob:http://tauri.localhost/file', 'file:///C:/private.txt', 'javascript:alert(1)', 'custom:execute']) {
      assert.equal(click(href).defaultPrevented, undefined);
    }
    assert.equal(click('https://example.com/download', { download: true }).defaultPrevented, undefined);
    assert.equal(click('https://example.com/already-handled', { defaultPrevented: true }).defaultPrevented, true);
    assert.equal(opened.length, 11);

    assert.equal(window.open(), null);
    assert.equal(window.open('   '), null);
    assert.equal(window.open('about:blank'), null);
    assert.equal(window.open('blob:http://tauri.localhost/file'), null);
    assert.equal(window.open('http://['), null);
    assert.equal(click('http://tauri.localhost/assets/style.css', { target: '_blank' }).defaultPrevented, true);
    assert.equal(click('http://tauri.localhost/index.html#heading', { ctrlKey: true, rawHref: '#heading' }).defaultPrevented, undefined);
    assert.deepEqual(windows, []);
    click('http://tauri.localhost/index.html?host=b', { ctrlKey: true });
    openDesktopWindow(new URL('http://tauri.localhost/#/s/b'));
    assert.deepEqual(windows, ['http://tauri.localhost/index.html?host=b', 'http://tauri.localhost/#/s/b']);

    // Custom-protocol origins serialize to "null" even for different hosts.
    globalThis.location = new URL('tauri://localhost/index.html?desktop-tab=tab-main-a');
    click('tauri://localhost/?host=b', { metaKey: true });
    window.open('tauri://other/?host=c');
    window.open('blob:tauri://localhost/file');
    assert.deepEqual(windows.slice(2), ['tauri://localhost/?host=b']);

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
