import { ArrowRightLeft, Import } from 'lucide-react';
import { Button } from '@/components/ui/button';
import type { Provider } from '@/backend/types';
import { useApp } from '@/store/app';
import { t } from '@/lib/i18n';

/** Terminal history is read-only: continue it as a terminal session or import it. */
export function NativeFooter({ id, provider, cwd }: { id: string; provider: Provider; cwd: string }) {
  const open = useApp(s => s.open);
  return (
    <div className="mx-auto flex w-full max-w-3xl shrink-0 flex-wrap items-center justify-center gap-2 px-4 py-3 pb-safe">
      <span className="w-full text-center text-ui-sm text-foreground-subtlest">{t('Terminal history is read-only.', '终端历史为只读。')}</span>
      {provider === 'claude' && (
        <Button variant="outline" size="lg" onClick={() => open({ type: 'new-session', resume: { id, provider, cwd } })}>
          <ArrowRightLeft className="size-4" /> {t('Continue as terminal session', '以终端会话继续')}
        </Button>
      )}
      <Button variant="outline" size="lg" onClick={() => open({ type: 'new-session', resume: { id, provider, cwd } })}>
        <Import className="size-4" /> {t('Import as managed session', '导入为托管会话')}
      </Button>
    </div>
  );
}
