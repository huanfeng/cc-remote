import { useEffect, useState, type FormEvent } from 'react';
import type { Api } from './ModelPicker';
import { confirmDialog } from './ConfirmDialog';
import type { SupplierProfile } from './profiles';
import { t } from './i18n';

/** Settings panel: OAuth tokens kept on this host. A session that picks one runs with the native configuration and only that token. */
export function OAuthProfiles({ api }: { api: Api }) {
  const [entries, setEntries] = useState<SupplierProfile[]>();
  const [supported, setSupported] = useState(true);
  const [name, setName] = useState('');
  const [token, setToken] = useState('');
  const [replacing, setReplacing] = useState('');
  const [replacement, setReplacement] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const raw = (id: string) => encodeURIComponent(id.replace(/^oauth:/, ''));
  async function load() {
    try {
      const list = await api<{ profiles: SupplierProfile[] }>('/api/profiles/oauth');
      setEntries(list.profiles);
    } catch (err) {
      if ((err as { status?: number }).status === 404) setSupported(false); // host predates OAuth tokens
      else setError((err as Error).message);
    }
  }
  useEffect(() => { void load(); }, [api]);
  async function run(action: () => Promise<unknown>) {
    if (busy) return false;
    setBusy(true); setError('');
    try { await action(); await load(); return true; } catch (err) { setError((err as Error).message); return false; } finally { setBusy(false); }
  }
  const add = async (event: FormEvent) => {
    event.preventDefault();
    if (await run(() => api('/api/profiles/oauth', { method: 'POST', body: JSON.stringify({ name, token }) }))) { setName(''); setToken(''); }
  };
  const replace = async (id: string) => {
    if (await run(() => api(`/api/profiles/oauth/${raw(id)}`, { method: 'PATCH', body: JSON.stringify({ token: replacement }) }))) { setReplacing(''); setReplacement(''); }
  };
  const remove = async (entry: SupplierProfile) => {
    if (!await confirmDialog(t(`Delete "${entry.name}"? Sessions using it cannot start until you pick another configuration.`, `删除「${entry.name}」？使用它的会话需要重新选择配置才能启动。`))) return;
    await run(() => api(`/api/profiles/oauth/${raw(entry.id)}`, { method: 'DELETE' }));
  };
  if (!supported) return <p role="status">{t('Upgrade this host to keep OAuth tokens.', '请升级此宿主以保存 OAuth 令牌。')}</p>;
  return <div>
    <section className="settings-card">
      <h3>{t('OAuth tokens', 'OAuth 令牌')}</h3>
      <p>{t('A session that picks one of these runs with this host\'s own Claude Code configuration (models, plugins, hooks), and only the login token differs. Create a token with ', '选择其中一项的会话沿用宿主自己的 Claude Code 配置（模型、插件、hooks），只替换登录令牌。令牌用此命令生成：')}<code>claude setup-token</code>{t('. Tokens stay on this host and are never shown again.', '。令牌只保存在此宿主，之后不会再显示。')}</p>
      {entries?.length === 0 && <p>{t('No tokens yet.', '还没有令牌。')}</p>}
      {entries?.map(entry => <div key={entry.id} className="field field-tall">
        <span>{entry.name}</span>
        {replacing === entry.id
          ? <span className="inline-actions"><input type="password" autoComplete="off" value={replacement} placeholder="sk-ant-oat…" aria-label={t('New token', '新令牌')} onChange={event => setReplacement(event.target.value)} /><button type="button" className="primary" disabled={busy || !replacement} onClick={() => void replace(entry.id)}>{t('Save', '保存')}</button><button type="button" className="secondary" disabled={busy} onClick={() => { setReplacing(''); setReplacement(''); }}>{t('Cancel', '取消')}</button></span>
          : <span className="inline-actions"><button type="button" className="secondary" disabled={busy} onClick={() => { setReplacing(entry.id); setReplacement(''); }}>{t('Replace token', '更换令牌')}</button><button type="button" className="danger-button" disabled={busy} onClick={() => void remove(entry)}>{t('Delete', '删除')}</button></span>}
      </div>)}
    </section>
    <form className="settings-card" onSubmit={event => void add(event)}>
      <h3>{t('Add a token', '添加令牌')}</h3>
      <label>{t('Name', '名称')}<input value={name} maxLength={60} autoComplete="off" onChange={event => setName(event.target.value)} /></label>
      <label>{t('Token', '令牌')}<input type="password" autoComplete="off" value={token} placeholder="sk-ant-oat…" onChange={event => setToken(event.target.value)} /></label>
      <button className="primary" disabled={busy || !name.trim() || !token.trim()}>{busy ? t('Saving…', '保存中…') : t('Add', '添加')}</button>
    </form>
    {error && <div className="error" role="alert">{error}</div>}
  </div>;
}
