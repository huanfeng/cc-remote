// The desktop app's "This computer" section: the cc-remote service installed
// here (features/desktop/LocalService.tsx).
import { t } from '@/lib/i18n';
import { GroupCard } from '@/features/dialogs/SurfaceShell';
import { LocalServicePanel } from '@/features/desktop/LocalService';
import { SectionHeader } from './parts';

export function ComputerSection({ mobile }: { mobile: boolean }) {
  return (
    <div>
      {!mobile && (
        <SectionHeader
          title={t('This computer', '本机服务')}
          description={t('The cc-remote service installed on the computer running this app. Starting, stopping and restarting go through its own command (cc-remote service).', '运行本应用的电脑上安装的 cc-remote 服务。启动、停止、重启都通过它自己的命令（cc-remote service）完成。')}
        />
      )}
      <GroupCard className="px-4 py-3">
        <LocalServicePanel connectLabel={t('Add and open', '添加并打开')} />
      </GroupCard>
    </div>
  );
}
