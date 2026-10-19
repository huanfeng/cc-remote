import { createContext, useContext, useEffect, useMemo, useState, type ReactNode, type RefObject } from 'react';
import { basename, splitPaths, type FileRef } from './filePaths';
import { t } from './i18n';

export type FileRequest = FileRef & { bases?: string[] };
export type FetchedFile = { blob: Blob; path: string };
/** Opens and fetches the host files a session names; absent where there is no session (native history). */
export type FileAccess = {
  open: (ref: FileRequest) => void;
  fetch: (ref: FileRequest, signal?: AbortSignal) => Promise<FetchedFile>;
  /** A short-lived URL the browser downloads the file from by itself (any size). */
  link: (ref: FileRequest) => Promise<string>;
};

export const FileContext = createContext<FileAccess | null>(null);
/** Directories relative paths in the Markdown being rendered may be relative to. */
export const BasesContext = createContext<string[]>([]);

export function useFiles() { return useContext(FileContext); }

/**
 * A URL to show a fetched image with. SVG gets a data: URL: a blob: URL is
 * same-origin, so an SVG opened from it in a new tab could run script with
 * the page's token in reach; a data: document has an opaque origin.
 */
export async function imageURL(file: FetchedFile): Promise<string> {
  if (!file.path.toLowerCase().endsWith('.svg')) return URL.createObjectURL(file.blob);
  const bytes = new Uint8Array(await file.blob.arrayBuffer());
  let binary = '';
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return `data:image/svg+xml;base64,${btoa(binary)}`;
}

export function releaseImageURL(url: string) { if (url.startsWith('blob:')) URL.revokeObjectURL(url); }

/** A file path in a message: a button that opens the viewer, or plain text without a session. */
export function FileLink({ file, children }: { file: FileRef; children: ReactNode }) {
  const files = useFiles();
  const bases = useContext(BasesContext);
  if (!files) return <>{children}</>;
  const label = file.line ? `${file.path}:${file.line}` : file.path;
  return <a href="#" className="file-link" title={t(`Open ${label}`, `打开 ${label}`)} onClick={event => { event.preventDefault(); files.open({ ...file, bases }); }}>{children}</a>;
}

/** Plain text (a tool's output, unhighlighted code) with the file paths in it as links. */
export function PathText({ text }: { text: string }) {
  const files = useFiles();
  const parts = useMemo(() => files && text.length <= MAX_LINKED ? splitPaths(text) : [text], [files, text]);
  return <>{parts.map((part, i) => typeof part === 'string' ? part : <FileLink key={i} file={part}>{part.text}</FileLink>)}</>;
}

// Longer texts stay plain: scanning them would hold up the page.
const MAX_LINKED = 200_000;

/**
 * Makes the file paths in HTML made elsewhere (highlighted code) open the
 * viewer. Only for markup React does not manage (dangerouslySetInnerHTML,
 * changing with key): it rewrites the element's text nodes.
 */
export function useLinkedPaths(ref: RefObject<HTMLElement | null>, key: unknown) {
  const files = useFiles();
  const bases = useContext(BasesContext);
  useEffect(() => {
    const root = ref.current;
    if (!root || !files || (root.textContent || '').length > MAX_LINKED) return;
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    const nodes: Text[] = [];
    for (let node = walker.nextNode(); node; node = walker.nextNode()) nodes.push(node as Text);
    for (const node of nodes) {
      if (node.parentElement?.closest('a')) continue;
      const parts = splitPaths(node.data);
      if (parts.every(part => typeof part === 'string')) continue;
      node.replaceWith(...parts.map(part => {
        if (typeof part === 'string') return part;
        const link = document.createElement('a');
        link.href = '#'; link.className = 'file-link'; link.textContent = part.text;
        link.dataset.path = part.path;
        if (part.line) link.dataset.line = String(part.line);
        link.title = t(`Open ${part.path}`, `打开 ${part.path}`);
        return link;
      }));
    }
    const click = (event: MouseEvent) => {
      const link = (event.target as Element | null)?.closest?.<HTMLAnchorElement>('a.file-link[data-path]');
      if (!link || !root.contains(link)) return;
      event.preventDefault();
      files.open({ path: link.dataset.path!, line: link.dataset.line ? Number(link.dataset.line) : undefined, bases });
    };
    root.addEventListener('click', click);
    return () => root.removeEventListener('click', click);
  }, [files, bases, key]);
}

/** An image the Markdown names by a host path, fetched with the access token. */
export function FileImage({ file, alt }: { file: FileRef; alt?: string }) {
  const files = useFiles();
  const bases = useContext(BasesContext);
  const [url, setUrl] = useState('');
  const [failed, setFailed] = useState(false);
  const key = `${file.path}\n${bases.join('\n')}`;
  useEffect(() => {
    if (!files) return;
    const controller = new AbortController(); let shown = '';
    setFailed(false); setUrl('');
    files.fetch({ ...file, bases }, controller.signal).then(imageURL).then(url => {
      if (controller.signal.aborted) { releaseImageURL(url); return; }
      shown = url; setUrl(url);
    }).catch(err => { if (err.name !== 'AbortError') setFailed(true); });
    return () => { controller.abort(); if (shown) releaseImageURL(shown); };
  }, [files, key]);
  const name = alt || basename(file.path);
  if (!files || failed) return <span className="file-image-missing">{t(`[image ${name} unavailable]`, `[图片 ${name} 无法显示]`)}</span>;
  if (!url) return <span className="file-image-missing">{t(`Loading ${name}…`, `加载 ${name}…`)}</span>;
  return <a href="#" className="file-image" title={t(`Open ${file.path}`, `打开 ${file.path}`)} onClick={event => { event.preventDefault(); files.open({ ...file, bases }); }}><img src={url} alt={alt || ''} loading="lazy" /></a>;
}
