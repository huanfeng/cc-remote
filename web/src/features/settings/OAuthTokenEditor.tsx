import { useState } from 'react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Spinner } from '@/components/ui/spinner';
import { SurfaceShell } from '@/features/dialogs/SurfaceShell';
import { ConfirmDialog, CopyField, ErrorText } from '@/features/dialogs/common';
import { t } from '@/lib/i18n';
import { useReportDirty } from './parts';

export function OAuthTokenEditor({ mobile, replacing, onClose, onSave }: {
  mobile: boolean; replacing?: { id: string; name: string }; onClose: () => void;
  onSave: (name: string, token: string) => Promise<void>;
}) {
  const [name, setName] = useState(replacing?.name ?? '');
  const [token, setToken] = useState('');
  const [busy, setBusy] = useState(false);
  const [discard, setDiscard] = useState(false);
  const [error, setError] = useState('');
  const dirty = !!token || name !== (replacing?.name ?? '');
  useReportDirty('oauth-editor', dirty);
  const close = () => { if (!busy) { if (dirty) setDiscard(true); else onClose(); } };
  const save = async () => {
    if (busy) return;
    if (!name.trim() || !token.trim()) { setError(t('Enter an account name and OAuth token.', '请填写账号名称和 OAuth 令牌。')); return; }
    setBusy(true); setError('');
    try { await onSave(name.trim(), token.trim()); setToken(''); onClose(); }
    catch { setError(t('Could not save the token. Check your connection and server version, then try again.', '令牌保存失败，请检查连接和服务器版本后重试。')); }
    finally { setBusy(false); }
  };
  return <>
    <SurfaceShell open mobile={mobile} onOpenChange={o => { if (!o) close(); }} title={replacing ? t('Replace OAuth token', '更换 OAuth 令牌') : t('Add OAuth account', '添加 OAuth 账号')} description={t('Official Claude authentication for Claude Code.', '配置 Claude Code 的官方 Claude 账号授权。')} footer={<>
      <Button variant="ghost" disabled={busy} onClick={close}>{t('Cancel', '取消')}</Button>
      <Button type="submit" form="oauth-token-editor" disabled={busy || !name.trim() || !token.trim()}>{busy && <Spinner className="size-3.5" />}{t('Save account', '保存账号')}</Button>
    </>}>
      <form id="oauth-token-editor" className="space-y-4 text-ui-sm" onSubmit={e => { e.preventDefault(); void save(); }}>
        <p className="text-foreground-subtle">{t('Use your official Claude account. No API address or API key is required. Existing models, plugins and hooks on the server are retained.', '使用 Claude 官方账号，无需填写 API 地址或 API Key；会话沿用服务器原有的模型、插件和 hooks 配置。')}</p>
        <label className="block space-y-2"><span>{t('Account display name', '账号显示名称')}</span><Input autoFocus={!replacing} size="lg" value={name} disabled={busy || !!replacing} maxLength={60} autoComplete="off" placeholder={t('For example: work account', '例如：工作账号')} onChange={e => setName(e.target.value)} /></label>
        <label className="block space-y-2"><span>{replacing ? t('New OAuth token', '新的 OAuth 令牌') : t('OAuth token', 'OAuth 令牌')}</span><Input autoFocus={!!replacing} type="password" size="lg" autoComplete="new-password" spellCheck={false} disabled={busy} value={token} placeholder="sk-ant-oat…" onChange={e => setToken(e.target.value)} /></label>
        <p className="text-foreground-subtle">{t('Saved tokens are never shown or filled in. The entered token is submitted only when you save.', '已有令牌不会显示或回填。点击保存后才会提交本次填写的令牌。')}</p>
        <details className="rounded-xl border border-border/60 p-3">
          <summary className="cursor-pointer font-medium">{t('How to get an OAuth token', '如何获取 OAuth 令牌')}</summary>
          <div className="mt-3 space-y-3 text-foreground-subtle">
            <ol className="list-decimal space-y-2 pl-5"><li>{t('Open a terminal on a trusted computer with Claude Code installed.', '在已安装 Claude Code 的可信电脑上打开终端。')}</li><li>{t('Run the following command and follow the browser instructions to authorize your Claude account.', '运行下方命令，按照浏览器提示授权 Claude 账号。')}</li></ol>
            <CopyField value="claude setup-token" />
            <p>{t('Copy the generated OAuth token and paste it into the token field above.', '复制命令生成的 OAuth 令牌，粘贴到上方的令牌输入框。')}</p>
          </div>
        </details>
        <p className="text-foreground-subtle">{replacing ? t('Saving replaces this account’s token for future launches.', '保存后，此账号后续启动的会话将使用新令牌。') : t('After saving, select this account in Claude Code settings or a session. Saving does not change your default selection.', '保存后，可在 Claude Code 配置或会话中选择此账号。保存不会自动更改默认供应商。')}</p>
        {error && <ErrorText>{error}</ErrorText>}
      </form>
    </SurfaceShell>
    <ConfirmDialog open={discard} onOpenChange={setDiscard} title={t('Discard this setup?', '放弃本次配置？')} description={t('The entered information and token will be discarded. Nothing has been saved.', '已填写的信息和令牌将被丢弃，目前尚未保存任何配置。')} confirmLabel={t('Discard', '放弃')} onConfirm={() => { setToken(''); onClose(); }} />
  </>;
}
