// Signing out of the host that served this page: the token saved on this
// device, its drafts and the session route are cleared. A standalone page
// forgets a host from its host list instead (the sidebar's host switcher).
import { useState } from 'react';
import { LogOut } from 'lucide-react';
import { useBackend } from '@/backend/backend';
import { STANDALONE } from '@/lib/hosts';
import { useIdentity } from '@/lib/identity';
import { t } from '@/lib/i18n';
import { ConfirmDialog } from '@/features/dialogs/common';
import { useConnection } from './connection';

export function SignOutRow() {
  const backend = useBackend();
  const id = useIdentity();
  const [asking, setAsking] = useState(false);
  if (STANDALONE || backend.kind !== 'http') return null;
  return (
    <>
      <button type="button" onClick={() => setAsking(true)}
        className="flex w-full items-center gap-2 border-t border-border/60 px-4 py-3 text-left text-ui-base text-destructive hover:bg-surface-hover">
        <LogOut className="size-4" />
        <span className="flex-1">{t('Sign out', '退出登录')}<span className="ml-2 text-ui-sm text-foreground-subtlest">{t('Clears the token saved on this device', '清除此设备保存的令牌')}</span></span>
      </button>
      <ConfirmDialog
        open={asking}
        onOpenChange={setAsking}
        title={t(`Sign out of ${id.name}?`, `退出 ${id.name}？`)}
        description={t('The token saved on this device is cleared; you need it to sign in again.', '此设备保存的令牌会被清除，再次登录需要重新输入令牌。')}
        confirmLabel={t('Sign out', '退出登录')}
        destructive
        onConfirm={() => { setAsking(false); useConnection.getState().signOut(); }}
      />
    </>
  );
}
