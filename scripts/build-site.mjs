import { copyFile, mkdir, writeFile } from 'node:fs/promises';
import { resolve, dirname } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { languages } from '../site/content.mjs';
import { renderPage } from '../site/template.mjs';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');

// Add only /about/. The established client root and its storage origin stay intact.
export async function buildSite(outputRoot = resolve(root, 'web/dist-standalone')) {
  const about = resolve(outputRoot, 'about');
  const assets = resolve(about, 'assets');
  await mkdir(assets, { recursive: true });
  await mkdir(resolve(about, 'en'), { recursive: true });
  for (const file of ['style.css', 'copy.js']) {
    await copyFile(resolve(root, 'site', file), resolve(assets, file));
  }
  await copyFile(resolve(root, 'web/public/icon.svg'), resolve(assets, 'icon.svg'));
  for (const file of ['desktop.png', 'mobile.png']) {
    await copyFile(resolve(root, 'docs/images', file), resolve(assets, file));
  }
  await writeFile(resolve(about, 'index.html'), renderPage(languages.zh));
  await writeFile(resolve(about, 'en/index.html'), renderPage(languages.en));
  return about;
}

if (process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === import.meta.url) {
  console.log(`Built introduction: ${await buildSite(process.argv[2])}`);
}
