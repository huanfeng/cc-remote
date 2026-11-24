import { useEffect, useId, useRef } from 'react';
import { getCurrentWebview } from '@tauri-apps/api/webview';
import { X } from 'lucide-react';
import { Popover as PopoverPrimitive } from 'radix-ui';
import { Button } from '@/components/ui/button';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { t } from '@/lib/i18n';
import { requestDesktopTabClose, resolveDesktopTabClose, useDesktopWorkspace } from '@/platform/desktopWorkspace';

export function DesktopTabClose({ label, name }: { label: string; name: string }) {
  const open = useDesktopWorkspace(state => state.closingTab === label);
  const trigger = useRef<HTMLButtonElement>(null);
  const title = useId();
  const cancel = () => resolveDesktopTabClose(label);

  useEffect(() => {
    if (!open) return;
    trigger.current?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    if (!document.hasFocus()) void getCurrentWebview().setFocus().catch(console.error);
    const dismiss = () => resolveDesktopTabClose(label);
    window.addEventListener('blur', dismiss);
    return () => window.removeEventListener('blur', dismiss);
  }, [label, open]);

  return (
    <Popover open={open} onOpenChange={show => show ? requestDesktopTabClose(label) : cancel()}>
      <PopoverTrigger asChild>
        <button ref={trigger} type="button" aria-label={t(`Close ${name}`, `关闭 ${name}`)} title={t('Close tab · Ctrl+W', '关闭标签页 · Ctrl+W')}
          className="mx-1 flex size-5 shrink-0 items-center justify-center rounded text-foreground-subtlest hover:bg-surface-hover hover:text-foreground aria-expanded:bg-surface-hover aria-expanded:text-foreground">
          <X className="size-3" />
        </button>
      </PopoverTrigger>
      {/* The native host view covers the area below the 40px title bar. */}
      <PopoverContent side="left" sideOffset={6} collisionPadding={3} aria-labelledby={title}
        className="h-8 w-max flex-row items-center gap-1 overflow-visible rounded-lg px-1 py-0">
        <span id={title} className="px-1 text-ui-xs font-medium">{t('Close this tab?', '关闭此标签页？')}</span>
        <Button size="sm" variant="ghost" className="focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-ring" onClick={cancel}>
          {t('Cancel', '取消')}
        </Button>
        <Button size="sm" variant="destructive" onClick={() => resolveDesktopTabClose(label, true)}>{t('Close tab', '关闭')}</Button>
        <PopoverPrimitive.Arrow className="fill-popover" />
      </PopoverContent>
    </Popover>
  );
}
