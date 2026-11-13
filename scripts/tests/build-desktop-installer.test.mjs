import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { desktopInstaller } from '../build-desktop-installer.mjs';

test('desktop entry uses the same installer with desktop enabled by default', () => {
  const source = readFileSync(new URL('../install.ps1', import.meta.url), 'utf8');
  const generated = desktopInstaller(source);
  assert.equal(generated.replace('[switch]$Desktop = $true,', '[switch]$Desktop,'), source);
  assert.notEqual(generated, source);
});

test('rejects missing or ambiguous desktop defaults instead of emitting a broken installer', () => {
  for (const source of ['', '[switch]$Desktop = $true,', '[switch]$Desktop, [switch]$Desktop,']) {
    assert.throws(() => desktopInstaller(source), /Expected one Desktop switch/);
  }
});
