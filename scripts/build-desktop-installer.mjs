import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// Both entry points use one implementation, including authentication and checksums.
export function desktopInstaller(source) {
  const marker = '[switch]$Desktop,';
  if (source.split(marker).length !== 2) throw new Error('Expected one Desktop switch');
  return source.replace(marker, '[switch]$Desktop = $true,');
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const source = readFileSync(new URL('./install.ps1', import.meta.url), 'utf8');
  writeFileSync(process.argv[2], desktopInstaller(source));
}
