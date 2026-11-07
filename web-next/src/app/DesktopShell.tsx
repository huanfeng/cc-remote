// Desktop layout: sidebar | main panel. The main panel is an inset rounded
// card on the sidebar colour (Codex/ZCode style); the sidebar is resizable
// and collapsible.
import { useRef } from 'react';
import { cn } from '@/components/lib/utils';
import { usePrefs, SIDEBAR_DEFAULT, SIDEBAR_MAX, SIDEBAR_MIN } from '@/lib/prefs';
import { t } from '@/lib/i18n';
import { useApp } from '@/store/app';
import { Sidebar } from '@/features/sidebar/Sidebar';
import { SessionWorkspace } from '@/features/workspace/SessionWorkspace';
import { EmptyState } from '@/features/workspace/EmptyState';
import { WindowControls } from './WindowControls';

export function DesktopShell() {
  const collapsed = usePrefs(s => s.sidebarCollapsed);
  const width = usePrefs(s => s.sidebarWidth);
  const selection = useApp(s => s.selection);
  return (
    <div className="flex h-full min-h-0 bg-background-win-alt">
      <aside
        className={cn('relative shrink-0 overflow-hidden transition-[width] duration-200 ease-out', collapsed && 'w-0')}
        style={collapsed ? undefined : { width }}
        aria-hidden={collapsed || undefined}
        inert={collapsed || undefined}
      >
        <div className="h-full" style={{ width }}>
          <Sidebar />
        </div>
        {!collapsed && <Resizer />}
      </aside>
      <main className={cn('relative flex min-w-0 flex-1 flex-col overflow-hidden bg-background', 'my-1.5 mr-1.5 rounded-xl border border-border/60 shadow-xs', collapsed && 'ml-1.5')}>
        {selection ? <SessionWorkspace mobile={false} /> : <EmptyState />}
      </main>
      <WindowControls />
    </div>
  );
}

/** Drag to resize (200–480 px, at most 40% of the window); double-click resets; arrows nudge. */
function Resizer() {
  const set = usePrefs(s => s.set);
  const width = usePrefs(s => s.sidebarWidth);
  const start = useRef<{ x: number; w: number } | null>(null);
  const clamp = (w: number) => Math.round(Math.min(SIDEBAR_MAX, window.innerWidth * 0.4, Math.max(SIDEBAR_MIN, w)));
  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={t('Resize sidebar', '调整侧栏宽度')}
      aria-valuenow={width}
      aria-valuemin={SIDEBAR_MIN}
      aria-valuemax={SIDEBAR_MAX}
      tabIndex={0}
      className="absolute inset-y-0 right-0 z-10 w-1.5 cursor-col-resize"
      onPointerDown={e => {
        start.current = { x: e.clientX, w: width };
        e.currentTarget.setPointerCapture(e.pointerId);
      }}
      onPointerMove={e => { if (start.current) set({ sidebarWidth: clamp(start.current.w + e.clientX - start.current.x) }); }}
      onPointerUp={() => { start.current = null; }}
      onDoubleClick={() => set({ sidebarWidth: SIDEBAR_DEFAULT })}
      onKeyDown={e => {
        const step = e.shiftKey ? 40 : 10;
        if (e.key === 'ArrowLeft') set({ sidebarWidth: clamp(width - step) });
        else if (e.key === 'ArrowRight') set({ sidebarWidth: clamp(width + step) });
        else if (e.key === 'Home') set({ sidebarWidth: SIDEBAR_MIN });
        else if (e.key === 'End') set({ sidebarWidth: clamp(SIDEBAR_MAX) });
      }}
    />
  );
}
