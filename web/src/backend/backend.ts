// The one seam between the UI and a host. The UI never fetches or polls by
// itself: it calls these methods and subscribes to the watch* streams. The
// mock backend pushes changes directly; the HTTP backend (http/httpBackend.ts)
// polls the host behind the same interface (list 3 s, open session 1.5 s,
// usage 60 s), so polling cadence, ETags and 409 resets stay out of the components.
import { createContext, useContext, useEffect, useState } from 'react';
import { BackendError } from './errors';
import type { SupplierConfigs, SupplierConfigWrite, SupplierProbe, SupplierProbeResult } from './supplierConfig';
import type {
  Attachment, CodexAccount, CodexAccountLogin, CodexAccountLoginStatus, CommandReply, FileContent, FileScope, HostIdentity, HostInfo, HostStatus, ModelList, NativeSession,
  NewSessionRequest, PendingRequest, Preferences, ProfileCatalog, ProfileUsageReply, Provider, RequestAnswer, SendRequest, Session,
  ProjectFileListing, ProjectGitChanges, ProjectGitDiff, ProjectGitStage, ProjectShell, ProjectShellHandlers, ProjectShellStart, ProjectShellStream,
  SessionEvent, SessionForkReply, SessionForkRequest, SessionPatch, SessionStats, SlashCommand, TaskInfo, TerminalHandlers, TerminalStream,
  UsageHistory, UsageReply, UsageMonitor, MonitorHistory,
  MessageSearchQuery, MessageSearchPage, MessageSearchHit, MessageSearchContext,
  ServerUpdateSnapshot, UpdateComponentId,
} from './types';

export type Unsubscribe = () => void;

export interface CodexAccountsAPI {
  list(signal?: AbortSignal): Promise<CodexAccount[]>;
  begin(name: string): Promise<CodexAccountLogin>;
  watch(id: string, onChange: (status: CodexAccountLoginStatus) => void, onError: (error: unknown) => void): Unsubscribe;
  cancel(id: string): Promise<void>;
  rename(id: string, name: string): Promise<void>;
  remove(id: string): Promise<void>;
}

export type StatuslineStyle = 'compact' | 'color' | 'multiline';
export interface StatuslineOptions { order?: string[]; segments: string[]; colors: Record<string, string>; shortLabels?: boolean; bars: boolean; warning: number; critical: number; separator: string }
export interface StatuslineSettings {
 options?: StatuslineOptions;
 capabilities?: { customize?: boolean; shortLabels?: boolean };
  installed: boolean;
  style: StatuslineStyle | '';
  revision: string;
  hasExisting?: boolean;
}

export { BackendError };
export type { Delivery } from './errors';

/** Upload progress and cancellation. */
export interface UploadOptions {
  /** Called with 0…1 as the file is sent (not called by backends that cannot tell). */
  onProgress?: (fraction: number) => void;
  signal?: AbortSignal;
}

/** Claude Code's own ~/.claude/settings.json, the "native" layer under CC Remote's defaults. */
export interface ClaudeSettings {
  document?: Record<string, unknown>;
  editDocument?: boolean;
  model: string;
  effortLevel: string;
  permissionMode: string;
  /** Opaque token from the host; send back unchanged. */
  revision: string;
  path: string;
}

/** What a watched transcript delivers: new or replaced events, and the live session record. */
export interface TranscriptUpdate {
  session: Session;
  /** Events appended since the last update. The first update of every watch carries the tail and has reset: true. */
  events: SessionEvent[];
  /** True when the transcript was renumbered and the receiver must drop what it holds. */
  reset?: boolean;
  hasMore: boolean;
  requests: PendingRequest[];
}

export interface Backend {
  readonly kind: 'mock' | 'http';

  info(): Promise<HostInfo>;
  componentUpdates?: {
    watch(onChange: (snapshot: ServerUpdateSnapshot | null) => void, onError: (e: unknown) => void): Unsubscribe;
    check(id: UpdateComponentId): Promise<ServerUpdateSnapshot>;
    install(id: UpdateComponentId, version?: string): Promise<ServerUpdateSnapshot>;
  };

  // Sessions
  /**
   * The host's managed sessions, re-delivered whenever the list changes (an
   * unchanged poll is not delivered). onError reports a failed poll; the watch keeps going.
   */
  watchSessions(onChange: (sessions: Session[]) => void, onError?: (e: BackendError) => void): Unsubscribe;
  createSession(body: NewSessionRequest): Promise<Session>;
  forkSession(id: string, body: SessionForkRequest, native?: Provider): Promise<SessionForkReply>;
  patchSession(id: string, patch: SessionPatch): Promise<Session>;
  deleteSession(id: string, opts?: { stop?: boolean }): Promise<void>;
  stop(id: string): Promise<void>;
  exit(id: string): Promise<void>;
  switchProfile(id: string, profileId: string, force?: boolean): Promise<Session>;

  // Transcript
  watchTranscript(id: string, onUpdate: (u: TranscriptUpdate) => void, opts?: { native?: Provider }): Unsubscribe;
  loadOlder(id: string, beforeSeq: number, opts?: { native?: Provider }): Promise<{ events: SessionEvent[]; hasMore: boolean }>;
  fullEvent(id: string, seq: number, opts?: { native?: Provider }): Promise<SessionEvent>;
  searchMessages(query: MessageSearchQuery, signal?: AbortSignal): Promise<MessageSearchPage>;
  messageContext(hit: MessageSearchHit, signal?: AbortSignal): Promise<MessageSearchContext>;

  // Composer
  send(id: string, body: SendRequest): Promise<{ status: 'running' | 'queued'; duplicate?: boolean }>;
  steer(id: string, text: string, sendId: string): Promise<{ status: string }>;
  /** Read-only check of a steer whose reply was lost (GET /steer/{sendId}); never re-sends. */
  steerStatus(id: string, sendId: string): Promise<{ status: string; delivery?: 'rejected' | 'pending' | 'unknown' }>;
  commands(id: string): Promise<SlashCommand[]>;
  runCommand(id: string, command: string): Promise<CommandReply>;
  mentions(id: string, q: string): Promise<string[]>;
  upload(id: string, file: File, opts?: UploadOptions): Promise<Attachment>;
  /**
   * A URL an <img> or link can use for a host resource that needs the access token
   * (an attachment's `/api/sessions/{id}/uploads/{uid}` url). Host paths are fetched
   * with the token and come back as object URLs, cached by the backend (do not
   * revoke them); data:, blob: and absolute URLs come back unchanged. Prefer useAssetUrl().
   */
  assetUrl(url: string): Promise<string>;
  editQueued(id: string, messageId: string, text: string): Promise<void>;
  removeQueued(id: string, messageId: string): Promise<void>;
  sendQueuedNow(id: string, messageId: string): Promise<void>;

  // Requests (approvals, questions, plans)
  answer(requestId: string, answer: RequestAnswer): Promise<void>;

  // Details
  tasks(id: string): Promise<TaskInfo[]>;
  stats(id: string): Promise<SessionStats>;
  projectFiles(id: string, path: string, signal?: AbortSignal): Promise<ProjectFileListing>;
  searchProjectFiles(id: string, query: string, signal?: AbortSignal): Promise<ProjectFileListing>;
  projectGitChanges(id: string, signal?: AbortSignal): Promise<ProjectGitChanges>;
  projectGitDiff(id: string, path: string, stage: ProjectGitStage, signal?: AbortSignal): Promise<ProjectGitDiff>;
  readFile(id: string, path: string, base?: string[], scope?: FileScope): Promise<FileContent>;
  /** A one-time download URL for a file too large to read inline (POST /file/link). */
  fileLink(id: string, path: string, base?: string[], scope?: FileScope): Promise<{ url: string; name: string; size: number }>;

  // Native history
  nativeSessions(provider: Provider): Promise<NativeSession[]>;
  importNative(id: string, provider: Provider, profileId?: string): Promise<Session>;

  // Terminal
  openTerminal(id: string, handlers: TerminalHandlers): TerminalStream;
  projectShells(signal?: AbortSignal): Promise<ProjectShell[]>;
  watchProjectShells(onChange: (shells: ProjectShell[]) => void, onError?: (error: unknown) => void): Unsubscribe;
  startProjectShell(body: ProjectShellStart, signal?: AbortSignal): Promise<ProjectShell>;
  renameProjectShell(shellId: string, name: string, signal?: AbortSignal): Promise<ProjectShell>;
  reorientProjectShell(shellId: string, splitDirection: NonNullable<ProjectShell['splitDirection']>): Promise<ProjectShell>;
  dismissProjectShell(shellId: string): Promise<void>;
  stopProjectShell(shellId: string, runId: string): Promise<void>;
  openProjectShell(shellId: string, runId: string, after: number, handlers: ProjectShellHandlers): ProjectShellStream;

  // Configuration
  models(opts: { provider: Provider; sessionId?: string; codexAccountId?: string; refresh?: boolean }): Promise<ModelList>;
  profiles(): Promise<ProfileCatalog>;
  profileUsage?(provider: Provider): Promise<ProfileUsageReply | null>;
  setDefaultProfile(app: Provider, profileId: string): Promise<void>;
  preferences(provider: Provider): Promise<Preferences>;
  /** confirmDangerous is required when the permission mode is dangerous (dangerousPermission()). */
  savePreferences(provider: Provider, prefs: Preferences & { confirmDangerous?: boolean }): Promise<Preferences>;
  claudeSettings(): Promise<ClaudeSettings>;
  statusline?(): Promise<StatuslineSettings | null>;
  saveStatusline?(v: { style: StatuslineStyle; revision: string; options?: StatuslineOptions }): Promise<StatuslineSettings>;
  removeStatusline?(v: { revision: string }): Promise<StatuslineSettings>;
  saveClaudeSettings(v: Omit<ClaudeSettings, 'path'> & { confirmDangerous?: boolean }): Promise<ClaudeSettings>;
  identity(): Promise<HostIdentity>;
  saveIdentity(v: { name: string; color: string; icon: string }): Promise<HostIdentity>;
  oauthProfiles(): Promise<{ id: string; name: string; notes?: string }[]>;
  probeSupplier?(v: SupplierProbe): Promise<SupplierProbeResult>;
  importCCSwitch?(id: string, app: Provider, revision: string): Promise<SupplierConfigs>;
  setCCSwitchVisibility?(hidden: boolean, revision: string): Promise<SupplierConfigs>;
  supplierConfigs?(): Promise<SupplierConfigs | null>;
  saveSupplierConfig?(id: string | undefined, v: SupplierConfigWrite): Promise<SupplierConfigs>;
  removeSupplierConfig?(id: string, revision: string): Promise<SupplierConfigs>;
  addOAuthProfile(name: string, token: string, notes?: string): Promise<void>;
  /** Edits the entry without changing its id; an omitted token keeps the stored credential. */
  editOAuthProfile(id: string, patch: { name: string; token?: string; notes?: string }): Promise<void>;
  removeOAuthProfile(id: string): Promise<void>;
  codexAccounts?: CodexAccountsAPI;

  // Usage and status
  watchUsage(opts: { provider: Provider; sessionId?: string }, onChange: (u: UsageReply) => void): Unsubscribe;
  usageHistory(hours: number): Promise<UsageHistory>;
  watchUsageMonitor?(onChange: (v: UsageMonitor) => void, onError: (e: unknown) => void): Unsubscribe;
  refreshUsageMonitor?(): Promise<UsageMonitor>;
  saveUsageMonitor?(intervalMinutes: 10 | 30): Promise<UsageMonitor>;
  monitorHistory?(account: string, hours: number): Promise<MonitorHistory>;
  status(): Promise<HostStatus>;
  /** The cheap part of status() (no totals, daily chart, session rows or log tail) for a fast first paint. */
  statusSummary?(): Promise<HostStatus>;

  /** Stops every poll and stream (the shell switches host or signs out). */
  dispose?(): void;
}

export const BackendContext = createContext<Backend | null>(null);

export function useBackend(): Backend {
  const b = useContext(BackendContext);
  if (!b) throw new Error('useBackend outside BackendContext');
  return b;
}

const direct = (url: string) => !url.startsWith('/');

/**
 * Resolves an attachment (or other host) URL for <img src> / <a href>:
 * undefined while a token-protected host resource loads or if it failed.
 */
export function useAssetUrl(url: string | undefined): string | undefined {
  const backend = useBackend();
  const [resolved, setResolved] = useState<{ url: string; src?: string }>();
  const immediate = !url || backend.kind === 'mock' || direct(url);
  useEffect(() => {
    if (immediate || !url) return;
    let live = true;
    backend.assetUrl(url).then(src => { if (live) setResolved({ url, src }); }, () => { if (live) setResolved({ url }); });
    return () => { live = false; };
  }, [backend, url, immediate]);
  if (!url) return undefined;
  if (immediate) return url;
  return resolved?.url === url ? resolved.src : undefined;
}
