import { useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react';
import { FileImage, FileText, FolderOpen, GitBranch, X } from 'lucide-react';
import { Tabs } from 'radix-ui';
import { useStore } from 'zustand';
import { useBackend } from '@/backend/backend';
import { cn } from '@/components/lib/utils';
import { basename } from '@/domain/filePaths';
import { t } from '@/lib/i18n';
import { hasFeature, useApp } from '@/store/app';
import { AttachmentImage } from './AttachmentImageDialog';
import { FilePane } from './FileViewer';
import { ProjectFiles } from './ProjectFiles';
import { ProjectGit } from './ProjectGit';
import { fileDockStore, fileEntryKey, projectFilesTab, projectGitTab, type DockEntry } from './fileDockState';
import type { FileReference } from './fileReference';
import { splitLine } from './fileView';

const DEFAULT_WIDTH = 440;
const MIN_WIDTH = 260;
const MAX_WIDTH = 720;

// The footer is kept out of the file pane's scroll area. On a narrow
// workspace the dock sits immediately above the footer, including with IME open.
export function FileDockLayout({ dockKey, projectCwd, mobile, children, footer, onReference }: {
  dockKey: string; projectCwd?: string; mobile: boolean; children: ReactNode; footer: ReactNode; onReference?: (reference: FileReference) => void;
}) {
  return (
    <div className="@container/file-dock relative flex min-h-0 flex-1">
      <div className="flex w-full min-w-0 flex-col @min-[42rem]/file-dock:grid @min-[42rem]/file-dock:grid-cols-[minmax(0,1fr)_auto] @min-[42rem]/file-dock:grid-rows-[minmax(0,1fr)_auto]">
        <div className="relative col-start-1 row-start-1 flex min-h-0 min-w-0 flex-1 flex-col">{children}</div>
        <FileDock dockKey={dockKey} projectCwd={projectCwd} mobile={mobile} onReference={onReference} />
        <div className="col-start-1 row-start-2 min-w-0 shrink-0">{footer}</div>
      </div>
    </div>
  );
}

const titleOf = (entry: DockEntry) => entry.kind === 'attachment'
  ? entry.attachment.name : basename(entry.scope ? entry.path : splitLine(entry.path).path);

const triggerClass = 'flex h-full min-w-0 items-center gap-1.5 border-t border-transparent text-ui-sm outline-none data-[state=active]:border-ring focus-visible:ring-1 focus-visible:ring-ring focus-visible:ring-inset';

function FileDock({ dockKey, projectCwd, mobile, onReference }: {
  dockKey: string; projectCwd?: string; mobile: boolean; onReference?: (reference: FileReference) => void;
}) {
  const backend = useBackend();
  const canBrowse = useApp(s => hasFeature(s.info, 'project-files'));
  const canQuickOpen = useApp(s => hasFeature(s.info, 'project-file-search'));
  const canReviewGit = useApp(s => hasFeature(s.info, 'project-git'));
  const store = fileDockStore(backend);
  const dock = useStore(store, state => state.docks.get(dockKey));
  const [width, setWidth] = useState(DEFAULT_WIDTH);
  const activeTab = useRef<HTMLButtonElement>(null);
  const showGit = Boolean(projectCwd && canReviewGit);
  const active = dock?.active === projectGitTab && !showGit ? projectFilesTab : dock?.active;
  const collapsed = dock?.collapsed;
  useLayoutEffect(() => {
    if (!collapsed) activeTab.current?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }, [active, collapsed]);
  if (!dock) return null;

  const actions = store.getState();
  const focusActiveTab = () => requestAnimationFrame(() => activeTab.current?.focus({ preventScroll: true }));
  const openTab = (entry: DockEntry) => {
    actions.open(dockKey, entry);
    focusActiveTab();
  };
  const closeTab = (id: string) => {
    actions.close(dockKey, id);
    focusActiveTab();
  };
  const openProjectFile = (path: string) => openTab({ kind: 'file', scope: 'project', path });
  return (
    <aside aria-label={t('File panel', '文件栏')} className={cn(
      'relative col-start-2 row-span-2 row-start-1 min-h-0 min-w-0 flex-col border-t border-border/60 bg-background',
      '@min-[42rem]/file-dock:h-auto @min-[42rem]/file-dock:max-w-[55cqw] @min-[42rem]/file-dock:border-t-0 @min-[42rem]/file-dock:border-l',
      'h-[min(20rem,calc(var(--app-height,100dvh)*0.44))] @min-[42rem]/file-dock:w-(--file-dock-width)',
      dock.collapsed ? 'hidden' : 'flex',
    )} style={{ '--file-dock-width': `${width}px` } as CSSProperties}>
      {!dock.collapsed && <FileDockResizer width={width} onResize={setWidth} />}

      <Tabs.Root value={active} onValueChange={id => actions.select(dockKey, id)} className="flex min-h-0 flex-1 flex-col">
        <Tabs.List aria-label={t('Project and open files', '项目与已打开的文件')} className="flex h-9 shrink-0 items-stretch border-b border-border/60 bg-surface/40">
          <Tabs.Trigger value={projectFilesTab} ref={active === projectFilesTab ? activeTab : undefined}
            className={cn(triggerClass, 'shrink-0 border-r border-r-border/60 px-2.5 text-foreground-subtle hover:bg-surface-hover data-[state=active]:bg-background data-[state=active]:text-foreground')}>
            <FolderOpen className="size-4 shrink-0" aria-hidden />
            <span>{t('Project files', '项目文件')}</span>
          </Tabs.Trigger>
          {showGit && <Tabs.Trigger value={projectGitTab} ref={active === projectGitTab ? activeTab : undefined}
            className={cn(triggerClass, 'shrink-0 border-r border-r-border/60 px-2.5 text-foreground-subtle hover:bg-surface-hover data-[state=active]:bg-background data-[state=active]:text-foreground')}>
            <GitBranch className="size-4 shrink-0" aria-hidden />
            <span>{t('Git changes', 'Git 改动')}</span>
          </Tabs.Trigger>}
          <div role="presentation" className="flex h-full min-w-0 flex-1 items-stretch gap-px overflow-x-auto overflow-y-hidden px-1 [scrollbar-width:none]">
            {dock.tabs.map(({ id, entry }) => {
              const name = titleOf(entry);
              const Icon = entry.kind === 'attachment' ? FileImage : FileText;
              return (
                <div key={id} role="presentation"
                  className={cn('flex max-w-48 shrink-0 items-center rounded-t-sm pr-1 text-foreground-subtle hover:bg-surface-hover', id === dock.active && 'bg-background text-foreground')}>
                  <Tabs.Trigger value={id} ref={id === dock.active ? activeTab : undefined} title={entry.kind === 'file' ? entry.path : name} aria-keyshortcuts="Delete"
                    className={cn(triggerClass, 'pl-2')}
                    onKeyDown={event => {
                      if (event.key !== 'Delete') return;
                      event.preventDefault();
                      closeTab(id);
                    }}>
                    <Icon className="size-3.5 shrink-0" aria-hidden />
                    <span className="truncate">{name}</span>
                  </Tabs.Trigger>
                  <button type="button" tabIndex={-1} aria-label={t(`Close ${name}`, `关闭 ${name}`)} title={t(`Close ${name}`, `关闭 ${name}`)}
                    onClick={() => closeTab(id)} className="ml-0.5 grid size-6 shrink-0 place-items-center rounded hover:bg-surface-hover hover:text-foreground">
                    <X className="size-3.5" />
                  </button>
                </div>
              );
            })}
          </div>
        </Tabs.List>

        <Tabs.Content value={projectFilesTab} forceMount className="flex min-h-0 flex-1 flex-col overflow-hidden outline-none data-[state=inactive]:hidden focus-visible:ring-1 focus-visible:ring-ring focus-visible:ring-inset">
          {projectCwd && canBrowse ? (
            <ProjectFiles sessionId={dockKey} cwd={projectCwd} visible={!dock.collapsed && active === projectFilesTab}
              onOpen={openProjectFile} onQuickOpen={canQuickOpen ? () => useApp.getState().open({ type: 'project-file-search', id: dockKey }) : undefined} />
          ) : (
            <p className="px-4 py-5 text-ui-sm text-foreground-subtle">
              {projectCwd ? t('Update the server to browse its project files.', '更新服务器后即可浏览项目文件。')
                : t('Open a server session with a project directory to browse files.', '打开有项目目录的服务器会话后即可浏览文件。')}
            </p>
          )}
        </Tabs.Content>

        {showGit && <Tabs.Content value={projectGitTab} forceMount className="flex min-h-0 flex-1 flex-col overflow-hidden outline-none data-[state=inactive]:hidden focus-visible:ring-1 focus-visible:ring-ring focus-visible:ring-inset">
          <ProjectGit sessionId={dockKey} visible={!dock.collapsed && active === projectGitTab} onReference={onReference} onOpenFile={openProjectFile} />
        </Tabs.Content>}

        {dock.tabs.map(({ id, entry }) => (
          <Tabs.Content key={id} forceMount value={id} className={cn('flex min-h-0 flex-1 flex-col overflow-hidden outline-none data-[state=inactive]:hidden focus-visible:ring-1 focus-visible:ring-ring focus-visible:ring-inset', dock.collapsed && 'hidden')}>
            {entry.kind === 'file' ? (
              <FilePane key={`${fileEntryKey(entry)}:${entry.source ?? ''}`} sessionId={dockKey} entry={entry} mobile={mobile} initialSource={entry.source}
                onOpen={next => openTab({ kind: 'file', ...next })} onReference={onReference} />
            ) : <AttachmentImage attachment={entry.attachment} />}
          </Tabs.Content>
        ))}
      </Tabs.Root>
    </aside>
  );
}

function FileDockResizer({ width, onResize }: { width: number; onResize: (width: number) => void }) {
  const drag = useRef<{ x: number; width: number; max: number }>(null);
  const label = t('Resize file panel', '调整文件栏宽度');
  const maxWidth = (el: HTMLElement) => {
    const workspace = el.parentElement?.parentElement;
    return Math.floor(Math.min(MAX_WIDTH, (workspace?.clientWidth ?? window.innerWidth) * 0.55));
  };
  const clamp = (w: number, max: number) => Math.round(Math.min(max, Math.max(MIN_WIDTH, w)));
  return (
    <div role="separator" aria-orientation="vertical" aria-label={label} title={label} tabIndex={0}
      aria-valuemin={MIN_WIDTH} aria-valuemax={MAX_WIDTH} aria-valuenow={width}
      className="absolute inset-y-0 -left-1 z-10 hidden w-2 cursor-col-resize touch-none select-none outline-none hover:bg-ring/45 focus-visible:bg-ring/60 @min-[42rem]/file-dock:block"
      onPointerDown={event => {
        if (event.button !== 0) return;
        drag.current = { x: event.clientX, width: event.currentTarget.parentElement!.clientWidth, max: maxWidth(event.currentTarget) };
        event.currentTarget.setPointerCapture(event.pointerId);
      }}
      onPointerMove={event => {
        const start = drag.current;
        if (start) onResize(clamp(start.width + start.x - event.clientX, start.max));
      }}
      onLostPointerCapture={() => { drag.current = null; }}
      onDoubleClick={() => onResize(DEFAULT_WIDTH)}
      onKeyDown={event => {
        const max = maxWidth(event.currentTarget);
        const current = Math.min(width, max);
        const step = event.shiftKey ? 40 : 10;
        const next = event.key === 'ArrowLeft' ? current + step : event.key === 'ArrowRight' ? current - step
          : event.key === 'Home' ? MIN_WIDTH : event.key === 'End' ? max : undefined;
        if (next === undefined) return;
        event.preventDefault();
        onResize(clamp(next, max));
      }} />
  );
}
