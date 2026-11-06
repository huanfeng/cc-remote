// Pure composer rules (no React, no '@/' imports) so node tests can load them.
// They reproduce the old client's behaviour: slash-command detection, send IDs
// that survive lost replies, steer availability and the attachment limits.

export const MAX_ATTACHMENTS = 8;
export const MAX_ATTACHMENT_BYTES = 20 * 1024 * 1024;

/** The command being typed: "/x" at the very start, before any whitespace. */
export function slashQuery(draft: string): string | undefined {
  if (!draft.startsWith('/') || /\s/.test(draft)) return;
  return draft.slice(1);
}

/** Host command names may or may not carry the leading slash. */
export const commandName = (name: string) => name.replace(/^\/+/, '');

export function filterCommands<T extends { name: string; description?: string }>(commands: readonly T[], query: string): T[] {
  const q = query.toLowerCase();
  const starts = commands.filter(c => commandName(c.name).toLowerCase().startsWith(q));
  if (!q) return starts;
  const contains = commands.filter(c => !starts.includes(c) && commandName(c.name).toLowerCase().includes(q));
  return [...starts, ...contains];
}

/** "/model" or "/effort" with no argument opens the quick switch instead of being sent. */
export function quickSwitchCommand(text: string): 'model' | 'effort' | undefined {
  const m = text.trim().match(/^\/(model|effort)$/);
  return m ? (m[1] as 'model' | 'effort') : undefined;
}

/** "/model X" typed for a terminal session: the model it switches to. */
export function modelArgument(text: string): string | undefined {
  const m = text.trim().match(/^\/model\s+(\S.*)$/);
  return m ? m[1].trim() : undefined;
}

export const isCommand = (text: string) => text.trim().startsWith('/');

/**
 * Send IDs. A message keeps its ID until the host accepts it, so retrying
 * after a lost reply does not give the agent a second copy
 * (internal/server/sends.go). New content gets a new ID.
 */
export class SendIds {
  private held = new Map<string, { content: string; id: string }>();
  private random: () => string;
  constructor(random: () => string = randomId) { this.random = random; }
  idFor(key: string, content: string): string {
    const h = this.held.get(key);
    if (h?.content === content) return h.id;
    const id = this.random();
    this.held.set(key, { content, id });
    return id;
  }
  get(key: string) { return this.held.get(key); }
  clear(key: string) { this.held.delete(key); }
}

export function randomId(): string {
  const bytes = new Uint8Array(16);
  globalThis.crypto.getRandomValues(bytes);
  return Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('');
}

/** The content a message's send ID is bound to: its text and attachments. */
export const sendContent = (text: string, attachmentIds: readonly string[] = []) => [text, ...attachmentIds].join('\n');

/** A steer whose reply was lost stays available, as a check, for the same text. */
export const needsSteerConfirmation = (held: { content: string } | undefined, text: string) =>
  !!held && held.content === text.trim();

export function canSteer(
  features: readonly string[] | undefined,
  session: { provider?: string; mode?: string; status?: string; queued?: readonly unknown[] } | undefined,
): boolean {
  return !!features?.includes('codexSteer') && session?.provider === 'codex' && session.mode !== 'terminal'
    && session.status === 'running' && !session.queued?.length;
}

/** Splits picked files into the ones that fit the limits and the reasons the rest were refused. */
export function admitFiles<F extends { name: string; size: number }>(files: readonly F[], existing: number): { accepted: F[]; tooLarge: F[]; overLimit: F[] } {
  const accepted: F[] = [], tooLarge: F[] = [], overLimit: F[] = [];
  for (const f of files) {
    if (f.size > MAX_ATTACHMENT_BYTES) tooLarge.push(f);
    else if (existing + accepted.length >= MAX_ATTACHMENTS) overLimit.push(f);
    else accepted.push(f);
  }
  return { accepted, tooLarge, overLimit };
}

/** Whether a keydown in the message box sends. IME composition never does. */
export function isSendKey(
  e: { key: string; shiftKey: boolean; altKey: boolean; ctrlKey: boolean; metaKey: boolean; isComposing?: boolean; keyCode?: number },
  sendKey: 'enter' | 'mod-enter',
  composing = false,
): boolean {
  if (e.key !== 'Enter' || e.isComposing || composing || e.keyCode === 229) return false;
  if (e.shiftKey || e.altKey) return false;
  const mod = e.ctrlKey || e.metaKey;
  return sendKey === 'enter' ? !mod : mod;
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(n < 10 * 1024 ? 1 : 0)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

export const DRAFT_TTL_MS = 7 * 24 * 3600_000;

/** Drops drafts older than the TTL and empty ones. */
export function pruneDrafts<D extends { text: string; attachments: readonly unknown[]; at: number }>(drafts: Record<string, D>, now: number): Record<string, D> {
  const out: Record<string, D> = {};
  for (const [k, d] of Object.entries(drafts)) {
    if (!d || now - d.at > DRAFT_TTL_MS) continue;
    if (!d.text && !d.attachments?.length) continue;
    out[k] = d;
  }
  return out;
}
