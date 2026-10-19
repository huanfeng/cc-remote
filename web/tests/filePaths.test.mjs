import { test } from 'node:test';
import assert from 'node:assert/strict';
import { absoluteDirs, dirname, fileKind, headingSlug, languageOf, parseFileHref, pathInCode, relativeTarget, remarkFilePaths, splitPaths } from '../src/filePaths.ts';

const paths = text => splitPaths(text).filter(item => typeof item !== 'string').map(item => item.line ? `${item.path}@${item.line}` : item.path);

test('paths in prose, with line numbers and next to Chinese text', () => {
  assert.deepEqual(paths('只有一个文件 docs/design/plugin-hook-channel.md（184 行），没有推送'), ['docs/design/plugin-hook-channel.md']);
  assert.deepEqual(paths('see cmd/cc-remote/listeners.go:243-264 and internal\\server\\files.go:12:5.'), ['cmd/cc-remote/listeners.go@243', 'internal\\server\\files.go@12']);
  assert.deepEqual(paths('在docs/a.md里和/home/me/app/main.go，还有 C:\\work\\shot.png'), ['docs/a.md', '/home/me/app/main.go', 'C:\\work\\shot.png']);
  assert.deepEqual(paths('打开 README.md 和 ./x/y.ts#L4'), ['README.md', './x/y.ts@4']);
});

test('things that are not files stay text', () => {
  assert.deepEqual(paths('Node.js, example.com, v0.10.0, e.g. and/or 1.5/2.0 and https://x.com/a/b.md'), []);
  const segments = splitPaths('a docs/x.md b');
  assert.equal(segments.map(item => typeof item === 'string' ? item : item.text).join(''), 'a docs/x.md b');
});

test('inline code holding a path', () => {
  assert.deepEqual(pathInCode('docs/设计/总览.md:3'), { path: 'docs/设计/总览.md', line: 3 });
  assert.deepEqual(pathInCode('main.go'), undefined);
  assert.deepEqual(pathInCode('web/src/App.tsx'), { path: 'web/src/App.tsx', line: undefined });
  assert.equal(pathInCode('git log -1'), undefined);
  assert.equal(pathInCode('a.b()'), undefined);
});

test('absolute directories a message names', () => {
  assert.deepEqual(absoluteDirs('worktree 在 /home/dufeng/develop/windinput/wt-plugin/WindInput\n提交：3a4a32c3'), ['/home/dufeng/develop/windinput/wt-plugin/WindInput']);
  assert.deepEqual(absoluteDirs('see https://example.com/a/b'), []);
  assert.deepEqual(absoluteDirs('D:\\Develop\\app\\ and /srv/x.'), ['D:\\Develop\\app', '/srv/x']);
});

test('relative link targets in a document', () => {
  assert.deepEqual(relativeTarget('../img/a%20b.png'), { path: '../img/a b.png', line: undefined });
  assert.deepEqual(relativeTarget('other.md#L7'), { path: 'other.md', line: 7 });
  assert.equal(relativeTarget('https://x.com/a.md'), undefined);
  assert.equal(relativeTarget('#section'), undefined);
  assert.equal(relativeTarget('mailto:a@b.c'), undefined);
  assert.deepEqual(relativeTarget('C:/a/b.md'), { path: 'C:/a/b.md', line: undefined });
});

test('kinds, languages and directories', () => {
  assert.equal(fileKind('a/B.MD'), 'markdown');
  assert.equal(fileKind('x.png'), 'image');
  assert.equal(fileKind('x.svg'), 'svg');
  assert.equal(fileKind('x.go'), 'text');
  assert.equal(languageOf('x.tsx'), 'typescript');
  assert.equal(languageOf('Makefile'), 'makefile');
  assert.equal(languageOf('notes.txt'), '');
  assert.equal(dirname('/a/b/c.md'), '/a/b');
  assert.equal(dirname('C:\\a\\c.md'), 'C:\\a');
  assert.equal(dirname('/c.md'), '/');
});

test('UNC shares are never file links', () => {
  assert.deepEqual(paths('see \\\\evil\\share\\a.png and //evil/share/b.md'), []);
  assert.equal(relativeTarget('\\\\evil\\share\\a.png'), undefined);
  assert.equal(relativeTarget('//evil/share/a.png'), undefined);
  assert.equal(pathInCode('\\\\evil\\share\\a.md'), undefined);
});

test('long runs of path characters are scanned in linear time', () => {
  for (const text of ['é.'.repeat(20000), 'éa/'.repeat(20000), 'é'.repeat(20000), 'a.'.repeat(20000)]) {
    const start = performance.now();
    splitPaths(text); absoluteDirs(text);
    assert.ok(performance.now() - start < 200, `${text.slice(0, 6)}… took ${Math.round(performance.now() - start)} ms`);
  }
});

test('pdf kind and heading anchors', () => {
  assert.equal(fileKind('a/b.PDF'), 'pdf');
  assert.equal(headingSlug('## 安装 Setup!'.replace(/^#+ /, '')), '安装-setup');
  assert.equal(headingSlug('API (v2)'), 'api-v2');
});

test('the remark pass turns paths into file links, and leaves links alone', () => {
  const tree = { type: 'root', children: [{ type: 'paragraph', children: [
    { type: 'text', value: '见 docs/a.md:3 和 ' }, { type: 'inlineCode', value: 'web/src/设计.md' },
    { type: 'link', url: 'https://x', children: [{ type: 'text', value: 'docs/b.md' }] },
  ] }] };
  remarkFilePaths()(tree);
  const nodes = tree.children[0].children;
  assert.deepEqual(nodes.map(node => node.type), ['text', 'link', 'text', 'link', 'link']);
  assert.deepEqual(parseFileHref(nodes[1].url), { path: 'docs/a.md', line: 3 });
  assert.equal(nodes[3].children[0].type, 'inlineCode');
  assert.deepEqual(parseFileHref(nodes[3].url), { path: 'web/src/设计.md', line: undefined });
  assert.equal(nodes[4].children[0].value, 'docs/b.md');
  assert.equal(parseFileHref('https://x'), undefined);
});
