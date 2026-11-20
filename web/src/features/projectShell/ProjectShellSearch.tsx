import { useEffect, useRef, useState, type RefObject } from 'react';
import { ArrowDown, ArrowDownToLine, ArrowUp, Pause, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { t } from '@/lib/i18n';
import type { ShellSearchDirection } from './findProjectShellMatch';
import type { createProjectShellTerminal } from './projectShellTerminal';

export function ProjectShellSearch({ terminal, onClose }: {
  terminal: RefObject<ReturnType<typeof createProjectShellTerminal> | null>;
  onClose(): void;
}) {
  const [query, setQuery] = useState('');
  const [noMatch, setNoMatch] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => {
    input.current?.focus();
    return () => clearTimeout(timer.current);
  }, []);

  const find = (value: string, direction: ShellSearchDirection) => {
    clearTimeout(timer.current);
    if (value) setNoMatch(terminal.current?.search(value, direction) === false);
    else {
      terminal.current?.clearSearch();
      setNoMatch(false);
    }
  };
  const close = () => {
    clearTimeout(timer.current);
    terminal.current?.clearSearch();
    onClose();
  };
  const label = t('Search terminal output', '搜索终端输出');
  const previous = t('Previous match', '上一个匹配');
  const next = t('Next match', '下一个匹配');
  const pause = t('Pause following output', '暂停跟随输出');
  const latest = t('Jump to latest output', '回到最新输出');
  const dismiss = t('Close terminal search', '关闭终端搜索');

  return (
    <div role="search" aria-label={label} className="flex shrink-0 flex-wrap items-center gap-1 border-b border-border/70 bg-background-win-alt px-2 py-1"
      onKeyDown={event => {
        if (event.key !== 'Escape' || event.nativeEvent.isComposing) return;
        event.preventDefault();
        event.stopPropagation();
        close();
      }}>
      <Input ref={input} autoComplete="off" spellCheck={false} aria-label={label} placeholder={label} value={query} className="min-w-24 flex-1"
        onChange={event => {
          const value = event.target.value;
          setQuery(value);
          setNoMatch(false);
          clearTimeout(timer.current);
          timer.current = setTimeout(() => find(value, 'previous'), 120);
        }}
        onKeyDown={event => {
          if (event.key !== 'Enter' || event.nativeEvent.isComposing) return;
          event.preventDefault();
          find(query, event.shiftKey ? 'previous' : 'next');
        }} />
      {noMatch && <span className="text-ui-xs text-foreground-subtle" role="status">{t('No matches', '无匹配项')}</span>}
      <div className="flex items-center gap-1">
        <Button type="button" variant="ghost" size="icon-sm" aria-label={previous} title={previous} disabled={!query} onClick={() => find(query, 'previous')}><ArrowUp /></Button>
        <Button type="button" variant="ghost" size="icon-sm" aria-label={next} title={next} disabled={!query} onClick={() => find(query, 'next')}><ArrowDown /></Button>
        <Button type="button" variant="ghost" size="icon-sm" aria-label={pause} title={pause} onClick={() => terminal.current?.pauseFollowing()}><Pause /></Button>
        <Button type="button" variant="ghost" size="icon-sm" aria-label={latest} title={latest} onClick={() => terminal.current?.scrollToBottom()}><ArrowDownToLine /></Button>
        <Button type="button" variant="ghost" size="icon-sm" aria-label={dismiss} title={dismiss} onClick={close}><X /></Button>
      </div>
    </div>
  );
}
