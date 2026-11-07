// Pieces of the composer: the inline menus (slash commands, @mentions), the
// attachment strip, the pending queue, the suggestion bar and the context ring.
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { ArrowUpRight, File, FileText, Folder, Pencil, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Spinner } from '@/components/ui/spinner';
import { cn } from '@/components/lib/utils';
import type { Attachment, PendingMessage } from '@/backend/types';
import { useAssetUrl } from '@/backend/backend';
import type { ContextReading } from '@/domain/contextReading';
import type { Provider } from '@/domain/providers';
import { locale, t } from '@/lib/i18n';
import { formatBytes } from './composerLogic';

// ---------- inline menu above the input card ----------

export interface MenuItem {
  key: string;
  icon?: ReactNode;
  label: ReactNode;
  hint?: ReactNode;
}

export function InlineMenu({ id, label, items, active, onActive, onPick, mobile, footer }: {
  id: string; label: string; items: MenuItem[]; active: number; onActive(i: number): void; onPick(i: number): void; mobile?: boolean; footer?: ReactNode;
}) {
  const list = useRef<HTMLDivElement>(null);
  useEffect(() => {
    list.current?.querySelector<HTMLElement>(`[data-index="${active}"]`)?.scrollIntoView({ block: 'nearest' });
  }, [active]);
  return (
    <div
      id={id} role="listbox" aria-label={label}
      className="absolute inset-x-0 bottom-full z-30 mb-2 overflow-hidden rounded-xl border border-popover-border bg-popover text-popover-foreground shadow-md animate-in fade-in-0 slide-in-from-bottom-1 duration-100"
    >
      <div ref={list} className={cn('max-h-72 overflow-y-auto overscroll-contain p-1', mobile && 'max-h-[min(18rem,calc(var(--app-height,100dvh)*0.38))]')}>
        {items.map((item, i) => (
          <button
            key={item.key} id={`${id}-${i}`} type="button" role="option" aria-selected={i === active} data-index={i} tabIndex={-1}
            onMouseDown={e => e.preventDefault()} onMouseMove={() => { if (i !== active) onActive(i); }} onClick={() => onPick(i)}
            className={cn(
              'flex w-full min-w-0 items-center gap-2.5 rounded-md px-2 py-1.5 text-left text-ui-base outline-none',
              i === active && 'bg-menu-hover',
              mobile && 'min-h-10',
            )}
          >
            {item.icon && <span className="flex size-4 shrink-0 items-center justify-center text-foreground-subtle">{item.icon}</span>}
            <span className="shrink-0 font-medium">{item.label}</span>
            {item.hint && <span className="min-w-0 truncate text-ui-sm text-foreground-subtle">{item.hint}</span>}
          </button>
        ))}
      </div>
      {footer && <div className="border-t border-border/60 px-3 py-1.5 text-ui-xs text-foreground-subtlest">{footer}</div>}
    </div>
  );
}

export function mentionItem(path: string): MenuItem {
  const dir = path.endsWith('/');
  const trimmed = dir ? path.slice(0, -1) : path;
  const cut = trimmed.lastIndexOf('/');
  return {
    key: path,
    icon: dir ? <Folder className="size-3.5" /> : <File className="size-3.5" />,
    label: `${trimmed.slice(cut + 1)}${dir ? '/' : ''}`,
    hint: cut > 0 ? trimmed.slice(0, cut + 1) : undefined,
  };
}

// ---------- attachments ----------

export interface Uploading {
  key: string;
  name: string;
  size: number;
  preview?: string;
  /** 0…1 once the backend reports it. */
  progress?: number;
}

/** An attachment image; host URLs need the token, so they load through the backend. */
function AttachmentImage({ attachment: a, className }: { attachment: Attachment; className?: string }) {
  const src = useAssetUrl(a.url);
  return src ? <img src={src} alt={a.name} className={className} /> : <span className={cn('block bg-surface', className)} aria-label={a.name} />;
}

export function AttachmentStrip({ attachments, uploading, onRemove, onCancel, mobile }: {
  attachments: Attachment[]; uploading: Uploading[]; onRemove?(id: string): void; onCancel?(key: string): void; mobile?: boolean;
}) {
  const [viewing, setViewing] = useState<Attachment | null>(null);
  if (!attachments.length && !uploading.length) return null;
  return (
    <div className="flex flex-wrap gap-2 px-3 pt-3">
      {attachments.map(a => {
        const image = a.kind === 'image' || a.mime.startsWith('image/');
        return image ? (
          <div key={a.id} className="group/att relative">
            <button type="button" onClick={() => setViewing(a)} className="block size-14 overflow-hidden rounded-lg border border-border/60 bg-surface" aria-label={t(`View ${a.name}`, `查看 ${a.name}`)}>
              <AttachmentImage attachment={a} className="size-full object-cover" />
            </button>
            {onRemove && <RemoveButton name={a.name} mobile={mobile} onClick={() => onRemove(a.id)} />}
          </div>
        ) : (
          <div key={a.id} className="group/att relative flex h-14 max-w-56 items-center gap-2 rounded-lg border border-border/60 bg-surface pl-2.5 pr-3">
            <FileText className="size-4 shrink-0 text-foreground-subtle" />
            <span className="flex min-w-0 flex-col">
              <span className="truncate text-ui-sm">{a.name}</span>
              <span className="text-ui-xs text-foreground-subtlest">{formatBytes(a.size)}</span>
            </span>
            {onRemove && <RemoveButton name={a.name} mobile={mobile} onClick={() => onRemove(a.id)} />}
          </div>
        );
      })}
      {uploading.map(u => (
        <div key={u.key} className="group/att relative">
          <div className="relative flex h-14 max-w-56 items-center gap-2 overflow-hidden rounded-lg border border-dashed border-border bg-surface/60 px-2.5" aria-busy>
            {u.preview ? <img src={u.preview} alt="" className="absolute inset-0 size-full object-cover opacity-40" /> : null}
            <Spinner className="relative size-3.5 text-foreground-subtle" />
            <span className="relative flex min-w-0 flex-col">
              <span className="truncate text-ui-sm">{u.name}</span>
              <span className="text-ui-xs tabular-nums text-foreground-subtlest">
                {u.progress !== undefined ? t(`Uploading ${Math.round(u.progress * 100)}%`, `上传中 ${Math.round(u.progress * 100)}%`) : t('Uploading…', '上传中…')}
              </span>
            </span>
            {u.progress !== undefined && (
              <span className="absolute inset-x-0 bottom-0 h-0.5 bg-progress-surface" aria-hidden>
                <span className="block h-full bg-brand transition-[width] duration-150" style={{ width: `${Math.round(u.progress * 100)}%` }} />
              </span>
            )}
          </div>
          {onCancel && <RemoveButton name={u.name} mobile={mobile} onClick={() => onCancel(u.key)} />}
        </div>
      ))}
      <Dialog open={!!viewing} onOpenChange={o => { if (!o) setViewing(null); }}>
        <DialogContent className="max-w-[min(92vw,1100px)] gap-2 p-2" showCloseButton>
          <DialogTitle className="sr-only">{viewing?.name}</DialogTitle>
          <DialogDescription className="sr-only">{t('Attachment preview', '附件预览')}</DialogDescription>
          {viewing && <AttachmentImage attachment={viewing} className="max-h-[80dvh] w-full rounded-xl object-contain" />}
        </DialogContent>
      </Dialog>
    </div>
  );
}

function RemoveButton({ name, onClick, mobile }: { name: string; onClick(): void; mobile?: boolean }) {
  return (
    <button
      type="button" onClick={onClick} aria-label={t(`Remove ${name}`, `移除 ${name}`)}
      className={cn(
        'absolute -right-1.5 -top-1.5 flex size-5 items-center justify-center rounded-full border border-border bg-popover text-foreground-subtle shadow-xs transition-opacity hover:text-foreground',
        // Phones: an invisible 36px hit area around the 20px badge.
        mobile ? 'before:absolute before:-inset-2 before:content-[""]' : 'opacity-0 focus-visible:opacity-100 group-hover/att:opacity-100',
      )}
    >
      <X className="size-3" />
    </button>
  );
}

// ---------- pending queue ----------

export function PendingQueue({ items, controls, onSendNow, onEdit, onRemove, mobile }: {
  items: PendingMessage[]; controls: boolean; mobile?: boolean;
  onSendNow(m: PendingMessage): Promise<void>; onEdit(m: PendingMessage, text: string): Promise<void>; onRemove(m: PendingMessage): Promise<void>;
}) {
  const [editing, setEditing] = useState<string>();
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  if (!items.length && !error) return null;

  const run = async (fn: () => Promise<void>, after?: () => void) => {
    if (busy) return;
    setBusy(true); setError('');
    try { await fn(); after?.(); } catch (e) { setError(e instanceof Error ? e.message : String(e)); } finally { setBusy(false); }
  };

  return (
    <section aria-label={t('Pending messages', '待发送消息')} className="mx-3 overflow-hidden rounded-t-xl border border-b-0 border-border/60 bg-surface/60">
      {items.length > 0 && (
        <div className="px-3 pt-2 text-ui-xs font-medium text-foreground-subtle">
          {t(`${items.length} queued · sent after the current task`, `${items.length} 条消息排队中 · 当前任务完成后发送`)}
        </div>
      )}
      <ul className="flex max-h-44 flex-col overflow-y-auto px-1.5 pb-1.5 pt-1">
        {items.map((m, i) => {
          const isEditing = !!m.id && editing === m.id;
          const note = !m.editable
            ? m.delivery === 'unknown'
              ? t('Delivery could not be confirmed. It will not be resent automatically; removing it only dismisses this notice.', '无法确认是否送达，不会自动重发；移除仅清除此提示。')
              : m.delivery === 'pending' ? t('Confirming delivery…', '正在确认送达…') : t('Delivered; waiting for the agent to take it.', '已投递，等待处理。')
            : undefined;
          return (
            <li key={m.id ?? `legacy-${i}`} className="group/q flex items-start gap-2 rounded-lg px-1.5 py-1 hover:bg-surface-hover">
              <span className="mt-[7px] size-1.5 shrink-0 rounded-full bg-foreground-subtlest/60" />
              <div className="min-w-0 flex-1">
                {isEditing ? (
                  <textarea
                    autoFocus value={text} disabled={busy} rows={2} aria-label={t('Edit pending message', '修改待发送消息')}
                    onChange={e => setText(e.target.value)}
                    onKeyDown={e => {
                      if (e.key === 'Escape') { e.preventDefault(); setEditing(undefined); }
                      if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing && e.keyCode !== 229) {
                        e.preventDefault();
                        if (text.trim()) void run(() => onEdit(m, text.trim()), () => setEditing(undefined));
                      }
                    }}
                    className={cn('w-full resize-none rounded-md border border-input-border bg-input px-2 py-1 text-ui-sm outline-none focus:border-input-border-focused', mobile && 'text-mobile-input-safe')}
                  />
                ) : (
                  <p className="line-clamp-2 whitespace-pre-wrap break-words text-ui-sm text-foreground">{m.text}</p>
                )}
                {!!m.payload?.attachments?.length && (
                  <p className="text-ui-xs text-foreground-subtlest">{t(`${m.payload.attachments.length} attachment(s)`, `${m.payload.attachments.length} 个附件`)}</p>
                )}
                {note && <p className="text-ui-xs text-foreground-subtlest">{note}</p>}
              </div>
              {controls && m.id && (
                <div className={cn('flex shrink-0 items-center gap-0.5', !mobile && !isEditing && 'opacity-60 group-hover/q:opacity-100 focus-within:opacity-100')}>
                  {isEditing ? (
                    <>
                      <Button size={mobile ? 'lg' : 'sm'} variant="ghost" className={cn(mobile && 'h-9')} disabled={busy} onClick={() => setEditing(undefined)}>{t('Cancel', '取消')}</Button>
                      <Button size={mobile ? 'lg' : 'sm'} variant="secondary" className={cn(mobile && 'h-9')} disabled={busy || !text.trim()} onClick={() => void run(() => onEdit(m, text.trim()), () => setEditing(undefined))}>{t('Save', '保存')}</Button>
                    </>
                  ) : (
                    <>
                      {m.canSendNow && (
                        <Button size={mobile ? 'lg' : 'sm'} variant="ghost" className={cn(mobile && 'h-9')} disabled={busy} onClick={() => void run(() => onSendNow(m))}>
                          <ArrowUpRight className="size-3" />{t('Send now', '立即发送')}
                        </Button>
                      )}
                      {m.editable && (
                        <Button size={mobile ? 'icon-lg' : 'icon-sm'} variant="ghost" className={cn(mobile && 'size-9')} disabled={busy} aria-label={t('Edit', '编辑')} title={t('Edit', '编辑')}
                          onClick={() => { setEditing(m.id); setText(m.text); setError(''); }}>
                          <Pencil className="size-3" />
                        </Button>
                      )}
                      {(m.editable || m.delivery === 'unknown') && (
                        <Button size={mobile ? 'icon-lg' : 'icon-sm'} variant="ghost" className={cn(mobile && 'size-9')} disabled={busy}
                          aria-label={m.delivery === 'unknown' ? t('Dismiss delivery notice', '清除投递提示') : t('Remove pending message', '移除待发送消息')}
                          onClick={() => void run(() => onRemove(m))}>
                          <X className="size-3.5" />
                        </Button>
                      )}
                    </>
                  )}
                </div>
              )}
            </li>
          );
        })}
      </ul>
      {error && (
        <p role="alert" className="flex items-center gap-2 border-t border-border/60 px-3 py-1.5 text-ui-xs text-destructive">
          <span className="flex-1">{error}</span>
          <button type="button" aria-label={t('Dismiss', '关闭')} onClick={() => setError('')} className={cn('-my-1 flex shrink-0 items-center justify-center rounded-md text-foreground-subtle hover:bg-hover hover:text-foreground', mobile ? 'size-9' : 'size-6')}><X className="size-3.5" /></button>
        </p>
      )}
    </section>
  );
}

// ---------- suggestion ----------

export function SuggestionBar({ text, busy, onSend, onEdit, mobile }: { text: string; busy: boolean; onSend(): void; onEdit(): void; mobile?: boolean }) {
  return (
    <div className="mb-2 flex min-w-0 items-center gap-1.5 px-1">
      <span className="shrink-0 text-ui-sm text-foreground-subtlest">{t('Suggested', '建议')}：</span>
      <button
        type="button" disabled={busy} onClick={onSend} title={t('Send this suggestion', '发送这条建议')}
        className={cn('min-w-0 truncate rounded-full border border-border/60 bg-surface px-3 py-1 text-ui-sm text-foreground transition-colors hover:bg-surface-hover disabled:opacity-50', mobile && 'py-2')}
      >
        {text}
      </button>
      <Button variant="ghost" size={mobile ? 'lg' : 'sm'} className={cn(mobile && 'h-9')} disabled={busy} onClick={onSend}>{t('Send', '发送')}</Button>
      <Button variant="ghost" size={mobile ? 'lg' : 'sm'} className={cn(mobile && 'h-9')} onClick={onEdit}>{t('Edit', '编辑')}</Button>
    </div>
  );
}

// ---------- context ring ----------

export function ContextRing({ reading, provider, mobile }: { reading: ContextReading; provider: Provider; mobile?: boolean }) {
  const known = reading.percent !== undefined;
  const pct = known ? Math.round(reading.percent!) : undefined;
  const warn = known && reading.percent! >= 80;
  const critical = known && reading.percent! >= 95;
  const tone = critical ? 'destructive' : warn ? 'warning' : undefined;
  const n = (v?: number) => (v === undefined ? t('Not reported', '未报告') : v.toLocaleString(locale));
  // A data ring, not a spinner: a full track always, and the used share drawn
  // from 12 o'clock. Without a reading only the track shows.
  const r = 7, c = 2 * Math.PI * r;
  const arc = known ? Math.max(0, Math.min(100, reading.percent!)) : 0;
  const label = t(`Context: ${known ? `${pct}%` : 'unknown'}`, `上下文：${known ? `${pct}%` : '未知'}`);
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button" aria-label={label} title={label}
          className={cn('flex shrink-0 items-center justify-center rounded-full text-foreground-subtle transition-colors hover:bg-hover hover:text-foreground', mobile ? 'size-9' : 'size-7')}
        >
          <svg viewBox="0 0 18 18" className="size-[18px]" aria-hidden>
            <circle cx="9" cy="9" r={r} fill="none" strokeWidth="2.25" className="stroke-border-hover" />
            {arc > 0 && (
              <circle cx="9" cy="9" r={r} fill="none" strokeWidth="2.25" strokeLinecap={arc >= 100 ? 'butt' : 'round'}
                transform="rotate(-90 9 9)"
                strokeDasharray={`${(c * arc) / 100} ${c}`}
                className={cn('transition-[stroke-dasharray] duration-500', tone === 'destructive' ? 'stroke-destructive' : tone === 'warning' ? 'stroke-warning' : 'stroke-foreground-subtle')} />
            )}
          </svg>
        </button>
      </PopoverTrigger>
      <PopoverContent side="top" align="end" className="w-64 gap-2 p-3">
        <div className="flex items-baseline justify-between">
          <span className="text-ui-base font-medium">{t('Context window', '上下文窗口')}</span>
          <span className={cn('text-ui-base tabular-nums', tone === 'warning' && 'text-warning', tone === 'destructive' && 'text-destructive')}>{known ? `${pct}%` : '—'}</span>
        </div>
        {known && (
          <div className="h-1 overflow-hidden rounded-full bg-surface">
            <div className={cn('h-full rounded-full', tone === 'destructive' ? 'bg-destructive' : tone === 'warning' ? 'bg-warning' : 'bg-foreground-subtle')} style={{ width: `${reading.percent}%` }} />
          </div>
        )}
        <dl className="grid grid-cols-[1fr_auto] gap-x-3 gap-y-1 text-ui-sm">
          <dt className="text-foreground-subtle">{t('Tokens', 'Token')}</dt><dd className="text-right tabular-nums">{n(reading.tokens)}</dd>
          <dt className="text-foreground-subtle">{t('Capacity', '容量')}</dt><dd className="text-right tabular-nums">{n(reading.capacity)}</dd>
          {reading.total && (
            <>
              <dt className="text-foreground-subtle">{t('Session total', '会话累计')}</dt><dd className="text-right tabular-nums">{n(reading.total.totalTokens)}</dd>
              <dt className="text-foreground-subtle">{t('Input', '输入')}</dt><dd className="text-right tabular-nums">{n(reading.total.inputTokens)}</dd>
              <dt className="text-foreground-subtle">{t('Output', '输出')}</dt><dd className="text-right tabular-nums">{n(reading.total.outputTokens)}</dd>
              <dt className="text-foreground-subtle">{t('Cached input', '缓存输入')}</dt><dd className="text-right tabular-nums">{n(reading.total.cachedInputTokens)}</dd>
            </>
          )}
        </dl>
        <p className="text-ui-xs text-foreground-subtlest">
          {provider === 'codex'
            ? t('From the latest model call Codex reported; totals include earlier calls.', '根据 Codex 报告的最近一次模型调用；累计包含此前调用。')
            : t('Reported by Claude; missing readings stay unknown.', '由 Claude 报告；未提供的读数显示为未知。')}
        </p>
      </PopoverContent>
    </Popover>
  );
}
