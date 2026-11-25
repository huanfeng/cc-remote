import { useEffect, useState } from 'react';
import { FileText, X } from 'lucide-react';
import { useBackend } from '@/backend/backend';
import type { ProjectFileListing } from '@/backend/types';
import { cn } from '@/components/lib/utils';
import { Button } from '@/components/ui/button';
import { Command, CommandDialog, CommandInput, CommandItem, CommandList } from '@/components/ui/command';
import { Spinner } from '@/components/ui/spinner';
import { t } from '@/lib/i18n';
import { useApp } from '@/store/app';
import { fileDockStore } from './fileDockState';

type Result = { query: string; attempt: number } & ({ listing: ProjectFileListing } | { error: string });

export function QuickOpenProjectFile({ id, open, onOpenChange, mobile }: {
  id: string; open: boolean; onOpenChange(open: boolean): void; mobile: boolean;
}) {
  const backend = useBackend();
  const cwd = useApp(s => s.sessions.find(session => session.id === id)?.cwd);
  const [query, setQuery] = useState('');
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<Result>();
  const q = query.trim();
  const current = result?.query === q && result.attempt === attempt ? result : undefined;
  const listing = current && 'listing' in current ? current.listing : undefined;
  const entries = listing?.entries ?? [];
  const title = t('Quick open project files', '快速打开项目文件');

  useEffect(() => {
    if (!open) { setQuery(''); setResult(undefined); return; }
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      backend.searchProjectFiles(id, q, controller.signal).then(listing => {
        if (!controller.signal.aborted) setResult({ query: q, attempt, listing });
      }, error => {
        if (!controller.signal.aborted) setResult({ query: q, attempt, error: error instanceof Error ? error.message : String(error) });
      });
    }, q ? 120 : 0);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [backend, id, open, q, attempt]);

  const pick = (path: string) => {
    const { selection } = useApp.getState();
    if (selection?.kind === 'session' && selection.id === id) {
      fileDockStore(backend).getState().open(id, { kind: 'file', path, scope: 'project' });
    }
    onOpenChange(false);
  };

  return (
    <CommandDialog open={open} onOpenChange={onOpenChange} title={title}
      description={t('Find a server file by name or relative path', '按文件名或相对路径查找服务器文件')}
      className={cn('max-w-xl rounded-2xl', mobile ? 'top-[max(env(safe-area-inset-top),12px)] max-w-[calc(100%-1.5rem)]' : 'top-[18vh]')}>
      <Command loop shouldFilter={false}>
        <div className="flex items-center border-b border-border [&>[data-slot=command-input-wrapper]]:min-w-0 [&>[data-slot=command-input-wrapper]]:flex-1 [&>[data-slot=command-input-wrapper]]:border-0">
          <CommandInput autoFocus value={query} onValueChange={setQuery} aria-label={title} maxLength={512}
            placeholder={t('Type a file name or path…', '输入文件名或路径…')}
            className={cn(mobile && 'text-mobile-input-safe')} inputGroupClassName={mobile ? 'h-11' : 'h-10'} />
          <Button type="button" variant="ghost" size="icon-md" className="mr-1 shrink-0" aria-label={t('Close', '关闭')} onClick={() => onOpenChange(false)}><X /></Button>
        </div>
        {cwd && <div className="truncate px-3 pt-2 text-ui-xs text-foreground-subtle" title={cwd}>{cwd}</div>}
        <CommandList className="p-1">
          {entries.map(entry => (
            <CommandItem key={entry.path} value={JSON.stringify(entry.path)} onSelect={() => pick(entry.path)} title={entry.path}
              className="min-h-9 max-md:min-h-11">
              <FileText className="size-4 text-foreground-subtle" aria-hidden />
              <span className="shrink-0 max-w-[56%] truncate">{entry.name}</span>
              <span className="ml-auto min-w-0 truncate text-ui-xs text-foreground-subtle">{entry.path === entry.name ? '' : entry.path}</span>
            </CommandItem>
          ))}
        </CommandList>
        {!current && <div role="status" className="flex items-center gap-2 px-3 pb-3 text-ui-sm text-foreground-subtle"><Spinner className="size-3.5" />{t('Searching files…', '正在查找文件…')}</div>}
        {current && 'error' in current && <div className="space-y-2 px-3 pb-3 text-ui-sm"><p role="alert">{current.error}</p><Button type="button" variant="outline" size="sm" onClick={() => setAttempt(n => n + 1)}>{t('Try again', '重试')}</Button></div>}
        {listing && entries.length === 0 && <p role="status" className="px-3 pb-3 text-ui-sm text-foreground-subtle">{q ? t('No matching files', '没有匹配的文件') : t('No project files found', '未找到项目文件')}</p>}
        {listing?.truncated && <p className="border-t border-border/60 px-3 py-2 text-ui-xs text-foreground-subtle">{t('Showing partial results. Enter a more specific name or path.', '当前显示部分结果，可输入更具体的名称或路径。')}</p>}
      </Command>
    </CommandDialog>
  );
}
