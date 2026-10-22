import { useCallback, useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent, type ReactNode } from 'react';
import { copyText } from './clipboard';
import { BasesContext, FileContext, imageURL, releaseImageURL, type FetchedFile, type FileAccess, type FileRequest } from './fileContext';
import { basename, dirname, fileKind, languageOf } from './filePaths';
import { useHighlight } from './highlight';
import { apiUrl } from './hosts';
import { lang, t } from './i18n';
import { BackIcon, CloseIcon, DownloadIcon } from './icons';
import Markdown from './Markdown';
import './files.css';

// Text files larger than this open as plain text without highlighting or rendering.
const MAX_RENDER_BYTES = 2 << 20;

// A document's relative paths resolve against its directory, then those of the message that opened it.
const NO_BASES: string[] = [];

type Loaded = FetchedFile & { text?: string; url?: string; binary?: boolean };

function fileQuery(ref: FileRequest) {
  const query = new URLSearchParams({ path: ref.path });
  for (const base of ref.bases || []) query.append('base', base);
  return query;
}

/** A failed file request; tooLarge: over the viewer's limit, though it can be downloaded. */
async function fileError(response: Response) {
  let message = t(`Cannot open the file (${response.status})`, `无法打开文件 (${response.status})`);
  try { message = (await response.json()).error || message; } catch {}
  return Object.assign(new Error(message), { tooLarge: response.status === 413 });
}

async function fetchFile(sessionId: string, token: string, ref: FileRequest, signal?: AbortSignal): Promise<FetchedFile> {
  const response = await fetch(apiUrl(`/api/sessions/${encodeURIComponent(sessionId)}/file?${fileQuery(ref)}`), { headers: { Authorization: `Bearer ${token}`, 'X-CC-Lang': lang }, cache: 'no-store', signal });
  if (!response.ok) throw await fileError(response);
  let path = ref.path;
  try { path = decodeURIComponent(response.headers.get('X-CC-Path') || '') || path; } catch {}
  return { blob: await response.blob(), path };
}

async function fileLink(sessionId: string, token: string, ref: FileRequest): Promise<string> {
  const response = await fetch(apiUrl(`/api/sessions/${encodeURIComponent(sessionId)}/file/link?${fileQuery(ref)}`), { method: 'POST', headers: { Authorization: `Bearer ${token}`, 'X-CC-Lang': lang }, cache: 'no-store' });
  if (!response.ok) throw await fileError(response);
  return apiUrl((await response.json()).url);
}

/** Lets the browser save a URL as a download (the host sends it as an attachment). */
function saveFrom(url: string, name: string) {
  const link = document.createElement('a'); link.href = url; link.download = name; link.rel = 'noopener';
  document.body.append(link); link.click(); link.remove();
}

/** Makes the host files a session names openable in a viewer (sessionId empty: none). */
export function FileProvider({ sessionId, token, children }: { sessionId: string; token: string; children: ReactNode }) {
  const [stack, setStack] = useState<FileRequest[]>([]);
  useEffect(() => setStack([]), [sessionId]);
  const access = useMemo<FileAccess | null>(() => sessionId ? {
    open: ref => setStack(old => [...old, ref]),
    fetch: (ref, signal) => fetchFile(sessionId, token, ref, signal),
    link: ref => fileLink(sessionId, token, ref),
  } : null, [sessionId, token]);
  const close = useCallback(() => setStack([]), []);
  const back = useCallback(() => setStack(old => old.slice(0, -1)), []);
  return <FileContext.Provider value={access}>
    {children}
    {access && stack.length > 0 && <FileViewer key={stack.length} request={stack[stack.length - 1]} access={access} canGoBack={stack.length > 1} back={back} close={close} />}
  </FileContext.Provider>;
}

function FileViewer({ request, access, canGoBack, back, close }: { request: FileRequest; access: FileAccess; canGoBack: boolean; back: () => void; close: () => void }) {
  const [file, setFile] = useState<Loaded>();
  const [error, setError] = useState('');
  // Over the viewer's limit: not shown, but downloadable through a link.
  const [tooLarge, setTooLarge] = useState(false);
  const [saving, setSaving] = useState(false);
  const [source, setSource] = useState(false);
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    const controller = new AbortController(); let url = '';
    access.fetch(request, controller.signal).then(async loaded => {
      const kind = fileKind(loaded.path);
      if (kind === 'image' || kind === 'svg') {
        const shown = await imageURL(loaded);
        if (controller.signal.aborted) { releaseImageURL(shown); return; }
        url = shown; setFile({ ...loaded, url });
        return;
      }
      // PDFs download: the page's CSP keeps them out of frames and objects.
      if (kind === 'pdf') { setFile({ ...loaded, binary: true }); return; }
      const bytes = new Uint8Array(await loaded.blob.arrayBuffer());
      if (controller.signal.aborted) return;
      // A NUL byte early on marks a binary file; it is offered as a download instead.
      if (bytes.subarray(0, 8000).includes(0)) { setFile({ ...loaded, binary: true }); return; }
      setFile({ ...loaded, text: new TextDecoder().decode(bytes) });
    }).catch(err => { if (err.name === 'AbortError') return; if (err.tooLarge) setTooLarge(true); else setError((err as Error).message); });
    return () => { controller.abort(); if (url) releaseImageURL(url); };
  }, [request, access]);
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => { if (event.key === 'Escape' && !event.isComposing) { event.preventDefault(); event.stopPropagation(); close(); } };
    document.addEventListener('keydown', onKey, true);
    return () => document.removeEventListener('keydown', onKey, true);
  }, [close]);

  const path = file?.path || request.path;
  const kind = fileKind(path);
  const download = async () => {
    if (file) {
      const url = URL.createObjectURL(file.blob);
      saveFrom(url, basename(path));
      window.setTimeout(() => URL.revokeObjectURL(url), 10_000);
      return;
    }
    if (!tooLarge || saving) return;
    setSaving(true);
    try { saveFrom(await access.link(request), basename(path)); } catch (err) { setError((err as Error).message); } finally { setSaving(false); }
  };
  const copyPath = async () => { try { await copyText(path); setCopied(true); window.setTimeout(() => setCopied(false), 1600); } catch {} };
  const large = !!file?.text && file.blob.size > MAX_RENDER_BYTES;

  let body: ReactNode;
  if (error) body = <div className="error" role="alert">{error}</div>;
  else if (tooLarge) body = <p className="file-note">{t('This file is too large to preview (over 20 MiB). ', '此文件超过 20 MiB，无法预览。')}<button type="button" className="tool-more" disabled={saving} onClick={() => void download()}>{saving ? t('Preparing…', '准备中…') : t('Download it', '下载')}</button></p>;
  else if (!file) body = <p className="file-note">{t('Loading…', '加载中…')}</p>;
  else if (file.url) body = <ImageView src={file.url} alt={basename(path)} />;
  else if (file.binary) body = <p className="file-note">{t('This file cannot be previewed. ', '此文件无法预览。')}<button type="button" className="tool-more" onClick={() => void download()}>{t('Download it', '下载')}</button></p>;
  else if (kind === 'markdown' && !source && !large) body = <div className="file-doc"><BasesContext.Provider value={request.bases || NO_BASES}><Markdown text={file.text!} dir={dirname(path)} /></BasesContext.Provider></div>;
  else body = <CodeView code={file.text!} lang={large ? '' : kind === 'markdown' ? 'markdown' : languageOf(path)} line={request.line} />;

  return <div className="modal-backdrop file-backdrop" onClick={close}>
    <div className="file-viewer" role="dialog" aria-modal="true" aria-label={basename(path)} onClick={event => event.stopPropagation()}>
      <div className="file-viewer-bar">
        {canGoBack && <button type="button" className="icon" title={t('Back', '返回')} aria-label={t('Back', '返回')} onClick={back}><BackIcon size={16} /></button>}
        <div className="file-viewer-title"><strong>{basename(path)}{request.line ? `:${request.line}` : ''}</strong><button type="button" className="file-viewer-path" title={t('Copy path', '复制路径')} onClick={() => void copyPath()}>{copied ? t('Path copied', '路径已复制') : path}</button></div>
        {kind === 'markdown' && file?.text !== undefined && !large && <button type="button" className={`file-toggle${source ? ' active' : ''}`} aria-pressed={source} onClick={() => setSource(!source)}>{source ? t('Rendered', '渲染') : t('Source', '源码')}</button>}
        <button type="button" className="icon" title={t('Download', '下载')} aria-label={t('Download', '下载')} disabled={!file && !tooLarge} onClick={() => void download()}><DownloadIcon size={16} /></button>
        <button type="button" className="icon" title={t('Close', '关闭')} aria-label={t('Close file viewer', '关闭文件查看')} onClick={close}><CloseIcon size={18} /></button>
      </div>
      <div className="file-viewer-body">{body}</div>
    </div>
  </div>;
}

// Zoom is relative to the image fitted to the viewer.
const MAX_ZOOM = 10;
type View = { scale: number; x: number; y: number };
const FIT: View = { scale: 1, x: 0, y: 0 };

/**
 * An image that zooms by itself, not the page: pinch, wheel or double-click
 * (double-tap) zooms at that point, a drag moves the zoomed image.
 */
function ImageView({ src, alt }: { src: string; alt: string }) {
  const box = useRef<HTMLDivElement>(null);
  const img = useRef<HTMLImageElement>(null);
  const [view, setViewState] = useState<View>(FIT);
  // The latest view at once: several wheel events can come before a render.
  const viewRef = useRef(view);
  const setView = useCallback((next: View) => { viewRef.current = next; setViewState(next); }, []);
  const [moving, setMoving] = useState(false);
  const [natural, setNatural] = useState(0); // the image's width in pixels, for the zoom shown
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  // The view and pointers a drag or pinch started from.
  const start = useRef<{ view: View; x: number; y: number; dist: number } | undefined>(undefined);
  const lastTap = useRef({ time: 0, x: 0, y: 0 });
  const touched = useRef(false); // the last press was not a mouse: taps make the double-tap
  const pinched = useRef(false); // two fingers were down: lifting them is no tap

  // Keeps the zoomed image over the box: no dragging it out of sight.
  const clamp = useCallback((scale: number, x: number, y: number): View => {
    const b = box.current, i = img.current;
    if (!b || !i || scale <= 1) return FIT;
    const maxX = Math.max(0, (i.offsetWidth * scale - b.clientWidth) / 2);
    const maxY = Math.max(0, (i.offsetHeight * scale - b.clientHeight) / 2);
    return { scale, x: Math.min(maxX, Math.max(-maxX, x)), y: Math.min(maxY, Math.max(-maxY, y)) };
  }, []);
  // Where a client point is, from the box's center (the image's, unzoomed).
  const fromCenter = (x: number, y: number) => {
    const r = box.current!.getBoundingClientRect();
    return { x: x - r.left - r.width / 2, y: y - r.top - r.height / 2 };
  };
  // Zooms from a view to scale, keeping the image point at `at` under `to`.
  const zoom = useCallback((from: View, scale: number, at: { x: number; y: number }, to = at) => {
    scale = Math.min(MAX_ZOOM, Math.max(1, scale));
    const k = scale / from.scale;
    setView(clamp(scale, to.x - (at.x - from.x) * k, to.y - (at.y - from.y) * k));
  }, [clamp, setView]);

  useEffect(() => {
    const el = box.current;
    if (!el) return;
    // React listens to wheel passively; a zoom must keep the page from scrolling.
    const onWheel = (event: WheelEvent) => {
      event.preventDefault();
      const r = el.getBoundingClientRect();
      const at = { x: event.clientX - r.left - r.width / 2, y: event.clientY - r.top - r.height / 2 };
      // A trackpad pinch comes as a wheel with Ctrl and small steps.
      const step = event.deltaMode === 1 ? event.deltaY * 33 : event.deltaY;
      zoom(viewRef.current, viewRef.current.scale * Math.exp(-step * (event.ctrlKey ? 0.01 : 0.002)), at);
    };
    const stop = (event: Event) => event.preventDefault(); // Safari's own pinch zoom
    el.addEventListener('wheel', onWheel, { passive: false });
    el.addEventListener('gesturestart', stop);
    return () => { el.removeEventListener('wheel', onWheel); el.removeEventListener('gesturestart', stop); };
  }, [zoom]);

  const begin = () => {
    const [a, b] = [...pointers.current.values()];
    if (!a) { start.current = undefined; setMoving(false); return; }
    const mid = b ? { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 } : a;
    start.current = { view: viewRef.current, x: mid.x, y: mid.y, dist: b ? Math.hypot(a.x - b.x, a.y - b.y) : 0 };
    setMoving(true);
  };
  const toggle = (x: number, y: number) => {
    if (viewRef.current.scale > 1) setView(FIT);
    else zoom(FIT, 2.5, fromCenter(x, y));
  };
  const onPointerDown = (event: ReactPointerEvent) => {
    if (event.button !== 0) return;
    touched.current = event.pointerType !== 'mouse';
    event.currentTarget.setPointerCapture(event.pointerId);
    pointers.current.set(event.pointerId, { x: event.clientX, y: event.clientY });
    pinched.current = pointers.current.size > 1; // a new press starts a new gesture
    begin();
  };
  const onPointerMove = (event: ReactPointerEvent) => {
    if (!pointers.current.has(event.pointerId) || !start.current) return;
    pointers.current.set(event.pointerId, { x: event.clientX, y: event.clientY });
    const s = start.current;
    const [a, b] = [...pointers.current.values()];
    if (b) { // pinch: zoom by the fingers' spread, around and with their midpoint
      const mid = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
      zoom(s.view, s.view.scale * Math.hypot(a.x - b.x, a.y - b.y) / s.dist, fromCenter(s.x, s.y), fromCenter(mid.x, mid.y));
    } else if (s.view.scale > 1) {
      setView(clamp(s.view.scale, s.view.x + a.x - s.x, s.view.y + a.y - s.y));
    }
  };
  const onPointerUp = (event: ReactPointerEvent) => {
    const s = start.current;
    const wasOne = pointers.current.size === 1;
    if (!pointers.current.delete(event.pointerId)) return;
    begin();
    // A tap, twice in a row on a touch screen: zoom in there or back out (a mouse has dblclick).
    if (touched.current && wasOne && !pinched.current && s && Math.hypot(event.clientX - s.x, event.clientY - s.y) < 10) {
      const last = lastTap.current, now = event.timeStamp;
      if (now - last.time < 300 && Math.hypot(event.clientX - last.x, event.clientY - last.y) < 30) {
        lastTap.current = { time: 0, x: 0, y: 0 };
        toggle(event.clientX, event.clientY);
      } else lastTap.current = { time: now, x: event.clientX, y: event.clientY };
    }
  };
  const step = (factor: number) => zoom(view, view.scale * factor, { x: 0, y: 0 });
  const shown = natural && img.current ? Math.round(img.current.offsetWidth * view.scale / natural * 100) : 0;

  return <div className="file-image-view">
    <div ref={box} className={`file-image-stage${view.scale > 1 ? ' zoomed' : ''}${moving ? ' moving' : ''}`} onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={onPointerUp} onPointerCancel={onPointerUp} onDoubleClick={event => { if (!touched.current) toggle(event.clientX, event.clientY); }}>
      <img ref={img} src={src} alt={alt} draggable={false} onLoad={event => setNatural(event.currentTarget.naturalWidth)} style={{ transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }} />
    </div>
    <div className="file-image-zoom" role="toolbar" aria-label={t('Zoom', '缩放')}>
      <button type="button" aria-label={t('Zoom out', '缩小')} title={t('Zoom out', '缩小')} disabled={view.scale <= 1} onClick={() => step(1 / 1.5)}>−</button>
      <button type="button" className="file-image-fit" title={t('Fit to the window', '适应窗口')} disabled={view.scale === 1} onClick={() => setView(FIT)}>{shown ? `${shown}%` : t('Fit', '适应')}</button>
      <button type="button" aria-label={t('Zoom in', '放大')} title={t('Zoom in', '放大')} disabled={view.scale >= MAX_ZOOM} onClick={() => step(1.5)}>+</button>
    </div>
  </div>;
}

/** Code with line numbers; line, when given, is marked and scrolled into view. */
function CodeView({ code, lang: language, line }: { code: string; lang: string; line?: number }) {
  const text = code.replace(/\n$/, '');
  const html = useHighlight(text, language);
  const count = text.split('\n').length;
  const mark = useRef<HTMLDivElement>(null);
  useEffect(() => { mark.current?.scrollIntoView({ block: 'center' }); }, [line, html]);
  const numbers = useMemo(() => Array.from({ length: count }, (_, i) => i + 1).join('\n'), [count]);
  return <div className="code-view">
    <pre className="code-gutter" aria-hidden="true">{numbers}</pre>
    <div className="code-main">
      {line && line <= count && <div ref={mark} className="code-mark" style={{ top: `calc(${line - 1} * var(--code-line))` }} />}
      <pre className="code-text">{html !== undefined ? <code className="hljs" dangerouslySetInnerHTML={{ __html: html }} /> : <code>{text}</code>}</pre>
    </div>
  </div>;
}
