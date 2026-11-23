import { useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react';
import { FileImage, FileText, PanelRightClose, PanelRightOpen, X } from 'lucide-react';
import { Tabs } from 'radix-ui';
import { useStore } from 'zustand';
import { useBackend } from '@/backend/backend';
import { cn } from '@/components/lib/utils';
import { basename } from '@/domain/filePaths';
import { t } from '@/lib/i18n';
import { AttachmentImage } from './AttachmentImageDialog';
import { FilePane } from './FileViewer';
import { ViewerIconButton } from './ViewerIconButton';
import { fileDockStore, fileEntryKey, type DockEntry } from './fileDockState';
import { splitLine } from './fileView';

const DEFAULT_WIDTH = 440;
const MIN_WIDTH = 260;
const MAX_WIDTH = 720;

// The footer is kept out of the file pane's scroll area. On a narrow
// workspace the dock sits immediately above the footer, including with IME open.
export function FileDockLayout({ dockKey, mobile, children, footer }: {
  dockKey: string; mobile: boolean; children: ReactNode; footer: ReactNode;
}) {
  return (
    <div className="@container/file-dock relative flex min-h-0 flex-1">
      <div className="flex w-full min-w-0 flex-col @min-[42rem]/file-dock:grid @min-[42rem]/file-dock:grid-cols-[minmax(0,1fr)_auto] @min-[42rem]/file-dock:grid-rows-[minmax(0,1fr)_auto]">
        <div className="relative col-start-1 row-start-1 flex min-h-0 min-w-0 flex-1 flex-col">{children}</div>
        <FileDock dockKey={dockKey} mobile={mobile} />
        <div className="col-start-1 row-start-2 min-w-0 shrink-0">{footer}</div>
      </div>
    </div>
  );
}

const titleOf = (entry: DockEntry) => entry.kind === 'attachment' ? entry.attachment.name : basename(splitLine(entry.path).path);

function FileDock({ dockKey, mobile }: { dockKey: string; mobile: boolean }) {
  const backend = useBackend();
  const store = fileDockStore(backend);
  const dock = useStore(store, state => state.docks.get(dockKey));
  const [width, setWidth] = useState(DEFAULT_WIDTH);
  const activeTab = useRef<HTMLButtonElement>(null);
  const active = dock?.active;
  const collapsed = dock?.collapsed;
  useLayoutEffect(() => {
    if (!collapsed) activeTab.current?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }, [active, collapsed]);
  if (!dock) return null;

  const actions = store.getState();
  const closeTab = (id: string) => {
    actions.close(dockKey, id);
    requestAnimationFrame(() => activeTab.current?.focus({ preventScroll: true }));
  };
  const activeEntry = dock.tabs.find(tab => tab.id === dock.active)?.entry;
  return (
    <aside aria-label={t('File panel', '文件栏')} className={cn(
      'relative col-start-2 row-span-2 row-start-1 flex min-h-0 min-w-0 flex-col border-t border-border/60 bg-background',
      '@min-[42rem]/file-dock:h-auto @min-[42rem]/file-dock:max-w-[55cqw] @min-[42rem]/file-dock:border-t-0 @min-[42rem]/file-dock:border-l',
      dock.collapsed
        ? 'h-9 shrink-0 @min-[42rem]/file-dock:w-10'
        : 'h-[min(20rem,calc(var(--app-height,100dvh)*0.44))] @min-[42rem]/file-dock:w-(--file-dock-width)',
    )} style={{ '--file-dock-width': `${width}px` } as CSSProperties}>
      {!dock.collapsed && <FileDockResizer width={width} onResize={setWidth} />}

      <Tabs.Root value={dock.active} onValueChange={id => actions.select(dockKey, id)} className="flex min-h-0 flex-1 flex-col">
        <div className={cn('flex h-9 shrink-0 items-center', dock.collapsed ? 'gap-1.5 px-1.5' : 'border-b border-border/60 bg-surface/40 pr-1')}>
          <Tabs.List aria-label={t('Open files', '已打开文件')} className={cn('flex h-full min-w-0 flex-1 items-stretch gap-px overflow-x-auto overflow-y-hidden pl-1 [scrollbar-width:none]', dock.collapsed && 'hidden')}>
            {dock.tabs.map(({ id, entry }) => {
              const name = titleOf(entry);
              const Icon = entry.kind === 'attachment' ? FileImage : FileText;
              return (
                <div key={id} role="presentation"
                  className={cn('flex max-w-48 shrink-0 items-center rounded-t-sm pr-1 text-foreground-subtle hover:bg-surface-hover', id === dock.active && 'bg-background text-foreground')}>
                  <Tabs.Trigger value={id} ref={id === dock.active ? activeTab : undefined} title={entry.kind === 'file' ? entry.path : name} aria-keyshortcuts="Delete"
                    className="flex h-full min-w-0 items-center gap-1.5 border-t border-transparent pl-2 text-ui-sm outline-none data-[state=active]:border-ring focus-visible:ring-1 focus-visible:ring-ring focus-visible:ring-inset"
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
          </Tabs.List>
          <ViewerIconButton label={dock.collapsed ? t('Expand file panel', '展开文件栏') : t('Collapse file panel', '收起文件栏')}
            expanded={!dock.collapsed} onClick={() => actions.collapse(dockKey, !dock.collapsed)}>
            {dock.collapsed ? <PanelRightOpen /> : <PanelRightClose />}
          </ViewerIconButton>
          {dock.collapsed && activeEntry && <span className="truncate text-ui-sm text-foreground-subtle @min-[42rem]/file-dock:hidden">{titleOf(activeEntry)}</span>}
        </div>

        {dock.tabs.map(({ id, entry }) => (
          <Tabs.Content key={id} forceMount value={id} className={cn('flex min-h-0 flex-1 flex-col overflow-hidden outline-none data-[state=inactive]:hidden focus-visible:ring-1 focus-visible:ring-ring focus-visible:ring-inset', dock.collapsed && 'hidden')}>
            {entry.kind === 'file' ? (
              <FilePane key={`${fileEntryKey(entry)}:${entry.source ?? ''}`} sessionId={dockKey} entry={entry} mobile={mobile} initialSource={entry.source}
                onOpen={next => actions.open(dockKey, { kind: 'file', ...next })} />
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
