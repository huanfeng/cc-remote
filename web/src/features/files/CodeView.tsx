// Code with line numbers, highlighted by shiki and virtualized so large files
// scroll smoothly. Tokens carry both themes (--shiki-light / --shiki-dark);
// files.css picks one by the page theme, so a theme switch needs no rework.
// `line`, when given, is marked and scrolled to the middle.
import { useEffect, useMemo, useRef, useState, type CSSProperties } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import type { HighlighterGeneric, ThemedToken } from 'shiki';
import { cn } from '@/components/lib/utils';
import { MAX_HIGHLIGHT_BYTES } from './fileView';
import './files.css';

const ROW = 20; // px, matches leading-5

type Highlighter = HighlighterGeneric<string, string>;
let highlighter: Promise<Highlighter> | undefined;
const loadedLangs = new Set<string>();

async function tokenize(code: string, lang: string): Promise<ThemedToken[][] | undefined> {
  if (!lang || code.length > MAX_HIGHLIGHT_BYTES) return undefined;
  const shiki = await import('shiki');
  const id = lang in shiki.bundledLanguages ? lang : shiki.bundledLanguagesAlias[lang as keyof typeof shiki.bundledLanguagesAlias] ? lang : '';
  if (!id) return undefined;
  highlighter ??= shiki.createHighlighter({ themes: ['github-light', 'github-dark'], langs: [] }) as Promise<Highlighter>;
  const h = await highlighter;
  if (!loadedLangs.has(id)) { await h.loadLanguage(id as never); loadedLangs.add(id); }
  return h.codeToTokens(code, { lang: id, themes: { light: 'github-light', dark: 'github-dark' }, defaultColor: false }).tokens;
}

export function CodeView({ code, lang, line, className }: { code: string; lang: string; line?: number; className?: string }) {
  const text = useMemo(() => code.replace(/\r\n/g, '\n').replace(/\n$/, ''), [code]);
  const lines = useMemo(() => text.split('\n'), [text]);
  const [tokens, setTokens] = useState<{ key: string; lines?: ThemedToken[][] }>({ key: '' });
  const key = `${lang}\0${text.length}\0${text.slice(0, 64)}`;
  useEffect(() => {
    let live = true;
    tokenize(text, lang).then(t => { if (live) setTokens({ key, lines: t }); }, () => { if (live) setTokens({ key }); });
    return () => { live = false; };
  }, [text, lang, key]);
  const highlighted = tokens.key === key ? tokens.lines : undefined;

  const scroller = useRef<HTMLDivElement>(null);
  const virtual = useVirtualizer({ count: lines.length, getScrollElement: () => scroller.current, estimateSize: () => ROW, overscan: 30 });
  const target = line && line >= 1 && line <= lines.length ? line : undefined;
  useEffect(() => {
    if (target) virtual.scrollToIndex(target - 1, { align: 'center' });
  }, [target, virtual]);

  const gutter = `${String(lines.length).length + 1}ch`;
  const longest = useMemo(() => lines.reduce((m, l) => Math.max(m, l.length), 0), [lines]);

  return (
    <div ref={scroller} className={cn('cc-file-code h-full min-h-0 overflow-auto bg-background font-mono text-[12px] leading-5 text-foreground', className)}>
      <div className="relative py-2" style={{ height: virtual.getTotalSize() + 16, minWidth: `calc(${gutter} + ${longest}ch + 3rem)` }}>
        {virtual.getVirtualItems().map(row => {
          const n = row.index + 1;
          const toks = highlighted?.[row.index];
          return (
            <div
              key={row.key}
              className={cn('absolute inset-x-0 flex whitespace-pre', n === target && 'bg-warning-surface')}
              style={{ top: row.start + 8, height: ROW }}
            >
              <span
                className={cn('sticky left-0 shrink-0 bg-background pr-4 pl-3 text-right text-foreground-subtlest select-none', n === target && 'bg-transparent text-foreground')}
                style={{ width: `calc(${gutter} + 1.75rem)` }}
                aria-hidden
              >
                {n}
              </span>
              <span className="pr-6">
                {toks
                  ? toks.map((tok, i) => <span key={i} data-tok="" style={tok.htmlStyle as CSSProperties}>{tok.content}</span>)
                  : lines[row.index] || ' '}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
