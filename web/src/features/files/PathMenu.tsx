import { useState, type ReactElement } from 'react';
import { ChevronDown, Copy, ExternalLink, FileText, FolderInput, FolderOpen, LoaderCircle, LocateFixed, SquareTerminal } from 'lucide-react';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { toast } from '@/components/ui/toast';
import { copyText } from '@/domain/clipboard';
import { basename, dirname } from '@/domain/filePaths';
import { apiBase } from '@/lib/hosts';
import { t } from '@/lib/i18n';
import { canUseLocalPaths, isAbsoluteLocalPath, openLocalPath, type PathAction } from '@/platform/pathActions';

/** Host paths stay useful in browsers; native actions are explicitly local. */
export function PathMenu({ path, kind = 'file', children, onOpenFile }: { path: string; kind?: 'file' | 'directory'; children?: ReactElement; onOpenFile?: () => void }) {
  const [pending, setPending] = useState<PathAction | null>(null);
  const parent = dirname(path.replace(/[\\/]+$/, '')) || dirname(path);
  const local = canUseLocalPaths(apiBase) && isAbsoluteLocalPath(path);
  const copy = async (value: string) => {
    try {
      await copyText(value);
      toast(t('Copied', '已复制'), { variant: 'success' });
    } catch {
      toast(t('Could not copy the path', '无法复制路径'), { variant: 'warning' });
    }
  };
  const act = async (action: PathAction) => {
    if (pending) return;
    setPending(action);
    try {
      await openLocalPath(apiBase, action === 'open' && kind === 'file' ? parent : path, action);
    } catch (error) {
      toast(t(`Could not open the local path: ${String(error)}`, `无法打开本机路径：${String(error)}`), { variant: 'warning' });
    } finally {
      setPending(null);
    }
  };

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        {children ?? (
          <Button variant="ghost" size="icon-md" disabled={!!pending} aria-label={t('Path actions', '路径操作')} title={t('Path actions', '路径操作')} className="w-9 text-foreground-subtle hover:text-foreground">
            {pending ? <LoaderCircle className="size-4 animate-spin" /> : <FolderOpen className="size-4" />}
            <ChevronDown className="size-2.5" />
          </Button>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="max-w-[min(24rem,calc(100vw-1rem))]">
        <DropdownMenuLabel className="max-w-80 whitespace-normal break-all font-mono text-ui-xs font-normal text-foreground-subtle">{path}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={() => void copy(path)}><Copy />{t('Copy full path', '复制完整路径')}</DropdownMenuItem>
        <DropdownMenuItem onSelect={() => void copy(basename(path))}><FileText />{t('Copy name', '复制名称')}</DropdownMenuItem>
        {parent && parent !== path && <DropdownMenuItem onSelect={() => void copy(parent)}><FolderInput />{t('Copy parent directory', '复制上级目录')}</DropdownMenuItem>}
        {local && <>
          <DropdownMenuSeparator />
          {kind === 'file' && onOpenFile && <DropdownMenuItem onSelect={onOpenFile}><ExternalLink />{t('Open on this computer', '在本机打开')}</DropdownMenuItem>}
          <DropdownMenuItem disabled={!!pending} onSelect={() => void act('open')}><FolderOpen />{t('Open folder on this computer', '在本机打开目录')}</DropdownMenuItem>
          <DropdownMenuItem disabled={!!pending} onSelect={() => void act('reveal')}><LocateFixed />{t('Reveal in file manager', '在文件管理器中定位')}</DropdownMenuItem>
          <DropdownMenuItem disabled={!!pending} onSelect={() => void act('terminal')}><SquareTerminal />{t('Open terminal here', '在此目录打开终端')}</DropdownMenuItem>
        </>}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
