// Domain types shared by the UI and every Backend implementation. They mirror
// the host's JSON (internal/store/store.go, internal/server/*.go); see
// docs/redesign-inventory.md for the protocol they come from.

export type Provider = 'claude' | 'codex';

export interface MonitorWindow {
  name: string;
  percent: number;
  windowMinutes?: number;
  resetsAt?: string;
}
export interface MonitorAccount {
  id: string;
  provider: Provider;
  name: string;
  profile?: string;
  active: boolean;
  identityKnown: boolean;
  available: boolean;
  reason?: string;
  plan?: string;
  windows: MonitorWindow[];
  fetchedAt?: string;
  stale?: boolean;
  error?: string;
}
export interface UsageMonitor { intervalMinutes: 10 | 30; accounts: MonitorAccount[] }
export interface MonitorHistory {
  hours: number;
  points: { at: string; intervalMinutes?: number; windows: MonitorWindow[] | null; error?: string }[];
}
export type SessionMode = 'managed' | 'terminal';
export type SessionStatus =
  | 'running' | 'waiting' | 'idle' | 'completed' | 'interrupted' | 'failed' | 'stopped' | 'error';

/** Features a host declares in /api/info; request bodies may only use fields a host declares. */
export type HostFeature = 'sendId' | 'mentions' | 'codex' | 'codexSteer' | 'queueControls' | 'archive';

export interface HostInfo {
  version: string;
  cwd: string;
  claudeAvailable: boolean;
  claudeVersion?: string;
  codexAvailable?: boolean;
  codexVersion?: string;
  providers?: Provider[];
  attachCommand?: string;
  restartPending?: boolean;
  language?: string;
  name?: string;
  color?: string;
  icon?: string;
  features?: HostFeature[];
}

export interface LiveStatus {
  model?: string;
  modelId?: string;
  effort?: string;
  costUSD: number;
  durationMs: number;
  contextTokens: number;
  contextSize: number;
  contextPercent: number;
  activity?: string;
  suggestion?: string;
  permissionMode?: string;
}

export interface Attachment {
  id: string;
  kind?: 'image' | string;
  name: string;
  mime: string;
  size: number;
  url: string;
}

export interface PendingMessage {
  id?: string;
  text: string;
  payload?: { attachments?: Attachment[] };
  editable?: boolean;
  canSendNow?: boolean;
  delivery?: 'pending' | 'unknown' | string;
}

export interface Session {
  id: string;
  provider?: Provider;
  profileId?: string;
  profileName?: string;
  title: string;
  cwd: string;
  truncated?: boolean;
  status?: SessionStatus;
  createdAt?: string;
  lastConversationAt?: string;
  conversationTimeEstimated?: boolean;
  updatedAt: string;
  mode?: SessionMode;
  online?: boolean;
  attached?: number;
  claudeSessionId?: string;
  permissionMode?: string;
  model?: string;
  effort?: string;
  /** Claude Code options a terminal session was started with. */
  claudeArgs?: string[];
  queued?: PendingMessage[];
  live?: LiveStatus;
  activeTasks?: number;
  /** Archived: hidden from the list until its next conversation (host feature `archive`). */
  archived?: boolean;
}

/** A session from the provider's own history (~/.claude/projects, Codex threads). */
export interface NativeSession {
  provider?: Provider;
  id: string;
  title: string;
  cwd: string;
  updatedAt: string;
  createdAt?: string;
  lastConversationAt?: string;
  conversationTimeEstimated?: boolean;
  truncated?: boolean;
}

export type EventKind =
  | 'user' | 'assistant' | 'assistant_delta' | 'tool' | 'tool_result' | 'error' | 'compact'
  | 'recap' | 'teammate' | 'status' | 'result' | 'session' | 'interaction';

export interface SessionEvent {
  seq: number;
  sessionId?: string;
  kind: EventKind | string;
  text?: string;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  payload?: any;
  createdAt: string;
  truncated?: boolean;
}

export type RequestKind = 'approval' | 'question' | 'plan';

export interface PendingRequest {
  id: string;
  sessionId?: string;
  kind: RequestKind;
  toolName: string;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  input: any;
  status: string;
  createdAt?: string;
}

export type Decision = 'allow' | 'deny' | 'terminal';

export interface RequestAnswer {
  decision: Decision;
  answers?: Record<string, string>;
  mode?: string;
  message?: string;
}

export interface SlashCommand {
  name: string;
  description: string;
  terminalOnly?: boolean;
}

export interface CommandReply {
  message?: string;
  openSettings?: boolean;
  sendMessage?: boolean;
  /** /help lists the commands the session supports. */
  commands?: SlashCommand[];
  /** /status returns the decorated session. */
  session?: Session;
}

export interface TaskInfo {
  id: string;
  toolUseId?: string;
  description: string;
  subagentType?: string;
  taskType?: 'local_bash' | 'in_process_teammate' | string;
  background: boolean;
  status: string;
  progress?: string;
  lastTool?: string;
  summary?: string;
  tokens?: number;
  toolUses?: number;
  durationMs?: number;
  startedAt: string;
  endedAt?: string;
}

export interface ModelOption {
  id: string;
  label: string;
  source: 'alias' | 'settings' | 'env' | 'recent' | 'api' | 'codex';
  description?: string;
  efforts?: string[];
  defaultEffort?: string;
  isDefault?: boolean;
}

export interface ModelList {
  models: ModelOption[];
  fetchedAt: string;
  restricted: boolean;
  warnings?: string[];
}

export interface SupplierProfile {
  id: string;
  name: string;
  app: Provider;
  kind?: 'oauth' | string;
}

export interface ProfileCatalog {
  schemaVersion?: number;
  capabilities?: { resumeSelection?: boolean; switchSupplier?: boolean; oauthProfiles?: boolean; defaultProfile?: boolean };
  source?: string;
  found?: boolean;
  unavailable?: { id: string; name: string; app: Provider; reason: string }[];
  profiles: SupplierProfile[];
  defaultProfiles: Partial<Record<Provider, string>>;
  configuredDefaults?: Partial<Record<Provider, string>>;
}

export interface UsageWindow {
  percent: number;
  resetsAt?: string;
  severity?: string;
  name?: string;
  windowMinutes?: number;
}

export interface UsageReply {
  available: boolean;
  reason?: 'third-party' | 'api-key' | 'no-login';
  provider?: Provider;
  plan?: string;
  profile?: string;
  account?: string;
  usage?: {
    windows?: UsageWindow[];
    fiveHour?: UsageWindow;
    weekly?: UsageWindow;
    models?: (UsageWindow & { name: string })[];
  };
  fetchedAt?: string;
  stale?: boolean;
  error?: string;
}

export interface UsageHistory {
  hours: number;
  points: { at: string; fiveHour?: number; weekly?: number }[];
}

export interface SessionStats {
  sessionId: string;
  turns: number;
  results: number;
  costUSD: number;
  costKnown: boolean;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheCreationTokens: number;
  durationMs: number;
  toolCalls: number;
  errors: number;
  lastActivityAt: string;
}

/** One session's row in /api/status: its identity plus its recorded statistics. */
export interface SessionStatusRow extends SessionStats {
  id: string;
  title: string;
  mode: SessionMode | string;
  status: SessionStatus | string;
  online: boolean;
  cwd: string;
}

export interface HostStatus {
  version: string;
  pid: number;
  startedAt: string;
  uptimeSeconds: number;
  listen: string;
  listenAddresses?: string[];
  listeners: { address: string; loopback: boolean; insecure?: boolean }[];
  dataDir: string;
  claudeConfigDir?: string;
  logFile?: string;
  goVersion: string;
  os: string;
  arch: string;
  claude: { executable: string; path: string; version: string; error?: string };
  service: { managed: boolean; manager: string };
  sessions: Record<'total' | 'managed' | 'terminal' | 'running' | 'waiting' | 'idle', number>;
  pendingRequests: number;
  activeRuns: number;
  storage?: { databaseBytes: number; logBytes: number; uploadsBytes?: number };
  /** Sums of every session's statistics (GET /api/status only, not /summary). */
  totals?: SessionStats;
  daily?: { date: string; turns: number; costUSD: number; tokens: number }[];
  sessionStats?: SessionStatusRow[];
  logTail?: string[];
}

export interface Preferences {
  provider?: Provider;
  permissionMode: string;
  model: string;
  effort: string;
  /** Opaque token from the host (a content hash); send back unchanged. */
  revision: string;
}

export interface HostIdentity {
  name: string;
  color: string;
  icon: string;
  shownName: string;
  shownColor: string;
  defaultName: string;
  defaultColor: string;
  palette: { key: string; hex: string }[];
}

export interface NewSessionRequest {
  provider?: Provider;
  profileId?: string;
  title: string;
  cwd: string;
  mode?: SessionMode;
  trustFolder?: boolean;
  resume?: string;
  /** Claude Code options for a terminal session (the host validates them). */
  claudeArgs?: string[];
  permissionMode?: string;
  model?: string;
  effort?: string;
  confirmDangerous?: boolean;
  /** Needs the host feature `archive`. */
  archived?: boolean;
}

export interface SessionPatch {
  permissionMode?: string;
  model?: string;
  effort?: string;
  confirmDangerous?: boolean;
  /** Needs the host feature `archive`. */
  archived?: boolean;
}

export interface SendRequest {
  text: string;
  attachmentIds?: string[];
  defer?: boolean;
  sendId?: string;
}

export interface Transcript {
  session: Session;
  events: SessionEvent[];
  hasMore: boolean;
}

export interface FileContent {
  path: string;
  mime: string;
  size: number;
  /** Text files only. */
  text?: string;
  /** Images and other binaries, as an object URL or data: URL. */
  url?: string;
  tooLarge?: boolean;
}

/** A saved connection to a host (standalone page / desktop app). */
export interface HostEntry {
  id: string;
  url: string;
  name?: string;
  color?: string;
  icon?: string;
  lan?: string;
}

// ---- Terminal stream ----

export interface TerminalSize {
  cols: number;
  rows: number;
  /** A local `ccr` terminal is attached and owns the size. */
  held: boolean;
}

export type TerminalState = 'connecting' | 'open' | 'paused' | 'exited' | 'failed';

export interface TerminalStream {
  /** Keystrokes and paste, as typed. */
  send(data: string): void;
  resize(cols: number, rows: number): void;
  redraw(): void;
  close(): void;
}

export interface TerminalHandlers {
  onData(chunk: Uint8Array | string): void;
  onSize(size: TerminalSize): void;
  /**
   * A stream reports 'connecting', then 'open', and ends with 'exited' (Claude Code
   * quit, close code 4001: do not reconnect) or 'failed' (reconnect with backoff).
   * opts.retry false marks a failure retrying cannot fix (the session is gone or
   * is not a terminal, the token was refused, the address is blocked).
   */
  onState(state: TerminalState, detail?: string, opts?: { retry?: boolean }): void;
}
