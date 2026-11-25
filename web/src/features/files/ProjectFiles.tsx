import { useEffect, useRef, useState } from 'react';
import { ArrowUp, ChevronRight, FileText, Folder, RefreshCw, Search } from 'lucide-react';
import { useBackend } from '@/backend/backend';
import type { ProjectFileListing } from '@/backend/types';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { basename } from '@/domain/filePaths';
import { t } from '@/lib/i18n';
import { ViewerIconButton } from './ViewerIconButton';

type DirectoryRequest = { path: string };
type DirectoryResult = { request: DirectoryRequest } & ({ listing: ProjectFileListing } | { error: string });

export function ProjectFiles({ sessionId, cwd, onOpen, onQuickOpen, visible }: {
  sessionId: string; cwd: string; onOpen: (path: string) => void; onQuickOpen?: () => void; visible: boolean;
}) {
  const backend = useBackend();
  const [request, setRequest] = useState<DirectoryRequest>({ path: '' });
  const [result, setResult] = useState<DirectoryResult>();
  const breadcrumbs = useRef<HTMLElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const current = result?.request === request ? result : undefined;
  const parts = request.path ? request.path.split('/') : [];
  const root = basename(cwd.replace(/[\\/]+$/, '')) || cwd || t('Project', '项目');

  useEffect(() => {
    if (!visible) return;
    const controller = new AbortController();
    backend.projectFiles(sessionId, request.path, controller.signal).then(listing => {
      if (!controller.signal.aborted) setResult({ request, listing });
    }, error => {
      if (!controller.signal.aborted) setResult({ request, error: error instanceof Error ? error.message : String(error) });
    });
    return () => controller.abort();
  }, [backend, sessionId, request, visible]);

  useEffect(() => {
    const nav = breadcrumbs.current;
    if (visible && nav) nav.scrollLeft = nav.scrollWidth;
  }, [request.path, visible]);

  const navigate = (path: string) => {
    if (list.current) {
      list.current.scrollTop = 0;
      list.current.focus({ preventScroll: true });
    }
    setRequest({ path });
  };
  const refresh = () => setRequest({ path: request.path });
  const currentDirectory = parts.at(-1) ?? root;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex h-9 shrink-0 items-center gap-0.5 border-b border-border/60 px-1">
        <ViewerIconButton label={t('Parent folder', '上级目录')} disabled={!parts.length}
          onClick={() => navigate(parts.slice(0, -1).join('/'))}><ArrowUp aria-hidden /></ViewerIconButton>
        <nav ref={breadcrumbs} aria-label={t('Project path', '项目路径')} className="min-w-0 flex-1 overflow-x-auto [scrollbar-width:none]">
          <ol className="flex w-max min-w-full items-center gap-0.5 px-1 text-ui-sm text-foreground-subtle">
            {['', ...parts].map((name, index) => {
              const path = parts.slice(0, index).join('/');
              const title = index === 0 ? cwd : path;
              const label = index === 0 ? root : name;
              return (
                <li key={path} className="flex items-center gap-0.5">
                  {index > 0 && <ChevronRight className="size-3 shrink-0 text-foreground-subtlest" aria-hidden />}
                  {index === parts.length
                    ? <span aria-current="location" title={title} className="max-w-44 truncate px-1 font-medium text-foreground">{label}</span>
                    : <button type="button" title={title} onClick={() => navigate(path)}
                      className="max-w-40 truncate rounded px-1 py-0.5 hover:bg-surface-hover hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring">{label}</button>}
                </li>
              );
            })}
          </ol>
        </nav>
        {onQuickOpen && <ViewerIconButton label={t('Quick open project files (Ctrl/⌘+P)', '快速打开项目文件（Ctrl/⌘+P）')} onClick={onQuickOpen}><Search aria-hidden /></ViewerIconButton>}
        <ViewerIconButton label={t('Refresh folder', '刷新目录')} disabled={!current} onClick={refresh}><RefreshCw aria-hidden /></ViewerIconButton>
      </div>

      <div ref={list} role="region" tabIndex={-1} aria-label={currentDirectory}
        className="min-h-0 flex-1 overflow-auto outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-ring">
        {!current ? (
          <div role="status" className="flex items-center gap-2 px-4 py-3 text-ui-sm text-foreground-subtle">
            <Spinner className="size-3.5" aria-hidden />{t('Loading files…', '正在加载文件…')}
          </div>
        ) : 'error' in current ? (
          <div className="space-y-2 px-4 py-3 text-ui-sm">
            <p role="alert" className="break-words text-foreground-subtle">{current.error || t('Could not open this folder', '无法打开此目录')}</p>
            <Button type="button" variant="outline" size="sm" onClick={() => { list.current?.focus(); refresh(); }}>{t('Try again', '重试')}</Button>
          </div>
        ) : (
          <>
            {current.listing.entries.length === 0 ? (
              <p role="status" className="px-4 py-3 text-ui-sm text-foreground-subtle">{t('This folder is empty', '此目录为空')}</p>
            ) : (
              <ul className="space-y-0.5 px-1 py-1">
                {current.listing.entries.map(entry => {
                  const directory = entry.kind === 'directory';
                  const Icon = directory ? Folder : FileText;
                  return (
                    <li key={entry.path}>
                      <button type="button" title={entry.path} onClick={() => directory ? navigate(entry.path) : onOpen(entry.path)}
                        className="flex min-h-8 w-full min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-left text-ui-sm hover:bg-surface-hover focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-ring">
                        <Icon className="size-4 shrink-0 text-foreground-subtle" aria-hidden />
                        <span className="min-w-0 flex-1 truncate">{entry.name}</span>
                        {directory && <ChevronRight className="size-3.5 shrink-0 text-foreground-subtle" aria-hidden />}
                      </button>
                    </li>
                  );
                })}
              </ul>
            )}
            {current.listing.truncated && <p className="border-t border-border/60 px-4 py-2 text-ui-xs text-foreground-subtle">
              {t('Only some entries are shown in this large folder.', '此目录内容较多，仅显示部分条目。')}
            </p>}
          </>
        )}
      </div>
    </div>
  );
}
