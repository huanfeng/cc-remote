import { useEffect, useEffectEvent, useRef, useState } from 'react';
import { ExternalLink } from 'lucide-react';
import type { CodexAccountsAPI } from '@/backend/backend';
import type { CodexAccount, CodexAccountLogin } from '@/backend/types';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Spinner } from '@/components/ui/spinner';
import { SurfaceShell } from '@/features/dialogs/SurfaceShell';
import { CopyField, ErrorText, errorMessage, errorStatus } from '@/features/dialogs/common';
import { t } from '@/lib/i18n';

function verificationLink(value: string): string | undefined {
  try {
    const url = new URL(value);
    if (url.origin === 'https://auth.openai.com' && url.pathname === '/codex/device' &&
        !url.username && !url.password && !url.search && !url.hash) return url.href;
  } catch { /* Ignore a malformed verification URL. */ }
}

export function CodexAccountEditor({ api, mobile, account, onSaved, onClose }: {
  api: CodexAccountsAPI;
  mobile: boolean;
  account?: CodexAccount;
  onSaved: () => void;
  onClose: () => void;
}) {
  const [name, setName] = useState(account?.name ?? '');
  const [login, setLogin] = useState<CodexAccountLogin>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const lifecycle = useRef<{ loginId?: string }>(null);
  const saved = useEffectEvent(() => { onSaved(); onClose(); });

  useEffect(() => {
    const active: { loginId?: string } = {};
    lifecycle.current = active;
    return () => {
      lifecycle.current = null;
      if (active.loginId) void api.cancel(active.loginId).catch(() => {});
    };
  }, [api]);

  useEffect(() => {
    const active = lifecycle.current;
    if (!login || !active) return;
    return api.watch(login.id, reply => {
      if (reply.status === 'pending') { setError(''); return; }
      active.loginId = undefined;
      setLogin(undefined);
      if (reply.status === 'complete') saved();
      else if (reply.status === 'failed') setError(reply.error || t('Sign-in failed. Request a new code to try again.', '登录失败。请重新获取设备码后重试。'));
    }, err => {
      const status = errorStatus(err);
      if (status === 404 || status === 410) {
        active.loginId = undefined;
        setLogin(undefined);
      }
      setError(errorMessage(err));
    });
  }, [api, login]);

  const submit = async () => {
    const active = lifecycle.current;
    const displayName = name.trim();
    if (!active || busy || login || !displayName) return;
    setBusy(true);
    setError('');
    try {
      if (account) {
        await api.rename(account.id, displayName);
        if (lifecycle.current === active) { onSaved(); onClose(); }
        return;
      }
      const reply = await api.begin(displayName);
      const url = verificationLink(reply.verificationUrl);
      if (lifecycle.current !== active || !url) {
        void api.cancel(reply.id).catch(() => {});
        if (lifecycle.current === active) setError(t('The host returned an invalid OpenAI verification link.', '宿主返回的 OpenAI 验证链接无效。'));
        return;
      }
      active.loginId = reply.id;
      setLogin({ ...reply, verificationUrl: url });
    } catch (err) {
      if (lifecycle.current === active) setError(errorMessage(err));
    } finally {
      if (lifecycle.current === active) setBusy(false);
    }
  };

  const close = () => { if (!busy) onClose(); };

  return (
    <SurfaceShell open mobile={mobile} onOpenChange={open => { if (!open) close(); }}
      title={account ? t('Rename ChatGPT account', '重命名 ChatGPT 账号') : t('Add ChatGPT account', '添加 ChatGPT 账号')}
      footer={<>
        <Button variant="ghost" disabled={busy} onClick={close}>
          {login ? t('Cancel sign-in', '取消登录') : t('Cancel', '取消')}
        </Button>
        {!login && <Button type="submit" form="codex-account-editor" disabled={busy || !name.trim()}>
          {busy && <Spinner className="size-3.5" />}
          {account ? t('Save name', '保存名称') : t('Get device code', '获取设备码')}
        </Button>}
      </>}>
      {login ? <div className="space-y-4 text-ui-sm">
        <p className="text-foreground-subtle">{t('Open the official OpenAI page, sign in with the ChatGPT account you want to add, and enter this code.', '打开 OpenAI 官方页面，登录要添加的 ChatGPT 账号并输入下方设备码。')}</p>
        <CopyField value={login.userCode} />
        <Button asChild variant="outline">
          <a href={login.verificationUrl} target="_blank" rel="noopener noreferrer">
            <ExternalLink className="size-4" />{t('Open OpenAI verification', '打开 OpenAI 验证页面')}
          </a>
        </Button>
        <p role="status" className="flex items-center gap-2 text-foreground-subtle">
          <Spinner className="size-3.5" />{t('Waiting for authorization…', '等待授权…')}
        </p>
      </div> : <form id="codex-account-editor" className="space-y-4 text-ui-sm" onSubmit={event => { event.preventDefault(); void submit(); }}>
        {!account && <p className="text-foreground-subtle">{t('If required, enable device-code sign-in in your ChatGPT security settings or ask your workspace admin to enable it.', '如果需要，请在 ChatGPT 安全设置中启用设备码登录，或请工作区管理员启用。')}</p>}
        <label className="block space-y-2">
          <span>{t('Account display name', '账号显示名称')}</span>
          <Input autoFocus size="lg" value={name} disabled={busy} maxLength={60} autoComplete="off" placeholder={t('For example: work account', '例如：工作账号')} onChange={event => setName(event.target.value)} />
        </label>
      </form>}
      {error && <div className="mt-3"><ErrorText>{error}</ErrorText></div>}
    </SurfaceShell>
  );
}
