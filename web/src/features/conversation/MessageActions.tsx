import { useState, type ReactNode } from 'react';
import { Copy, GitBranch, MoreHorizontal, Pencil } from 'lucide-react';
import { useBackend } from '@/backend/backend';
import { Button } from '@/components/ui/button';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { Spinner } from '@/components/ui/spinner';
import { toast } from '@/components/ui/toast';
import { copyText } from '@/domain/clipboard';
import type { TranscriptEvent } from '@/domain/toolGroups';
import { t } from '@/lib/i18n';
import { useConversation } from './context';
import { useHistoryEdit } from './HistoryComposer';
import { useForkConversation } from './useForkConversation';

export function MessageActions({ event }: { event: TranscriptEvent }) {
  const backend = useBackend();
  const { session, native, mobile } = useConversation();
  const editor = useHistoryEdit();
  const { enabled, unavailable, busy, fork } = useForkConversation(session, native);
  const [loading, setLoading] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);
  const canFork = enabled && event.branchable && !!event.branchAnchor;
  const canEdit = canFork && event.kind === 'user' && !!event.editableInBranch && !!editor;
  const canCopy = !!event.text;
  const seq = event.actionSeq ?? event.seq;
  if (!canCopy && !canFork) return null;

  async function perform(action: 'copy' | 'edit') {
    if (loading) return;
    setLoading(true);
    try {
      const full = event.truncated ? await backend.fullEvent(session.id, seq, { native }) : event;
      if (action === 'copy') {
        if (full.text) {
          await copyText(full.text);
          toast(t('Message copied.', '已复制消息。'), { dedupeKey: 'message-copy' });
        }
      } else if (canEdit && !editor?.busy) {
        if (event.truncated && full.branchAnchor !== event.branchAnchor) {
          throw new Error(t('This message changed. Reload it and try again.', '这条消息已变化，请重新加载后再试。'));
        }
        const attachments = Array.isArray(full.payload?.attachments) ? full.payload.attachments : [];
        editor?.edit({ seq, token: event.branchAnchor!, text: full.text ?? '', attachments });
      }
    } catch (err) {
      toast(err instanceof Error ? err.message : String(err), { variant: 'warning' });
    } finally {
      setLoading(false);
    }
  }

  const size = mobile ? 'icon-md' : 'icon-sm';
  const shortcut = (label: string, icon: ReactNode, action: () => void, disabled = false) => (
    <Tooltip>
      <TooltipTrigger asChild><Button type="button" variant="ghost" size={size} className="text-foreground-subtle"
        aria-label={label} disabled={disabled} onClick={action}>{icon}</Button></TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
  return (
    <div data-open={menuOpen || undefined} className="flex items-center gap-0.5 transition-opacity [@media(hover:hover)]:opacity-0 group-focus-within/message:opacity-100 group-hover/message:opacity-100 data-[open=true]:opacity-100">
      {canCopy && shortcut(t('Copy message', '复制消息'), <Copy className="size-3.5" />, () => void perform('copy'), loading)}
      {canEdit && shortcut(t('Edit in a new branch', '编辑后在新分支发送'), <Pencil className="size-3.5" />,
        () => void perform('edit'), unavailable || busy || loading || editor?.busy)}
      {canFork && (
        <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
          <DropdownMenuTrigger asChild>
            <Button type="button" variant="ghost" size={size} className="text-foreground-subtle"
              aria-label={t('More message actions', '更多消息操作')}>
              {busy ? <Spinner className="size-4" /> : <MoreHorizontal className="size-4" />}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align={event.kind === 'user' ? 'end' : 'start'}>
            <DropdownMenuItem disabled={unavailable || busy || editor?.busy} onSelect={() => void fork(seq, event.branchAnchor)}>
              <GitBranch />{t('Branch from here', '从这里分支')}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </div>
  );
}
