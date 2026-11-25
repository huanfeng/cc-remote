import { useEffect, useMemo, useRef, useState } from 'react';
import { ArrowLeft, FileText, MessageSquarePlus, RefreshCw } from 'lucide-react';
import { useBackend, type Backend } from '@/backend/backend';
import type { ProjectGitChange, ProjectGitChanges, ProjectGitDiff } from '@/backend/types';
import { cn } from '@/components/lib/utils';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { basename } from '@/domain/filePaths';
import { t } from '@/lib/i18n';
import type { FileReference } from './fileReference.ts';
import { gitDiffReference, parseGitDiff } from './projectGitDiff.ts';
import { ViewerIconButton } from './ViewerIconButton';

type Load<T> = { backend: Backend; key: string } & ({ value: T } | { error: string });
type Stage = ProjectGitChange['stage'];

const stages: Stage[] = ['staged', 'working', 'untracked'];

function stageLabel(stage: Stage) {
  if (stage === 'staged') return t('Staged', '已暂存');
  if (stage === 'working') return t('Unstaged', '未暂存');
  return t('Untracked', '未跟踪');
}

function changeLabel(status: string) {
  switch (status) {
    case 'added': return ['A', t('Added', '新增'), 'text-success'];
    case 'deleted': return ['D', t('Deleted', '删除'), 'text-destructive'];
    case 'renamed': return ['R', t('Renamed', '重命名'), 'text-foreground'];
    case 'copied': return ['C', t('Copied', '复制'), 'text-foreground'];
    case 'type-changed': return ['T', t('Type changed', '类型变更'), 'text-warning'];
    case 'conflicted': return ['!', t('Merge conflict', '合并冲突'), 'text-destructive'];
    case 'untracked': return ['?', t('Untracked', '未跟踪'), 'text-success'];
    default: return ['M', t('Modified', '修改'), 'text-warning'];
  }
}

function changePath(change: ProjectGitChange) {
  return change.originalPath ? `${change.originalPath} → ${change.path}` : change.path;
}

function waiting() {
  return <div role="status" className="flex items-center gap-2 px-4 py-3 text-ui-sm text-foreground-subtle">
    <Spinner className="size-3.5" aria-hidden />{t('Loading…', '加载中…')}
  </div>;
}

function retry(error: string, onRetry: () => void) {
  return <div className="space-y-2 px-4 py-3 text-ui-sm">
    <p role="alert" className="break-words text-foreground-subtle">{error}</p>
    <Button type="button" variant="outline" size="sm" onClick={onRetry}>{t('Try again', '重试')}</Button>
  </div>;
}

export function ProjectGit({ sessionId, visible, onReference, onOpenFile }: {
  sessionId: string; visible: boolean; onReference?: (reference: FileReference) => void; onOpenFile?: (path: string) => void;
}) {
  const backend = useBackend();
  const [revision, setRevision] = useState(0);
  const [listing, setListing] = useState<Load<ProjectGitChanges>>();
  const [diff, setDiff] = useState<Load<ProjectGitDiff>>();
  const [selected, setSelected] = useState<ProjectGitChange>();
  const region = useRef<HTMLDivElement>(null);
  const listKey = JSON.stringify([sessionId, revision]);
  const diffKey = JSON.stringify([sessionId, revision, selected?.stage, selected?.path]);
  const currentList = listing?.backend === backend && listing.key === listKey ? listing : undefined;
  const currentDiff = diff?.backend === backend && diff.key === diffKey ? diff : undefined;
  const refresh = () => setRevision(value => value + 1);

  useEffect(() => {
    if (!visible) return;
    const controller = new AbortController();
    backend.projectGitChanges(sessionId, controller.signal).then(value => {
      if (!controller.signal.aborted) setListing({ backend, key: listKey, value });
    }, error => {
      if (!controller.signal.aborted) setListing({ backend, key: listKey, error: String(error?.message || error) });
    });
    return () => controller.abort();
  }, [backend, listKey, sessionId, visible]);

  useEffect(() => {
    if (!visible || !selected) return;
    const controller = new AbortController();
    backend.projectGitDiff(sessionId, selected.path, selected.stage, controller.signal).then(value => {
      if (!controller.signal.aborted) setDiff({ backend, key: diffKey, value });
    }, error => {
      if (!controller.signal.aborted) setDiff({ backend, key: diffKey, error: String(error?.message || error) });
    });
    return () => controller.abort();
  }, [backend, diffKey, selected, sessionId, visible]);

  const show = (change?: ProjectGitChange) => {
    if (region.current) {
      region.current.scrollTop = 0;
      region.current.scrollLeft = 0;
      region.current.focus({ preventScroll: true });
    }
    setSelected(change);
  };

  return <div className="flex min-h-0 flex-1 flex-col">
    <div className="flex h-9 shrink-0 items-center gap-0.5 border-b border-border/60 px-1">
      {selected && <ViewerIconButton label={t('Back to Git changes', '返回 Git 改动')} onClick={() => show()}><ArrowLeft aria-hidden /></ViewerIconButton>}
      {selected ? (
        <span title={changePath(selected)} className="min-w-0 flex-1 truncate px-1.5 text-ui-sm font-medium">{changePath(selected)}</span>
      ) : <span className="min-w-0 flex-1 px-2 text-ui-sm font-medium">{t('Changed files', '已变更的文件')}</span>}
      {selected && onReference && <ViewerIconButton label={t('Reference path in chat', '引用路径到对话')}
        onClick={() => onReference({ path: selected.path })}><MessageSquarePlus aria-hidden /></ViewerIconButton>}
      {selected && onOpenFile && selected.status !== 'deleted' && <ViewerIconButton label={t('Open file preview', '打开文件预览')}
        onClick={() => onOpenFile(selected.path)}><FileText aria-hidden /></ViewerIconButton>}
      <ViewerIconButton label={t('Refresh Git changes', '刷新 Git 改动')} onClick={refresh}><RefreshCw aria-hidden /></ViewerIconButton>
    </div>
    <div ref={region} role="region" tabIndex={0} aria-label={selected ? t('File diff', '文件差异') : t('Git changes', 'Git 改动')}
      className="min-h-0 flex-1 overflow-auto outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-ring">
      {selected ? (
        !currentDiff ? waiting() : 'error' in currentDiff ? retry(currentDiff.error, refresh)
          : <DiffContent key={diffKey} diff={currentDiff.value} change={selected} onReference={onReference} />
      ) : (
        !currentList ? waiting() : 'error' in currentList ? retry(currentList.error, refresh)
          : <ChangeList listing={currentList.value} onSelect={show} />
      )}
    </div>
  </div>;
}

function ChangeList({ listing, onSelect }: { listing: ProjectGitChanges; onSelect: (change: ProjectGitChange) => void }) {
  if (!listing.repository) return <p role="status" className="px-4 py-3 text-ui-sm text-foreground-subtle">
    {t('This project is outside a Git repository.', '此项目目录未关联 Git 仓库。')}
  </p>;
  return <>
    {!listing.entries.length && <p role="status" className="px-4 py-3 text-ui-sm text-foreground-subtle">
      {t('No Git changes in this project.', '此项目没有 Git 改动。')}
    </p>}
    {stages.map(stage => {
      const changes = listing.entries.filter(entry => entry.stage === stage);
      if (!changes.length) return null;
      const label = stageLabel(stage);
      return <section key={stage} aria-label={label} className="border-b border-border/60 px-1 py-1">
        <h3 className="px-2 py-1 text-ui-xs font-medium text-foreground-subtle">{label} · {changes.length}</h3>
        <ul className="space-y-0.5">
          {changes.map(change => {
            const [symbol, status, color] = changeLabel(change.status);
            const name = basename(change.path);
            const extra = change.originalPath ? change.originalPath : change.path.slice(0, -name.length).replace(/\/$/, '');
            return <li key={change.path}>
              <button type="button" title={`${status} · ${changePath(change)}`} aria-label={`${status} · ${changePath(change)}`}
                className="flex min-h-8 w-full min-w-0 items-center gap-2 rounded-md px-2 py-1 text-left text-ui-sm hover:bg-surface-hover focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-ring"
                onClick={() => onSelect(change)}>
                <span aria-hidden className={cn('w-4 shrink-0 text-center font-mono text-ui-xs font-semibold', color)}>{symbol}</span>
                <span className="min-w-0 flex-1 truncate">{name}
                  {extra && <span className="ml-2 text-ui-xs text-foreground-subtle">{extra}</span>}
                </span>
              </button>
            </li>;
          })}
        </ul>
      </section>;
    })}
    {listing.truncated && <p role="status" className="px-3 py-2 text-ui-xs text-foreground-subtle">
      {t('Only some changed files are shown.', '仅显示部分改动文件。')}
    </p>}
  </>;
}

function DiffContent({ diff, change, onReference }: { diff: ProjectGitDiff; change: ProjectGitChange; onReference?: (reference: FileReference) => void }) {
  const { rows, truncated } = useMemo(() => {
    const text = diff.truncated ? diff.text.slice(0, diff.text.lastIndexOf('\n') + 1) : diff.text;
    return parseGitDiff(text);
  }, [diff.text, diff.truncated]);
  const code = useRef<HTMLDivElement>(null);
  const [selection, setSelection] = useState<[number, number]>();
  const reference = selection && gitDiffReference(rows, ...selection);
  const oldReference = selection && gitDiffReference(rows, ...selection, 'before');
  const includesRemoved = selection && rows.slice(Math.min(...selection), Math.max(...selection) + 1).some(row => row.kind === 'removed');
  const currentContext = change.stage === 'staged' ? stageLabel('staged') : undefined;
  const oldContext = t('Before the Git change', 'Git 改动前');

  useEffect(() => {
    if (!onReference) return;
    const rowOf = (node: Node | null) => {
      const element = node instanceof Element ? node : node?.parentElement;
      const row = element?.closest<HTMLElement>('code[data-diff-row]');
      if (row && code.current?.contains(row)) return Number(row.dataset.diffRow);
    };
    const update = () => {
      const selected = document.getSelection();
      if (!selected || selected.isCollapsed) { setSelection(undefined); return; }
      const from = rowOf(selected.anchorNode);
      const to = rowOf(selected.focusNode);
      setSelection(from === undefined || to === undefined ? undefined : [from, to]);
    };
    document.addEventListener('selectionchange', update);
    return () => document.removeEventListener('selectionchange', update);
  }, [onReference]);

  if (!rows.length && !diff.truncated) return <p role="status" className="px-4 py-3 text-ui-sm text-foreground-subtle">
    {t('No diff for this file.', '此文件没有差异。')}
  </p>;
  return <>
    <div className="sticky top-0 left-0 z-10 flex h-8 items-center justify-between gap-2 border-b border-border/60 bg-background px-3 text-ui-xs text-foreground-subtle">
      <span>{stageLabel(change.stage)}</span>
      {onReference && <div className="flex items-center gap-1">
        {includesRemoved && oldReference && <Button type="button" variant="ghost" size="sm" className="h-7"
          onPointerDown={event => event.preventDefault()} onClick={() => onReference({ path: change.originalPath ?? change.path, ...oldReference, context: oldContext })}>
          {t('Reference old lines', '引用原行')}
        </Button>}
        {reference && <Button type="button" variant="ghost" size="sm" className="h-7"
          onPointerDown={event => event.preventDefault()} onClick={() => onReference({ path: change.path, ...reference, context: currentContext })}>
          <MessageSquarePlus aria-hidden />{includesRemoved ? t('Reference new lines', '引用新行') : t('Reference selected lines', '引用所选行')}
        </Button>}
      </div>}
    </div>
    <div ref={code} className="w-max min-w-full font-mono text-ui-xs leading-5">
      {rows.map((row, index) => {
        if (row.kind === 'meta' || row.kind === 'hunk') return <div key={index}
          className={cn('whitespace-pre px-3', row.kind === 'hunk' ? 'bg-surface font-medium text-foreground' : 'text-foreground-subtle')}>{row.text || ' '}</div>;
        const line = row.after;
        const before = row.before;
        const referenceLabel = line && t(`Reference line ${line} in chat`, `引用第 ${line} 行到对话`);
        return <div key={index} className={cn('group flex min-w-max items-stretch',
          row.kind === 'added' && 'bg-success-surface text-success-surface-foreground',
          row.kind === 'removed' && 'bg-destructive-surface text-destructive-surface-foreground')}>
          {onReference && row.kind === 'removed' && before ? <button type="button"
            aria-label={t(`Reference old line ${before} in chat`, `引用原第 ${before} 行到对话`)}
            className="w-10 shrink-0 select-none border-r border-border/40 px-1.5 text-right text-foreground-subtle hover:bg-surface-hover hover:text-foreground focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-ring"
            onClick={() => onReference({ path: change.originalPath ?? change.path, startLine: before, text: row.text, context: oldContext })}>{before}</button>
            : <span aria-label={before ? t(`Old line ${before}`, `原第 ${before} 行`) : undefined}
              className="w-10 shrink-0 select-none border-r border-border/40 px-1.5 text-right text-foreground-subtle">{before}</span>}
          {onReference && line && referenceLabel ? <button type="button" aria-label={referenceLabel} title={referenceLabel}
            className="relative w-10 shrink-0 select-none border-r border-border/40 px-1.5 text-right text-foreground-subtle hover:bg-surface-hover hover:text-foreground focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-ring"
            onClick={() => onReference({ path: change.path, startLine: line, text: row.text, context: currentContext })}>
            <span>{line}</span>
            <MessageSquarePlus aria-hidden className="absolute left-0.5 top-0.5 hidden size-3.5 group-focus-within:block group-hover:block" />
          </button> : <span aria-label={line ? t(`New line ${line}`, `新第 ${line} 行`) : undefined}
            className="w-10 shrink-0 select-none border-r border-border/40 px-1.5 text-right text-foreground-subtle">{line}</span>}
          <span aria-hidden className="w-5 shrink-0 select-none text-center">{row.kind === 'added' ? '+' : row.kind === 'removed' ? '−' : ' '}</span>
          <code data-diff-row={index} className="whitespace-pre pr-3">{row.text || ' '}</code>
        </div>;
      })}
    </div>
    {(diff.truncated || truncated) && <p role="status" className="sticky left-0 border-t border-border/60 px-3 py-2 text-ui-xs text-foreground-subtle">
      {t('Only part of this diff is shown.', '仅显示部分差异。')}
    </p>}
  </>;
}
