// Session details: identifiers, where and when, what is running, the supplier,
// configuration (reported vs configured, with source), per-session overrides,
// statistics, and deletion.
import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { RotateCcw, Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { cn } from '@/components/lib/utils';
import { BackendError, useBackend } from '@/backend/backend';
import type { Preferences, Session, SessionStats } from '@/backend/types';
import { select, useApp } from '@/store/app';
import { t, lang, locale } from '@/lib/i18n';
import { dangerousPermission, providerName, providerOf } from '@/domain/providers';
import { conversationTime } from '@/domain/conversationTime';
import { modeLabel, statusLabel, statusTone, tonePill } from '@/features/session/status';
import { permissionLabel } from '@/features/pickers';
import { GroupCard, InfoRow, Section, SurfaceShell } from '@/features/dialogs/SurfaceShell';
import {
  ConfirmDialog, CopyButton, ErrorText, SimpleSelect, errorMessage, profileLabel, profilesFor, supplierOptions, useProfileCatalog,
} from '@/features/dialogs/common';
import { EffortField, ModelField, PermissionField } from '@/features/dialogs/pickers';
import { compactNumber, costText, durationText, agoText } from './usageMath';

type Overrides = { model: string; effort: string; permissionMode: string };
const overridesOf = (s: Session): Overrides => ({ model: s.model ?? '', effort: s.effort ?? '', permissionMode: s.permissionMode ?? '' });
const same = (a: Overrides, b: Overrides) => a.model === b.model && a.effort === b.effort && a.permissionMode === b.permissionMode;

export function SessionDetails({ open, onOpenChange, mobile, id }: { open: boolean; onOpenChange: (o: boolean) => void; mobile: boolean; id: string }) {
  const session = useApp(s => s.sessions.find(x => x.id === id));
  const [dirty, setDirty] = useState(false);
  const [guard, setGuard] = useState(false);
  const request = (o: boolean) => {
    if (!o && dirty) { setGuard(true); return; }
    onOpenChange(o);
  };
  return (
    <SurfaceShell
      open={open}
      onOpenChange={request}
      mobile={mobile}
      title={session?.title || t('Session details', '会话详情')}
      description={t('Session details', '会话详情')}
      className="sm:max-w-[680px]"
    >
      {session ? <Details session={session} onDirty={setDirty} onDeleted={() => onOpenChange(false)} /> : (
        <p className="py-10 text-center text-ui-sm text-foreground-subtle">{t('This session no longer exists.', '此会话已不存在。')}</p>
      )}
      <ConfirmDialog
        open={guard}
        onOpenChange={setGuard}
        title={t('Discard unsaved changes?', '放弃未保存的修改？')}
        description={t('The session overrides you edited have not been saved.', '你修改的会话覆盖尚未保存。')}
        confirmLabel={t('Discard', '放弃')}
        destructive
        onConfirm={() => { setGuard(false); setDirty(false); onOpenChange(false); }}
      />
    </SurfaceShell>
  );
}

function Details({ session: s, onDirty, onDeleted }: { session: Session; onDirty: (d: boolean) => void; onDeleted: () => void }) {
  const backend = useBackend();
  const provider = providerOf(s);
  const [stats, setStats] = useState<SessionStats | null>();
  const [prefs, setPrefs] = useState<Preferences>();
  useEffect(() => {
    let live = true;
    backend.stats(s.id).then(v => { if (live) setStats(v); }, () => { if (live) setStats(null); });
    return () => { live = false; };
  }, [backend, s.id, s.updatedAt]);
  useEffect(() => {
    let live = true;
    backend.preferences(provider).then(p => { if (live) setPrefs(p); }, () => {});
    return () => { live = false; };
  }, [backend, provider]);

  const tone = statusTone(s);
  const time = conversationTime(s, Date.now(), lang);
  const when = (iso?: string) => (iso && Number.isFinite(Date.parse(iso)) ? `${new Date(iso).toLocaleString(locale)} · ${agoText(iso)}` : t('Unknown', '未知'));

  return (
    <div className="flex flex-col gap-6">
      <Section title={t('Overview', '概况')}>
        <GroupCard>
          <InfoRow label={t('Status', '状态')}>
            <span className={cn('inline-flex h-5 items-center rounded-full px-2 text-ui-xs font-medium', tonePill[tone])}>{statusLabel(s.status, s.activeTasks)}</span>
            {s.mode === 'terminal' && <span className="ml-2 text-ui-sm text-foreground-subtle">{s.online ? t('Process running', '进程运行中') : t('Process exited', '进程已退出')}{s.attached ? t(` · ${s.attached} terminal attached`, ` · ${s.attached} 个终端接入`) : ''}</span>}
          </InfoRow>
          <InfoRow label={t('Engine', '引擎')}>{providerName(provider)} · {modeLabel(s)}</InfoRow>
          <InfoRow label={t('Project directory', '项目目录')} mono>
            <span className="flex items-start gap-1"><span className="min-w-0 flex-1 break-all">{s.cwd}</span><CopyButton text={s.cwd} className="-my-1" /></span>
          </InfoRow>
          <InfoRow label={t('Created', '创建时间')}>{when(s.createdAt)}</InfoRow>
          <InfoRow label={t('Last conversation', '最后对话')} title={time.title}>
            {when(s.lastConversationAt)}{s.conversationTimeEstimated && <span className="ml-1 text-foreground-subtlest">{t('(estimated)', '（估算）')}</span>}
          </InfoRow>
        </GroupCard>
      </Section>

      <Section title={t('Identifiers', '标识')}>
        <GroupCard>
          <IdRow label={t('Session ID', '会话 ID')} value={s.id} />
          {s.claudeSessionId && <IdRow label={provider === 'codex' ? t('Codex thread ID', 'Codex 线程 ID') : t('Claude session ID', 'Claude 会话 ID')} value={s.claudeSessionId} />}
        </GroupCard>
      </Section>

      <Section title={t('Current execution', '当前执行')}>
        <GroupCard>
          <InfoRow label={t('Activity', '当前活动')}>{s.live?.activity || <Muted>{t('Not reported', '未报告')}</Muted>}</InfoRow>
          <InfoRow label={t('Queued messages', '排队消息')}>{s.queued?.length ?? 0}</InfoRow>
          <InfoRow label={t('Background tasks', '后台任务')}>{s.activeTasks ?? 0}</InfoRow>
          {s.live && s.live.contextSize > 0 && (
            <InfoRow label={t('Context', '上下文')}>
              {compactNumber(s.live.contextTokens)} / {compactNumber(s.live.contextSize)} · {Math.round(s.live.contextPercent)}%
            </InfoRow>
          )}
        </GroupCard>
      </Section>

      <SupplierSection session={s} />
      <ConfigSection session={s} prefs={prefs} onDirty={onDirty} />
      <StatsSection stats={stats} session={s} />
      <ManageSection session={s} onDeleted={onDeleted} />
    </div>
  );
}

const Muted = ({ children }: { children: ReactNode }) => <span className="text-foreground-subtlest">{children}</span>;

function IdRow({ label, value }: { label: string; value: string }) {
  return (
    <InfoRow label={label}>
      <span className="flex items-center gap-1"><code className="min-w-0 flex-1 truncate font-mono text-ui-sm" title={value}>{value}</code><CopyButton text={value} className="-my-1" /></span>
    </InfoRow>
  );
}

function SupplierSection({ session: s }: { session: Session }) {
  const backend = useBackend();
  const provider = providerOf(s);
  const { catalog, error: catalogError } = useProfileCatalog();
  const [chosen, setChosen] = useState(s.profileId ?? '');
  const [busy, setBusy] = useState(false);
  const [force, setForce] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  useEffect(() => { setChosen(s.profileId ?? ''); }, [s.profileId]);

  if (catalogError) return null;
  if (catalog && !catalog.capabilities?.switchSupplier) return null;
  const current = profilesFor(catalog, provider).find(p => p.id === s.profileId);
  const options = supplierOptions(catalog, provider);
  if (s.profileId && !options.some(o => o.value === s.profileId)) options.push({ value: s.profileId, label: t(`Unavailable · ${s.profileId}`, `不可用 · ${s.profileId}`) });

  const run = async (withForce = false) => {
    setBusy(true); setError(''); setNotice('');
    try {
      await backend.switchProfile(s.id, chosen, withForce);
      setForce(false);
      setNotice(t('Supplier switched. Interrupted requests are not resent; continue when ready.', '供应商已切换。被中断的请求不会自动重发，准备好后可继续对话。'));
    } catch (e) {
      if (e instanceof BackendError && e.status === 409 && !withForce) setForce(true);
      else { setForce(false); setError(errorMessage(e)); }
    } finally {
      setBusy(false);
    }
  };

  return (
    <Section title={t('Supplier', '供应商')} description={t('Switching restarts the session process with its saved context; file changes stay.', '切换会重启会话进程并恢复已有上下文，已经完成的文件修改会保留。')}>
      <GroupCard>
        <InfoRow label={t('Current', '当前')}>{s.profileName || (current && profileLabel(current)) || s.profileId || t('Native configuration', '原生配置')}</InfoRow>
        <div className="flex flex-wrap items-center gap-2 border-t border-border/60 px-4 py-3">
          <SimpleSelect value={chosen} onChange={v => { setChosen(v); setError(''); setNotice(''); }} options={options} disabled={!catalog || busy} className="min-w-0 flex-1 sm:w-auto" ariaLabel={t('Supplier', '供应商')} />
          <Button variant="outline" size="lg" disabled={busy || !catalog || chosen === (s.profileId ?? '')} onClick={() => void run()}>
            {busy && <Spinner className="size-3.5" />}
            {t('Switch', '切换')}
          </Button>
        </div>
      </GroupCard>
      <ErrorText>{error}</ErrorText>
      {notice && <p role="status" className="text-ui-sm text-success">{notice}</p>}
      <ConfirmDialog
        open={force}
        onOpenChange={setForce}
        title={t('Interrupt and switch supplier?', '中断并切换供应商？')}
        description={t('This session is busy. Switching now stops the current work and background tasks, keeps the saved context and does not replay the interrupted request. Finished actions cannot be undone.', '此会话正在运行。立即切换将停止当前执行及后台任务，保留已保存的上下文，不会自动重发被中断的请求。已经完成的操作无法撤销。')}
        confirmLabel={t('Interrupt and switch', '中断并切换')}
        destructive
        busy={busy}
        onConfirm={() => void run(true)}
      />
    </Section>
  );
}

function ConfigSection({ session: s, prefs, onDirty }: { session: Session; prefs?: Preferences; onDirty: (d: boolean) => void }) {
  const backend = useBackend();
  const provider = providerOf(s);
  const saved = useMemo(() => overridesOf(s), [s.model, s.effort, s.permissionMode]);
  const [draft, setDraft] = useState(saved);
  const [base, setBase] = useState(saved);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const dirty = !same(draft, saved);
  useEffect(() => { onDirty(dirty); }, [dirty, onDirty]);
  // A change from elsewhere (header chips, another tab) carries over to an untouched draft.
  if (!same(base, saved)) {
    setBase(saved);
    if (same(draft, base)) setDraft(saved);
  }

  const source = (key: keyof Overrides) => {
    if (s[key]) return { value: s[key]!, from: t('Session override', '本会话覆盖') };
    const d = prefs?.[key];
    if (d) return { value: d, from: t('CC Remote default', 'CC Remote 默认') };
    return { value: '', from: t(`${providerName(provider)} configuration`, `${providerName(provider)} 原生配置`) };
  };
  const reported = { model: s.live?.model || s.live?.modelId, effort: s.live?.effort, permissionMode: s.live?.permissionMode };
  const show = (key: keyof Overrides, v?: string) => (!v ? undefined : key === 'permissionMode' ? permissionLabel(v, provider) : v);

  const save = async () => {
    setBusy(true); setError(''); setNotice('');
    try {
      await backend.patchSession(s.id, { ...draft, ...(dangerousPermission(draft.permissionMode, provider) ? { confirmDangerous: true } : {}) });
      setNotice(s.mode === 'terminal'
        ? t('Saved. It applies the next time the process can safely restart.', '已保存，进程可安全重启时生效。')
        : t('Saved. Model and effort apply next turn; permissions after a safe restart.', '已保存。模型和推理强度下一轮生效；权限在进程可安全重启时生效。'));
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const rows: { key: keyof Overrides; label: string }[] = [
    { key: 'model', label: t('Model', '模型') },
    { key: 'effort', label: t('Effort', '推理强度') },
    { key: 'permissionMode', label: provider === 'codex' ? t('Sandbox', '沙箱权限') : t('Permission', '权限') },
  ];

  return (
    <Section title={t('Configuration', '配置')} description={t('Reported is what the CLI says it runs with; configured is what the next turn or restart uses.', '「已报告」是 CLI 实际运行的值；「已配置」是下一轮或下次启动使用的值。')}>
      <GroupCard>
        <div className="grid grid-cols-[minmax(0,7rem)_minmax(0,1fr)_minmax(0,1fr)] gap-3 border-b border-border/60 px-4 py-2 text-ui-xs text-foreground-subtlest max-sm:grid-cols-[minmax(0,5rem)_minmax(0,1fr)_minmax(0,1fr)]">
          <span />
          <span>{t('Reported', '已报告')}</span>
          <span>{t('Configured', '已配置')}</span>
        </div>
        {rows.map(({ key, label }) => {
          const src = source(key);
          return (
            <div key={key} className="grid grid-cols-[minmax(0,7rem)_minmax(0,1fr)_minmax(0,1fr)] items-start gap-3 border-t border-border/60 px-4 py-2.5 first:border-t-0 max-sm:grid-cols-[minmax(0,5rem)_minmax(0,1fr)_minmax(0,1fr)]">
              <span className="text-ui-sm text-foreground-subtle">{label}</span>
              <span className="truncate text-ui-sm">{show(key, reported[key]) ?? <Muted>{t('Not reported', '未报告')}</Muted>}</span>
              <span className="min-w-0">
                <span className="block truncate text-ui-sm">{show(key, src.value) ?? <Muted>{t('Inherited (unknown)', '继承（值未知）')}</Muted>}</span>
                <span className="block truncate text-ui-xs text-foreground-subtlest">{src.from}</span>
              </span>
            </div>
          );
        })}
      </GroupCard>

      <div className="mt-2 flex flex-col gap-3 rounded-xl border border-border/70 p-4">
        <div className="text-ui-base font-medium">{t('Session overrides', '本会话覆盖')}</div>
        <OverrideRow label={t('Model', '模型')}>
          <ModelField provider={provider} sessionId={s.id} value={draft.model} onChange={model => setDraft(d => ({ ...d, model }))} inheritLabel={t('Inherit', '继承默认')} />
        </OverrideRow>
        <OverrideRow label={t('Effort', '推理强度')}>
          <EffortField provider={provider} sessionId={s.id} model={draft.model} value={draft.effort} onChange={effort => setDraft(d => ({ ...d, effort }))} inheritLabel={t('Inherit', '继承默认')} />
        </OverrideRow>
        <OverrideRow label={provider === 'codex' ? t('Sandbox', '沙箱权限') : t('Permission mode', '权限模式')}>
          <PermissionField provider={provider} value={draft.permissionMode} effective={prefs?.permissionMode} onChange={permissionMode => setDraft(d => ({ ...d, permissionMode }))} inheritLabel={t('Inherit', '继承默认')} />
        </OverrideRow>
        <ErrorText>{error}</ErrorText>
        {notice && !dirty && <p role="status" className="text-ui-sm text-success">{notice}</p>}
        <div className="flex flex-wrap items-center gap-2 border-t border-border/60 pt-3">
          <Button variant="ghost" size="lg" disabled={busy || (!draft.model && !draft.effort && !draft.permissionMode)} onClick={() => setDraft({ model: '', effort: '', permissionMode: '' })}>
            <RotateCcw className="size-3.5" /> {t('Restore inheritance', '恢复继承')}
          </Button>
          <div className="flex-1" />
          <Button variant="ghost" size="lg" disabled={busy || !dirty} onClick={() => setDraft(saved)}>{t('Cancel', '取消')}</Button>
          <Button size="lg" disabled={busy || !dirty} onClick={() => void save()}>
            {busy && <Spinner className="size-3.5" />}
            {busy ? t('Saving…', '保存中…') : t('Save', '保存')}
          </Button>
        </div>
      </div>
    </Section>
  );
}

function OverrideRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-center gap-3 max-sm:flex-col max-sm:items-stretch max-sm:gap-1.5">
      <span className="flex-1 text-ui-sm text-foreground-subtle">{label}</span>
      {children}
    </div>
  );
}

function StatsSection({ stats, session: s }: { stats?: SessionStats | null; session: Session }) {
  return (
    <Section title={t('Statistics', '统计')} description={t('Events CC Remote recorded for this session; subscription quotas are separate.', '此会话在 CC Remote 记录的事件；订阅额度另计。')}>
      {stats === undefined ? (
        <div className="flex h-20 items-center justify-center"><Spinner className="size-4 text-foreground-subtle" /></div>
      ) : stats === null ? (
        <p className="text-ui-sm text-foreground-subtle">{t('Statistics unavailable.', '统计暂不可用。')}</p>
      ) : (
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
          <Tile label={t('Turns', '轮次')} value={compactNumber(stats.turns)} />
          <Tile label={t('Cost (est.)', '费用（估算）')} value={stats.costKnown ? costText(stats.costUSD) : '—'} />
          <Tile label={t('Run time', '执行耗时')} value={durationText(stats.durationMs / 1000)} />
          <Tile label={t('Tool calls', '工具调用')} value={compactNumber(stats.toolCalls)} />
          <Tile label={t('Input tokens', '输入 tokens')} value={compactNumber(stats.inputTokens)} />
          <Tile label={t('Output tokens', '输出 tokens')} value={compactNumber(stats.outputTokens)} />
          <Tile label={t('Cache read', '缓存读取')} value={compactNumber(stats.cacheReadTokens)} />
          <Tile label={t('Errors', '错误')} value={compactNumber(stats.errors)} tone={stats.errors > 0 ? 'bad' : undefined} />
          <p className="col-span-full text-ui-xs text-foreground-subtlest">
            {t(`Last recorded event ${agoText(stats.lastActivityAt || s.updatedAt)}`, `最后记录事件：${agoText(stats.lastActivityAt || s.updatedAt)}`)}
          </p>
        </div>
      )}
    </Section>
  );
}

function Tile({ label, value, tone }: { label: string; value: ReactNode; tone?: 'bad' }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5 rounded-xl border border-border/70 bg-card px-3 py-2">
      <span className="truncate text-ui-xs text-foreground-subtle">{label}</span>
      <span className={cn('text-ui-lg font-semibold tabular-nums', tone === 'bad' && 'text-destructive')}>{value}</span>
    </div>
  );
}

function ManageSection({ session: s, onDeleted }: { session: Session; onDeleted: () => void }) {
  const backend = useBackend();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  // The host refuses (409) to delete a busy session without stop; then the dialog asks to stop first.
  const [mustStop, setMustStop] = useState(false);
  const active = mustStop || s.status === 'running' || s.status === 'waiting' || (s.activeTasks ?? 0) > 0 || (s.mode === 'terminal' && !!s.online);

  const remove = async () => {
    setBusy(true); setError('');
    try {
      await backend.deleteSession(s.id, active ? { stop: true } : undefined);
      setOpen(false);
      const sel = useApp.getState().selection;
      if (sel?.kind === 'session' && sel.id === s.id) select(null);
      onDeleted();
    } catch (e) {
      if (e instanceof BackendError && e.status === 409 && !active) setMustStop(true);
      else setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Section title={t('Manage', '管理')}>
      <GroupCard className="border-destructive/25">
        <div className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center">
          <div className="min-w-0 flex-1">
            <div className="text-ui-base font-medium">{t('Delete session', '删除会话')}</div>
            <div className="text-ui-sm text-foreground-subtle">{t("Removes it from CC Remote. Project files and the provider's own history stay.", '从 CC Remote 中移除。项目文件和提供方自己的历史记录会保留。')}</div>
          </div>
          <Button variant="destructive" size="lg" onClick={() => setOpen(true)}><Trash2 className="size-3.5" /> {t('Delete…', '删除…')}</Button>
        </div>
      </GroupCard>
      <ConfirmDialog
        open={open}
        onOpenChange={o => { setOpen(o); if (!o) { setError(''); setMustStop(false); } }}
        title={t(`Delete "${s.title}"?`, `删除「${s.title}」？`)}
        description={active ? t('This session is still running. It will be stopped first.', '此会话仍在运行，将先停止它。') : undefined}
        confirmLabel={busy ? t('Deleting…', '删除中…') : active ? t('Stop and delete', '先停止并删除') : t('Delete', '删除')}
        destructive
        busy={busy}
        onConfirm={() => void remove()}
      >
        <div className="flex flex-col gap-2 text-ui-sm">
          <div className="font-medium text-foreground">{t('This deletes:', '将删除：')}</div>
          <ul className="flex list-disc flex-col gap-1 pl-5 text-foreground-subtle">
            <li>{t('The conversation recorded by CC Remote', 'CC Remote 记录的对话')}</li>
            <li>{t('Uploaded attachments and queued messages', '上传的附件和排队消息')}</li>
            <li>{t('Pending approvals and questions', '待处理的审批和提问')}</li>
            {active && <li>{t('The running process and its background tasks', '正在运行的进程及其后台任务')}</li>}
          </ul>
          <div className="text-foreground-subtlest">{t('Kept: the project directory and the provider history.', '保留：项目目录和提供方的历史记录。')}</div>
          <ErrorText>{error}</ErrorText>
        </div>
      </ConfirmDialog>
    </Section>
  );
}
