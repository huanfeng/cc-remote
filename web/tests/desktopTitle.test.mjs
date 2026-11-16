import { test } from 'node:test';
import assert from 'node:assert/strict';
import { desktopWindowTitle } from '../src/domain/desktopTitle.ts';

test('native title includes the app version while preserving host names and waiting badges', () => {
  assert.equal(desktopWindowTitle('CC Remote', '0.31.0'), 'CC Remote v0.31.0');
  assert.equal(desktopWindowTitle('(2) Workstation · CC Remote', '0.31.0'), '(2) Workstation · CC Remote v0.31.0');
  assert.equal(desktopWindowTitle('另一台宿主 · CC Remote', '1.0.0-rc.1'), '另一台宿主 · CC Remote v1.0.0-rc.1');
  assert.equal(desktopWindowTitle('', '0.31.0'), 'CC Remote v0.31.0');
  assert.equal(desktopWindowTitle('Workstation · CC Remote'), 'Workstation · CC Remote');
});
