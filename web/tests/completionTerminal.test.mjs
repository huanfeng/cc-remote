import { test } from 'node:test';
import assert from 'node:assert/strict';
import { registerHooks } from 'node:module';

test('terminal read readiness waits for each completion redraw and ignores stale renders', async () => {
  const callbacks = [];
  class Terminal {
    options = {};
    unicode = {};
    buffer = { active: { viewportY: 1, baseY: 1 } };
    constructor() { globalThis.__completionTerminal = this; }
    loadAddon() {} registerLinkProvider() {} open() {} onData() {} onSelectionChange() {}
    attachCustomKeyEventHandler() {} focus() {} reset() {} dispose() {}
    write(_chunk, done) { callbacks.push(done); }
  }
  globalThis.__completionTerminalClass = Terminal;
  const observer = class { observe() {} disconnect() {} };
  globalThis.MutationObserver = observer;
  globalThis.ResizeObserver = observer;
  globalThis.getComputedStyle = () => ({ getPropertyValue: () => '' });
  globalThis.requestAnimationFrame = () => 1;
  globalThis.cancelAnimationFrame = () => {};
  globalThis.window = { setTimeout };
  globalThis.document = { hidden: false, documentElement: {}, addEventListener() {}, removeEventListener() {} };
  const data = source => 'data:text/javascript,' + encodeURIComponent(source);
  const mocks = {
    '@xterm/xterm': data('export const Terminal=globalThis.__completionTerminalClass;'),
    '@xterm/addon-fit': data('export class FitAddon { proposeDimensions() { return null; } }'),
    '@xterm/addon-unicode11': data('export class Unicode11Addon {}'),
    '@xterm/addon-web-links': data('export class WebLinksAddon {}'),
    '@/domain/terminalPaths': data('export const filePathLinks=()=>({});'),
    '@/domain/clipboard': data('export const copyText=async()=>{};'),
    '@/terminal/terminalTheme': data('export const mergeTerminalTheme=()=>({});'),
    '@/lib/i18n': data('export const t=en=>en;'),
  };
  const hook = registerHooks({ resolve(specifier, context, next) {
    if (mocks[specifier]) return { url: mocks[specifier], shortCircuit: true };
    if (specifier === './terminalSize') return { url: new URL('../src/features/terminal/terminalSize.ts', import.meta.url).href, shortCircuit: true };
    return next(specifier, context);
  } });
  let engine;
  try {
    const { createTerminalEngine } = await import('../src/features/terminal/terminalEngine.ts');
    let handlers, redraws = 0;
    engine = createTerminalEngine({
      backend: { openTerminal: (_id, h) => { handlers = h; return { close() {}, redraw() { redraws++; } }; } },
      sessionId: 'task', mobile: false, box: { clientWidth: 500, clientHeight: 500 }, screen: { toggleAttribute() {}, addEventListener() {}, removeEventListener() {} },
      zoom: () => 1, fitDevice: () => false, onUi() {}, openFile() {},
    });
    handlers.onState('open');
    handlers.onData('old output'); callbacks.shift()();
    assert.equal(engine.latestOutputVisible('turn-1'), false, 'prior output cannot confirm the new completion');
    assert.equal(redraws, 1);
    assert.equal(engine.latestOutputVisible('turn-1'), false);
    assert.equal(redraws, 1, 'only one redraw request per completion');
    handlers.onData('turn-1 repaint');
    assert.equal(engine.latestOutputVisible('turn-2'), false);
    callbacks.shift()();
    assert.equal(engine.latestOutputVisible('turn-2'), false, 'old completion render callback is ignored');
    handlers.onData('turn-2 repaint'); callbacks.shift()();
    assert.equal(engine.latestOutputVisible('turn-2'), true);
    globalThis.__completionTerminal.buffer.active.viewportY = 0;
    assert.equal(engine.latestOutputVisible('turn-2'), false, 'scrollback is not latest output');
    engine.reconnect(); handlers.onState('open');
    assert.equal(engine.latestOutputVisible('turn-2'), false, 'reconnect must repaint again');
  } finally {
    engine?.dispose();
    hook.deregister();
    for (const key of ['__completionTerminal', '__completionTerminalClass', 'MutationObserver', 'ResizeObserver', 'getComputedStyle', 'requestAnimationFrame', 'cancelAnimationFrame', 'window', 'document']) delete globalThis[key];
  }
});
