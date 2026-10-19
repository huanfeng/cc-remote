import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
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

async function fetchFile(sessionId: string, token: string, ref: FileRequest, signal?: AbortSignal): Promise<FetchedFile> {
  const query = new URLSearchParams({ path: ref.path });
  for (const base of ref.bases || []) query.append('base', base);
  const response = await fetch(apiUrl(`/api/sessions/${encodeURIComponent(sessionId)}/file?${query}`), { headers: { Authorization: `Bearer ${token}`, 'X-CC-Lang': lang }, cache: 'no-store', signal });
  if (!response.ok) {
    let message = t(`Cannot open the file (${response.status})`, `无法打开文件 (${response.status})`);
    try { message = (await response.json()).error || message; } catch {}
    throw new Error(message);
  }
  let path = ref.path;
  try { path = decodeURIComponent(response.headers.get('X-CC-Path') || '') || path; } catch {}
  return { blob: await response.blob(), path };
}

/** Makes the host files a session names openable in a viewer (sessionId empty: none). */
export function FileProvider({ sessionId, token, children }: { sessionId: string; token: string; children: ReactNode }) {
  const [stack, setStack] = useState<FileRequest[]>([]);
  useEffect(() => setStack([]), [sessionId]);
  const access = useMemo<FileAccess | null>(() => sessionId ? {
    open: ref => setStack(old => [...old, ref]),
    fetch: (ref, signal) => fetchFile(sessionId, token, ref, signal),
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
    }).catch(err => { if (err.name !== 'AbortError') setError((err as Error).message); });
    return () => { controller.abort(); if (url) releaseImageURL(url); };
  }, [request, access]);
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => { if (event.key === 'Escape' && !event.isComposing) { event.preventDefault(); event.stopPropagation(); close(); } };
    document.addEventListener('keydown', onKey, true);
    return () => document.removeEventListener('keydown', onKey, true);
  }, [close]);

  const path = file?.path || request.path;
  const kind = fileKind(path);
  const download = () => {
    if (!file) return;
    const url = URL.createObjectURL(file.blob);
    const link = document.createElement('a'); link.href = url; link.download = basename(path); document.body.append(link); link.click(); link.remove();
    window.setTimeout(() => URL.revokeObjectURL(url), 10_000);
  };
  const copyPath = async () => { try { await copyText(path); setCopied(true); window.setTimeout(() => setCopied(false), 1600); } catch {} };
  const large = !!file?.text && file.blob.size > MAX_RENDER_BYTES;

  let body: ReactNode;
  if (error) body = <div className="error" role="alert">{error}</div>;
  else if (!file) body = <p className="file-note">{t('Loading…', '加载中…')}</p>;
  else if (file.url) body = <div className="file-image-view"><img src={file.url} alt={basename(path)} /></div>;
  else if (file.binary) body = <p className="file-note">{t('This file cannot be previewed. ', '此文件无法预览。')}<button type="button" className="tool-more" onClick={download}>{t('Download it', '下载')}</button></p>;
  else if (kind === 'markdown' && !source && !large) body = <div className="file-doc"><BasesContext.Provider value={request.bases || NO_BASES}><Markdown text={file.text!} dir={dirname(path)} /></BasesContext.Provider></div>;
  else body = <CodeView code={file.text!} lang={large ? '' : kind === 'markdown' ? 'markdown' : languageOf(path)} line={request.line} />;

  return <div className="modal-backdrop file-backdrop" onClick={close}>
    <div className="file-viewer" role="dialog" aria-modal="true" aria-label={basename(path)} onClick={event => event.stopPropagation()}>
      <div className="file-viewer-bar">
        {canGoBack && <button type="button" className="icon" title={t('Back', '返回')} aria-label={t('Back', '返回')} onClick={back}><BackIcon size={16} /></button>}
        <div className="file-viewer-title"><strong>{basename(path)}{request.line ? `:${request.line}` : ''}</strong><button type="button" className="file-viewer-path" title={t('Copy path', '复制路径')} onClick={() => void copyPath()}>{copied ? t('Path copied', '路径已复制') : path}</button></div>
        {kind === 'markdown' && file?.text !== undefined && !large && <button type="button" className={`file-toggle${source ? ' active' : ''}`} aria-pressed={source} onClick={() => setSource(!source)}>{source ? t('Rendered', '渲染') : t('Source', '源码')}</button>}
        <button type="button" className="icon" title={t('Download', '下载')} aria-label={t('Download', '下载')} disabled={!file} onClick={download}><DownloadIcon size={16} /></button>
        <button type="button" className="icon" title={t('Close', '关闭')} aria-label={t('Close file viewer', '关闭文件查看')} onClick={close}><CloseIcon size={18} /></button>
      </div>
      <div className="file-viewer-body">{body}</div>
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
