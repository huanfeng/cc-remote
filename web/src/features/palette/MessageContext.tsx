import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { ArrowUpRight, ChevronLeft, X } from 'lucide-react';
import { useBackend } from '@/backend/backend';
import type { MessageSearchContext, MessageSearchHit } from '@/backend/types';
import { cn } from '@/components/lib/utils';
import { Button } from '@/components/ui/button';
import { providerName } from '@/domain/providers';
import { t, locale } from '@/lib/i18n';
import { setTerminalView } from '@/lib/prefs';
import { select } from '@/store/app';

const dateTime = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' });

export function messageTime(at: string) {
  const time = Date.parse(at);
  return Number.isFinite(time) ? dateTime.format(time) : '';
}

export function messageRole(kind: string) {
  return kind === 'user' ? t('You', '你') : t('Assistant', '助手');
}

export function MessageContext({ hit, onBack, onClose }: { hit: MessageSearchHit; onBack: () => void; onClose: () => void }) {
  const backend = useBackend();
  const [context, setContext] = useState<MessageSearchContext>();
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  const match = useRef<HTMLElement>(null);
  const back = useRef<HTMLButtonElement>(null);
  const title = hit.title || hit.cwd || hit.sessionId;
  const role = [providerName(hit.provider), hit.native && t('Terminal history', '终端历史')].filter(Boolean).join(' · ');

  useEffect(() => {
    const abort = new AbortController();
    setError('');
    void backend.messageContext(hit, abort.signal).then(value => {
      if (!abort.signal.aborted) setContext(value);
    }).catch((err: unknown) => {
      if (!abort.signal.aborted) setError(err instanceof Error ? err.message : String(err));
    });
    return () => abort.abort();
  }, [backend, hit, attempt]);

  useLayoutEffect(() => { back.current?.focus(); }, []);
  useLayoutEffect(() => {
    match.current?.scrollIntoView({ block: 'start', behavior: 'instant' });
  }, [context]);

  function openConversation() {
    onClose();
    if (hit.native) select({ kind: 'native', id: hit.sessionId, provider: hit.native });
    else {
      setTerminalView(hit.sessionId, false);
      select({ kind: 'session', id: hit.sessionId });
    }
  }

  return (
    <div className="flex min-h-0 flex-col overflow-hidden">
      <div className="flex items-center gap-2 border-b border-border/60 px-2 py-2">
        <Button ref={back} type="button" variant="ghost" size="icon-md" aria-label={t('Back to results', '返回搜索结果')} title={t('Back to results', '返回搜索结果')} onClick={onBack}>
          <ChevronLeft />
        </Button>
        <div className="min-w-0 flex-1">
          <h2 className="truncate text-ui-base font-medium" title={title}>{title}</h2>
          <p className="truncate text-ui-xs text-foreground-subtle" title={hit.cwd}>{role}{hit.cwd && ` · ${hit.cwd}`}</p>
        </div>
        <Button type="button" variant="ghost" size="icon-md" aria-label={t('Close', '关闭')} onClick={onClose}><X /></Button>
      </div>
      <div className="max-h-[min(65dvh,520px)] overflow-y-auto px-3 py-3 sm:px-4" aria-busy={!context && !error}>
        {error ? (
          <div className="flex flex-wrap items-center justify-between gap-2 text-ui-sm" role="alert">
            <span className="min-w-0 break-words text-destructive">{error}</span>
            <Button type="button" variant="outline" size="sm" onClick={() => setAttempt(value => value + 1)}>{t('Try again', '重试')}</Button>
          </div>
        ) : !context ? (
          <p className="py-3 text-ui-sm text-foreground-subtle" role="status">{t('Loading messages…', '正在加载消息…')}</p>
        ) : (
          <div className="flex flex-col gap-2">
            {context.events.map(event => {
              const selected = event.seq === context.matchSeq;
              return (
                <article key={event.seq} ref={selected ? match : undefined}
                  className={cn('scroll-mt-3 rounded-lg border border-border/60 px-3 py-2', selected && 'border-icon-blue/60 bg-selected')}>
                  <header className="mb-1 flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-ui-xs text-foreground-subtle">
                    <strong className="font-medium text-foreground">{messageRole(event.kind)}</strong>
                    {event.createdAt && <time dateTime={event.createdAt}>{messageTime(event.createdAt)}</time>}
                    {selected && <span className="text-icon-blue">{t('Search match', '命中消息')}</span>}
                  </header>
                  <div className="whitespace-pre-wrap break-words text-ui-base">{event.text}</div>
                </article>
              );
            })}
          </div>
        )}
      </div>
      <div className="flex justify-end border-t border-border/60 p-2">
        <Button type="button" variant="outline" size="sm" onClick={openConversation}>
          <ArrowUpRight /> {t('Open conversation', '打开会话')}
        </Button>
      </div>
    </div>
  );
}
