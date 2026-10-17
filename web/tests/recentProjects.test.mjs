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
