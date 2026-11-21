import type { ProjectShell } from '@/backend/types';
import { t } from '@/lib/i18n';

export function projectShellLabel(shell: ProjectShell, fallbackOrdinal?: number): string {
  const ordinal = shell.ordinal ?? fallbackOrdinal;
  return shell.name || (ordinal && ordinal > 0 ? t(`Terminal ${ordinal}`, `终端 ${ordinal}`) : t('Terminal', '终端'));
}
