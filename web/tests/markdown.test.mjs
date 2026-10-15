import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { normalizeClaudeMarkdown, INSIGHT_LANG } from '../src/markdownPreprocess.ts';

const here = dirname(fileURLToPath(import.meta.url));
const web = join(here, '..');
const RULE = '─'.repeat(49);
const OPEN = '★ Insight ' + '─'.repeat(37);

function insights(output) {
  return output.split('\n').filter(line => line.endsWith(INSIGHT_LANG)).length;
}

test('backtick-wrapped insight becomes a fenced container with its body intact', () => {
  const out = normalizeClaudeMarkdown(['前文', '`' + OPEN + '`', '- 第一点', '- 第二点', '`' + RULE + '`', '后文'].join('\n'));
  assert.equal(out, ['前文', '', '```' + INSIGHT_LANG, '- 第一点', '- 第二点', '```', '', '后文'].join('\n'));
});

test('insight without backticks is detected too', () => {
  const out = normalizeClaudeMarkdown([OPEN, '行一', '行二', RULE].join('\n'));
  assert.equal(out, ['', '```' + INSIGHT_LANG, '行一', '行二', '```', ''].join('\n'));
});

test('closing rule glued to the last sentence is split off', () => {
  const out = normalizeClaudeMarkdown([OPEN, '没有解决…对齐的问题。 beam_backtrack 用 ... ' + RULE, '之后'].join('\n'));
  assert.equal(out, ['', '```' + INSIGHT_LANG, '没有解决…对齐的问题。 beam_backtrack 用 ...', '```', '', '之后'].join('\n'));
});

test('markers inside an existing fenced code block are left untouched', () => {
  const source = ['```text', OPEN, 'x', RULE, '```', '~~~', RULE, '~~~'].join('\n');
  assert.equal(normalizeClaudeMarkdown(source), source);
});

test('multiple insights each become their own container', () => {
  const block = [OPEN, 'a', RULE].join('\n');
  assert.equal(insights(normalizeClaudeMarkdown([block, '中间', block].join('\n'))), 2);
});

test('CRLF input is normalized before matching', () => {
  const out = normalizeClaudeMarkdown(['`' + OPEN + '`', 'a', '`' + RULE + '`'].join('\r\n'));
  assert.ok(!out.includes('\r'));
  assert.equal(insights(out), 1);
});

test('fence inside the insight body grows the container fence', () => {
  const out = normalizeClaudeMarkdown([OPEN, '```js', RULE, '```', RULE].join('\n'));
  assert.equal(out, ['', '````' + INSIGHT_LANG, '```js', RULE, '```', '````', ''].join('\n'));
});

test('an unterminated (streaming) insight still renders as a container', () => {
  assert.equal(insights(normalizeClaudeMarkdown([OPEN, '还在输出'].join('\n'))), 1);
});

test('bare and glued rules outside insights become thematic breaks, tables are kept', () => {
  assert.equal(normalizeClaudeMarkdown(['段落', RULE, '下一段'].join('\n')), ['段落', '', '***', '', '下一段'].join('\n'));
  assert.equal(normalizeClaudeMarkdown('句子。 ' + RULE), ['句子。', '', '***', ''].join('\n'));
  const table = '| a | ' + RULE + ' |';
  assert.equal(normalizeClaudeMarkdown(table), table);
  assert.equal(normalizeClaudeMarkdown('短线 ───'), '短线 ───');
});

test('rendered fixture contains tables, insight cards, code blocks and safe links', async () => {
  const { build } = await import('esbuild');
  const outdir = join(web, 'node_modules', '.cache', 'md-test');
  mkdirSync(outdir, { recursive: true });
  const outfile = join(outdir, 'Markdown.mjs');
  await build({ entryPoints: [join(web, 'src', 'Markdown.tsx')], outfile, bundle: true, format: 'esm', platform: 'node', jsx: 'automatic', packages: 'external', loader: { '.css': 'empty' }, logLevel: 'silent' });
  const { default: Markdown } = await import(pathToFileURL(outfile).href);
  const { createElement } = await import('react');
  const { renderToStaticMarkup } = await import('react-dom/server');
  const html = renderToStaticMarkup(createElement(Markdown, { text: readFileSync(join(here, 'fixtures', 'claude-output.md'), 'utf8') }));
  assert.equal((html.match(/<div class="md-table"><table>/g) || []).length, 2);
  assert.match(html, /<th[^>]*>配万象模型时的推导值<\/th>/);
  assert.match(html, /<th style="text-align:center">居中<\/th>/);
  assert.equal((html.match(/class="md-insight"/g) || []).length, 2);
  assert.match(html, /<div class="md-insight-body"><ul>/);
  assert.match(html, /没有解决…对齐的问题。 beam_backtrack 用 \.\.\.<\/p>/);
  assert.ok(!/<p>[^<]*─{10}/.test(html), 'no box-drawing rule glued into a paragraph');
  assert.match(html, /│ host     │ 托管会话     │/);
  assert.match(html, /★ Insight ─+  \(代码块里不应被转换\)/);
  assert.match(html, /<div class="md-code-bar"><span>go<\/span><button type="button">复制<\/button>/);
  assert.match(html, /<input type="checkbox" disabled="" checked=""\/>/);
  assert.match(html, /<del>删除线<\/del>/);
  assert.match(html, /<a href="https:\/\/example.com\/docs" target="_blank" rel="noopener noreferrer">文档<\/a>/);
  assert.ok(!html.includes('javascript:'), 'javascript: urls are stripped');
  assert.match(html, /href="#user-content-fn-1"(?![^>]*target)/);
  assert.match(html, /<img src="https:\/\/example.com\/wide-image.png" alt="图片" loading="lazy"\/>/);
  assert.match(html, /推导结果如下（单换行应当保留，<br\/>/);
  assert.ok((html.match(/<hr\/>/g) || []).length >= 2);
});
