// Images Markdown names by a host path (![](docs/shot.png)): the page cannot
// load them by URL (the host wants the token), so they are read through the
// backend's file endpoint and shown as object URLs (SVG as a data: URL). Loads
// are cached per session and path, so re-renders while a reply streams do not
// fetch again. External http(s) and data: images are left to the browser.
import { createContext, useCallback, useContext, useEffect, useState } from 'react';
import { useBackend } from '@/backend/backend';
import type { FileRef } from '@/domain/filePaths';
import { basename } from '@/domain/filePaths';
import { svgDataUrl } from '@/features/files/fileView';
import { t } from '@/lib/i18n';

/** Resolves a host image path (relative to bases) to a URL an <img> can show. */
export type ResolveImage = (path: string, bases: string[]) => Promise<string>;
export const MarkdownImages = createContext<ResolveImage | null>(null);

const MAX_CACHED = 100;
const cache = new Map<string, Promise<string>>();

function remember(key: string, load: () => Promise<string>) {
  let p = cache.get(key);
  if (p) { cache.delete(key); cache.set(key, p); return p; }
  p = load();
  p.catch(() => cache.delete(key));
  cache.set(key, p);
  while (cache.size > MAX_CACHED) {
    const [k, v] = cache.entries().next().value!;
    cache.delete(k);
    void v.then(url => { if (url.startsWith('blob:')) URL.revokeObjectURL(url); }, () => {});
  }
  return p;
}

/** The resolver for a session's Markdown (undefined id: native history, no file access). */
export function useImageResolver(sessionId: string | undefined): ResolveImage | null {
  const backend = useBackend();
  const resolve = useCallback<ResolveImage>((path, bases) => remember(`${backend.kind}\n${sessionId}\n${path}\n${bases.join('\n')}`, async () => {
    const file = await backend.readFile(sessionId!, path, bases);
    if (file.tooLarge) throw new Error('too large');
    if (file.text !== undefined && (file.mime === 'image/svg+xml' || /\.svg$/i.test(file.path))) return svgDataUrl(file.text);
    if (!file.url) throw new Error('not an image');
    return file.url;
  }), [backend, sessionId]);
  return sessionId ? resolve : null;
}

export function HostImage({ file, alt, bases, onOpen }: { file: FileRef; alt?: string; bases: string[]; onOpen?: () => void }) {
  const resolve = useContext(MarkdownImages);
  const key = `${file.path}\n${bases.join('\n')}`;
  const [state, setState] = useState<{ key: string; url?: string; failed?: boolean }>();
  useEffect(() => {
    if (!resolve) return;
    let live = true;
    resolve(file.path, bases).then(url => { if (live) setState({ key, url }); }, () => { if (live) setState({ key, failed: true }); });
    return () => { live = false; };
    // bases is keyed by its content.
  }, [resolve, key]);
  const name = alt || basename(file.path);
  const current = state?.key === key ? state : undefined;
  if (!resolve || current?.failed) return <span className="text-ui-sm text-foreground-subtlest">{t(`[image ${name} unavailable]`, `[图片 ${name} 无法显示]`)}</span>;
  if (!current?.url) return <span className="text-ui-sm text-foreground-subtlest">{t(`Loading ${name}…`, `加载 ${name}…`)}</span>;
  const img = <img src={current.url} alt={alt || ''} loading="lazy" className="max-w-full rounded-lg" />;
  if (!onOpen) return img;
  return (
    <a href="#" title={t(`Open ${file.path}`, `打开 ${file.path}`)} onClick={e => { e.preventDefault(); onOpen(); }}>{img}</a>
  );
}
