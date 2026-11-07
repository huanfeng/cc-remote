// The transcript of a session: a centred column that stays pinned to the
// bottom while output streams (use-stick-to-bottom), loads older pages when
// scrolled near the top (keeping the scroll position), and shows pending
// requests, the working indicator and a "jump to latest" button.
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { ArrowDownIcon, LoaderCircleIcon, MessageSquareIcon } from 'lucide-react';
import { useStickToBottom } from 'use-stick-to-bottom';
import { useBackend } from '@/backend/backend';
import type { PendingRequest, Provider, Session, TaskInfo } from '@/backend/types';
import type { TranscriptItem } from '@/domain/toolGroups';
import { readCodexUsage } from '@/domain/codexStatus';
import { providerName, providerOf } from '@/domain/providers';
import { loadOlder, type TranscriptState } from '@/store/transcripts';
import { useApp } from '@/store/app';
import { cn } from '@/components/lib/utils';
import { t } from '@/lib/i18n';
import { MarkdownFiles, type OpenFile } from '@/features/markdown/Markdown';
import { MarkdownImages, useImageResolver } from '@/features/markdown/images';
import { RequestCard } from '@/features/requests/RequestCard';
import { toast } from '@/components/ui/toast';
import { locateCall, revealCall } from './locate';
import { agentStateFrom, buildItems, containsCall, extractTurns, interactionsByCall, placeRequests, turnOpeners } from './fold';
import { ConversationContext, type ConversationContextValue } from './context';
import { TranscriptItems } from './TranscriptItems';
import { TurnRail } from './TurnRail';
import { WorkingIndicator } from './WorkingIndicator';
import { CodexUsageFooter } from './CodexCards';

export interface ConversationViewProps {
  /** Selection key the transcript is cached under (store/transcripts). */
  tkey: string;
  session: Session;
  transcript: TranscriptState;
  /** Set for read-only terminal history. */
  native?: Provider;
  mobile: boolean;
}

const NO_TASKS: TaskInfo[] = [];
const SHOW_JUMP = 240, HIDE_JUMP = 40;

/** The session's tasks (subagents, background commands) for agent cards; refreshed as the transcript moves. */
function useTasks(session: Session, native: Provider | undefined, agents: number, lastSeq: number): TaskInfo[] {
  const backend = useBackend();
  const [tasks, setTasks] = useState<TaskInfo[]>(NO_TASKS);
  const running = session.status === 'running';
  const tick = running ? lastSeq : 0;
  useEffect(() => {
    if (native || (!agents && !session.activeTasks)) return;
    let live = true;
    const timer = window.setTimeout(() => {
      backend.tasks(session.id).then(list => { if (live) setTasks(list); }, () => {});
    }, tick ? 600 : 0);
    return () => { live = false; window.clearTimeout(timer); };
  }, [backend, native, session.id, session.status, session.activeTasks, agents, tick]);
  return tasks;
}

function Skeleton() {
  return (
    <div className="flex flex-col gap-6 py-4" aria-busy="true" aria-label={t('Loading the conversation', '正在加载对话')}>
      {[0, 1, 2].map(i => (
        <div key={i} className="flex flex-col gap-2">
          <div className="ml-auto h-9 w-2/5 animate-pulse rounded-xl bg-surface" />
          <div className="h-3 w-3/4 animate-pulse rounded bg-surface" />
          <div className="h-3 w-2/3 animate-pulse rounded bg-surface" />
          <div className="h-6 w-1/3 animate-pulse rounded-md bg-surface" />
        </div>
      ))}
    </div>
  );
}

function Empty({ session, native }: { session: Session; native?: Provider }) {
  return (
    <div className="flex min-h-[40vh] flex-col items-center justify-center gap-2 text-center">
      <MessageSquareIcon className="size-5 text-foreground-subtlest" />
      <p className="text-ui-base font-medium text-foreground">{native ? t('No messages to show', '没有可显示的消息') : t('Start the conversation', '开始对话')}</p>
      {!native && (
        <p className="max-w-sm text-ui-sm text-foreground-subtle">
          {t(`Describe a task below; ${providerName(providerOf(session))} replies here.`, `在下方描述任务，${providerName(providerOf(session))} 会在这里回复。`)}
        </p>
      )}
    </div>
  );
}

export function ConversationView({ tkey, session, transcript, native, mobile }: ConversationViewProps) {
  const backend = useBackend();
  const { scrollRef, contentRef, scrollToBottom, stopScroll } = useStickToBottom({ initial: 'instant', resize: 'instant' });
  const events = transcript.events;
  const running = !native && session.status === 'running';
  const live = !native && (session.status === 'running' || session.status === 'waiting');
  const lastSeq = events.at(-1)?.seq ?? 0;
  const agentCount = useMemo(() => events.reduce((n, e) => n + (e.kind === 'tool' && (e.payload?.name === 'Agent' || e.payload?.name === 'Task') ? 1 : 0), 0), [events]);
  const tasks = useTasks(session, native, agentCount, lastSeq);
  const agentRunning = useMemo(() => agentStateFrom(tasks), [tasks]);

  // Fold and group; stabilize against the previous render so unchanged items keep their identity.
  const prev = useRef<TranscriptItem[] | undefined>(undefined);
  const built = useMemo(() => buildItems(events, { running, live, agentRunning, prev: prev.current }), [events, running, live, agentRunning]);
  useEffect(() => { prev.current = built.items; }, [built]);
  const items = built.items;

  const requests = useMemo(() => transcript.requests.filter((r: PendingRequest) => !r.sessionId || r.sessionId === session.id), [transcript.requests, session.id]);
  const placement = useMemo(() => placeRequests(items, requests), [items, requests]);
  const interactions = useMemo(() => interactionsByCall(items, events), [items, events]);
  const openers = useMemo(() => turnOpeners(items), [items]);
  const turns = useMemo(() => extractTurns(items), [items]);
  const usage = useMemo(() => providerOf(session) === 'codex' ? readCodexUsage(events) : undefined, [session, events]);

  // The file viewer reads a trailing ":N" as the line to show.
  const openFile = useCallback((path: string, base?: string[], line?: number) => {
    useApp.getState().open({ type: 'file', sessionId: session.id, path: line ? `${path}:${line}` : path, ...(base?.length ? { base } : {}) });
  }, [session.id]);
  const images = useImageResolver(native ? undefined : session.id);
  const openMarkdownFile = useCallback<OpenFile>((ref, bases) => openFile(ref.path, bases, ref.line), [openFile]);

  const ctx = useMemo<ConversationContextValue>(() => ({
    tkey, session, native, mobile, tasks, requestsByCall: placement.byCall, interactions,
    streamingSeq: built.streamingSeq, openers, openFile: native ? undefined : openFile,
  }), [tkey, session, native, mobile, tasks, placement.byCall, interactions, built.streamingSeq, openers, openFile]);

  // ---- Locate a call (task panel): open its group/card, scroll to it and flash it. ----
  const itemsRef = useRef(items);
  itemsRef.current = items;
  useEffect(() => {
    let timer = 0;
    const onLocate = (e: Event) => {
      const id = (e as CustomEvent<{ toolUseId?: string }>).detail?.toolUseId;
      if (!id) return;
      if (!containsCall(itemsRef.current, id)) {
        toast(t('That call is not in the loaded part of the conversation.', '该调用不在已加载的对话记录中。'), { dedupeKey: 'locate-call' });
        return;
      }
      locateCall(id);
      stopScroll();
      window.clearTimeout(timer);
      // Let the groups open (their height transition takes ~200ms) before measuring.
      timer = window.setTimeout(() => {
        const el = scrollRef.current;
        if (el) revealCall(el, id);
      }, 260);
    };
    window.addEventListener('cc-locate-call', onLocate);
    return () => { window.removeEventListener('cc-locate-call', onLocate); window.clearTimeout(timer); };
  }, [scrollRef, stopScroll]);

  // ---- Older pages: a sentinel ~800px above the top, keeping the scroll position. ----
  const sentinel = useRef<HTMLDivElement>(null);
  const anchor = useRef<{ height: number; top: number } | null>(null);
  const firstSeq = events[0]?.seq;
  const older = useCallback(() => {
    const el = scrollRef.current;
    if (!el || !transcript.hasMore || transcript.loadingOlder) return;
    anchor.current = { height: el.scrollHeight, top: el.scrollTop };
    void loadOlder(backend, tkey, session.id, native);
  }, [backend, native, scrollRef, session.id, tkey, transcript.hasMore, transcript.loadingOlder]);
  useEffect(() => {
    const el = scrollRef.current, target = sentinel.current;
    if (!el || !target || !transcript.loaded || !transcript.hasMore || transcript.loadingOlder) return;
    const observer = new IntersectionObserver(entries => { if (entries.some(e => e.isIntersecting)) older(); }, { root: el, rootMargin: '800px 0px 0px 0px' });
    observer.observe(target);
    return () => observer.disconnect();
  }, [older, scrollRef, transcript.loaded, transcript.hasMore, transcript.loadingOlder, firstSeq]);
  useLayoutEffect(() => {
    const el = scrollRef.current, saved = anchor.current;
    if (!el || !saved || transcript.loadingOlder) return;
    anchor.current = null;
    el.scrollTop = saved.top + (el.scrollHeight - saved.height);
  }, [firstSeq, transcript.loadingOlder, scrollRef]);

  // ---- "Jump to latest": shown past 240px from the bottom, hidden again within 40px. ----
  const [away, setAway] = useState(false);
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const onScroll = () => {
      const distance = el.scrollHeight - el.scrollTop - el.clientHeight;
      setAway(was => was ? distance > HIDE_JUMP : distance > SHOW_JUMP);
    };
    onScroll();
    el.addEventListener('scroll', onScroll, { passive: true });
    return () => el.removeEventListener('scroll', onScroll);
  }, [scrollRef, transcript.loaded]);

  return (
    <ConversationContext.Provider value={ctx}>
      <MarkdownFiles.Provider value={native ? null : openMarkdownFile}>
      <MarkdownImages.Provider value={images}>
        <div className="relative flex min-h-0 flex-1 flex-col">
          <div ref={scrollRef as React.RefCallback<HTMLDivElement>} className="min-h-0 flex-1 overflow-y-auto [overflow-anchor:none]">
            <div ref={contentRef as React.RefCallback<HTMLDivElement>}
              className={cn('@container/conversation mx-auto w-full max-w-3xl', mobile ? 'px-3 pt-4 pb-4' : 'px-4 pt-6 pb-8')}>
              {transcript.hasMore && (
                <div className="mb-4 flex justify-center">
                  <div ref={sentinel} aria-hidden className="h-px w-px" />
                  <button type="button" onClick={older} disabled={transcript.loadingOlder}
                    className="inline-flex h-7 items-center gap-1.5 rounded-full border border-border/60 px-3 text-ui-sm text-foreground-subtle transition-colors hover:bg-surface-hover disabled:opacity-60">
                    {transcript.loadingOlder && <LoaderCircleIcon className="size-3.5 animate-spin" />}
                    {transcript.loadingOlder ? t('Loading earlier messages…', '正在加载更早的消息…') : t('Load earlier messages', '加载更早的消息')}
                  </button>
                </div>
              )}
              {session.truncated && !transcript.hasMore && (
                <p className="mb-4 rounded-lg bg-surface px-3 py-2 text-center text-ui-sm text-foreground-subtle">
                  {t('This record is long; only its latest part is shown.', '这段记录较长，只显示了最近的部分。')}
                </p>
              )}
              {transcript.error && <p className="mb-3 text-center text-ui-sm text-destructive">{transcript.error}</p>}
              {!transcript.loaded ? <Skeleton />
                : items.length === 0 && !placement.loose.length ? <Empty session={session} native={native} />
                : <TranscriptItems items={items} />}
              {usage && <CodexUsageFooter usage={usage} />}
              {!native && transcript.loaded && <WorkingIndicator session={session} events={events} />}
              {placement.loose.length > 0 && (
                <div className="mt-4 flex flex-col gap-3">
                  {placement.loose.map(request => <RequestCard key={request.id} request={request} session={session} mobile={mobile} />)}
                </div>
              )}
            </div>
          </div>
          {!mobile && <TurnRail turns={turns} scroller={scrollRef} />}
          <div className={cn('pointer-events-none absolute inset-x-0 bottom-3 flex justify-center transition-all duration-200',
            away ? 'translate-y-0 opacity-100' : 'translate-y-2 opacity-0')}>
            <button type="button" tabIndex={away ? 0 : -1} aria-hidden={!away} onClick={() => void scrollToBottom()}
              aria-label={t('Jump to latest', '回到最新')}
              className={cn('flex size-8 items-center justify-center rounded-full border border-border bg-popover text-foreground-subtle shadow-md transition-colors hover:bg-surface-hover hover:text-foreground',
                away && 'pointer-events-auto')}>
              <ArrowDownIcon className="size-4" />
            </button>
          </div>
        </div>
      </MarkdownImages.Provider>
      </MarkdownFiles.Provider>
    </ConversationContext.Provider>
  );
}
