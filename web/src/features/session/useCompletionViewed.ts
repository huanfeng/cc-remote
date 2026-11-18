import { useEffect } from 'react';
import type { Session } from '@/backend/types';
import { useApp } from '@/store/app';
import { useCompletionRead } from '@/store/completionRead';
import { canViewCompletion, isCompletionPending } from '@/domain/completionRead';
import { hostKey } from '@/lib/hosts';

/** Wait for the loaded latest output, an unobscured foreground view, and a paint. */
export function useCompletionViewed(session: Session, latest: () => { loaded: boolean; lastSeq: number; atBottom: boolean }) {
  const records = useCompletionRead(s => s.records);
  const obscured = useApp(s => !!s.surface || s.drawerOpen);
  useEffect(() => {
    if (!isCompletionPending(session, records[hostKey(session.id)])) return;
    let frame = 0;
    const check = () => {
      if (!canViewCompletion(session, { ...latest(), visible: !document.hidden, focused: document.hasFocus(), obscured })) return;
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        if (canViewCompletion(session, { ...latest(), visible: !document.hidden, focused: document.hasFocus(), obscured })) useCompletionRead.getState().markViewed(session);
      });
    };
    // Scrolling in xterm and scrolling a conversation use different event sources.
    const timer = window.setInterval(check, 500);
    check();
    return () => { clearInterval(timer); cancelAnimationFrame(frame); };
  }, [session, records, obscured, latest]);
}
