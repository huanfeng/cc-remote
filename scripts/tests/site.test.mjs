import assert from 'node:assert/strict';
import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import test from 'node:test';
import { buildSite } from '../build-site.mjs';

test('adding the introduction preserves the existing client and resolves local resources in both languages', async t => {
  const output = await mkdtemp(join(tmpdir(), 'cc-remote-pages-'));
  t.after(() => rm(output, { recursive: true, force: true }));
  await mkdir(join(output, 'assets'));
  await writeFile(join(output, 'index.html'), 'existing client');
  await writeFile(join(output, 'assets/client.js'), 'existing client code');
  await buildSite(output);
  assert.equal(await readFile(join(output, 'index.html'), 'utf8'), 'existing client');
  assert.equal(await readFile(join(output, 'assets/client.js'), 'utf8'), 'existing client code');
  for (const page of ['about/index.html', 'about/en/index.html']) {
    const html = await readFile(join(output, page), 'utf8');
    assert.match(html, /install-desktop\.ps1 \| iex/);
    assert.match(html, /install\.sh \| sh/);
    assert.match(html, /connect-src 'none'/);
    for (const [, resource] of html.matchAll(/(?:src|href)="([.][^"#]+\.(?:css|js|svg|png))"/g)) {
      await readFile(resolve(dirname(join(output, page)), resource));
    }
  }
});
