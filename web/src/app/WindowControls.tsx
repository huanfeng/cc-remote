// Minimise / maximise / close for the Tauri desktop shell on Windows and
// Linux (macOS keeps its native traffic lights). Rendered only inside the
// desktop app; platform/desktop.ts sets `.desktop-shell` and the platform
// class on <html> and provides window.__CC_DESKTOP__.
import { useEffect, useState } from 'react';
import { getCurrentWindow } from '@tauri-apps/api/window';
import { Copy, Minus, Square, X } from 'lucide-react';
import { cn } from '@/components/lib/utils';
import { t } from '@/lib/i18n';
import type {} from '@/platform/desktop';

export function WindowControls() {
  const [maximized, setMaximized] = useState(false);
  const bridge = typeof window !== 'undefined' ? window.__CC_DESKTOP__ : undefined;
  useEffect(() => {
    if (!bridge) return;
    const win = getCurrentWindow();
    let disposed = false;
    let stop: (() => void) | undefined;
    const update = () => { void win.isMaximized().then(value => { if (!disposed) setMaximized(value); }).catch(() => {}); };
    update();
    void win.onResized(update).then(unlisten => { if (disposed) unlisten(); else stop = unlisten; });
    return () => { disposed = true; stop?.(); };
  }, [bridge]);
  if (!bridge || bridge.platform === 'mac') return null;
  const btn = 'flex h-10 w-11 items-center justify-center text-foreground-subtle hover:bg-surface-hover hover:text-foreground';
  return (
    <div className="-mr-1 flex shrink-0 self-stretch">
      <button type="button" aria-label={t('Minimize', '最小化')} className={btn} onClick={bridge.minimize}><Minus className="size-3.5" /></button>
      <button type="button" aria-label={maximized ? t('Restore', '还原') : t('Maximize', '最大化')} title={maximized ? t('Restore', '还原') : t('Maximize', '最大化')} className={btn} onClick={bridge.toggleMaximize}>{maximized ? <Copy className="size-3" /> : <Square className="size-3" />}</button>
      <button type="button" aria-label={t('Close', '关闭')} className={cn(btn, 'hover:bg-destructive hover:text-destructive-foreground')} onClick={bridge.close}><X className="size-4" /></button>
    </div>
  );
}
