// Message-like transcript items: the user's bubble, the assistant's reply,
// errors, compaction dividers, recaps and teammate messages.
import { memo, useState } from 'react';
import { ChevronRightIcon, FileIcon, TriangleAlertIcon, UsersIcon, XIcon } from 'lucide-react';
import type { Attachment } from '@/backend/types';
import { useAssetUrl } from '@/backend/backend';
import { compactSummary, type TranscriptEvent } from '@/domain/toolGroups';
import { providerName, providerOf } from '@/domain/providers';
import { usePrefs } from '@/lib/prefs';
import { cn } from '@/components/lib/utils';
import { t } from '@/lib/i18n';
import { Markdown } from '@/features/markdown/Markdown';
import { slashCommand } from './fold';
import { useConversation } from './context';
import { Expand } from './tools/ToolGroup';

const formatSize = (n: number) => n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MB` : n >= 1024 ? `${Math.round(n / 1024)} KB` : `${n} B`;
const isImage = (a: Attachment) => a.kind === 'image' || a.mime?.startsWith('image/');

function Attachments({ items, mobile }: { items: Attachment[]; mobile: boolean }) {
  const images = items.filter(isImage), files = items.filter(a => !isImage(a));
  return (
    <div className="flex flex-wrap justify-end gap-1.5">
      {images.map(a => <AttachmentThumb key={a.id} attachment={a} mobile={mobile} />)}
      {files.map(a => (
        <span key={a.id} title={a.name} className="inline-flex h-8 max-w-56 items-center gap-1.5 rounded-lg border border-border/60 bg-card px-2 text-ui-sm">
          <FileIcon className="size-3.5 shrink-0 text-foreground-subtlest" />
          <span className="min-w-0 truncate text-foreground">{a.name}</span>
          {a.size > 0 && <span className="shrink-0 text-foreground-subtlest">{formatSize(a.size)}</span>}
        </span>
      ))}
    </div>
  );
}

/** Host attachment URLs need the token: the image loads through the backend as an object URL. */
function AttachmentThumb({ attachment: a, mobile }: { attachment: Attachment; mobile: boolean }) {
  const src = useAssetUrl(a.url);
  const box = cn('block overflow-hidden rounded-lg border border-border/60 bg-surface transition-opacity hover:opacity-90', mobile ? 'size-20' : 'size-24');
  if (!src) return <span className={box} title={a.name} aria-label={a.name} />;
  return (
    <a href={src} target="_blank" rel="noopener noreferrer" title={a.name} className={box}>
      <img src={src} alt={a.name} loading="lazy" className="size-full object-cover" />
    </a>
  );
}

export const UserMessage = memo(function UserMessage({ event }: { event: TranscriptEvent }) {
  const { mobile } = useConversation();
  const attachments: Attachment[] = Array.isArray(event.payload?.attachments) ? event.payload.attachments : [];
  const command = slashCommand(event.text);
  const text = command ? command.rest : event.text || '';
  return (
    <div data-turn-seq={event.seq} className="flex scroll-mt-4 flex-col items-end gap-1.5 pt-2">
      {event.payload?.steer && <span className="text-ui-sm text-foreground-subtlest">{t('Steered into the running turn', '已插入当前轮次')}</span>}
      {attachments.length > 0 && <Attachments items={attachments} mobile={mobile} />}
      {(command || text) && (
        <div className={cn('rounded-xl rounded-tr-sm bg-user-message-bubble px-3.5 py-2 text-foreground', mobile ? 'max-w-[90%]' : 'max-w-[85%]')}>
          {command && (
            <span className={cn('inline-flex items-center rounded-md bg-background/70 px-1.5 py-px font-mono text-[12.5px] text-icon-blue', text && 'mb-1')}>
              /{command.name}
            </span>
          )}
          {text && <Markdown text={text} className="[&_.cc-code]:my-2" />}
        </div>
      )}
    </div>
  );
});

export const AssistantMessage = memo(function AssistantMessage({ event, streaming, opener }: { event: TranscriptEvent; streaming: boolean; opener: boolean }) {
  const { session } = useConversation();
  const provider = providerOf(session);
  const model = session.live?.model || session.model;
  return (
    <div className="min-w-0">
      {opener && (
        <div className="mb-1.5 flex items-center gap-1.5 text-ui-sm text-foreground-subtlest">
          <span className={cn('size-1.5 rounded-full', provider === 'codex' ? 'bg-foreground-subtle' : 'bg-brand')} />
          <span className="font-medium text-foreground-subtle">{providerName(provider)}</span>
          {model && <span>· {model}</span>}
        </div>
      )}
      <Markdown text={event.text || ''} streaming={streaming} />
    </div>
  );
});

export function ErrorCallout({ event }: { event: TranscriptEvent }) {
  return (
    <div role="alert" className="flex items-start gap-2 rounded-xl bg-destructive-surface px-3 py-2.5 text-destructive-surface-foreground">
      <TriangleAlertIcon className="mt-0.5 size-4 shrink-0" />
      <p className="min-w-0 text-ui-base whitespace-pre-wrap break-words">{event.text || t('An error occurred.', '出现错误。')}</p>
    </div>
  );
}

export function CompactDivider({ event }: { event: TranscriptEvent }) {
  return (
    <div className="flex items-center gap-3 py-1 text-ui-sm text-foreground-subtlest" role="separator">
      <span className="h-px flex-1 bg-border/60" />
      <span className="shrink-0">{compactSummary(event.payload)}</span>
      <span className="h-px flex-1 bg-border/60" />
    </div>
  );
}

export function Recap({ event }: { event: TranscriptEvent }) {
  const { session } = useConversation();
  const key = `${session.id}:${event.seq}`;
  const hidden = usePrefs(s => s.hiddenRecaps.includes(key));
  if (hidden) return null;
  const dismiss = () => {
    const { hiddenRecaps, set } = usePrefs.getState();
    set({ hiddenRecaps: [...hiddenRecaps.filter(entry => typeof entry === 'string').slice(-199), key] });
  };
  return (
    <aside className="relative rounded-xl border border-border/60 bg-surface/60 py-2.5 pr-9 pl-3.5">
      <div className="mb-0.5 text-ui-sm font-medium text-foreground-subtle">{t('While you were away…', '你离开期间…')}</div>
      <Markdown text={(event.text || '').replace(/^(While you were away|你离开期间)[:：]\s*/, '')} className="text-foreground-subtle" />
      <button type="button" onClick={dismiss} aria-label={t('Dismiss', '关闭')}
        className="absolute top-2 right-2 flex size-6 items-center justify-center rounded-md text-foreground-subtlest transition-colors hover:bg-surface-hover hover:text-foreground">
        <XIcon className="size-3.5" />
      </button>
    </aside>
  );
}

// Claude Code names a teammate's colour; mapped onto the semantic tokens.
const TEAMMATE_DOT: Record<string, string> = {
  red: 'bg-destructive', orange: 'bg-warning', yellow: 'bg-warning', green: 'bg-success',
  blue: 'bg-icon-blue', cyan: 'bg-icon-blue', purple: 'bg-icon-purple', pink: 'bg-icon-purple',
};

export function TeammateMessage({ event }: { event: TranscriptEvent }) {
  const [open, setOpen] = useState(false);
  const p = event.payload ?? {};
  const name = p.name || p.teammate_id || t('teammate', '队友');
  return (
    <div className="rounded-xl border border-border/60 bg-card">
      <button type="button" aria-expanded={open} onClick={() => setOpen(!open)}
        className="flex w-full min-w-0 items-center gap-2 rounded-xl px-3 py-2 text-left text-ui-base transition-colors hover:bg-surface-hover/60">
        <UsersIcon className="size-3.5 shrink-0 text-foreground-subtlest" />
        <span className={cn('size-2 shrink-0 rounded-full', TEAMMATE_DOT[p.color] || 'bg-foreground-subtlest')} />
        <span className="shrink-0 font-medium text-foreground">@{name}</span>
        <span className="min-w-0 truncate text-foreground-subtle">{p.summary || t('Message from a teammate', '队友发来的消息')}</span>
        <ChevronRightIcon className={cn('ml-auto size-3.5 shrink-0 text-foreground-subtlest transition-transform duration-200', open && 'rotate-90')} />
      </button>
      <Expand open={open}><div className="border-t border-border/60 px-3 py-2.5"><Markdown text={event.text || ''} /></div></Expand>
    </div>
  );
}
