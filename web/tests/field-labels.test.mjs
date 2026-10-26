import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import ts from 'typescript';

// Labels forward clicks anywhere inside them to their first form control.
// Composite selectors must have independent headings, leaving only the trigger active.
test('composite selectors are never wrapped in native labels', () => {
  const root = new URL('../src/', import.meta.url);
  let controls = 0;
  for (const name of readdirSync(root).filter(name => name.endsWith('.tsx'))) {
    const file = ts.createSourceFile(name, readFileSync(new URL(name, root), 'utf8'), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
    function visit(node) {
      if ((ts.isJsxSelfClosingElement(node) || ts.isJsxOpeningElement(node)) && ['Select', 'ModelPicker'].includes(node.tagName.getText(file))) {
        controls++;
        for (let parent = node.parent; parent; parent = parent.parent) {
          assert.ok(!ts.isJsxElement(parent) || parent.openingElement.tagName.getText(file) !== 'label', `${name}: selector nested inside label`);
        }
      }
      ts.forEachChild(node, visit);
    }
    visit(file);
  }
  assert.ok(controls >= 17, 'the regression check must inspect the actual settings controls');
});
