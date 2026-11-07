// Phone layout (≤767 px), after ZCode's remote-control mobile shell: two
// pages, the session list (home) and the conversation, switched by the URL
// hash so the system back gesture returns home. Over a conversation the list
// is also available as a left drawer.
//
// The drawer is a Radix Dialog: it moves focus in, traps it, claims Escape
// (so Escape closes the drawer instead of reaching the "stop the running
// turn" listener) and returns focus to the trigger when it closes.
import { Dialog as DialogPrimitive } from 'radix-ui';
import { useApp } from '@/store/app';
import { t } from '@/lib/i18n';
import { Sidebar } from '@/features/sidebar/Sidebar';
import { SessionWorkspace } from '@/features/workspace/SessionWorkspace';

export function MobileShell() {
  const selection = useApp(s => s.selection);
  const drawerOpen = useApp(s => s.drawerOpen);
  const set = useApp(s => s.set);
  return (
    <div className="relative h-full min-h-0 overflow-hidden bg-background">
      {selection ? <SessionWorkspace mobile /> : <Sidebar variant="mobile" />}
      <DialogPrimitive.Root open={!!selection && drawerOpen} onOpenChange={open => set({ drawerOpen: open })}>
        <DialogPrimitive.Portal>
          <DialogPrimitive.Overlay
            className="fixed inset-0 z-40 bg-black/30 backdrop-blur-[2px] data-open:animate-in data-open:fade-in-0 data-closed:animate-out data-closed:fade-out-0"
          />
          <DialogPrimitive.Content
            aria-describedby={undefined}
            className="fixed inset-y-0 left-0 z-50 w-[min(86vw,22rem)] shadow-xl outline-none data-open:animate-in data-open:slide-in-from-left data-closed:animate-out data-closed:slide-out-to-left duration-200"
          >
            <DialogPrimitive.Title className="sr-only">{t('Sessions', '会话')}</DialogPrimitive.Title>
            <Sidebar variant="mobile" onClose={() => set({ drawerOpen: false })} />
          </DialogPrimitive.Content>
        </DialogPrimitive.Portal>
      </DialogPrimitive.Root>
    </div>
  );
}
