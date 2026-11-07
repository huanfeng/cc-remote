// "Attach from a local terminal": the ccr command that joins this terminal session.
import { SquareTerminal } from 'lucide-react';
import { Kbd } from '@/components/ui/kbd';
import { useApp } from '@/store/app';
import { t } from '@/lib/i18n';
import { SurfaceShell } from './SurfaceShell';
import { CopyField } from './common';

export function AttachDialog({ open, onOpenChange, mobile, id }: { open: boolean; onOpenChange: (o: boolean) => void; mobile: boolean; id: string }) {
  const info = useApp(s => s.info);
  const command = `${info?.attachCommand || 'ccr attach'} ${id.slice(0, 8)}`;
  return (
    <SurfaceShell
      open={open}
      onOpenChange={onOpenChange}
      mobile={mobile}
      mobileVariant="sheet"
      title={t('Attach from a local terminal', '从本地终端接入')}
      description={t('Run this on the host to continue the session in a terminal, just like running Claude Code directly.', '在宿主上运行以下命令，即可在本地终端继续此会话，和直接运行 Claude Code 一样。')}
      className="sm:max-w-[480px]"
    >
      <div className="flex flex-col gap-4">
        <div className="flex items-center gap-2">
          <SquareTerminal className="size-4 shrink-0 text-foreground-subtle" />
          <CopyField value={command} className="flex-1" />
        </div>
        <ul className="flex flex-col gap-2 text-ui-sm leading-5 text-foreground-subtle">
          <li className="flex gap-2">
            <span className="shrink-0"><Kbd>Ctrl ]</Kbd></span>
            <span>{t('detaches. Claude Code and its subagents keep running; switch between the web and the terminal any time.', '断开连接。Claude Code 和其中的子代理会继续运行，网页与终端可随时切换。')}</span>
          </li>
          <li className="flex gap-2">
            <code className="shrink-0 rounded-md bg-surface px-1.5 font-mono text-ui-xs leading-5 text-foreground">ccr ls</code>
            <span>{t('lists recent sessions to pick from; plain ccr in a project directory attaches to its terminal session.', '列出最近的会话供选择；在项目目录直接运行 ccr 会接入该目录的终端会话。')}</span>
          </li>
        </ul>
      </div>
    </SurfaceShell>
  );
}
