import { test } from 'node:test';
import assert from 'node:assert/strict';
import { isLoopbackHost, isAbsoluteLocalPath } from '../src/domain/localPaths.ts';

test('local host actions accept only HTTP loopback origins', () => {
  for (const value of ['http://localhost:8787', 'https://127.0.0.1', 'http://[::1]', 'http://127.3.2.1']) assert.equal(isLoopbackHost(value), true, value);
  for (const value of ['', 'file:///tmp', 'https://localhost.example', 'http://192.168.1.2', 'http://0.0.0.0', 'http://user@localhost', 'http://localhost\n']) assert.equal(isLoopbackHost(value), false, value);
});

test('absolute local paths preserve Unicode and shell metacharacters while rejecting network and device forms', () => {
  for (const value of ['/tmp/空 格 & \'$.txt', 'C:\\空 格 & \'$.txt', 'D:/project']) assert.equal(isAbsoluteLocalPath(value), true, value);
  for (const value of ['', './project', 'C:relative', '//server/share', '\\\\server\\share', '\\\\?\\C:\\folder', '\\\\.\\pipe\\test', 'C:\\file:stream', '/tmp/bad\nname']) assert.equal(isAbsoluteLocalPath(value), false, value);
});

test('native path actions require desktop and forward exact structured arguments', async () => {
  const { canUseLocalPaths, openLocalPath } = await import('../src/platform/pathActions.ts');
  const originalWindow = Object.getOwnPropertyDescriptor(globalThis, 'window');
  const originalTauri = Object.getOwnPropertyDescriptor(globalThis, 'isTauri');
  const calls = [];
  try {
    Object.defineProperty(globalThis, 'window', { configurable: true, value: { __TAURI_INTERNALS__: { async invoke(...args) { calls.push(args); } } } });
    Object.defineProperty(globalThis, 'isTauri', { configurable: true, value: false });
    assert.equal(canUseLocalPaths('http://localhost:8787'), false);
    await assert.rejects(openLocalPath('http://localhost:8787', '/tmp/project', 'open'));
    Object.defineProperty(globalThis, 'isTauri', { configurable: true, value: true });
    await assert.rejects(openLocalPath('http://192.168.1.2:8787', '/tmp/project', 'open'));
    await assert.rejects(openLocalPath('http://localhost:8787', './relative', 'open'));
    const path = "C:/空 格 & '$folder";
    await openLocalPath('http://localhost:8787', path, 'terminal');
    assert.deepEqual(calls, [['open_local_path', { hostUrl: 'http://localhost:8787', path, action: 'terminal' }, undefined]]);
  } finally {
    if (originalWindow) Object.defineProperty(globalThis, 'window', originalWindow); else delete globalThis.window;
    if (originalTauri) Object.defineProperty(globalThis, 'isTauri', originalTauri); else delete globalThis.isTauri;
  }
});
