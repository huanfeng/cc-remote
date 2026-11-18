import { test } from 'node:test';
import assert from 'node:assert/strict';
import { validateSupplierStep } from '../src/features/settings/supplierWizardState.ts';

const draft = { name: 'Work', app: 'claude', baseUrl: 'https://api.example.com', model: '', authType: 'apiKey', credential: 'secret' };
test('connection step rejects embedded credentials and requires a usable API URL', () => {
  for (const baseUrl of ['', 'file:///tmp/api', 'https://user:secret@example.com', 'https://example.com?key=secret', 'https://example.com/#secret']) {
    assert.equal(validateSupplierStep({ ...draft, baseUrl }, 1, false), 'url');
  }
  assert.equal(validateSupplierStep(draft, 1, false), undefined);
  assert.equal(validateSupplierStep({ ...draft, name: '  ' }, 1, false), 'name');
});
test('new credentials are required; editing can preserve a saved credential', () => {
  assert.equal(validateSupplierStep({ ...draft, credential: ' ' }, 2, false), 'credential');
  assert.equal(validateSupplierStep({ ...draft, credential: '' }, 2, true), undefined);
  assert.equal(validateSupplierStep(draft, 2, false), undefined);
});
