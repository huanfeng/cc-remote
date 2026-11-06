// Pure helpers for the file viewer (imported by node tests: relative imports with extensions).
import { basename, fileKind, languageOf, type FileKind } from '../../domain/filePaths.ts';

/** Text files larger than this open as plain text, without rendering or highlighting. */
export const MAX_RENDER_BYTES = 2 << 20;
/** Highlighting a long file blocks the page; past this it is shown plain. */
export const MAX_HIGHLIGHT_BYTES = 512 << 10;

/** "src/a.ts:12", "src/a.ts:12:4", "src/a.ts#L12" → the path and the line. */
export function splitLine(path: string): { path: string; line?: number } {
  const m = /^(.*?)(?::(\d+)(?:[-:]\d+)?|#L(\d+)(?:-L?\d+)?)$/.exec(path);
  // "C:" alone is a drive, not a line.
  if (!m || !m[1] || /^[A-Za-z]$/.test(m[1])) return { path };
  const line = Number(m[2] ?? m[3]);
  return line > 0 ? { path: m[1], line } : { path: m[1] };
}

export type ViewKind = 'image' | 'markdown' | 'code' | 'binary' | 'too-large';

/** How a loaded file is shown. */
export function viewKind(file: { path: string; mime: string; text?: string; url?: string; tooLarge?: boolean }): ViewKind {
  if (file.tooLarge) return 'too-large';
  const kind: FileKind = fileKind(file.path);
  if (kind === 'image' || kind === 'svg' || file.mime.startsWith('image/')) {
    return file.url || (kind === 'svg' && file.text !== undefined) ? 'image' : 'binary';
  }
  // PDFs download: the page's CSP keeps them out of frames and objects.
  if (kind === 'pdf' || file.mime === 'application/pdf') return 'binary';
  if (file.text === undefined || looksBinary(file.text)) return 'binary';
  return kind === 'markdown' ? 'markdown' : 'code';
}

/** A NUL character early on marks a binary file. */
export function looksBinary(text: string): boolean {
  return text.slice(0, 8000).includes('\0');
}

/** SVG is only ever shown as a data: URL, so its scripts and external references stay inert. */
export function svgDataUrl(text: string): string {
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(text)}`;
}

// Where the shiki grammar name differs from the highlight.js one languageOf gives.
const SHIKI_BY_EXT: Record<string, string> = {
  tsx: 'tsx', jsx: 'jsx', html: 'html', htm: 'html', vue: 'vue', svg: 'xml', toml: 'toml', m: 'objective-c',
  dockerfile: 'docker', proto: 'proto', zig: 'zig', dart: 'dart', ex: 'elixir', exs: 'elixir', tf: 'hcl', nix: 'nix',
  svelte: 'svelte', astro: 'astro', ps1: 'powershell', bat: 'bat', cmd: 'bat', mdx: 'mdx', env: 'dotenv',
};

/** The shiki language for a file, or '' for plain text. */
export function shikiLang(path: string): string {
  const name = basename(path).toLowerCase();
  if (name === 'dockerfile') return 'docker';
  if (name === 'go.mod' || name === 'go.sum') return '';
  const ext = name.includes('.') ? name.slice(name.lastIndexOf('.') + 1) : '';
  return SHIKI_BY_EXT[ext] ?? languageOf(path);
}

/** The directory shown dimmed next to the name ("" for a bare name). */
export function dirOf(path: string): string {
  const i = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'));
  return i < 0 ? '' : path.slice(0, i + 1);
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`;
  return `${(n / 1024 / 1024).toFixed(1)} MiB`;
}
