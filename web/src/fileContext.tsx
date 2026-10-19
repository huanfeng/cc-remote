import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
import { basename, type FileRef } from './filePaths';
import { t } from './i18n';

export type FileRequest = FileRef & { bases?: string[] };
export type FetchedFile = { blob: Blob; path: string };
/** Opens and fetches the host files a session names; absent where there is no session (native history). */
export type FileAccess = { open: (ref: FileRequest) => void; fetch: (ref: FileRequest, signal?: AbortSignal) => Promise<FetchedFile> };

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
