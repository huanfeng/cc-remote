// New session: engine, supplier, Claude's terminal/managed choice, the project
// directory (with recent projects), a name and optional overrides. The resume
// variant continues a session from the terminal history, as a terminal session
// (Claude) or by importing it as a managed one.
import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { AlertTriangle, ChevronRight, FolderOpen, MessageSquare, SquareTerminal } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Checkbox } from '@/components/ui/checkbox';
import { Spinner } from '@/components/ui/spinner';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import { cn } from '@/components/lib/utils';
import { BackendError, useBackend } from '@/backend/backend';
import type { NativeSession, NewSessionRequest, Provider } from '@/backend/types';
import { select, useApp } from '@/store/app';
import { usePrefs } from '@/lib/prefs';
import { t } from '@/lib/i18n';
import { hostProviders, newSessionBody, providerName } from '@/domain/providers';
import { matchProjects, projectKey, projectName, recentProjects } from '@/domain/recentProjects';
import { shortPath } from '@/features/session/status';
import { SurfaceShell } from './SurfaceShell';
import { ErrorText, SimpleSelect, errorMessage, supplierOptions, useProfileCatalog } from './common';
import { EffortField, ModelField, PermissionField } from './pickers';

type Resume = { id: string; provider: Provider; cwd: string };

/** Opens a session just created (or imported) here, with the sidebar on the session list it is in. */
function openCreated(id: string) {
  useApp.getState().set({ sidebarTab: 'sessions' });
  select({ kind: 'session', id });
}

export function NewSessionDialog({ open, onOpenChange, mobile, resume }: { open: boolean; onOpenChange: (o: boolean) => void; mobile: boolean; resume?: Resume }) {
  return resume
    ? <ResumeDialog open={open} onOpenChange={onOpenChange} mobile={mobile} resume={resume} />
    : <CreateDialog open={open} onOpenChange={onOpenChange} mobile={mobile} />;
}

const absolute = (p: string) => /^([a-z]:[\\/]|\\\\|\/|~[\\/]?)/i.test(p.trim());

function useRecentProjects() {
  const backend = useBackend();
  const sessions = useApp(s => s.sessions);
  // Only the providers this host declares; a Claude-only host has no Codex history.
  const providerKey = useApp(s => hostProviders(s.info).join(','));
  const [native, setNative] = useState<NativeSession[]>([]);
  useEffect(() => {
    let live = true;
    const providers = providerKey.split(',') as Provider[];
    Promise.allSettled(providers.map(p => backend.nativeSessions(p))).then(rs => {
      if (live) setNative(rs.flatMap(r => (r.status === 'fulfilled' ? r.value : [])));
    });
    return () => { live = false; };
  }, [backend, providerKey]);
  return useMemo(() => recentProjects([
    ...sessions.map(s => ({ cwd: s.cwd, updatedAt: s.lastConversationAt ?? s.updatedAt })),
    ...native.map(n => ({ cwd: n.cwd, updatedAt: n.lastConversationAt ?? n.updatedAt })),
  ], 8), [sessions, native]);
}

function CreateDialog({ open, onOpenChange, mobile }: { open: boolean; onOpenChange: (o: boolean) => void; mobile: boolean }) {
  const backend = useBackend();
  const info = useApp(s => s.info);
  const setPrefs = usePrefs(s => s.set);
  const providers = hostProviders(info);
  const selected = useApp(s => (s.selection?.kind === 'session' ? s.sessions.find(x => x.id === s.selection!.id) : undefined));
  const [provider, setProvider] = useState<Provider>(() => (selected?.provider === 'codex' && providers.includes('codex') ? 'codex' : 'claude'));
  const [mode, setMode] = useState<'terminal' | 'managed'>(() => usePrefs.getState().newSessionMode);
  const [profileId, setProfileId] = useState('');
  const [cwd, setCwd] = useState('');
  const [cwdTouched, setCwdTouched] = useState(false);
  const [title, setTitle] = useState('');
  const [trust, setTrust] = useState(false);
  // The host answered 428 trust-required: the checkbox is highlighted until ticked.
  const [trustNeeded, setTrustNeeded] = useState(false);
  const trustRef = useRef<HTMLButtonElement>(null);
  const [advanced, setAdvanced] = useState(false);
  const [overrides, setOverrides] = useState({ permissionMode: '', model: '', effort: '' });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const { catalog } = useProfileCatalog();
  const recent = useRecentProjects();

  // A folder the host already trusts needs no tick: the host decides (428 trust-required otherwise).
  // Pre-fill the latest project until the user types something else.
  useEffect(() => {
    if (!cwdTouched && !cwd && recent[0]) setCwd(recent[0].path);
  }, [recent, cwd, cwdTouched]);

  const terminal = provider === 'claude' && mode === 'terminal';
  const available = provider === 'codex' ? info?.codexAvailable !== false : info?.claudeAvailable !== false;
  const valid = absolute(cwd) && available;
  const chips = matchProjects(recent, cwd);
  const overridden = !!(overrides.permissionMode || overrides.model || overrides.effort);

  const chooseProvider = (p: Provider) => {
    if (p === provider) return;
    setProvider(p); setProfileId(''); setOverrides({ permissionMode: '', model: '', effort: '' }); setTrust(false); setTrustNeeded(false); setError('');
  };

  const submit = async (e?: FormEvent) => {
    e?.preventDefault();
    if (!valid || busy) return;
    setBusy(true); setError('');
    try {
      const body = newSessionBody(provider, title, cwd, overrides, mode, trust, profileId) as NewSessionRequest;
      const session = await backend.createSession(body);
      if (provider === 'claude') setPrefs({ newSessionMode: mode });
      onOpenChange(false);
      openCreated(session.id);
    } catch (err) {
      if (err instanceof BackendError && (err.status === 428 || err.code === 'trust-required')) {
        setTrustNeeded(true);
        setError(t('Claude Code has not trusted this folder yet. Tick "Trust this folder" and create again.', 'Claude Code 尚未信任此文件夹。请勾选「信任此文件夹」后重新创建。'));
        trustRef.current?.focus();
      } else {
        setError(errorMessage(err));
      }
    } finally {
      setBusy(false);
    }
  };

  const footer = (
    <>
      {!mobile && <Button variant="ghost" size="lg" onClick={() => onOpenChange(false)}>{t('Cancel', '取消')}</Button>}
      <Button size="lg" type="submit" form="new-session-form" disabled={!valid || busy} className={cn(mobile && 'flex-1')}>
        {busy && <Spinner className="size-3.5" />}
        {busy ? t('Creating…', '创建中…') : t('Create session', '创建会话')}
      </Button>
    </>
  );

  return (
    <SurfaceShell
      open={open}
      onOpenChange={onOpenChange}
      mobile={mobile}
      title={t('New session', '新建会话')}
      description={t('Pick an engine and a project directory on the host.', '选择引擎和宿主上的项目目录。')}
      className="sm:max-w-[560px]"
      footer={footer}
    >
      <form id="new-session-form" onSubmit={e => void submit(e)} className="flex flex-col gap-5">
        {providers.length > 1 && (
          <div role="radiogroup" aria-label={t('Engine', '引擎')} className="grid grid-cols-2 gap-2">
            {providers.map(p => (
              <ChoiceCard
                key={p}
                active={provider === p}
                onClick={() => chooseProvider(p)}
                title={providerName(p)}
                description={p === 'codex' ? t('Managed sessions · sandbox permissions', '托管会话 · 沙箱权限') : t('Terminal or managed sessions', '终端或托管会话')}
                badge={(p === 'codex' ? info?.codexAvailable === false : info?.claudeAvailable === false) ? t('Unavailable', '不可用') : undefined}
              />
            ))}
          </div>
        )}

        {provider === 'claude' && (
          <Field label={t('Session type', '会话类型')}>
            <div role="radiogroup" aria-label={t('Session type', '会话类型')} className="grid grid-cols-2 gap-1 rounded-xl bg-surface p-1">
              <Segment active={mode === 'terminal'} onClick={() => setMode('terminal')} icon={<SquareTerminal className="size-4" />}
                title={t('Terminal session', '终端会话')} description={t('A local terminal can attach any time', '本地终端可随时接入')} />
              <Segment active={mode === 'managed'} onClick={() => setMode('managed')} icon={<MessageSquare className="size-4" />}
                title={t('Managed session', '托管会话')} description={t('Runs in the background, web only', '后台运行，仅网页使用')} />
            </div>
          </Field>
        )}

        <Field label={t('Project directory', '项目目录')} hint={t('An absolute path that already exists on the host.', '宿主上已存在的绝对路径。')}>
          <div className="relative">
            <FolderOpen className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-foreground-subtlest" />
            <Input
              size="lg"
              value={cwd}
              autoFocus={!mobile}
              spellCheck={false}
              autoCapitalize="off"
              autoCorrect="off"
              onChange={e => { setCwd(e.target.value); setCwdTouched(true); }}
              placeholder={t('D:\\Projects\\app or /home/me/app', 'D:\\Projects\\app 或 /home/me/app')}
              aria-invalid={cwd.trim() !== '' && !absolute(cwd) ? true : undefined}
              className={cn('pl-8 font-mono text-ui-sm', mobile && 'h-10 text-mobile-input-safe')}
            />
          </div>
          {chips.length > 0 && (
            <div className="flex flex-wrap gap-1.5" role="group" aria-label={t('Recent projects', '最近项目')}>
              {chips.map(p => {
                const active = projectKey(p.path) === projectKey(cwd);
                return (
                  <button
                    key={p.path}
                    type="button"
                    title={p.path}
                    onClick={() => { setCwd(p.path); setCwdTouched(true); }}
                    className={cn(
                      'inline-flex h-7 max-w-[14rem] items-center gap-1.5 rounded-lg border px-2.5 text-ui-sm transition-colors max-md:h-9',
                      active ? 'border-transparent bg-selected text-foreground' : 'border-border/70 text-foreground-subtle hover:bg-surface-hover hover:text-foreground',
                    )}
                  >
                    <span className="truncate font-medium">{p.name}</span>
                    <span className="truncate text-ui-xs text-foreground-subtlest max-sm:hidden">{shortPath(p.path)}</span>
                  </button>
                );
              })}
            </div>
          )}
          {cwd.trim() !== '' && !absolute(cwd) && <p className="text-ui-sm text-destructive">{t('Use an absolute path.', '请使用绝对路径。')}</p>}
        </Field>

        <Field label={t('Name', '名称')} optional>
          <Input
            size="lg"
            value={title}
            onChange={e => setTitle(e.target.value)}
            placeholder={cwd.trim() ? projectName(cwd.trim()) : t('Defaults to the folder name', '默认使用文件夹名称')}
            className={cn(mobile && 'h-10 text-mobile-input-safe')}
          />
        </Field>

        <Field label={t('Supplier', '供应商')} hint={t('Kept for this session when the default changes.', '默认项改变后，此会话仍保留所选配置。')}>
          <SimpleSelect value={profileId} onChange={setProfileId} options={supplierOptions(catalog, provider)} disabled={!catalog} className="w-full" ariaLabel={t('Supplier', '供应商')} />
          {catalog?.found === false && <p className="text-ui-sm text-foreground-subtlest">{t('CC-Switch database not found on this host.', '此宿主未找到 CC-Switch 数据库。')}</p>}
        </Field>

        {terminal && (
          <label className={cn(
            'flex cursor-pointer items-start gap-2.5 rounded-xl border px-3 py-2.5 transition-colors',
            trustNeeded && !trust ? 'border-warning bg-warning-surface ring-2 ring-warning/30' : 'border-border/70 bg-surface',
          )}>
            <Checkbox ref={trustRef} checked={trust} aria-invalid={trustNeeded && !trust ? true : undefined}
              onCheckedChange={v => { setTrust(v === true); if (v === true) { setTrustNeeded(false); setError(''); } }} className="mt-0.5" />
            <span className="text-ui-sm leading-5">
              <span className="font-medium text-foreground">{t('Trust this folder', '信任此文件夹')}</span>
              <span className="block text-foreground-subtle">{t('Claude Code may read, edit and run files in it — the same as confirming "trust this folder" in the terminal. Not needed for a folder the host already trusts.', 'Claude Code 可以读取、修改和执行其中的文件，等同于在终端里确认「信任此文件夹」。宿主已信任的文件夹可不勾选。')}</span>
            </span>
          </label>
        )}

        <Collapsible open={advanced} onOpenChange={setAdvanced}>
          <CollapsibleTrigger asChild>
            <button type="button" className="flex items-center gap-1 text-ui-sm font-medium text-foreground-subtle hover:text-foreground max-md:min-h-9">
              <ChevronRight className={cn('size-3.5 transition-transform', advanced && 'rotate-90')} />
              {t('Advanced', '高级')}
              {overridden && !advanced && <span className="ml-1 rounded-md bg-surface px-1.5 text-ui-xs text-foreground-subtle">{t('customised', '已自定义')}</span>}
            </button>
          </CollapsibleTrigger>
          <CollapsibleContent>
            <div className="mt-3 flex flex-col gap-3 rounded-xl border border-border/70 p-3">
              <InlineField label={provider === 'codex' ? t('Sandbox', '沙箱权限') : t('Permission mode', '权限模式')}>
                <PermissionField provider={provider} value={overrides.permissionMode} onChange={permissionMode => setOverrides(o => ({ ...o, permissionMode }))} />
              </InlineField>
              <InlineField label={t('Model', '模型')}>
                <ModelField key={provider} provider={provider} value={overrides.model} onChange={model => setOverrides(o => ({ ...o, model }))} />
              </InlineField>
              <InlineField label={t('Effort', '推理强度')}>
                <EffortField provider={provider} model={overrides.model} value={overrides.effort} onChange={effort => setOverrides(o => ({ ...o, effort }))} />
              </InlineField>
              <p className="text-ui-xs text-foreground-subtlest">{t('Empty choices follow the CC Remote defaults in Settings.', '未选择的项跟随设置中的 CC Remote 默认值。')}</p>
            </div>
          </CollapsibleContent>
        </Collapsible>

        {!available && <ErrorText>{t(`${providerName(provider)} is not available on this host.`, `此宿主上 ${providerName(provider)} 不可用。`)}</ErrorText>}
        <ErrorText>{error}</ErrorText>
      </form>
    </SurfaceShell>
  );
}

function ResumeDialog({ open, onOpenChange, mobile, resume }: { open: boolean; onOpenChange: (o: boolean) => void; mobile: boolean; resume: Resume }) {
  const backend = useBackend();
  const [stopped, setStopped] = useState(false);
  const [profileId, setProfileId] = useState('');
  const [busy, setBusy] = useState<'' | 'terminal' | 'import'>('');
  const [error, setError] = useState('');
  const { catalog } = useProfileCatalog();
  const claude = resume.provider !== 'codex';
  const canPick = !!catalog?.capabilities?.resumeSelection;

  const run = async (kind: 'terminal' | 'import') => {
    if (!stopped || busy) return;
    setBusy(kind); setError('');
    try {
      const session = kind === 'terminal'
        ? await backend.createSession({ mode: 'terminal', trustFolder: true, resume: resume.id, cwd: resume.cwd, title: '', ...(profileId ? { profileId } : {}) })
        : await backend.importNative(resume.id, resume.provider, profileId || undefined);
      onOpenChange(false);
      openCreated(session.id);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy('');
    }
  };

  const footer = (
    <>
      {!mobile && <Button variant="ghost" size="lg" onClick={() => onOpenChange(false)}>{t('Cancel', '取消')}</Button>}
      <Button variant={claude ? 'outline' : 'default'} size="lg" disabled={!stopped || !!busy} onClick={() => void run('import')} className={cn(mobile && 'flex-1')}>
        {busy === 'import' && <Spinner className="size-3.5" />}
        {t('Import as managed session', '导入为托管会话')}
      </Button>
      {claude && (
        <Button size="lg" disabled={!stopped || !!busy} onClick={() => void run('terminal')} className={cn(mobile && 'flex-1')}>
          {busy === 'terminal' && <Spinner className="size-3.5" />}
          {t('Continue as terminal session', '以终端会话继续')}
        </Button>
      )}
    </>
  );

  return (
    <SurfaceShell
      open={open}
      onOpenChange={onOpenChange}
      mobile={mobile}
      mobileVariant="sheet"
      title={t('Continue this session', '继续此会话')}
      description={resume.cwd ? shortPath(resume.cwd) : undefined}
      className="sm:max-w-[520px]"
      footer={footer}
    >
      <div className="flex flex-col gap-4">
        <div className="flex gap-2.5 rounded-xl bg-warning-surface px-3 py-2.5 text-ui-sm leading-5 text-warning-surface-foreground">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" />
          <span>{t('Running the same session in two places at once makes its state and file edits conflict. Stop it in its original terminal first.', '同一个会话同时在两处运行会导致状态和文件修改冲突。请先在原终端停止该会话。')}</span>
        </div>
        {canPick && (
          <Field label={t('Supplier', '供应商')}>
            <SimpleSelect value={profileId} onChange={setProfileId} options={supplierOptions(catalog, resume.provider, true)} className="w-full" ariaLabel={t('Supplier', '供应商')} />
          </Field>
        )}
        <label className="flex cursor-pointer items-center gap-2.5 rounded-xl border border-border/70 px-3 py-2.5 max-md:min-h-11">
          <Checkbox checked={stopped} onCheckedChange={v => setStopped(v === true)} />
          <span className="text-ui-base">{t('I have stopped this session', '我已停止该会话')}</span>
        </label>
        {claude && (
          <p className="text-ui-sm text-foreground-subtle">
            {t("A terminal session runs Claude Code in the session's project directory and trusts it, as when you resumed it there.", '终端会话会在该会话的项目目录中运行 Claude Code，并像在那里恢复会话一样信任此目录。')}
          </p>
        )}
        <ErrorText>{error}</ErrorText>
      </div>
    </SurfaceShell>
  );
}

function Field({ label, hint, optional, children }: { label: string; hint?: string; optional?: boolean; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-baseline gap-1.5 text-ui-sm font-medium text-foreground">
        {label}
        {optional && <span className="font-normal text-foreground-subtlest">{t('optional', '可选')}</span>}
      </div>
      {children}
      {hint && <p className="text-ui-xs text-foreground-subtlest">{hint}</p>}
    </div>
  );
}

function InlineField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-3 max-sm:flex-col max-sm:items-stretch max-sm:gap-1.5">
      <span className="flex-1 text-ui-sm text-foreground-subtle">{label}</span>
      {children}
    </div>
  );
}

function ChoiceCard({ active, onClick, title, description, badge }: { active: boolean; onClick: () => void; title: string; description: string; badge?: string }) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={active}
      onClick={onClick}
      className={cn(
        'flex min-h-16 flex-col items-start gap-0.5 rounded-xl border px-3.5 py-3 text-left transition-colors',
        active ? 'border-foreground/25 bg-card-selected/60 ring-1 ring-foreground/10' : 'border-border/70 hover:bg-surface-hover',
      )}
    >
      <span className="flex w-full items-center gap-2">
        <span className={cn('flex size-4 items-center justify-center rounded-full border', active ? 'border-foreground bg-foreground' : 'border-border-hover')}>
          {active && <span className="size-1.5 rounded-full bg-background" />}
        </span>
        <span className="text-ui-base font-semibold">{title}</span>
        {badge && <span className="ml-auto rounded-md bg-surface px-1.5 text-ui-xs text-foreground-subtle">{badge}</span>}
      </span>
      <span className="pl-6 text-ui-sm text-foreground-subtle">{description}</span>
    </button>
  );
}

function Segment({ active, onClick, icon, title, description }: { active: boolean; onClick: () => void; icon: React.ReactNode; title: string; description: string }) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={active}
      onClick={onClick}
      className={cn(
        'flex items-start gap-2 rounded-lg px-2.5 py-2 text-left transition-colors',
        active ? 'bg-card text-foreground shadow-sm ring-1 ring-border/70' : 'text-foreground-subtle hover:text-foreground',
      )}
    >
      <span className="mt-0.5 shrink-0">{icon}</span>
      <span className="min-w-0">
        <span className="block text-ui-base font-medium">{title}</span>
        <span className="block text-ui-xs text-foreground-subtlest">{description}</span>
      </span>
    </button>
  );
}
