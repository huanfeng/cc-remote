// The expanded body of one tool call: its input shaped by tool kind (command,
// inline diff, file link) and its result, clipped until "show all". Calls the
// host cut in the listing are fetched whole when opened.
import { useEffect, useState, type ReactNode } from 'react';
import { ChevronDownIcon, FileIcon } from 'lucide-react';
import { useBackend } from '@/backend/backend';
import type { ToolCall } from '@/domain/toolGroups';
import { mergeEvents, useTranscripts } from '@/store/transcripts';
import { cn } from '@/components/lib/utils';
import { t } from '@/lib/i18n';
import { clipOutput, editDiff, type DiffLine } from '../fold';
import { useConversation } from '../context';

const MONO = 'font-mono text-[12px] leading-[1.55]';

export function Label({ children }: { children: ReactNode }) {
  return <div className="mb-1 text-ui-sm text-foreground-subtlest">{children}</div>;
}

/** Long text shows its head (60 lines / 6000 characters) until "show all". */
export function OutputBlock({ text, error, className }: { text: string; error?: boolean; className?: string }) {
  const [all, setAll] = useState(false);
  const clip = clipOutput(text);
  const shown = all ? text : clip.text;
  return (
    <div className={className}>
      <pre className={cn(MONO, 'max-h-96 overflow-auto rounded-lg border border-border/60 bg-surface px-2.5 py-2 whitespace-pre-wrap break-words',
        error ? 'text-destructive' : 'text-foreground-subtle')}>
        {shown || <span className="text-foreground-subtlest">{t('(no output)', '（无输出）')}</span>}
      </pre>
      {clip.clipped && (
        <button type="button" onClick={() => setAll(!all)} className="mt-1 inline-flex items-center gap-1 rounded-md px-1 text-ui-sm text-foreground-subtle hover:text-foreground">
          <ChevronDownIcon className={cn('size-3.5 transition-transform', all && 'rotate-180')} />
          {all ? t('Show less', '收起') : clip.hiddenLines ? t(`Show all (${clip.hiddenLines} more lines)`, `显示全部（还有 ${clip.hiddenLines} 行）`) : t('Show all', '显示全部')}
        </button>
      )}
    </div>
  );
}

export function DiffView({ lines, max = 2000, className }: { lines: DiffLine[]; max?: number; className?: string }) {
  const shown = lines.slice(0, max);
  return (
    <div className={cn('overflow-hidden rounded-lg border border-border/60 bg-surface', className)}>
      <div className={cn(MONO, 'max-h-[28rem] overflow-auto py-1')}>
        {shown.map((line, i) => (
          <div key={i} className={cn('flex min-w-max px-2.5 whitespace-pre',
            line.type === 'add' && 'bg-diff-added text-diff-added-foreground',
            line.type === 'del' && 'bg-diff-removed text-diff-removed-foreground',
            line.type === 'hunk' && 'text-icon-blue',
            line.type === 'meta' && 'text-foreground-subtlest',
            line.type === 'ctx' && 'text-foreground-subtle')}>
            <span className="w-4 shrink-0 select-none text-foreground-subtlest">{line.type === 'add' ? '+' : line.type === 'del' ? '−' : ' '}</span>
            <span>{line.text || ' '}</span>
          </div>
        ))}
      </div>
      {lines.length > max && (
        <div className="border-t border-border/60 px-2.5 py-1.5 text-ui-sm text-foreground-subtlest">
          {t(`${lines.length - max} more lines not shown`, `还有 ${lines.length - max} 行未显示`)}
        </div>
      )}
    </div>
  );
}

/** A host file path: opens the file viewer (plain text in read-only history). */
export function PathLink({ path, line }: { path: string; line?: number }) {
  const { openFile } = useConversation();
  const label = line ? `${path}:${line}` : path;
  const body = <><FileIcon className="size-3.5 shrink-0" /><span className="truncate">{label}</span></>;
  if (!openFile) return <span className={cn(MONO, 'inline-flex max-w-full items-center gap-1.5 text-foreground-subtle')}>{body}</span>;
  return (
    <button type="button" onClick={() => openFile(path, undefined, line)} title={t(`Open ${path}`, `打开 ${path}`)}
      className={cn(MONO, 'inline-flex max-w-full items-center gap-1.5 rounded-md text-icon-blue hover:underline')}>
      {body}
    </button>
  );
}

function useFullCall(call: ToolCall) {
  const backend = useBackend();
  const { tkey, session, native } = useConversation();
  const [state, setState] = useState<'idle' | 'loading' | 'failed'>('idle');
  const cut = call.truncated?.join(',');
  useEffect(() => {
    if (!cut) return;
    let live = true;
    setState('loading');
    Promise.all(cut.split(',').map(seq => backend.fullEvent(session.id, Number(seq), native ? { native } : undefined)))
      .then(events => {
        if (!live) return;
        useTranscripts.getState().patch(tkey, s => ({ events: mergeEvents(s.events, events) }));
        setState('idle');
      }, () => { if (live) setState('failed'); });
    return () => { live = false; };
  }, [backend, cut, native, session.id, tkey]);
  return state;
}

const str = (v: unknown) => typeof v === 'string' ? v : '';

function InputView({ call }: { call: ToolCall }) {
  const input = call.input ?? {};
  const path = str(input.file_path) || str(input.notebook_path);
  if (typeof input.command === 'string') {
    return (
      <pre className={cn(MONO, 'overflow-x-auto rounded-lg border border-border/60 bg-surface px-2.5 py-2 whitespace-pre-wrap break-all text-foreground')}>
        <span className="select-none text-foreground-subtlest">$ </span>{input.command}
      </pre>
    );
  }
  if (call.name === 'Edit' && path) {
    return <div className="space-y-1.5"><PathLink path={path} /><DiffView lines={editDiff(str(input.old_string), str(input.new_string))} /></div>;
  }
  if (call.name === 'MultiEdit' && path && Array.isArray(input.edits)) {
    return (
      <div className="space-y-1.5">
        <PathLink path={path} />
        {input.edits.map((edit: { old_string?: string; new_string?: string }, i: number) => <DiffView key={i} lines={editDiff(str(edit.old_string), str(edit.new_string))} />)}
      </div>
    );
  }
  if (call.name === 'Write' && path) {
    return <div className="space-y-1.5"><PathLink path={path} /><OutputBlock text={str(input.content)} /></div>;
  }
  if (path) {
    const range = [input.offset != null && t(`from line ${input.offset}`, `第 ${input.offset} 行起`), input.limit != null && t(`${input.limit} lines`, `${input.limit} 行`)].filter(Boolean).join(' · ');
    return <div className="flex min-w-0 items-center gap-2"><PathLink path={path} line={typeof input.offset === 'number' ? input.offset : undefined} />{range && <span className="shrink-0 text-ui-sm text-foreground-subtlest">{range}</span>}</div>;
  }
  if (input === undefined || (typeof input === 'object' && !Object.keys(input).length)) return null;
  return <OutputBlock text={typeof input === 'string' ? input : JSON.stringify(input, null, 2)} />;
}

export function ToolDetail({ call }: { call: ToolCall }) {
  const full = useFullCall(call);
  // Edit/Write results only confirm success; the diff above says it all.
  const quietResult = call.status === 'ok' && ['Edit', 'MultiEdit', 'Write', 'NotebookEdit'].includes(call.name);
  return (
    <div className="space-y-2.5 py-2 pr-1 pl-6">
      {call.input !== undefined && <InputView call={call} />}
      {full === 'loading' && <p className="text-ui-sm text-foreground-subtlest">{t('Loading the full call…', '正在加载完整内容…')}</p>}
      {full === 'failed' && <p className="text-ui-sm text-destructive">{t('Could not load the full call.', '完整内容加载失败。')}</p>}
      {call.status === 'running' && <p className="text-ui-sm text-foreground-subtlest">{t('Running…', '运行中…')}</p>}
      {call.status === 'missing' && <p className="text-ui-sm text-foreground-subtlest">{t('No result was recorded.', '没有记录到结果。')}</p>}
      {call.result && !quietResult && (
        <div>
          <Label>{call.status === 'error' ? t('Result (failed)', '结果（失败）') : t('Result', '结果')}</Label>
          <OutputBlock text={call.resultText} error={call.status === 'error'} />
        </div>
      )}
    </div>
  );
}
