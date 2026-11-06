// Minimise / maximise / close for the Tauri desktop shell on Windows and
// Linux (macOS keeps its native traffic lights). Rendered only inside the
// desktop app; the phase-4 Tauri bootstrap sets `.desktop-shell` and the
// platform class on <html> and provides window.__CC_DESKTOP__.
import { Minus, Square, X } from 'lucide-react';
import { cn } from '@/components/lib/utils';
import { t } from '@/lib/i18n';

interface DesktopBridge {
  platform: 'windows' | 'linux' | 'mac';
  minimize(): void;
  toggleMaximize(): void;
  close(): void;
}

declare global {
  interface Window { __CC_DESKTOP__?: DesktopBridge }
}

export function WindowControls() {
  const bridge = typeof window !== 'undefined' ? window.__CC_DESKTOP__ : undefined;
  if (!bridge || bridge.platform === 'mac') return null;
  const btn = 'flex h-8 w-11 items-center justify-center text-foreground-subtle hover:bg-surface-hover hover:text-foreground';
  return (
    <div className="fixed right-0 top-0 z-[60] flex">
      <button type="button" aria-label={t('Minimize', '最小化')} className={btn} onClick={bridge.minimize}><Minus className="size-3.5" /></button>
      <button type="button" aria-label={t('Maximize', '最大化')} className={btn} onClick={bridge.toggleMaximize}><Square className="size-3" /></button>
      <button type="button" aria-label={t('Close', '关闭')} className={cn(btn, 'hover:bg-destructive hover:text-destructive-foreground')} onClick={bridge.close}><X className="size-4" /></button>
    </div>
  );
}
