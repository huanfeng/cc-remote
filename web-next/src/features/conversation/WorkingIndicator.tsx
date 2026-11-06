// The line at the end of a running transcript: "Claude is working…" with the
// live activity, "Compacting context…", or — once the turn ended — the
// background tasks that outlive it.
import { memo } from 'react';
import { LayersIcon } from 'lucide-react';
import type { Session, SessionEvent } from '@/backend/types';
import { isCompacting } from '@/domain/toolGroups';
import { providerName, providerOf } from '@/domain/providers';
import { useApp } from '@/store/app';
import { t } from '@/lib/i18n';

export const WorkingIndicator = memo(function WorkingIndicator({ session, events }: { session: Session; events: SessionEvent[] }) {
  const running = session.status === 'running';
  const background = session.activeTasks ?? 0;
  if (running) {
    const compacting = isCompacting(events);
    const name = providerName(providerOf(session));
    const activity = session.live?.activity;
    return (
      <div className="mt-4 flex min-w-0 items-center gap-2 px-1.5 text-ui-base" role="status" aria-live="polite">
        <span className="relative flex size-2 shrink-0">
          <span className="absolute inline-flex size-full animate-ping rounded-full bg-brand opacity-40 motion-reduce:hidden" />
          <span className="relative inline-flex size-2 rounded-full bg-brand" />
        </span>
        <span className="animated-gradient-text shrink-0 font-medium">
          {compacting ? t('Compacting context…', '正在压缩上下文…') : t(`${name} is working…`, `${name} 正在工作…`)}
        </span>
        {activity && !compacting && <span className="min-w-0 truncate text-ui-sm text-foreground-subtlest">{activity}</span>}
      </div>
    );
  }
  if (background > 0) {
    return (
      <div className="mt-4 flex items-center gap-2 px-1.5 text-ui-sm text-foreground-subtle" role="status">
        <LayersIcon className="size-3.5 text-foreground-subtlest" />
        <span>{t(`${background} background task${background > 1 ? 's' : ''} still running`, `${background} 个后台任务仍在运行`)}</span>
        <button type="button" onClick={() => useApp.getState().open({ type: 'tasks', id: session.id })}
          className="rounded-md px-1 text-icon-blue hover:underline">
          {t('View', '查看')}
        </button>
      </div>
    );
  }
  return null;
});
