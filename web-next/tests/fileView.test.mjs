import { test } from 'node:test';
import assert from 'node:assert/strict';
import { dirOf, formatBytes, looksBinary, shikiLang, splitLine, svgDataUrl, viewKind } from '../src/features/files/fileView.ts';

test('a :line suffix is split off the path', () => {
  assert.deepEqual(splitLine('src/a.ts:12'), { path: 'src/a.ts', line: 12 });
  assert.deepEqual(splitLine('src/a.ts:12:4'), { path: 'src/a.ts', line: 12 });
  assert.deepEqual(splitLine('src/a.ts#L7-L9'), { path: 'src/a.ts', line: 7 });
  assert.deepEqual(splitLine('C:\\app\\main.go:3'), { path: 'C:\\app\\main.go', line: 3 });
  assert.deepEqual(splitLine('src/a.ts'), { path: 'src/a.ts' });
  assert.deepEqual(splitLine('C:1'), { path: 'C:1' });
  assert.deepEqual(splitLine('a.ts:0'), { path: 'a.ts' });
});

test('files are shown by kind', () => {
  assert.equal(viewKind({ path: 'a.png', mime: 'image/png', url: 'blob:x' }), 'image');
  assert.equal(viewKind({ path: 'a.svg', mime: 'image/svg+xml', text: '<svg/>' }), 'image');
  assert.equal(viewKind({ path: 'a.png', mime: 'image/png' }), 'binary');
  assert.equal(viewKind({ path: 'a.pdf', mime: 'application/pdf', url: 'blob:x' }), 'binary');
  assert.equal(viewKind({ path: 'README.md', mime: 'text/markdown', text: '# hi' }), 'markdown');
  assert.equal(viewKind({ path: 'main.go', mime: 'text/plain', text: 'package main' }), 'code');
  assert.equal(viewKind({ path: 'a.bin', mime: 'application/octet-stream', text: 'ab\0cd' }), 'binary');
  assert.equal(viewKind({ path: 'big.log', mime: 'text/plain', tooLarge: true }), 'too-large');
});

test('binary detection looks at the start only', () => {
  assert.equal(looksBinary('abc'), false);
  assert.equal(looksBinary('a\0'), true);
  assert.equal(looksBinary('a'.repeat(9000) + '\0'), false);
});

test('svg becomes an inert data: URL', () => {
  assert.ok(svgDataUrl('<svg onload="x()"/>').startsWith('data:image/svg+xml;charset=utf-8,%3Csvg'));
});

test('shiki languages', () => {
  assert.equal(shikiLang('a/b.tsx'), 'tsx');
  assert.equal(shikiLang('index.html'), 'html');
  assert.equal(shikiLang('main.go'), 'go');
  assert.equal(shikiLang('Dockerfile'), 'docker');
  assert.equal(shikiLang('notes.txt'), '');
});

test('directory and size labels', () => {
  assert.equal(dirOf('web/src/App.tsx'), 'web/src/');
  assert.equal(dirOf('App.tsx'), '');
  assert.equal(formatBytes(512), '512 B');
  assert.equal(formatBytes(2048), '2.0 KiB');
  assert.equal(formatBytes(3 * 1024 * 1024), '3.0 MiB');
});
