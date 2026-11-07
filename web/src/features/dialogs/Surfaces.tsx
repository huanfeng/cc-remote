// Mounts the surface (dialog, sheet, palette) named by useApp().surface. A
// closed surface stays mounted for its exit animation, then unmounts; a new
// surface replaces the current one at once. Also runs the notifications.
import { useEffect, useState } from 'react';
import { useApp, type Surface } from '@/store/app';
import { useIsMobile } from '@/lib/hooks';
import { CommandPalette } from '@/features/palette/CommandPalette';
import { SettingsPage } from '@/features/settings/SettingsPage';
import { StatusPanel } from '@/features/status/StatusPanel';
import { SessionDetails } from '@/features/status/SessionDetails';
import { TaskPanel } from '@/features/status/TaskPanel';
import { NewSessionDialog } from './NewSessionDialog';
import { AttachDialog } from './AttachDialog';
import { FileSurface } from './FileSurface';
import { useNotifications } from './notifications';

const EXIT_MS = 220;

const surfaceKey = (s: NonNullable<Surface>) => {
  switch (s.type) {
    case 'session-details': case 'tasks': case 'attach': return `${s.type}:${s.id}`;
    // Same key for another file of the session: FileViewer keeps its back stack.
    case 'file': return `file:${s.sessionId}`;
    case 'new-session': return `new:${s.resume?.id ?? ''}`;
    default: return s.type;
  }
};

export function Surfaces() {
  useNotifications();
  const surface = useApp(s => s.surface);
  const mobile = useIsMobile();
  const [shown, setShown] = useState<Surface>(surface);

  useEffect(() => {
    if (surface) { setShown(surface); return; }
    const id = window.setTimeout(() => setShown(null), EXIT_MS);
    return () => window.clearTimeout(id);
  }, [surface]);

  const current = surface ?? shown;
  if (!current) return null;
  const open = !!surface;
  const onOpenChange = (o: boolean) => {
    // Only close the surface this element shows; another may have replaced it.
    if (!o && useApp.getState().surface === surface) useApp.getState().close();
  };
  const key = surfaceKey(current);
  const common = { open, onOpenChange, mobile };

  switch (current.type) {
    case 'new-session': return <NewSessionDialog key={key} {...common} resume={current.resume} />;
    case 'settings': return <SettingsPage key={key} {...common} section={current.section} />;
    case 'status': return <StatusPanel key={key} {...common} />;
    case 'session-details': return <SessionDetails key={key} {...common} id={current.id} />;
    case 'tasks': return <TaskPanel key={key} {...common} id={current.id} />;
    case 'file': return <FileSurface key={key} {...common} sessionId={current.sessionId} path={current.path} base={current.base} />;
    case 'palette': return <CommandPalette key={key} {...common} />;
    case 'attach': return <AttachDialog key={key} {...common} id={current.id} />;
  }
}
