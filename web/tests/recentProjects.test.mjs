import { test } from 'node:test';
import assert from 'node:assert/strict';
import { matchProjects, recentProjects } from '../src/recentProjects.ts';

test('recent projects: newest first, one entry per directory', () => {
  const list = recentProjects([
    { cwd: 'D:\\Develop\\app', updatedAt: '2026-10-01T00:00:00Z' },
    { cwd: '/home/me/lib', updatedAt: '2026-10-03T00:00:00Z' },
    { cwd: 'd:/develop/app/', updatedAt: '2026-10-05T00:00:00Z' },
    { cwd: '', updatedAt: '2026-10-06T00:00:00Z' },
    { cwd: '/home/me/Lib', updatedAt: '2026-10-02T00:00:00Z' },
  ]);
  assert.deepEqual(list.map(item => [item.path, item.name]), [['d:/develop/app', 'app'], ['/home/me/lib', 'lib'], ['/home/me/Lib', 'Lib']]);
});

test('recent projects: typing filters, a picked path shows them all', () => {
  const list = recentProjects([{ cwd: '/srv/web', updatedAt: '2026-10-02T00:00:00Z' }, { cwd: '/srv/api', updatedAt: '2026-10-01T00:00:00Z' }]);
  assert.deepEqual(matchProjects(list, 'AP').map(item => item.name), ['api']);
  assert.equal(matchProjects(list, '/srv/web/').length, 2);
  assert.equal(matchProjects(list, '').length, 2);
  assert.equal(matchProjects(recentProjects([{ cwd: 'C:\\', updatedAt: '' }]), '')[0].path, 'C:\\');
});

test('recent projects share Claude and Codex paths and deduplicate across providers', () => {
  const list = recentProjects([
    { provider: 'claude', cwd: 'D:/projects/shared', updatedAt: '2026-10-25T00:00:00Z' },
    { provider: 'codex', cwd: 'd:\\projects\\shared', updatedAt: '2026-10-27T00:00:00Z' },
    { provider: 'claude', cwd: '/srv/claude', updatedAt: '2026-10-26T00:00:00Z' },
    { provider: 'codex', cwd: '/srv/codex', updatedAt: '2026-10-24T00:00:00Z' },
  ]);
  assert.deepEqual(list.map(item => item.path), ['d:\\projects\\shared', '/srv/claude', '/srv/codex']);
});
