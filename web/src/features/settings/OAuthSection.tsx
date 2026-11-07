// OAuth tokens kept on this host. A session that picks one runs with the
// host's own Claude Code configuration and only that login token.
import { useEffect, useState, type FormEvent } from 'react';
import { KeyRound, Plus } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Spinner } from '@/components/ui/spinner';
import { cn } from '@/components/lib/utils';
import { useBackend } from '@/backend/backend';
import { t } from '@/lib/i18n';
import { GroupCard } from '@/features/dialogs/SurfaceShell';
import { ConfirmDialog, ErrorText, errorMessage, errorStatus } from '@/features/dialogs/common';
import { GroupTitle, SectionHeader, useReportDirty } from './parts';

type Entry = { id: string; name: string };

export function OAuthSection({ mobile }: { mobile: boolean }) {
  const backend = useBackend();
  const [entries, setEntries] = useState<Entry[]>();
  const [unsupported, setUnsupported] = useState(false);
  const [name, setName] = useState('');
  const [token, setToken] = useState('');
  const [replacing, setReplacing] = useState('');
  const [replacement, setReplacement] = useState('');
  const [removing, setRemoving] = useState<Entry>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  useReportDirty('oauth', !!(name.trim() || token.trim() || replacement.trim()));

  const load = async () => {
    try { setEntries(await backend.oauthProfiles()); } catch (e) {
      if (errorStatus(e) === 404) setUnsupported(true); else setError(errorMessage(e));
    }
  };
  useEffect(() => { void load(); }, [backend]);

  const run = async (action: () => Promise<unknown>) => {
    setBusy(true); setError('');
    try { await action(); await load(); return true; } catch (e) { setError(errorMessage(e)); return false; } finally { setBusy(false); }
  };
  const add = async (e: FormEvent) => {
    e.preventDefault();
    if (!name.trim() || !token.trim()) return;
    if (await run(() => backend.addOAuthProfile(name.trim(), token.trim()))) { setName(''); setToken(''); }
  };
  const replace = async (id: string) => {
    if (await run(() => backend.replaceOAuthProfile(id, replacement.trim()))) { setReplacing(''); setReplacement(''); }
  };
  const inputCls = cn(mobile && 'h-10 text-mobile-input-safe');

  return (
    <div>
      {!mobile && (
        <SectionHeader
          title={t('OAuth tokens', 'OAuth 令牌')}
          description={<>{t("A session that picks one runs with this host's own Claude Code configuration (models, plugins, hooks); only the login token differs. Create one with ", '选择其中一项的会话沿用宿主自己的 Claude Code 配置（模型、插件、hooks），只替换登录令牌。令牌用此命令生成：')}<code className="rounded bg-surface px-1 font-mono text-ui-xs">claude setup-token</code>{t('. Tokens stay on this host and are never shown again.', '。令牌只保存在此宿主，之后不会再显示。')}</>}
        />
      )}
      {unsupported ? (
        <p className="text-ui-sm text-foreground-subtle">{t('Upgrade this host to keep OAuth tokens.', '请升级此宿主以保存 OAuth 令牌。')}</p>
      ) : (
        <>
          <GroupTitle>{t('Saved tokens', '已保存的令牌')}</GroupTitle>
          <GroupCard>
            {!entries ? (
              <div className="flex h-16 items-center justify-center"><Spinner className="size-4 text-foreground-subtle" /></div>
            ) : entries.length === 0 ? (
              <p className="px-4 py-5 text-center text-ui-sm text-foreground-subtle">{t('No tokens yet.', '还没有令牌。')}</p>
            ) : entries.map(entry => (
              <div key={entry.id} className="border-t border-border/60 px-4 py-3 first:border-t-0">
                <div className="flex items-center gap-3">
                  <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-surface text-foreground-subtle"><KeyRound className="size-4" /></span>
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-ui-base font-medium">{entry.name}</div>
                    <div className="truncate font-mono text-ui-xs text-foreground-subtlest">{entry.id}</div>
                  </div>
                  {replacing !== entry.id && (
                    <div className="flex shrink-0 gap-1">
                      <Button variant="outline" size="lg" disabled={busy} onClick={() => { setReplacing(entry.id); setReplacement(''); }}>{t('Replace', '更换')}</Button>
                      <Button variant="ghost" size="lg" disabled={busy} className="text-destructive hover:text-destructive" onClick={() => setRemoving(entry)}>{t('Delete', '删除')}</Button>
                    </div>
                  )}
                </div>
                {replacing === entry.id && (
                  <form className="mt-3 flex flex-wrap items-center gap-2" onSubmit={e => { e.preventDefault(); if (replacement.trim()) void replace(entry.id); }}>
                    <Input type="password" autoComplete="off" autoFocus size="lg" value={replacement} placeholder="sk-ant-oat…" aria-label={t('New token', '新令牌')}
                      onChange={e => setReplacement(e.target.value)} className={cn('min-w-0 flex-1 font-mono', inputCls)} />
                    <Button type="button" variant="ghost" size="lg" disabled={busy} onClick={() => { setReplacing(''); setReplacement(''); }}>{t('Cancel', '取消')}</Button>
                    <Button type="submit" size="lg" disabled={busy || !replacement.trim()}>{busy && <Spinner className="size-3.5" />}{t('Save', '保存')}</Button>
                  </form>
                )}
              </div>
            ))}
          </GroupCard>

          <GroupTitle>{t('Add a token', '添加令牌')}</GroupTitle>
          <form onSubmit={e => void add(e)}>
            <GroupCard className="flex flex-col gap-3 p-4">
              <label className="flex flex-col gap-1.5">
                <span className="text-ui-sm font-medium">{t('Name', '名称')}</span>
                <Input size="lg" value={name} maxLength={60} autoComplete="off" placeholder={t('Such as work account', '例如 工作账号')} onChange={e => setName(e.target.value)} className={inputCls} />
              </label>
              <label className="flex flex-col gap-1.5">
                <span className="text-ui-sm font-medium">{t('Token', '令牌')}</span>
                <Input size="lg" type="password" autoComplete="off" value={token} placeholder="sk-ant-oat…" onChange={e => setToken(e.target.value)} className={cn('font-mono', inputCls)} />
              </label>
              <div className="flex justify-end">
                <Button type="submit" size="lg" disabled={busy || !name.trim() || !token.trim()}>
                  {busy ? <Spinner className="size-3.5" /> : <Plus className="size-3.5" />}
                  {t('Add token', '添加令牌')}
                </Button>
              </div>
            </GroupCard>
          </form>
          {error && <div className="mt-3"><ErrorText>{error}</ErrorText></div>}
        </>
      )}
      <ConfirmDialog
        open={!!removing}
        onOpenChange={o => { if (!o) setRemoving(undefined); }}
        title={t(`Delete "${removing?.name ?? ''}"?`, `删除「${removing?.name ?? ''}」？`)}
        description={t('Sessions using it cannot start until you pick another configuration.', '使用它的会话需要重新选择配置才能启动。')}
        confirmLabel={t('Delete', '删除')}
        destructive
        busy={busy}
        onConfirm={() => { const r = removing; setRemoving(undefined); if (r) void run(() => backend.removeOAuthProfile(r.id)); }}
      />
    </div>
  );
}
