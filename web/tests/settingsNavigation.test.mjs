import { test } from 'node:test';
import assert from 'node:assert/strict';
import { settingsDestination } from '../src/features/settings/settingsNavigation.ts';

test('existing settings shortcuts still lead to their reorganized content', () => {
  assert.equal(settingsDestination('defaults'), 'claude');
  assert.equal(settingsDestination('oauth'), 'suppliers');
  assert.equal(settingsDestination('codex'), 'codex');
  assert.equal(settingsDestination('host'), 'host');
  assert.equal(settingsDestination('browser'), 'browser');
  assert.equal(settingsDestination(undefined), undefined);
});
