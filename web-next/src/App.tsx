import { useEffect, useMemo } from 'react';
import { Tooltip as TooltipPrimitive } from 'radix-ui';
import { BackendContext } from '@/backend/backend';
import { MockBackend } from '@/backend/mock/mockBackend';
import { demoHosts } from '@/backend/mock/fixtures';
import { useApp } from '@/store/app';
import { releaseMissing } from '@/store/transcripts';
import { useHosts } from '@/lib/hosts';
import { useApplyTheme } from '@/lib/theme';
import { useIsMobile } from '@/lib/hooks';
import { t } from '@/lib/i18n';
import { trackVisualViewport } from '@/domain/viewport';
import { toast } from '@/components/ui/toast';
import { DesktopShell } from '@/app/DesktopShell';
import { MobileShell } from '@/app/MobileShell';
import { Surfaces } from '@/features/dialogs/Surfaces';
import { useGlobalShortcuts } from '@/app/shortcuts';

export function App() {
  // Phase 1 runs against the in-memory host; phase 2 swaps in HttpBackend here.
  const backend = useMemo(() => new MockBackend(), []);
  useApplyTheme();
  useEffect(() => trackVisualViewport(), []);

  useEffect(() => {
    if (backend.kind === 'mock') useHosts.getState().set({ hosts: demoHosts, currentId: demoHosts[0].id });
    let live = true;
    backend.info()
      .then(i => { if (live) useApp.getState().setInfo(i); })
      .catch(e => { if (live) toast(t(`Could not reach the host: ${e instanceof Error ? e.message : e}`, `无法连接宿主：${e instanceof Error ? e.message : e}`)); });
    const stop = backend.watchSessions(s => {
      useApp.getState().setSessions(s);
      // Sessions gone from the host (deleted here or elsewhere) release their cache and draft.
      releaseMissing(new Set(s.map(x => x.id)));
    });
    return () => { live = false; stop(); };
  }, [backend]);

  return (
    <BackendContext.Provider value={backend}>
      <TooltipPrimitive.Provider delayDuration={400} skipDelayDuration={200}>
        <Shell />
        <Surfaces />
      </TooltipPrimitive.Provider>
    </BackendContext.Provider>
  );
}

function Shell() {
  const mobile = useIsMobile();
  useGlobalShortcuts();
  return mobile ? <MobileShell /> : <DesktopShell />;
}
