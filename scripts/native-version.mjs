import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// Stamp only our package versions; dependency versions and resolutions stay locked.
export function stampNativeVersion(root, version) {
  if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/.test(version)) {
    throw new Error(`Invalid release version: ${version}`);
  }
  const edits = [];
  for (const path of ['desktop/package.json', 'desktop/src-tauri/tauri.conf.json']) {
    const file = resolve(root, path);
    const source = readFileSync(file, 'utf8');
    const previous = JSON.parse(source).version;
    const updated = source.replace(/("version"\s*:\s*)"[^"]+"/, `$1"${version}"`);
    if (JSON.parse(updated).version !== version || typeof previous !== 'string') {
      throw new Error(`Missing version in ${path}`);
    }
    edits.push([file, updated]);
  }
  for (const [directory, name] of [
    ['desktop/src-tauri', 'cc-remote-desktop'],
    ['tray', 'cc-remote-tray'],
  ]) {
    for (const basename of ['Cargo.toml', 'Cargo.lock']) {
      const file = resolve(root, directory, basename);
      let found = 0;
      const updated = readFileSync(file, 'utf8').replace(
        /^\[\[?package\]?\][\s\S]*?(?=^\[|(?![\s\S]))/gm,
        (block) => {
          if (!new RegExp(`^name = "${name}"\\r?$`, 'm').test(block)) return block;
          return block.replace(/^version = "[^"]+"/m, () => {
            found++;
            return `version = "${version}"`;
          });
        },
      );
      if (found !== 1) throw new Error(`Expected one ${name} version in ${file}, got ${found}`);
      edits.push([file, updated]);
    }
  }
  // Validate every input before changing any file.
  for (const [file, source] of edits) writeFileSync(file, source);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  stampNativeVersion(resolve(import.meta.dirname, '..'), process.argv[2]);
}
