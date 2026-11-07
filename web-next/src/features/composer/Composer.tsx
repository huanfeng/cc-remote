// The message composer: a Codex-style input card at the bottom of the
// conversation column. Drafts (text + uploaded attachments) are kept per
// session; slash commands, @mentions, the pending queue, Codex steer and the
// suggestion bar sit around the card. Request bodies follow the host's
// features (sendId, defer, mentions, queueControls, codexSteer).
import { useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type ClipboardEvent, type DragEvent, type KeyboardEvent, type ReactNode } from 'react';
import { ArrowUp, AtSign, CornerDownRight, Hand, Maximize2, Plus, Square, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { Spinner } from '@/components/ui/spinner';
import { toast } from '@/components/ui/toast';
import { cn } from '@/components/lib/utils';
import { useBackend } from '@/backend/backend';
import type { PendingMessage, Session, SlashCommand } from '@/backend/types';
import type { TranscriptState } from '@/store/transcripts';
import { hasFeature, useApp } from '@/store/app';
import { usePrefs } from '@/lib/prefs';
import { t } from '@/lib/i18n';
import { providerName, providerOf } from '@/domain/providers';
import { contextReading } from '@/domain/contextReading';
import { mentionAt } from '@/domain/mentions';
import { PermissionPicker } from '@/features/pickers/Pickers';
import { QuickSwitch, patchOverride, useModelSwitchConfirm, type QuickKind } from '@/features/pickers/QuickSwitch';
import {
  MAX_ATTACHMENTS, admitFiles, canSteer, commandName, filterCommands, isCommand, isSendKey, modelArgument,
  needsSteerConfirmation, quickSwitchCommand, sendContent, slashQuery,
} from './composerLogic';
import { sendIds } from './sendIds';
import { useDraft, useDrafts } from './drafts';
import { AttachmentStrip, ContextRing, InlineMenu, PendingQueue, SuggestionBar, mentionItem, type Uploading } from './parts';

export interface ComposerProps {
  session: Session;
  transcript: TranscriptState;
  mobile: boolean;
}

const errorText = (e: unknown) => (e instanceof Error ? e.message : String(e));

export function Composer({ session, transcript, mobile }: ComposerProps) {
  const backend = useBackend();
  const info = useApp(s => s.info);
  const openSurface = useApp(s => s.open);
  const sendKey = usePrefs(s => s.sendKey);
  const id = session.id;
  const provider = providerOf(session);
  const draft = useDraft(id);
  const { setText, setAttachments, clear } = useDrafts.getState();
  const text = draft.text;
  const attachments = draft.attachments;

  const box = useRef<HTMLTextAreaElement>(null);
  const menuId = useId();
  const fileInput = useRef<HTMLInputElement>(null);
  const composing = useRef(false);
  const [busy, setBusy] = useState(false);
  const [uploading, setUploading] = useState<Uploading[]>([]);
  const [error, setError] = useState('');
  const [dragging, setDragging] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const [overflowing, setOverflowing] = useState(false);
  const [caret, setCaret] = useState(text.length);
  const [quick, setQuick] = useState<QuickKind | null>(null);
  const [, rerender] = useState(0);
  const { confirm: confirmModel, dialog: modelDialog } = useModelSwitchConfirm();

  const running = session.status === 'running';
  const features = info?.features;
  const queueControls = hasFeature(info, 'queueControls');
  const mentionsOn = hasFeature(info, 'mentions');
  const available = provider === 'codex' ? info?.codexAvailable !== false : info?.claudeAvailable !== false;
  const trimmed = text.trim();
  const steerKey = `steer:${id}`;
  const steerConfirming = needsSteerConfirmation(sendIds.get(steerKey), text);
  const steerable = canSteer(features, session) && !transcript.requests.length;
  const empty = !trimmed && !attachments.length;
  const showStop = running && empty && !uploading.length;

  const setDraftText = useCallback((v: string) => setText(id, v), [id, setText]);

  // ---------- slash commands ----------
  const [commands, setCommands] = useState<SlashCommand[]>([]);
  useEffect(() => {
    let live = true;
    backend.commands(id).then(c => { if (live) setCommands(c); }).catch(() => {});
    return () => { live = false; };
  }, [backend, id]);
  const slash = slashQuery(text);
  const [slashClosedFor, setSlashClosedFor] = useState<string | null>(null);
  const slashItems = useMemo(() => (slash === undefined ? [] : filterCommands(commands, slash)), [commands, slash]);
  const slashOpen = slash !== undefined && slashItems.length > 0 && slashClosedFor !== text;
  const [slashActive, setSlashActive] = useState(0);
  useEffect(() => { setSlashActive(0); }, [slash]);

  const pickCommand = (c: SlashCommand) => {
    const name = commandName(c.name);
    if (name === 'model' || name === 'effort') { setDraftText(''); setQuick(name); return; }
    const next = `/${name} `;
    setDraftText(next);
    focusAt(next.length);
  };

  // ---------- @mentions ----------
  const mention = mentionsOn ? mentionAt(text, Math.min(caret, text.length)) : undefined;
  const mentionQuery = mention?.query;
  const [paths, setPaths] = useState<{ q: string; paths: string[] }>({ q: '', paths: [] });
  const [mentionClosedAt, setMentionClosedAt] = useState(-1);
  const [mentionActive, setMentionActive] = useState(0);
  useEffect(() => {
    if (mentionQuery === undefined) return;
    let live = true;
    const timer = setTimeout(() => {
      backend.mentions(id, mentionQuery)
        .then(p => { if (live) { setPaths({ q: mentionQuery, paths: p }); setMentionActive(0); } })
        .catch(() => { if (live) setPaths({ q: mentionQuery, paths: [] }); });
    }, 120);
    return () => { live = false; clearTimeout(timer); };
  }, [backend, id, mentionQuery]);
  useEffect(() => { if (!mention) setMentionClosedAt(-1); }, [mention?.start]); // eslint-disable-line react-hooks/exhaustive-deps
  const mentionOpen = !!mention && mention.start !== mentionClosedAt && paths.q === mentionQuery && paths.paths.length > 0 && !slashOpen;

  const pickMention = (path: string) => {
    if (!mention) return;
    const dir = path.endsWith('/');
    const insert = `@${path}${dir ? '' : ' '}`;
    const after = text.slice(mention.end);
    const next = text.slice(0, mention.start) + insert + (!dir && after.startsWith(' ') ? after.slice(1) : after);
    setDraftText(next);
    focusAt(mention.start + insert.length);
  };

  function focusAt(at: number) {
    setCaret(at);
    requestAnimationFrame(() => {
      const el = box.current;
      if (!el) return;
      el.focus();
      el.setSelectionRange(at, at);
    });
  }

  // ---------- auto-grow ----------
  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    el.style.height = 'auto';
    const max = Math.round(window.innerHeight * 0.4);
    const h = el.scrollHeight;
    el.style.height = `${Math.min(h, max)}px`;
    setOverflowing(h > max + 1);
  }, [text, mobile]);

  // Desktop: the box is ready to type into when a session opens.
  useEffect(() => { if (!mobile) box.current?.focus({ preventScroll: true }); }, [mobile]);

  // ---------- attachments ----------
  const addFiles = async (files: File[]) => {
    if (!files.length) return;
    const { accepted, tooLarge, overLimit } = admitFiles(files, attachments.length + uploading.length);
    if (tooLarge.length) toast(t(`Over 20 MiB, not attached: ${tooLarge.map(f => f.name).join(', ')}`, `超过 20 MiB，未添加：${tooLarge.map(f => f.name).join('、')}`), { variant: 'warning', position: 'bottom-center' });
    if (overLimit.length) toast(t(`Up to ${MAX_ATTACHMENTS} attachments per message`, `每条消息最多 ${MAX_ATTACHMENTS} 个附件`), { variant: 'warning', position: 'bottom-center' });
    await Promise.all(accepted.map(async file => {
      const key = `${file.name}:${file.size}:${Math.random().toString(36).slice(2)}`;
      const preview = file.type.startsWith('image/') ? URL.createObjectURL(file) : undefined;
      setUploading(u => [...u, { key, name: file.name, size: file.size, preview }]);
      try {
        const a = await backend.upload(id, file);
        setAttachments(id, list => [...list, a]);
      } catch (e) {
        toast(t(`Upload failed: ${file.name} · ${errorText(e)}`, `上传失败：${file.name} · ${errorText(e)}`), { variant: 'warning', position: 'bottom-center' });
      } finally {
        setUploading(u => u.filter(x => x.key !== key));
        if (preview) URL.revokeObjectURL(preview);
      }
    }));
  };
  const onPaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    const files = Array.from(e.clipboardData.files);
    if (files.length) { e.preventDefault(); void addFiles(files); }
  };
  const hasFiles = (e: DragEvent) => e.dataTransfer.types.includes('Files');
  const dropHandlers = {
    onDragOver: (e: DragEvent) => { if (!hasFiles(e)) return; e.preventDefault(); e.dataTransfer.dropEffect = 'copy'; setDragging(true); },
    onDragLeave: (e: DragEvent) => { if (!e.currentTarget.contains(e.relatedTarget as Node)) setDragging(false); },
    onDrop: (e: DragEvent) => { if (!hasFiles(e)) return; e.preventDefault(); setDragging(false); void addFiles(Array.from(e.dataTransfer.files)); },
  };

  // ---------- sending ----------
  const deferBody = queueControls ? { defer: true } : {};
  const sendIdBody = (key: string, content: string) => (hasFeature(info, 'sendId') ? { sendId: sendIds.idFor(key, content) } : {});

  async function steer() {
    if (busy) return;
    const body = text.trim();
    const confirming = needsSteerConfirmation(sendIds.get(steerKey), body);
    if (!confirming && (!steerable || uploading.length || attachments.length || !body || isCommand(body))) return;
    setBusy(true); setError('');
    try {
      const sendId = sendIds.idFor(steerKey, body);
      if (confirming) {
        // A lost reply: ask what became of the original submission; never resubmit it.
        const r = await backend.steerStatus(id, sendId);
        if (r.delivery === 'rejected') {
          sendIds.clear(steerKey);
          setError(t('The instruction was not taken. You can add it again.', '这条指令未被接收，可以重新追加。'));
        } else if (r.delivery === 'pending' || r.delivery === 'unknown') {
          setError(r.delivery === 'pending'
            ? t('Still confirming delivery; check again shortly.', '仍在确认送达，请稍后再检查。')
            : t('Delivery could not be confirmed yet; check again shortly.', '暂时无法确认是否送达，请稍后再检查。'));
        } else {
          sendIds.clear(steerKey);
          clear(id, { text: body, attachmentIds: [] });
          toast(t('The instruction was added to the current task.', '指令已追加到当前任务。'), { position: 'bottom-center', variant: 'success' });
        }
        return;
      }
      await backend.steer(id, body, sendId);
      sendIds.clear(steerKey);
      clear(id, { text: body, attachmentIds: [] });
    } catch (e) {
      // A rejected steer was not taken; anything else may have been: keep the ID to check.
      if ((e as { delivery?: string }).delivery === 'rejected') sendIds.clear(steerKey);
      setError(errorText(e));
    } finally {
      setBusy(false); rerender(n => n + 1);
    }
  }

  async function submit() {
    if (steerConfirming) { await steer(); return; }
    const body = text.trim();
    if ((!body && !attachments.length) || busy || uploading.length) return;
    const kind = quickSwitchCommand(body);
    if (kind) { setDraftText(''); setQuick(kind); return; }
    setBusy(true); setError('');
    try {
      if (isCommand(body)) {
        if (attachments.length) throw new Error(t('Remove the attachments before running a command, or send them with a regular message.', '请先移除附件再执行命令，或用普通消息发送附件。'));
        const model = session.mode === 'terminal' ? modelArgument(body) : undefined;
        if (model && !(await confirmModel(session, model))) return;
        const result = await backend.runCommand(id, body);
        if (result.sendMessage) await backend.send(id, { text: body, ...sendIdBody(id, body) });
        sendIds.clear(id);
        clear(id, { text: body, attachmentIds: [] });
        if (result.openSettings) openSurface({ type: 'settings' });
        if (result.message) toast(result.message, { position: 'bottom-center' });
      } else {
        const attachmentIds = attachments.map(a => a.id);
        await backend.send(id, { text: body, attachmentIds, ...deferBody, ...sendIdBody(id, sendContent(body, attachmentIds)) });
        sendIds.clear(id);
        clear(id, { text: body, attachmentIds });
      }
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }

  async function sendSuggestion(suggestion: string) {
    if (busy) return;
    const key = `suggestion:${id}`;
    setBusy(true); setError('');
    try {
      await backend.send(id, { text: suggestion, ...deferBody, ...sendIdBody(key, suggestion) });
      sendIds.clear(key);
    } catch (e) { setError(errorText(e)); } finally { setBusy(false); }
  }

  const stop = () => { void backend.stop(id).catch(e => setError(errorText(e))); };

  // ---------- keys ----------
  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    const composingNow = e.nativeEvent.isComposing || composing.current || e.keyCode === 229;
    const menu = slashOpen
      ? { n: slashItems.length, active: slashActive, set: setSlashActive, pick: (i: number) => pickCommand(slashItems[i]), close: () => setSlashClosedFor(text) }
      : mentionOpen
        ? { n: paths.paths.length, active: mentionActive, set: setMentionActive, pick: (i: number) => pickMention(paths.paths[i]), close: () => setMentionClosedAt(mention!.start) }
        : undefined;
    if (menu && !composingNow) {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        menu.set((menu.active + (e.key === 'ArrowDown' ? 1 : -1) + menu.n) % menu.n);
        return;
      }
      // Enter on a fully typed command runs it; otherwise Enter / Tab complete the name.
      const exact = slashOpen && e.key === 'Enter' && commandName(slashItems[slashActive]?.name ?? '').toLowerCase() === slash?.toLowerCase();
      if (!exact && ((e.key === 'Enter' && !e.shiftKey && !e.altKey) || e.key === 'Tab')) {
        e.preventDefault();
        menu.pick(Math.min(menu.active, menu.n - 1));
        return;
      }
      if (e.key === 'Escape') { e.preventDefault(); menu.close(); return; }
    }
    if (e.key === 'Enter' && e.altKey && !composingNow) {
      // Alt+Enter is a newline everywhere; browsers do not all insert one by themselves.
      e.preventDefault();
      if (!document.execCommand('insertText', false, '\n')) {
        const el = e.currentTarget, at = el.selectionStart, end = el.selectionEnd;
        setDraftText(text.slice(0, at) + '\n' + text.slice(end));
        focusAt(at + 1);
      }
      return;
    }
    if (isSendKey({ ...e, isComposing: e.nativeEvent.isComposing, keyCode: e.keyCode }, sendKey, composing.current)) {
      e.preventDefault();
      void submit();
    }
  };

  const insertAt = () => {
    const el = box.current;
    const at = el?.selectionStart ?? text.length;
    const before = text.slice(0, at);
    const pad = before && !/\s$/.test(before) ? ' ' : '';
    setDraftText(`${before}${pad}@${text.slice(at)}`);
    focusAt(at + pad.length + 1);
  };
  const toggleSlash = () => {
    if (slash !== undefined) { setDraftText(''); focusAt(0); return; }
    setDraftText('/'); focusAt(1);
  };

  const onPermission = async (mode: string) => {
    try {
      await patchOverride(backend, session, { permissionMode: mode });
      toast(running ? t('Permissions saved. They apply after the current task.', '权限已保存，当前任务结束后生效。') : t('Permissions saved.', '权限已保存。'), { position: 'bottom-center', variant: 'success' });
    } catch (e) { setError(errorText(e)); }
  };

  const reading = contextReading(provider, session.live, transcript.events);
  const queued = session.queued ?? [];
  const suggestion = session.live?.suggestion;
  const showSuggestion = !!suggestion && !running && session.status !== 'waiting' && !text && !attachments.length;

  const queueOps = {
    onSendNow: (m: PendingMessage) => backend.sendQueuedNow(id, m.id!),
    onEdit: (m: PendingMessage, v: string) => backend.editQueued(id, m.id!, v),
    onRemove: (m: PendingMessage) => backend.removeQueued(id, m.id!),
  };

  const placeholder = running
    ? t('The message is sent after the current task', '消息会在当前任务完成后发送')
    : t(`Ask ${providerName(provider)} anything, @ for files, / for commands`, `向 ${providerName(provider)} 提问，@ 引用文件，/ 使用命令`);
  const sendDisabled = busy || !!uploading.length || empty || (!isCommand(trimmed) && !available);
  const sendLabel = running ? t('Queue after the current task', '当前任务完成后发送') : t('Send', '发送');
  const keyHint = sendKey === 'enter' ? t('Enter to send · Shift+Enter for a new line', 'Enter 发送 · Shift+Enter 换行') : t('Ctrl+Enter to send', 'Ctrl+Enter 发送');
  const iconSize = mobile ? 'icon-lg' : 'icon-md';

  const card = (
    <div
      {...dropHandlers}
      className={cn(
        'relative flex flex-col rounded-2xl border-[0.5px] border-input-border bg-input shadow-(--shadow-composer-soft) transition-colors',
        'hover:border-input-border-hover focus-within:!border-input-border-focused focus-within:bg-input-focused',
        dragging && 'border-brand ring-1 ring-brand/30',
        mobile && 'rounded-b-none border-x-0 border-b-0 shadow-none',
      )}
    >
      {dragging && (
        <div className="pointer-events-none absolute inset-0 z-20 flex items-center justify-center rounded-2xl bg-background/70 backdrop-blur-[2px]">
          <span className="flex items-center gap-2 rounded-full border border-border bg-popover px-4 py-2 text-ui-base shadow-sm">
            <Hand className="size-4" />{t('Drop to attach (≤ 20 MiB each, up to 8)', '松开以添加附件（单个 ≤ 20 MiB，最多 8 个）')}
          </span>
        </div>
      )}
      {slashOpen && (
        <InlineMenu
          id={menuId} label={t('Commands', '命令')} mobile={mobile} active={slashActive} onActive={setSlashActive}
          onPick={i => pickCommand(slashItems[i])}
          items={slashItems.map(c => ({
            key: c.name, icon: <SlashGlyph />, label: `/${commandName(c.name)}`,
            hint: `${c.description}${c.terminalOnly ? t(' · in the terminal', ' · 终端中') : ''}`,
          }))}
          footer={t('↑↓ choose · Enter / Tab pick · Esc close', '↑↓ 选择 · Enter / Tab 选中 · Esc 关闭')}
        />
      )}
      {mentionOpen && (
        <InlineMenu
          id={menuId} label={t('Files in the session', '会话中的文件')} mobile={mobile} active={mentionActive} onActive={setMentionActive}
          onPick={i => pickMention(paths.paths[i])} items={paths.paths.map(mentionItem)}
        />
      )}

      <AttachmentStrip attachments={attachments} uploading={uploading} mobile={mobile} onRemove={aid => setAttachments(id, l => l.filter(a => a.id !== aid))} />

      <div className="relative">
        <textarea
          ref={box}
          rows={1}
          value={text}
          aria-label={t(`Message to ${providerName(provider)}`, `发送给 ${providerName(provider)} 的消息`)}
          placeholder={placeholder}
          role="combobox"
          aria-autocomplete="list"
          aria-haspopup="listbox"
          aria-expanded={slashOpen || mentionOpen}
          aria-controls={slashOpen || mentionOpen ? menuId : undefined}
          aria-activedescendant={slashOpen ? `${menuId}-${slashActive}` : mentionOpen ? `${menuId}-${mentionActive}` : undefined}
          onChange={e => { setDraftText(e.target.value); setCaret(e.target.selectionStart); }}
          onSelect={e => setCaret(e.currentTarget.selectionStart)}
          onPaste={onPaste}
          onKeyDown={onKeyDown}
          onCompositionStart={() => { composing.current = true; }}
          onCompositionEnd={() => { composing.current = false; }}
          className={cn(
            'block w-full resize-none bg-transparent px-4 pb-1 pt-3 leading-6 text-foreground outline-none placeholder:text-foreground-subtlest',
            mobile ? 'min-h-11 text-mobile-input-safe' : 'min-h-[52px] text-ui-base',
            overflowing ? 'overflow-y-auto pr-10' : 'overflow-y-hidden',
          )}
        />
        {overflowing && (
          <Button
            variant="ghost" size={mobile ? 'icon-lg' : 'icon-sm'} className={cn('absolute right-2 top-2 text-foreground-subtle', mobile && 'right-1 top-1 size-9')}
            aria-label={t('Enlarge message box', '放大输入框')} title={t('Enlarge', '放大')} onClick={() => setExpanded(true)}
          >
            <Maximize2 className="size-3.5" />
          </Button>
        )}
      </div>

      <div className={cn('flex items-center gap-0.5 px-2 pb-2', mobile && 'gap-1')}>
        <ToolButton size={iconSize} label={t('Attach files', '添加附件')} disabled={busy || attachments.length + uploading.length >= MAX_ATTACHMENTS} onClick={() => fileInput.current?.click()}>
          <Plus className="size-4" />
        </ToolButton>
        <input ref={fileInput} type="file" multiple hidden onChange={e => { void addFiles(Array.from(e.target.files ?? [])); e.target.value = ''; }} />
        <ToolButton size={iconSize} label={t('Commands', '命令')} disabled={!!trimmed && slash === undefined} onClick={toggleSlash}>
          <SlashGlyph />
        </ToolButton>
        {mentionsOn && (
          <ToolButton size={iconSize} label={t('Mention a file', '引用文件')} onClick={insertAt}>
            <AtSign className="size-3.5" />
          </ToolButton>
        )}
        <PermissionPicker
          provider={provider} value={session.permissionMode ?? ''} effective={session.live?.permissionMode}
          inheritLabel="" variant={mobile ? 'icon' : 'chip'} mobile={mobile} disabled={busy}
          onChange={v => void onPermission(v)}
        />
        <div className="flex-1" />
        <ContextRing reading={reading} provider={provider} mobile={mobile} />
        {(steerable || steerConfirming) && !showStop && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="secondary" size={mobile ? 'lg' : 'default'} className="ml-1 rounded-full px-3"
                disabled={busy || (!steerConfirming && (!!uploading.length || !!attachments.length || !trimmed || isCommand(trimmed)))}
                onClick={() => void steer()}
              >
                <CornerDownRight className="size-3.5" />
                {steerConfirming ? t('Check instruction', '检查追加指令') : t('Add to current task', '追加到当前任务')}
              </Button>
            </TooltipTrigger>
            <TooltipContent className="max-w-64">
              {steerConfirming
                ? t('Checks the earlier submission by its send ID; nothing is sent twice.', '用原发送 ID 确认上次的结果，不会重复提交。')
                : t('Adds this instruction to the running turn. Text only; it never starts a new turn.', '把这条指令追加到正在运行的任务。仅支持文本，不会启动新任务。')}
            </TooltipContent>
          </Tooltip>
        )}
        {showStop ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                type="button" onClick={stop} aria-label={t('Stop', '停止')}
                className={cn('ml-1 flex shrink-0 items-center justify-center rounded-full bg-foreground text-background transition-opacity hover:opacity-85', mobile ? 'size-9' : 'size-8')}
              >
                <Square className="size-3 fill-current" />
              </button>
            </TooltipTrigger>
            {/* The tooltip claims Escape; here Escape still means stop. */}
            <TooltipContent onEscapeKeyDown={stop}>{t('Stop the running turn (Esc)', '停止当前任务（Esc）')}</TooltipContent>
          </Tooltip>
        ) : (
          <Tooltip>
            <TooltipTrigger asChild>
              <span className="ml-1 inline-flex">
                <button
                  type="button" onClick={() => void submit()} disabled={sendDisabled} aria-label={sendLabel}
                  className={cn(
                    'flex shrink-0 items-center justify-center rounded-full bg-foreground text-background transition-[opacity,background-color] hover:opacity-85 disabled:pointer-events-none disabled:bg-foreground/20 disabled:text-background',
                    mobile ? 'size-9' : 'size-8',
                  )}
                >
                  {busy || uploading.length ? <Spinner className="size-4" /> : <ArrowUp className="size-4" strokeWidth={2.25} />}
                </button>
              </span>
            </TooltipTrigger>
            <TooltipContent>{!available && !isCommand(trimmed) ? t(`${providerName(provider)} is not available on this host`, `此宿主上 ${providerName(provider)} 不可用`) : `${sendLabel} · ${keyHint}`}</TooltipContent>
          </Tooltip>
        )}
      </div>
    </div>
  );

  return (
    <div className={cn('mx-auto w-full max-w-3xl shrink-0', mobile ? 'px-0 pb-safe' : 'px-4 pb-4')}>
      {showSuggestion && <SuggestionBar text={suggestion!} busy={busy} mobile={mobile} onSend={() => void sendSuggestion(suggestion!)} onEdit={() => { setDraftText(suggestion!); focusAt(suggestion!.length); }} />}
      <PendingQueue items={queued} controls={queueControls} mobile={mobile} {...queueOps} />
      <QuickSwitch
        session={session} kind={quick ?? 'model'} open={!!quick} mobile={mobile} side="top"
        onOpenChange={o => { if (!o) { setQuick(null); box.current?.focus(); } }}
        anchor={card}
      />
      {error && (
        <div role="alert" className={cn('mt-1.5 flex items-center gap-2 px-2 text-ui-sm text-destructive', mobile && 'px-4 pb-1')}>
          <span className="min-w-0 flex-1 break-words">{error}</span>
          <button type="button" aria-label={t('Dismiss', '关闭')} onClick={() => setError('')}
            className={cn('-my-1 flex shrink-0 items-center justify-center rounded-md text-foreground-subtle hover:bg-hover hover:text-foreground', mobile ? '-mr-2 size-9' : 'size-6')}>
            <X className="size-3.5" />
          </button>
        </div>
      )}
      {modelDialog}
      <Dialog open={expanded} onOpenChange={o => { setExpanded(o); if (!o) requestAnimationFrame(() => box.current?.focus()); }}>
        <DialogContent className={cn('flex max-w-3xl flex-col gap-3', mobile && 'h-[90dvh]')}>
          <DialogTitle>{t('Edit message', '编辑消息')}</DialogTitle>
          <DialogDescription className="sr-only">{keyHint}</DialogDescription>
          <textarea
            autoFocus value={text}
            onChange={e => setDraftText(e.target.value)}
            onCompositionStart={() => { composing.current = true; }}
            onCompositionEnd={() => { composing.current = false; }}
            onKeyDown={e => {
              if (isSendKey({ ...e, isComposing: e.nativeEvent.isComposing, keyCode: e.keyCode }, sendKey, composing.current)) {
                e.preventDefault(); setExpanded(false); void submit();
              }
            }}
            className={cn('h-[60dvh] w-full flex-1 resize-none rounded-xl border border-input-border bg-input p-3 leading-6 outline-none focus:border-input-border-focused', mobile ? 'text-mobile-input-safe' : 'text-ui-base')}
          />
          <div className="flex items-center justify-between gap-2">
            <span className="text-ui-sm text-foreground-subtlest">{keyHint}</span>
            <div className="flex gap-2">
              <Button variant="ghost" onClick={() => setExpanded(false)}>{t('Done', '完成')}</Button>
              <Button disabled={sendDisabled} onClick={() => { setExpanded(false); void submit(); }}>
                <ArrowUp className="size-3.5" />{sendLabel}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function ToolButton({ label, onClick, disabled, size, children }: { label: string; onClick(): void; disabled?: boolean; size: 'icon-md' | 'icon-lg'; children: ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button type="button" variant="ghost" size={size} aria-label={label} disabled={disabled} onClick={onClick}
          className={cn('rounded-full text-foreground-subtle hover:text-foreground', size === 'icon-lg' && 'size-9')}>
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

/** The command trigger is the "/" character itself; the lucide Slash icon reads too large. */
function SlashGlyph() {
  return <span aria-hidden className="flex size-4 items-center justify-center text-ui-base leading-none">/</span>;
}
