// The file viewer: a large dialog (a full-screen sheet on phones) showing a
// host file the session names — images zoomable, Markdown rendered or as
// source, code highlighted with line numbers and the `:line` marked, anything
// else as a download. Links inside a document open further files on a back
// stack; a new `path` from the surface store is pushed onto it as well.
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { Dialog as DialogPrimitive } from 'radix-ui';
import { ArrowLeft, Check, Copy, Download, FileCode2, FileImage, FileText, FileWarning, X } from 'lucide-react';
import { useBackend } from '@/backend/backend';
import type { FileContent } from '@/backend/types';
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { cn } from '@/components/lib/utils';
import { basename, dirname, type FileRef } from '@/domain/filePaths';
import { copyText } from '@/domain/clipboard';
import { useIsMobile } from '@/lib/hooks';
import { t } from '@/lib/i18n';
import { Markdown, MarkdownFiles } from '@/features/markdown/Markdown';
import { CodeView } from './CodeView';
import { ImageView } from './ImageView';
import { MAX_RENDER_BYTES, dirOf, formatBytes, shikiLang, splitLine, svgDataUrl, viewKind } from './fileView';

type Entry = { path: string; base?: string[] };
const entryKey = (e: Entry) => `${e.path}\0${(e.base ?? []).join('\0')}`;

export function FileViewer({ sessionId, path, base, onClose }: { sessionId: string; path: string; base?: string[]; onClose: () => void }) {
  const mobile = useIsMobile();
  const [stack, setStack] = useState<Entry[]>(() => [{ path, base }]);
  // The surface store may open another file while this one is shown: it goes on the back stack.
  const propKey = entryKey({ path, base });
  const seen = useRef(propKey);
  useEffect(() => {
    if (seen.current === propKey) return;
    seen.current = propKey;
    setStack(old => [...old, { path, base }]);
  }, [propKey, path, base]);
  const push = useCallback((e: Entry) => setStack(old => entryKey(old[old.length - 1]) === entryKey(e) ? old : [...old, e]), []);
  const back = useCallback(() => setStack(old => old.length > 1 ? old.slice(0, -1) : old), []);
  const top = stack[stack.length - 1];

  return (
    <Dialog open onOpenChange={open => { if (!open) onClose(); }}>
      <DialogContent
        showCloseButton={false}
        aria-describedby={undefined}
        className={cn(
          'flex flex-col gap-0 overflow-hidden bg-background p-0',
          mobile
            ? 'inset-0 top-0 left-0 h-dvh w-screen max-w-none translate-x-0 translate-y-0 rounded-none border-0 data-open:zoom-in-100 data-closed:zoom-out-100'
            : 'h-[85vh] w-[calc(100%-4rem)] max-w-5xl rounded-2xl',
        )}
      >
        <FilePane
          key={`${stack.length}:${entryKey(top)}`}
          sessionId={sessionId}
          entry={top}
          mobile={mobile}
          canGoBack={stack.length > 1}
          onBack={back}
          onOpen={push}
        />
      </DialogContent>
    </Dialog>
  );
}

type Loaded = { file: FileContent; src?: string };

function FilePane({ sessionId, entry, mobile, canGoBack, onBack, onOpen }: {
  sessionId: string; entry: Entry; mobile: boolean; canGoBack: boolean; onBack: () => void; onOpen: (e: Entry) => void;
}) {
  const backend = useBackend();
  const { path: requested, line } = useMemo(() => splitLine(entry.path), [entry.path]);
  const [loaded, setLoaded] = useState<Loaded>();
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  // A Markdown file opened at a line shows its source, where the line is.
  const [source, setSource] = useState(!!line);

  useEffect(() => {
    let live = true;
    setLoaded(undefined);
    setError('');
    backend.readFile(sessionId, requested, entry.base).then(async file => {
      let src: string | undefined;
      if (viewKind(file) === 'image') src = await imageSource(file);
      if (live) setLoaded({ file, src });
    }, err => { if (live) setError(err instanceof Error ? err.message : String(err)); });
    return () => { live = false; };
  }, [backend, sessionId, requested, entry.base, attempt]);

  const file = loaded?.file;
  const shownPath = file?.path || requested;
  const name = basename(shownPath);
  const kind = file ? viewKind(file) : undefined;
  const large = !!file?.text && Math.max(file.size, file.text.length) > MAX_RENDER_BYTES;
  // The file in hand downloads at once; one sent without content (too large, or a binary) through a host link.
  const [saving, setSaving] = useState(false);
  const download = useCallback(async () => {
    if (!file || saving) return;
    if (file.url || file.text !== undefined) { save(file, name); return; }
    setSaving(true);
    try {
      const link = await backend.fileLink(sessionId, requested, entry.base);
      saveUrl(link.url, link.name || name);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }, [file, saving, name, backend, sessionId, requested, entry.base]);

  // Links in a document resolve against its directory first, then the directories it was opened with.
  const openLink = useCallback((ref: FileRef, bases: string[]) => {
    const dirs = [...new Set([dirname(shownPath), ...bases, ...(entry.base ?? [])].filter(Boolean))];
    onOpen({ path: ref.line ? `${ref.path}:${ref.line}` : ref.path, base: dirs });
  }, [shownPath, entry.base, onOpen]);

  let body: ReactNode;
  if (error) {
    body = (
      <Notice icon={<FileWarning />} title={t('Cannot open the file', '无法打开文件')} detail={error}>
        <Button variant="outline" size="lg" onClick={() => setAttempt(a => a + 1)}>{t('Try again', '重试')}</Button>
      </Notice>
    );
  } else if (!file) {
    body = <div className="flex h-full items-center justify-center gap-2 text-ui-sm text-foreground-subtle"><Spinner className="size-3.5" />{t('Loading…', '加载中…')}</div>;
  } else if (kind === 'too-large') {
    body = (
      <Notice icon={<FileWarning />} title={t('File too large', '文件过大')} detail={t(`${formatBytes(file.size)} is over the preview limit.`, `${formatBytes(file.size)}，超过预览上限。`)}>
        <DownloadButton saving={saving} onClick={() => void download()} />
      </Notice>
    );
  } else if (kind === 'image' && loaded?.src) {
    body = <ImageView src={loaded.src} alt={name} />;
  } else if (kind === 'binary' || kind === 'image') {
    body = (
      <Notice icon={<FileText />} title={t('This file cannot be previewed', '此文件无法预览')} detail={`${file.mime || t('binary', '二进制')} · ${formatBytes(file.size)}`}>
        <DownloadButton saving={saving} onClick={() => void download()} />
      </Notice>
    );
  } else if (kind === 'markdown' && !source && !large) {
    body = (
      <div className="h-full overflow-auto">
        <MarkdownFiles.Provider value={openLink}>
          <div className={cn('mx-auto max-w-3xl', mobile ? 'px-4 py-4' : 'px-10 py-8')}>
            <Markdown text={file.text!} />
          </div>
        </MarkdownFiles.Provider>
      </div>
    );
  } else {
    body = <CodeView code={file.text ?? ''} lang={large ? '' : kind === 'markdown' ? 'markdown' : shikiLang(shownPath)} line={line} />;
  }

  return (
    <>
      <header className={cn('flex shrink-0 items-center gap-1.5 border-b border-border/60 bg-background pr-2 pl-3', mobile ? 'min-h-12 pt-safe' : 'h-12')}>
        {canGoBack && <IconButton label={t('Back', '返回')} onClick={onBack}><ArrowLeft /></IconButton>}
        <KindIcon kind={kind} />
        <DialogTitle className="flex min-w-0 flex-1 items-baseline gap-2 text-ui-base font-medium text-foreground">
          <span className="shrink-0 truncate">{name}{line ? <span className="text-foreground-subtle">:{line}</span> : null}</span>
          {!mobile && <span className="min-w-0 truncate text-ui-sm font-normal text-foreground-subtlest" title={shownPath}>{dirOf(shownPath)}</span>}
        </DialogTitle>
        {kind === 'markdown' && !large && (
          <div role="group" className={cn('mr-1 flex shrink-0 items-center rounded-lg bg-surface', mobile ? 'p-0' : 'p-0.5')}>
            {[false, true].map(isSource => (
              <button
                key={String(isSource)}
                type="button"
                aria-pressed={source === isSource}
                onClick={() => setSource(isSource)}
                className={cn(
                  'rounded-md text-ui-sm transition-colors',
                  mobile ? 'h-9 px-3' : 'h-6 px-2',
                  source === isSource ? 'bg-background text-foreground shadow-xs' : 'text-foreground-subtle hover:text-foreground',
                )}
              >
                {isSource ? t('Source', '源码') : t('Rendered', '渲染')}
              </button>
            ))}
          </div>
        )}
        <CopyPathButton path={shownPath} />
        {file && <IconButton label={t('Download', '下载')} disabled={saving} onClick={() => void download()}>{saving ? <Spinner /> : <Download />}</IconButton>}
        <DialogPrimitive.Close asChild>
          <Button variant="ghost" size="icon-md" aria-label={t('Close', '关闭')} className={cn('text-foreground-subtle hover:text-foreground', mobile && 'size-9')}><X /></Button>
        </DialogPrimitive.Close>
      </header>
      <div className="relative min-h-0 flex-1">{body}</div>
      {!mobile && file && (kind === 'code' || (kind === 'markdown' && (source || large))) && (
        <footer className="flex h-7 shrink-0 items-center gap-3 border-t border-border/60 px-4 text-ui-xs text-foreground-subtlest tabular-nums">
          <span>{t(`${countLines(file.text ?? '')} lines`, `${countLines(file.text ?? '')} 行`)}</span>
          <span>{formatBytes(file.size)}</span>
          {large && <span>{t('Shown as plain text (over 2 MiB)', '超过 2 MiB，按纯文本显示')}</span>}
          <span className="ml-auto">{large ? 'text' : kind === 'markdown' ? 'markdown' : shikiLang(shownPath) || 'text'}</span>
        </footer>
      )}
    </>
  );
}

const countLines = (text: string) => (text.replace(/\n$/, '').match(/\n/g)?.length ?? 0) + 1;

/** An image's src: SVG only ever as a data: URL, so it cannot run scripts or load anything. */
async function imageSource(file: FileContent): Promise<string | undefined> {
  const svg = file.mime === 'image/svg+xml' || /\.svg$/i.test(file.path);
  if (!svg) return file.url;
  if (file.text !== undefined) return svgDataUrl(file.text);
  if (!file.url) return undefined;
  if (file.url.startsWith('data:')) return file.url;
  const text = await (await fetch(file.url)).text();
  return svgDataUrl(text);
}

function save(file: FileContent, name: string) {
  let url = file.url;
  let made = false;
  if (!url && file.text !== undefined) { url = URL.createObjectURL(new Blob([file.text], { type: file.mime || 'text/plain' })); made = true; }
  if (!url) return;
  saveUrl(url, name);
  if (made) window.setTimeout(() => URL.revokeObjectURL(url), 10_000);
}

/** Lets the browser save a URL as a download. */
function saveUrl(url: string, name: string) {
  const a = document.createElement('a');
  a.href = url; a.download = name; a.rel = 'noopener';
  document.body.append(a); a.click(); a.remove();
}

function DownloadButton({ saving, onClick }: { saving: boolean; onClick: () => void }) {
  return (
    <Button variant="outline" size="lg" disabled={saving} onClick={onClick}>
      {saving ? <Spinner /> : <Download />}{saving ? t('Preparing…', '准备中…') : t('Download', '下载')}
    </Button>
  );
}

function CopyPathButton({ path }: { path: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try { await copyText(path); setCopied(true); window.setTimeout(() => setCopied(false), 1600); } catch { /* the path stays visible to select */ }
  };
  return <IconButton label={copied ? t('Path copied', '路径已复制') : t('Copy path', '复制路径')} onClick={() => void copy()}>{copied ? <Check /> : <Copy />}</IconButton>;
}

function IconButton({ label, disabled, onClick, children }: { label: string; disabled?: boolean; onClick: () => void; children: ReactNode }) {
  const mobile = useIsMobile();
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="ghost" size="icon-md" aria-label={label} disabled={disabled} className={cn('text-foreground-subtle hover:text-foreground [&_svg]:size-4', mobile && 'size-9')} onClick={onClick}>{children}</Button>
      </TooltipTrigger>
      <TooltipContent side="bottom">{label}</TooltipContent>
    </Tooltip>
  );
}

function KindIcon({ kind }: { kind?: string }) {
  const Icon = kind === 'image' ? FileImage : kind === 'code' ? FileCode2 : FileText;
  return <Icon className="ml-1 size-4 shrink-0 text-foreground-subtlest" aria-hidden />;
}

function Notice({ icon, title, detail, children }: { icon: ReactNode; title: string; detail?: string; children?: ReactNode }) {
  return (
    <div className="flex h-full items-center justify-center p-6">
      <div className="flex max-w-sm flex-col items-center gap-3 rounded-xl border border-border/60 bg-card px-6 py-5 text-center">
        <span className="text-foreground-subtlest [&_svg]:size-6">{icon}</span>
        <div className="text-ui-base font-medium text-foreground">{title}</div>
        {detail && <p className="text-ui-sm break-all text-foreground-subtle">{detail}</p>}
        {children}
      </div>
    </div>
  );
}
