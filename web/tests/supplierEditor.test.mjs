import { test } from 'node:test';
import assert from 'node:assert/strict';
import { supplierDraft, supplierJSON, parseSupplierJSON, validateSupplier, hasOneMContext, modelWithoutContext, withOneMContext } from '../src/features/settings/supplierEditorState.ts';

test('editing initializes all public fields while never prefilling a credential', () => {
  const draft = supplierDraft({ name:'Work', app:'claude', baseUrl:'https://api.example.com', model:'x', authType:'apiKey', credential:'never-copy', modelMappings:{sonnet:'relay-sonnet'}, website:'https://example.com', notes:'Work account', timeoutMs:30000 });
  assert.equal(draft.credential, '');
  assert.equal(draft.modelMappings.sonnet, 'relay-sonnet');
  assert.equal(draft.timeoutMs, 30000);
  assert.ok(!supplierJSON({ ...draft, credential:'never-serialize' }).includes('never-serialize'));
});
test('JSON editor permits only typed public connection fields', () => {
  const draft = supplierDraft();
  assert.equal(parseSupplierJSON(supplierJSON(draft), draft).credential, '');
  for (const value of [{credential:'secret'}, {env:{ANTHROPIC_API_KEY:'secret'}}, {codexOptions:{apiKey:'secret'}}, {modelMappings:{sonnet:42}}, {timeoutMs:'300'}]) {
    assert.throws(() => parseSupplierJSON(JSON.stringify(value), draft));
  }
  assert.throws(() => parseSupplierJSON('{broken', draft));
});
test('JSON cannot change the app and existing credential input stays separate', () => {
  const draft = { ...supplierDraft(), credential:'write-only' };
  assert.equal(parseSupplierJSON('{"name":"Changed"}', draft).credential, 'write-only');
  assert.throws(() => parseSupplierJSON('{"app":"codex"}', draft));
});
test('direct save validates connection and credentials without sequential steps', () => {
  const d = { ...supplierDraft(), name:'Work', baseUrl:'https://api.example.com' };
  assert.equal(validateSupplier(d, false), 'credential');
  assert.equal(validateSupplier(d, true), undefined);
  assert.equal(validateSupplier({ ...d, website:'https://u:secret@example.com' }, true), 'website');
  assert.equal(validateSupplier({ ...d, timeoutMs:-1 }, true), 'options');
});

test('new Claude suppliers default to Auth Token while Codex and saved authentication are preserved', () => {
  assert.equal(supplierDraft().authType, 'authToken');
  assert.equal(supplierDraft({app:'codex'}).authType, 'apiKey');
  assert.equal(supplierDraft({app:'claude', authType:'apiKey'}).authType, 'apiKey');
});


test('1M context declarations roundtrip without duplicating or creating an empty model', () => {
  assert.equal(hasOneMContext('relay[1m]  '), true);
  assert.equal(modelWithoutContext('relay[1M]'), 'relay');
  assert.equal(withOneMContext(' relay[1m] ', true), 'relay[1M]');
  assert.equal(withOneMContext('relay[1M]', false), 'relay');
  assert.equal(withOneMContext(' ', true), '');
  assert.equal(withOneMContext('[1M]', true), '');
  const draft = supplierDraft({model:'main[1M]',modelMappings:{sonnet:'relay[1M]'}});
  assert.equal(parseSupplierJSON(supplierJSON(draft), draft).modelMappings.sonnet, 'relay[1M]');
  assert.equal(parseSupplierJSON(supplierJSON(draft), draft).model, 'main[1M]');
});

test('Fable request models and independent display names survive the public JSON roundtrip', () => {
  const draft = supplierDraft({name:'Relay', baseUrl:'https://api.example.com', modelMappings:{fable:'hard-model[1M]'}, modelNames:{fable:'Hard tasks', sonnet:'Daily coding'}});
  const restored = parseSupplierJSON(supplierJSON(draft), draft);
  assert.equal(restored.modelMappings.fable, 'hard-model[1M]');
  assert.equal(restored.modelNames.fable, 'Hard tasks');
  assert.equal(restored.modelNames.sonnet, 'Daily coding');
  assert.equal(validateSupplier(restored, true), undefined);
  assert.equal(validateSupplier({...draft, modelNames:{fable:'x'.repeat(161)}}, true), 'options');
  assert.throws(() => parseSupplierJSON('{"modelNames":{"unknown":"x"}}', draft));
  assert.throws(() => parseSupplierJSON('{"modelNames":{"sonnet":42}}', draft));
});
