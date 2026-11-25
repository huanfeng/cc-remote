import { test } from 'node:test';
import assert from 'node:assert/strict';
import { releaseNotes, updaterConfig, updaterManifest } from '../desktop-updater.mjs';

test('updater manifests point to the exact version and the renamed signed installer', () => {
  const result = updaterManifest('owner/fork', '1.2.3-rc.1', 'YWJj\n');
  assert.deepEqual(Object.keys(result.platforms), ['windows-x86_64']);
  assert.equal(result.version, '1.2.3-rc.1');
  assert.match(result.notes, /https:\/\/github\.com\/owner\/fork\/releases\/tag\/v1\.2\.3-rc\.1/);
  assert.equal(result.platforms['windows-x86_64'].signature, 'YWJj');
  assert.equal(result.platforms['windows-x86_64'].url, 'https://github.com/owner/fork/releases/download/v1.2.3-rc.1/cc-remote-desktop_1.2.3-rc.1_windows_amd64-setup.exe');
});

test('release notes come from the exact matching version heading', () => {
  const markdown = [
    '## 发布新版本', '### v1.2.30 更新内容', '- unrelated',
    '### v1.2.3 更新内容', '', '- 版本更新', '#### Notes', '- More changes', '',
    '### v1.2.3-rc.1 更新内容', '- preview', '## 工作流', '- unrelated',
  ].join('\r\n');
  const notes = releaseNotes(markdown, '1.2.3');
  assert.equal(notes, '- 版本更新\n#### Notes\n- More changes');
  assert.equal(releaseNotes(markdown, '1.2.3-rc.1'), '- preview');
  assert.equal(releaseNotes(markdown, '1.2.4'), '');
  assert.equal(updaterManifest('owner/repo', '1.2.3', 'YWJj', notes).notes, notes);
});

test('release configuration requires a valid public key and enables signed artifacts', () => {
  const bytes = Buffer.alloc(42); bytes.write('Ed');
  const key = Buffer.from(`untrusted comment: test\n${bytes.toString('base64')}\n`).toString('base64');
  const config = updaterConfig('owner/fork', key);
  assert.equal(config.bundle.createUpdaterArtifacts, true);
  assert.equal(config.plugins.updater.pubkey, key);
  assert.deepEqual(config.plugins.updater.endpoints, ['https://github.com/owner/fork/releases/latest/download/latest.json']);
  for (const invalid of ['', 'placeholder', Buffer.from('not a public key').toString('base64')]) {
    assert.throws(() => updaterConfig('owner/fork', invalid), /public key/);
  }
});

test('invalid metadata fails before publishing a manifest', () => {
  assert.throws(() => updaterManifest('owner/repo', 'v1.2.3', 'YWJj'), /version/);
  assert.throws(() => updaterManifest('owner/repo', '1.2.3', ''), /signature/);
  assert.throws(() => updaterManifest('owner/repo', '1.2.3', 'invalid signature'), /signature/);
  assert.throws(() => updaterManifest('owner/repo/extra', '1.2.3', 'YWJj'), /repository/);
});
