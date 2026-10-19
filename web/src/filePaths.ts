// Finds the host file paths Claude names in its messages, so the web page can
// open them: "docs/design/x.md", "/home/me/app/main.go:12", "C:\app\a.png".
// The host decides what may be opened; this only decides what looks like a path.

export type FileRef = { path: string; line?: number };
export type PathSegment = string | (FileRef & { text: string });
export type FileKind = 'markdown' | 'image' | 'svg' | 'pdf' | 'text';

// Extensions a bare file name ("README.md", no directory) needs to count as a
// file: documents and images, not "Node.js" or "example.com".
const BARE_EXT = new Set(['md', 'markdown', 'mdx', 'txt', 'png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'avif', 'log', 'csv']);
const IMAGE_EXT = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp', 'bmp', 'avif', 'ico']);
const MARKDOWN_EXT = new Set(['md', 'markdown', 'mdx']);

// ASCII and Latin letters only: Chinese text often runs straight into a path
// ("在docs/a.md里"), so CJK characters end it. Inline code can hold any.
const SEG = String.raw`[A-Za-z0-9_.@+~\u00C0-\u024F-]+`;
// The lookbehinds hold every character a path may contain, so a long run of
// them is scanned once, not once per position.
const PATH = new RegExp(String.raw`(?<![A-Za-z0-9_.@+~/\\:À-ɏ-])((?:[A-Za-z]:[\\/]|[\\/]|\.{1,2}[\\/]|~[\\/])?(?:${SEG}[\\/])*${SEG}\.[A-Za-z][A-Za-z0-9]{0,9})(:\d+(?:[-:]\d+)?|#L\d+(?:-L?\d+)?)?(?![A-Za-z0-9_@+~/\\-])`, 'g');
const ABS_DIR = /(?<![A-Za-z0-9_.@+~/\\:À-ɏ-])((?:[A-Za-z]:[\\/]|\/)(?:[A-Za-z0-9_.@+~\u00C0-\u024F-]+[\\/]?)+)/g;

const extOf = (path: string) => { const name = basename(path); const dot = name.lastIndexOf('.'); return dot > 0 ? name.slice(dot + 1).toLowerCase() : ''; };
const hasSeparator = (path: string) => /[\\/]/.test(path);

export function basename(path: string): string {
  return path.split(/[\\/]/).filter(Boolean).at(-1) || path;
}

export function dirname(path: string): string {
  const index = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'));
  return index <= 0 ? path.slice(0, index + 1) : path.slice(0, index);
}

function lineOf(suffix: string | undefined): number | undefined {
  const match = suffix && /\d+/.exec(suffix);
  return match ? Number(match[0]) : undefined;
}

function accept(path: string): boolean {
  // URLs, and UNC shares (\\host\share) the host must never connect to.
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(path) || /^[\\/]{2}/.test(path)) return false;
  if (!hasSeparator(path)) return BARE_EXT.has(extOf(path));
  return /[A-Za-z]/.test(extOf(path));
}

/** Splits text into plain runs and the file paths in it. */
export function splitPaths(text: string): PathSegment[] {
  const out: PathSegment[] = [];
  let last = 0;
  for (const match of text.matchAll(PATH)) {
    const [whole, path, suffix] = match;
    // A URL's path ("https://x.com/a.md") is not a host file.
    const before = text.slice(Math.max(0, match.index! - 12), match.index!);
    if (!accept(path) || /[a-z][a-z0-9+.-]*:\/*[^\s]*$/i.test(before)) continue;
    if (match.index! > last) out.push(text.slice(last, match.index!));
    out.push({ text: whole, path, line: lineOf(suffix) });
    last = match.index! + whole.length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

/** The file an inline code span names when it is a path, as `docs/设计.md:3`. */
export function pathInCode(code: string): FileRef | undefined {
  const match = /^(\S+?)(:\d+(?:[-:]\d+)?|#L\d+(?:-L?\d+)?)?$/.exec(code.trim());
  if (!match || /[`'"<>|*?{}()[\]]/.test(match[1])) return undefined;
  const path = match[1];
  if (!/^[^.].*\.[A-Za-z][A-Za-z0-9]{0,9}$/.test(basename(path)) && !(path.startsWith('.') && hasSeparator(path))) return undefined;
  return accept(path) ? { path, line: lineOf(match[2]) } : undefined;
}

/** Absolute paths in text, which relative paths in it may be relative to. */
export function absoluteDirs(text: string): string[] {
  const out = new Set<string>();
  for (const match of text.matchAll(ABS_DIR)) {
    const before = text.slice(Math.max(0, match.index! - 12), match.index!);
    if (/[a-z][a-z0-9+.-]*:\/*[^\s]*$/i.test(before)) continue;
    out.add(match[1].replace(/[\\/.]+$/, '') || match[1]);
  }
  return [...out].slice(0, 12);
}

/** A link or image target in Markdown that names a host file, relative to the document. */
export function relativeTarget(href: string | undefined): FileRef | undefined {
  if (!href || href.startsWith('#') || /^[\\/]{2}/.test(href) || /^[a-z][a-z0-9+.-]*:/i.test(href) && !/^[A-Za-z]:[\\/]/.test(href)) return undefined;
  const [path, hash = ''] = href.split('#', 2);
  let decoded = path.split('?')[0];
  try { decoded = decodeURIComponent(decoded); } catch {}
  return decoded ? { path: decoded, line: lineOf(/^L\d+/.test(hash) ? hash : undefined) } : undefined;
}

export function fileKind(path: string): FileKind {
  const ext = extOf(path);
  if (MARKDOWN_EXT.has(ext)) return 'markdown';
  if (IMAGE_EXT.has(ext)) return 'image';
  if (ext === 'svg') return 'svg';
  if (ext === 'pdf') return 'pdf';
  return 'text';
}

/** A heading's anchor as GitHub writes it: "## 安装 Setup!" → "安装-setup". */
export function headingSlug(text: string): string {
  return text.trim().toLowerCase().replace(/[^\p{L}\p{N}\s_-]/gu, '').replace(/\s/g, '-');
}

const LANGUAGES: Record<string, string> = {
  go: 'go', ts: 'typescript', tsx: 'typescript', mts: 'typescript', cts: 'typescript', js: 'javascript', jsx: 'javascript', mjs: 'javascript', cjs: 'javascript',
  py: 'python', rs: 'rust', rb: 'ruby', java: 'java', kt: 'kotlin', kts: 'kotlin', swift: 'swift', c: 'c', h: 'c', cc: 'cpp', cpp: 'cpp', cxx: 'cpp', hpp: 'cpp', hh: 'cpp',
  cs: 'csharp', php: 'php', lua: 'lua', pl: 'perl', r: 'r', sql: 'sql', sh: 'bash', bash: 'bash', zsh: 'bash', json: 'json', jsonc: 'json', yaml: 'yaml', yml: 'yaml',
  toml: 'ini', ini: 'ini', conf: 'ini', cfg: 'ini', xml: 'xml', html: 'xml', htm: 'xml', svg: 'xml', vue: 'xml', css: 'css', scss: 'scss', less: 'less',
  md: 'markdown', markdown: 'markdown', diff: 'diff', patch: 'diff', graphql: 'graphql', gql: 'graphql', m: 'objectivec', mk: 'makefile',
};

/** The highlight.js language for a file, or '' for plain text. */
export function languageOf(path: string): string {
  const name = basename(path).toLowerCase();
  if (name === 'makefile' || name === 'gnumakefile') return 'makefile';
  return LANGUAGES[extOf(path)] || '';
}

// Links to host files use their own scheme; the Markdown renderer opens them in the viewer.
export const FILE_SCHEME = 'ccfile:';

export function fileHref(ref: FileRef): string {
  return FILE_SCHEME + encodeURIComponent(ref.path) + (ref.line ? `#L${ref.line}` : '');
}

export function parseFileHref(href: string | undefined): FileRef | undefined {
  if (!href?.startsWith(FILE_SCHEME)) return undefined;
  const [path, hash = ''] = href.slice(FILE_SCHEME.length).split('#', 2);
  try { return { path: decodeURIComponent(path), line: lineOf(hash || undefined) }; } catch { return undefined; }
}

type MdNode = { type: string; value?: string; url?: string; title?: null; children?: MdNode[] };

// Text and inline code that name a file become links; existing links stay as they are.
function linkPaths(node: MdNode): void {
  if (!node.children || node.type === 'link' || node.type === 'linkReference') return;
  const children: MdNode[] = [];
  for (const child of node.children) {
    if (child.type === 'text' && child.value) {
      for (const part of splitPaths(child.value)) children.push(typeof part === 'string' ? { type: 'text', value: part } : { type: 'link', url: fileHref(part), title: null, children: [{ type: 'text', value: part.text }] });
    } else if (child.type === 'inlineCode' && child.value && pathInCode(child.value)) {
      children.push({ type: 'link', url: fileHref(pathInCode(child.value)!), title: null, children: [child] });
    } else {
      linkPaths(child);
      children.push(child);
    }
  }
  node.children = children;
}

/** remark plugin: file paths in the text become ccfile: links. */
export const remarkFilePaths = () => linkPaths;
