import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

export function updaterConfig(repository, publicKey) {
  if (!/^[\w.-]+\/[\w.-]+$/.test(repository)) throw new Error('Invalid GitHub repository');
  const text = Buffer.from(publicKey.trim(), 'base64').toString('utf8');
  const key = Buffer.from(text.trim().split(/\r?\n/)[1] ?? '', 'base64');
  if (key.length !== 42 || key.subarray(0, 2).toString() !== 'Ed') throw new Error('Invalid Tauri updater public key');
  return {
    bundle: { createUpdaterArtifacts: true },
    plugins: { updater: { pubkey: publicKey.trim(), requireSignedVersion: true, endpoints: [`https://github.com/${repository}/releases/latest/download/latest.json`] } },
  };
}

export function releaseNotes(markdown, version) {
  const lines = [];
  let matching = false;
  for (const line of markdown.split(/\r?\n/)) {
    const heading = /^(#{1,3})[ \t]+(\S+)/.exec(line);
    if (heading) {
      if (matching) break;
      matching = heading[1] === '###' && heading[2] === `v${version}`;
    } else if (matching) {
      lines.push(line);
    }
  }
  return lines.join('\n').trim();
}

function documentedNotes(version) {
  return releaseNotes(readFileSync(new URL('../docs/release.md', import.meta.url), 'utf8'), version);
}

export function updaterManifest(repository, version, signature, notes = '') {
  if (!/^[\w.-]+\/[\w.-]+$/.test(repository)) throw new Error('Invalid GitHub repository');
  if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/.test(version)) throw new Error('Invalid version');
  if (!signature.trim() || !/^[A-Za-z0-9+/=\r\n]+$/.test(signature.trim())) throw new Error('Missing or invalid updater signature');
  return {
    version,
    notes: notes.trim() || `CC Remote ${version}\nhttps://github.com/${repository}/releases/tag/v${version}`,
    platforms: {
      'windows-x86_64': {
        signature: signature.trim(),
        url: `https://github.com/${repository}/releases/download/v${version}/cc-remote-desktop_${version}_windows_amd64-setup.exe`,
      },
    },
  };
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const [mode, output, ...args] = process.argv.slice(2);
  if (mode === 'notes') {
    const [version] = args;
    const notes = documentedNotes(version);
    writeFileSync(output, notes ? `${notes}\n` : '');
  } else {
    const [repository, version, signaturePath] = args;
    const data = mode === 'config' ? updaterConfig(repository, process.env.TAURI_UPDATER_PUBLIC_KEY ?? '')
      : mode === 'manifest' ? updaterManifest(repository, version, readFileSync(signaturePath, 'utf8'), documentedNotes(version))
      : (() => { throw new Error('Expected config, manifest or notes'); })();
    writeFileSync(output, `${JSON.stringify(data, null, 2)}\n`);
  }
}
