// A terminal session's own screen: xterm on the host's PTY stream, the same
// Claude Code a local `ccr` shows. A slim toolbar shows the connection and
// who sets the size; on phones a key bar and an input bar replace the
// keyboard the screen would otherwise need. See terminalEngine.ts for the
// connection and size logic.
import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { ArrowUp, Copy, Minus, MonitorSmartphone, Plus, RotateCw, Smartphone } from 'lucide-react';
import { useBackend } from '@/backend/backend';
import type { Session } from '@/backend/types';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { cn } from '@/components/lib/utils';
import { useApp } from '@/store/app';
import { usePrefs } from '@/lib/prefs';
import { t } from '@/lib/i18n';
import { createTerminalEngine, initialUi, type TerminalEngine, type TerminalUi } from './terminalEngine';
import { MAX_ATTEMPTS, PHONE_KEYS, pastePayload, stepZoom } from './terminalSize';
import './terminal.css';

export function TerminalView({ session, mobile }: { session: Session; mobile: boolean }) {
  const backend = useBackend();
  const box = useRef<HTMLDivElement>(null);
  const screen = useRef<HTMLDivElement>(null);
  const engine = useRef<TerminalEngine | undefined>(undefined);
  const zoom = usePrefs(s => s.terminalZoom);
  const fitDevice = usePrefs(s => s.terminalFit) && mobile;
  const zoomRef = useRef(zoom); zoomRef.current = zoom;
  const fitRef = useRef(fitDevice); fitRef.current = fitDevice;
  const [ui, setUi] = useState<TerminalUi>(() => initialUi(mobile, fitDevice));
  const sessionId = session.id;

  useEffect(() => {
    if (!box.current || !screen.current) return;
    setUi(initialUi(mobile, fitRef.current));
    const e = createTerminalEngine({
      backend, sessionId, mobile, box: box.current, screen: screen.current,
      zoom: () => zoomRef.current,
      fitDevice: () => fitRef.current,
      openFile: ref => useApp.getState().open({ type: 'file', sessionId, path: ref.line ? `${ref.path}:${ref.line}` : ref.path }),
      onUi: patch => setUi(old => ({ ...old, ...patch })),
    });
    engine.current = e;
    return () => { e.dispose(); engine.current = undefined; };
  }, [backend, sessionId, mobile]);

  useEffect(() => { engine.current?.layout(); }, [zoom]);
  // Only a change of "fit device" switches modes (a mount, even StrictMode's second one, must not).
  const prevFit = useRef(fitDevice);
  useEffect(() => {
    if (prevFit.current === fitDevice) return;
    prevFit.current = fitDevice;
    engine.current?.follow(fitDevice);
  }, [fitDevice]);

  const open = ui.state === 'open';
  return (
    <div className="flex min-h-0 flex-1 flex-col bg-background">
      <TerminalToolbar ui={ui} mobile={mobile} fitDevice={fitDevice} zoom={zoom} engine={engine} />
      <div className="relative min-h-0 flex-1 bg-terminal-bg">
        <div
          ref={box}
          data-terminal=""
          className={cn(
            'terminal-xterm-shell absolute inset-0 overscroll-contain',
            mobile ? 'px-1.5 py-1' : 'px-3 py-2',
            mobile && !ui.controlling ? 'overflow-auto' : 'overflow-hidden',
          )}
          onMouseDown={() => engine.current?.focus()}
        >
          {/* The engine marks it data-following before measuring, so the class must not wait for a render. */}
          <div ref={screen} className="h-full w-full data-following:h-auto data-following:w-max" />
        </div>
        <TerminalOverlay ui={ui} onReconnect={() => engine.current?.reconnect()} />
      </div>
      {mobile && <PhoneInput engine={engine} disabled={!open} />}
    </div>
  );
}

function TerminalToolbar({ ui, mobile, fitDevice, zoom, engine }: {
  ui: TerminalUi; mobile: boolean; fitDevice: boolean; zoom: number; engine: React.RefObject<TerminalEngine | undefined>;
}) {
  const setPrefs = usePrefs(s => s.set);
  const open = ui.state === 'open';
  const retrying = ui.state === 'connecting' && ui.attempt > 0;
  const label = {
    connecting: retrying ? t(`Reconnecting (${ui.attempt}/${MAX_ATTEMPTS})…`, `重连中（${ui.attempt}/${MAX_ATTEMPTS}）…`) : t('Connecting…', '连接中…'),
    open: t('Connected', '已连接'),
    paused: t('Paused in the background', '后台已暂停'),
    exited: t('Exited', '已退出'),
    failed: t('Disconnected', '未连接'),
  }[ui.state];
  const dot = {
    connecting: 'bg-warning animate-pulse', open: 'bg-success', paused: 'bg-foreground-subtlest', exited: 'bg-foreground-subtlest', failed: 'bg-destructive',
  }[ui.state];
  const mode = ui.held
    ? t('A local terminal is attached and sets the size', '本地终端已接入，尺寸由其决定')
    : ui.controlling ? t('controlling', '控制中') : t('following', '跟随中');
  const modeTip = ui.held
    ? t('A local terminal (ccr) is attached and keeps its own size', '本地终端（ccr）已接入，尺寸以它为准')
    : ui.controlling ? t('This view sets the terminal size', '终端尺寸由此处决定') : t('Showing the size another front end set', '显示的是其他端设定的尺寸');

  return (
    <div className={cn('flex shrink-0 items-center gap-2 border-b border-border/60 bg-background text-ui-sm', mobile ? 'h-11 pr-1 pl-3' : 'h-9 px-3')}>
      <span className={cn('size-1.5 shrink-0 rounded-full', dot)} aria-hidden />
      <span className="shrink-0 text-foreground-subtle">{label}</span>
      {ui.size && (
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="min-w-0 truncate text-foreground-subtlest tabular-nums">
              <span className="font-mono">{ui.size.cols}×{ui.size.rows}</span> · {mode}
            </span>
          </TooltipTrigger>
          <TooltipContent side="bottom">{modeTip}</TooltipContent>
        </Tooltip>
      )}
      <div className="ml-auto flex shrink-0 items-center gap-0.5">
        {ui.hasSelection && (
          <ToolButton mobile={mobile} label={t('Copy selection', '复制选中内容')} onClick={() => engine.current?.copySelection()}><Copy /></ToolButton>
        )}
        {mobile && !ui.controlling && !ui.held && (
          <>
            <ToolButton mobile label={t('Smaller', '缩小')} disabled={zoom <= 0.5} onClick={() => setPrefs({ terminalZoom: stepZoom(zoom, -1) })}><Minus /></ToolButton>
            <span className="w-9 text-center text-foreground-subtlest tabular-nums">{Math.round(zoom * 100)}%</span>
            <ToolButton mobile label={t('Larger', '放大')} disabled={zoom >= 3} onClick={() => setPrefs({ terminalZoom: stepZoom(zoom, 1) })}><Plus /></ToolButton>
          </>
        )}
        {mobile && !ui.held && (
          <Button
            variant="ghost" size="sm" aria-pressed={fitDevice}
            className={cn('h-9 px-2.5 text-ui-sm text-foreground-subtle', fitDevice && 'bg-selected text-foreground')}
            onClick={() => setPrefs({ terminalFit: !fitDevice })}
          >
            <Smartphone className="size-3.5" />{t('Fit device', '适配设备')}
          </Button>
        )}
        {!mobile && !ui.controlling && !ui.held && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="ghost" size="sm" className="h-6 text-ui-sm text-foreground-subtle" onClick={() => engine.current?.take()}>
                <MonitorSmartphone className="size-3.5" />{t('Use this window size', '使用此窗口尺寸')}
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">{t('Resize the terminal to this window; other front ends follow', '把终端尺寸改成本窗口大小，其他端随之跟随')}</TooltipContent>
          </Tooltip>
        )}
        <ToolButton mobile={mobile} label={t('Redraw the screen', '重绘整屏')} disabled={!open} onClick={() => engine.current?.redraw()}><RotateCw /></ToolButton>
      </div>
    </div>
  );
}

function ToolButton({ mobile = false, label, disabled, onClick, children }: { mobile?: boolean; label: string; disabled?: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="ghost" size={mobile ? 'icon-lg' : 'icon-sm'} aria-label={label} disabled={disabled} className={cn('text-foreground-subtle hover:text-foreground', mobile ? 'size-9 [&_svg]:size-4' : '[&_svg]:size-3.5')} onClick={onClick}>
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent side="bottom">{label}</TooltipContent>
    </Tooltip>
  );
}

function TerminalOverlay({ ui, onReconnect }: { ui: TerminalUi; onReconnect: () => void }) {
  if (ui.state === 'open') return null;
  if (ui.state === 'connecting' && ui.attempt === 0) {
    return (
      <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
        <div className="flex items-center gap-2 text-ui-sm text-foreground-subtle duration-300 animate-in fade-in-0">
          <Spinner className="size-3.5" />{t('Connecting to the terminal…', '正在连接终端…')}
        </div>
      </div>
    );
  }
  const card = {
    connecting: {
      title: t(`Connection lost, reconnecting (${ui.attempt}/${MAX_ATTEMPTS})`, `连接中断，正在重连（${ui.attempt}/${MAX_ATTEMPTS}）`),
      action: undefined,
    },
    paused: { title: t('Paused while the page is in the background', '页面在后台，已暂停连接'), action: undefined },
    exited: { title: t('Claude Code exited', 'Claude Code 已退出'), action: t('Start again', '重新启动') },
    failed: { title: t('Could not connect to the terminal', '无法连接终端'), action: t('Connect again', '重新连接') },
  }[ui.state];
  return (
    <div className="absolute inset-0 flex items-center justify-center bg-terminal-bg/70 p-6 backdrop-blur-[1px]">
      <div role="status" className="flex max-w-sm flex-col items-center gap-3 rounded-xl border border-border/60 bg-card px-5 py-4 text-center shadow-sm">
        <div className="flex items-center gap-2 text-ui-base text-foreground">
          {ui.state === 'connecting' && <Spinner className="size-3.5" />}
          {card.title}
        </div>
        {ui.reason && <p className="text-ui-sm text-foreground-subtle text-wrap-phrase">{ui.reason}</p>}
        {card.action && (
          <Button variant="outline" size="lg" onClick={onReconnect}>
            <RotateCw />{card.action}
          </Button>
        )}
      </div>
    </div>
  );
}

function PhoneInput({ engine, disabled }: { engine: React.RefObject<TerminalEngine | undefined>; disabled: boolean }) {
  const [draft, setDraft] = useState('');
  const composing = useRef(false);
  const press = (data: (app: boolean) => string) => { const e = engine.current; if (e) e.send(data(e.appCursor())); };
  // A message as the web sends one: pasted (newlines stay in it), then Enter on its own.
  const submit = () => {
    const e = engine.current;
    if (!e || disabled) return;
    if (!draft) { e.send('\r'); return; }
    e.send(pastePayload(draft, e.bracketedPaste()));
    window.setTimeout(() => engine.current?.send('\r'), 300);
    setDraft('');
  };
  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key !== 'Enter' || event.shiftKey) return;
    if (event.nativeEvent.isComposing || composing.current || event.keyCode === 229) return;
    event.preventDefault();
    submit();
  };
  return (
    <div className="shrink-0 border-t border-border/60 bg-background pb-safe">
      <div role="toolbar" aria-label={t('Terminal keys', '终端按键')} className="flex gap-1.5 overflow-x-auto px-2 pt-1.5 scrollbar-hide">
        {PHONE_KEYS.map(key => (
          <button
            key={key.label}
            type="button"
            title={key.title}
            disabled={disabled}
            className="h-9 min-w-11 shrink-0 rounded-md border border-border/60 bg-surface px-2.5 font-mono text-ui-sm text-foreground active:bg-surface-hover disabled:opacity-50"
            onPointerDown={event => event.preventDefault() /* keep the keyboard up */}
            onClick={() => press(key.data)}
          >
            {key.label}
          </button>
        ))}
      </div>
      <form className="flex items-end gap-2 px-2 py-1.5" onSubmit={event => { event.preventDefault(); submit(); }}>
        <textarea
          rows={1}
          value={draft}
          enterKeyHint="send"
          autoCapitalize="off"
          autoCorrect="off"
          spellCheck={false}
          aria-label={t('Text for the terminal', '发送到终端的文字')}
          placeholder={t('Type, then send (empty sends Enter)', '输入后发送（空内容即回车）')}
          className="field-sizing-content max-h-32 min-h-9 flex-1 resize-none rounded-lg border border-border/60 bg-input px-3 py-1.5 text-mobile-input-safe text-foreground outline-none placeholder:text-foreground-subtlest focus:border-border"
          onChange={event => setDraft(event.target.value)}
          onCompositionStart={() => { composing.current = true; }}
          onCompositionEnd={() => { composing.current = false; }}
          onKeyDown={onKeyDown}
        />
        <Button type="submit" size="icon-lg" className="size-9 rounded-lg" disabled={disabled} aria-label={t('Send', '发送')} onPointerDown={event => event.preventDefault()}>
          <ArrowUp />
        </Button>
      </form>
    </div>
  );
}
