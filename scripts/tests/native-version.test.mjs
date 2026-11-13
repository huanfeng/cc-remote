import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { stampNativeVersion } from '../native-version.mjs';

function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'cc-remote-version-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const originals = new Map();
  function put(path, content) {
    writeFileSync(join(root, path), content);
    originals.set(path, content);
  }
  mkdirSync(join(root, 'desktop/src-tauri'), { recursive: true });
  mkdirSync(join(root, 'tray'));
  put('desktop/package.json', '{"name":"cc-remote-desktop","version":"0.1.0"}\n');
  put('desktop/src-tauri/tauri.conf.json', '{"version":"0.1.0","productName":"CC Remote"}\n');
  for (const [directory, name] of [['desktop/src-tauri', 'cc-remote-desktop'], ['tray', 'cc-remote-tray']]) {
    put(`${directory}/Cargo.toml`, `[package]\r\nname = "${name}"\r\nversion = "0.1.0"\r\n\r\n[dependencies]\r\nserde = "1"\r\n`);
    // Exercise a dependency before and after our own package, including EOF.
    put(`${directory}/Cargo.lock`, `version = 4\n\n[[package]]\nname = "a-dependency"\nversion = "0.1.0"\n\n[[package]]\nname = "${name}"\nversion = "0.1.0"\n\n[[package]]\nname = "z-dependency"\nversion = "0.1.0"\n`);
  }
  return { root, originals };
}

for (const version of ['1.23.4', '1.23.4-rc.1', '0.0.0-ci']) {
  test(`stamps ${version} across JSON, manifests and locks without changing dependencies`, (t) => {
    const { root, originals } = fixture(t);
    stampNativeVersion(root, version);
    for (const [path, source] of originals) {
      const expected = path.endsWith('.json')
        ? source.replace('"0.1.0"', `"${version}"`)
        : path.endsWith('Cargo.toml')
          ? source.replace('version = "0.1.0"', `version = "${version}"`)
          : source.replace(/(name = "cc-remote-(?:desktop|tray)"\n)version = "0.1.0"/, `$1version = "${version}"`);
      assert.equal(readFileSync(join(root, path), 'utf8'), expected, path);
    }
    stampNativeVersion(root, version); // Repeating a stamp is harmless.
  });
}

test('invalid versions leave every file unchanged', (t) => {
  const { root, originals } = fixture(t);
  for (const version of [undefined, '', 'v1.2.3', '1.2', '../1.2.3', '01.2.3', '1.2.3-']) {
    assert.throws(() => stampNativeVersion(root, version), /Invalid release version/);
  }
  for (const [path, source] of originals) assert.equal(readFileSync(join(root, path), 'utf8'), source);
});

test('missing lock package fails before writing any file', (t) => {
  const { root, originals } = fixture(t);
  writeFileSync(join(root, 'tray/Cargo.lock'), 'version = 4\n');
  originals.set('tray/Cargo.lock', 'version = 4\n');
  assert.throws(() => stampNativeVersion(root, '1.2.3'), /Expected one cc-remote-tray version/);
  for (const [path, source] of originals) assert.equal(readFileSync(join(root, path), 'utf8'), source);
});
