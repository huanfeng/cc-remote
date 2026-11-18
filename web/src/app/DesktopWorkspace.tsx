import { useEffect, useRef, type ReactNode } from 'react';
import { dismissOverlayShortcut, overlayHistory } from '@/lib/overlayHistory';
import { ArrowLeft, ArrowRight, Info, List, MoreHorizontal, Plus, RotateCw, Settings, Undo2, X } from 'lucide-react';
import { useHosts } from '@/lib/hosts';
import { t } from '@/lib/i18n';
import { cn } from '@/components/lib/utils';
import { StandaloneHome } from '@/features/standalone/StandaloneHome';
import { DesktopAboutDialog } from '@/features/settings/DesktopUpdate';
import { HostGlyph } from '@/features/settings/parts';
import { openDesktopAbout, subscribeDesktopUpdate, useDesktopUpdate } from '@/platform/desktopUpdate';
import { activateDesktopTab, closeDesktopTab, desktopShortcut, newDesktopWindow, reconcileDesktopTabs, reorderDesktopTab, reopenDesktopTab, sendDesktopTabCommand, showDesktopHosts, showDesktopMenu, subscribeDesktopWorkspace, useDesktopWorkspace } from '@/platform/desktopWorkspace';
import { WindowControls } from './WindowControls';
import { installDesktopTitlebar } from '@/platform/desktopTitlebar';

const iconButton = 'flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-foreground-subtle hover:bg-surface-hover hover:text-foreground disabled:pointer-events-none disabled:opacity-30';

function HeaderButton({ label, disabled, onClick, children }: { label: string; disabled?: boolean; onClick(): void; children: ReactNode }) {
  return <button type="button" title={label} aria-label={label} disabled={disabled} onClick={onClick} className={iconButton}>{children}</button>;
}

/** The main Webview owns only chrome. Server Webviews occupy the area below y=40. */
export function DesktopWorkspace() {
  const { tabs, active, panel, error, closed } = useDesktopWorkspace();
  const hosts = useHosts(s => s.hosts);
  const status = useDesktopUpdate(s => s.status);
  const aboutOpen = useDesktopUpdate(s => s.aboutOpen);
  const selected = tabs.find(x => x.label === active);
  const list = useRef<HTMLDivElement>(null);
  const titlebar = useRef<HTMLElement>(null);
  const visible = !panel && !aboutOpen && !error;
  useEffect(() => subscribeDesktopWorkspace(), []);
  useEffect(() => subscribeDesktopUpdate(), []);
  useEffect(() => {
    if (titlebar.current && window.__CC_DESKTOP__) return installDesktopTitlebar(titlebar.current, window.__CC_DESKTOP__.platform);
  }, []);
  useEffect(() => {
    document.title = selected?.title || `${hosts.find(x => x.id === selected?.hostId)?.name || ''}${selected ? ' · ' : ''}CC Remote`;
  }, [selected?.title, selected?.hostId, hosts]);
  useEffect(() => { list.current?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest', inline: 'nearest' }); }, [active, visible]);
  useEffect(() => {
    const keydown = (event: KeyboardEvent) => {
      if (dismissOverlayShortcut(event)) return;
      if (event.isComposing) return;
      const key = event.key.toLowerCase();
      const modifier = event.ctrlKey || event.metaKey;
      if ((!modifier && key === 'f5') || (modifier && key === 'r')) {
        event.preventDefault();
        if (selected && visible) void sendDesktopTabCommand({ action: 'reload' });
        return;
      }
      if (event.altKey && !modifier && (key === 'arrowleft' || key === 'arrowright')) {
        event.preventDefault();
        if (key === 'arrowleft' && overlayHistory().dismiss()) return;
        if (selected && visible) void sendDesktopTabCommand({ action: key === 'arrowleft' ? 'back' : 'forward' });
        return;
      }
      if (!modifier || event.altKey) return;
      const action = key === 'tab' ? (event.shiftKey ? 'previous' : 'next')
        : key === 't' ? (event.shiftKey ? 'reopen' : 'new') : key === 'w' ? 'close'
          : key === 'n' && event.shiftKey ? 'new-window' : undefined;
      if (action) { event.preventDefault(); desktopShortcut(action); }
    };
    window.addEventListener('keydown', keydown);
    return () => window.removeEventListener('keydown', keydown);
  }, [selected, visible]);

  const nameOf = (id: string) => hosts.find(x => x.id === id)?.name || t('Host', '宿主');
  const settings = () => {
    if (!selected) { showDesktopHosts(); return; }
    activateDesktopTab(selected.label);
    void sendDesktopTabCommand({ action: 'settings' });
  };
  return <div className="flex h-full min-h-0 flex-col bg-background-win-alt text-foreground">
    <header ref={titlebar} className="desktop-titlebar flex h-10 shrink-0 select-none items-center gap-1 border-b border-border/70 px-1 platform-mac-desktop:pl-[78px]" aria-label={t('Window title bar', '窗口标题栏')}>
      <div title={t('Drag to move; double-click to maximize', '拖动移动窗口，双击最大化')}
        className="flex h-8 shrink-0 items-center gap-1.5 px-2 text-ui-xs">
        <HostGlyph icon="logo" color="#293c2e" className="size-5 rounded-md [&_svg]:size-3" />
        <span className="font-semibold">CC Remote</span><span className="text-foreground-subtlest">{status?.currentVersion ? `v${status.currentVersion}` : '…'}</span>
        {status?.version && <span className="size-1.5 rounded-full bg-success" aria-label={t('Update available', '有可用更新')} />}
      </div>
      <nav className="flex shrink-0 items-center" aria-label={t('Tab navigation', '标签页导航')}>
        <HeaderButton label={t('Back · Alt+Left', '后退 · Alt+←')} disabled={!visible || !selected?.canBack} onClick={() => { void sendDesktopTabCommand({ action: 'back' }); }}><ArrowLeft className="size-3.5" /></HeaderButton>
        <HeaderButton label={t('Forward · Alt+Right', '前进 · Alt+→')} disabled={!visible || !selected?.canForward} onClick={() => { void sendDesktopTabCommand({ action: 'forward' }); }}><ArrowRight className="size-3.5" /></HeaderButton>
        <HeaderButton label={t('Refresh this tab · Ctrl+R', '刷新当前标签页 · Ctrl+R')} disabled={!visible || !selected} onClick={() => { void sendDesktopTabCommand({ action: 'reload' }); }}><RotateCw className="size-3.5" /></HeaderButton>
      </nav>
      <div ref={list} role="tablist" aria-label={t('Server tabs', '服务器标签页')}
        className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
        onWheel={event => { if (Math.abs(event.deltaY) > Math.abs(event.deltaX)) { event.currentTarget.scrollLeft += event.deltaY; } }}>
        {tabs.map(tab => {
          const host = hosts.find(x => x.id === tab.hostId);
          const current = active === tab.label && visible;
          return <div key={tab.label} draggable onDragStart={event => { event.dataTransfer.setData('application/cc-remote-tab', tab.label); event.dataTransfer.effectAllowed = 'move'; }}
            onDragOver={event => { if (event.dataTransfer.types.includes('application/cc-remote-tab')) event.preventDefault(); }}
            onDrop={event => { const from = event.dataTransfer.getData('application/cc-remote-tab'); if (from) { event.preventDefault(); reorderDesktopTab(from, tab.label); } }}
            onAuxClick={event => { if (event.button === 1) { event.preventDefault(); closeDesktopTab(tab.label); } }}
            className={cn('group flex h-8 w-44 min-w-24 max-w-48 shrink-0 items-center rounded-md border', current ? 'border-border/80 bg-card shadow-xs' : 'border-transparent hover:bg-surface-hover')}>
            <button id={`tab-${tab.label}`} type="button" role="tab" aria-selected={current} tabIndex={active === tab.label ? 0 : -1}
              title={[nameOf(tab.hostId), tab.error || (tab.phase === 'ready' ? t('Connected', '已连接') : tab.phase === 'connecting' ? t('Connecting…', '连接中…') : t('Disconnected', '未连接')), tab.waiting ? t(`${tab.waiting} waiting`, `${tab.waiting} 项待处理`) : ''].filter(Boolean).join(' · ')}
              className="flex h-full min-w-0 flex-1 items-center gap-2 pl-2 text-left text-ui-xs" onClick={() => activateDesktopTab(tab.label)}
              onKeyDown={event => {
                const index = tabs.indexOf(tab);
                const next = event.key === 'ArrowRight' ? tabs[(index + 1) % tabs.length] : event.key === 'ArrowLeft' ? tabs[(index - 1 + tabs.length) % tabs.length] : event.key === 'Home' ? tabs[0] : event.key === 'End' ? tabs[tabs.length - 1] : undefined;
                if (next) { event.preventDefault(); activateDesktopTab(next.label); document.getElementById(`tab-${next.label}`)?.focus(); }
              }}>
              <span className={cn('size-1.5 shrink-0 rounded-full', tab.phase === 'connecting' || !tab.phase ? 'animate-pulse bg-warning' : tab.error || tab.phase !== 'ready' ? 'bg-foreground-subtlest' : '')} style={tab.phase === 'ready' && !tab.error ? { backgroundColor: host?.color || 'var(--color-success)' } : undefined} />
              <span className="min-w-0 flex-1 truncate">{nameOf(tab.hostId)}</span>
              {tab.waiting > 0 && <span className="rounded bg-warning/15 px-1 text-[10px] text-warning">{tab.waiting}</span>}
            </button>
            <button type="button" aria-label={t(`Close ${nameOf(tab.hostId)}`, `关闭 ${nameOf(tab.hostId)}`)} title={t('Close tab · Ctrl+W', '关闭标签页 · Ctrl+W')}
              className="mx-1 flex size-5 shrink-0 items-center justify-center rounded text-foreground-subtlest hover:bg-surface-hover hover:text-foreground" onClick={() => closeDesktopTab(tab.label)}><X className="size-3" /></button>
          </div>;
        })}
      </div>
      <HeaderButton label={t('New tab · Ctrl+T', '新建标签页 · Ctrl+T')} onClick={showDesktopHosts}><Plus className="size-4" /></HeaderButton>
      <div className="h-full min-w-8 shrink-0 self-stretch" title={t('Drag to move; double-click to maximize', '拖动移动窗口，双击最大化')} />
      <HeaderButton label={t('Tabs and window menu', '标签页与窗口菜单')} onClick={showDesktopMenu}><MoreHorizontal className="size-4" /></HeaderButton>
      <WindowControls />
    </header>
    <main className="relative min-h-0 flex-1">
      {error ? <div role="alert" className="m-auto flex max-w-lg flex-col gap-3 p-10 text-ui-sm"><strong>{t('Could not open desktop tab', '无法打开桌面标签页')}</strong><p className="break-words text-foreground-subtle">{error}</p>
        <button type="button" className="self-start rounded border px-3 py-1.5 hover:bg-surface-hover" onClick={() => { useDesktopWorkspace.setState({ error: '' }); reconcileDesktopTabs(); }}>{t('Retry', '重试')}</button></div>
        : panel === 'menu' ? <div className="mx-auto flex h-full max-w-2xl flex-col gap-5 overflow-auto px-6 py-8">
          <div className="flex items-center justify-between"><h1 className="text-ui-lg font-semibold">{t('Tabs and window', '标签页与窗口')}</h1><button className={iconButton} onClick={showDesktopMenu} aria-label={t('Close menu', '关闭菜单')}><X className="size-4" /></button></div>
          <div className="grid grid-cols-2 gap-2">
            <MenuAction icon={<Plus className="size-4" />} onClick={showDesktopHosts} shortcut="Ctrl T">{t('New tab', '新建标签页')}</MenuAction>
            <MenuAction icon={<List className="size-4" />} onClick={newDesktopWindow} shortcut="Ctrl Shift N">{t('New window', '新建窗口')}</MenuAction>
            <MenuAction icon={<Undo2 className="size-4" />} onClick={reopenDesktopTab} disabled={!closed.length} shortcut="Ctrl Shift T">{t('Reopen closed tab', '恢复关闭的标签页')}</MenuAction>
            <MenuAction icon={<Settings className="size-4" />} onClick={settings} disabled={!selected}>{t('Current host settings', '当前宿主设置')}</MenuAction>
            <MenuAction icon={<Info className="size-4" />} onClick={openDesktopAbout}>{t('About & updates', '关于与更新')}</MenuAction>
          </div>
          <div className="space-y-1"><h2 className="mb-2 text-ui-xs text-foreground-subtle">{t('Open tabs · Ctrl Tab to switch', '已打开的标签页 · Ctrl Tab 切换')}</h2>
            {tabs.map(tab => <button key={tab.label} onClick={() => activateDesktopTab(tab.label)} className="flex w-full items-center gap-3 rounded-lg border border-border/50 px-3 py-2 text-left hover:bg-surface-hover"><span className="min-w-0 flex-1 truncate text-ui-sm">{nameOf(tab.hostId)}</span>{tab.waiting > 0 && <span className="text-ui-xs text-warning">{t(`${tab.waiting} waiting`, `${tab.waiting} 项待处理`)}</span>}{tab.label === active && <span className="text-ui-xs text-foreground-subtlest">{t('Current', '当前')}</span>}</button>)}
          </div>
          <p className="text-ui-xs leading-5 text-foreground-subtlest">{t('Closing a tab leaves its host sessions running. Drag tabs to reorder; middle-click to close. The window close button keeps the main app in the system tray.', '关闭标签页后，宿主上的会话继续运行。拖动标签页可调整顺序，中键点击可关闭。主窗口关闭后保留在系统托盘。')}</p>
        </div> : panel === 'hosts' || !selected ? <StandaloneHome choose /> : <div className="flex h-full items-center justify-center text-ui-sm text-foreground-subtlest">{t('Opening host…', '正在打开宿主…')}</div>}
    </main>
    <DesktopAboutDialog />
  </div>;
}

function MenuAction({ icon, onClick, shortcut, disabled, children }: { icon: ReactNode; onClick(): void; shortcut?: string; disabled?: boolean; children: ReactNode }) {
  return <button type="button" disabled={disabled} onClick={onClick} className="flex items-center gap-2 rounded-lg border border-border/60 bg-card px-3 py-3 text-left text-ui-sm hover:bg-surface-hover disabled:opacity-40">
    {icon}<span className="flex-1">{children}</span>{shortcut && <span className="text-[10px] text-foreground-subtlest">{shortcut}</span>}
  </button>;
}
