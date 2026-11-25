// Code with line numbers, highlighted by shiki and virtualized so large files
// scroll smoothly. Tokens carry both themes (--shiki-light / --shiki-dark);
// files.css picks one by the page theme, so a theme switch needs no rework.
// `line`, when given, is marked and scrolled to the middle.
import { useEffect, useMemo, useRef, useState, type CSSProperties } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import type { HighlighterGeneric, ThemedToken } from 'shiki';
import { MessageSquarePlus } from 'lucide-react';
import { cn } from '@/components/lib/utils';
import { Button } from '@/components/ui/button';
import { t } from '@/lib/i18n';
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

type LineRange = { anchor: number; end: number };

function selectionLines(container: HTMLElement, selection: Selection | null): LineRange | undefined {
  if (!selection || selection.isCollapsed) return;
  const lineAt = (node: Node | null) => {
    if (!node || !container.contains(node)) return;
    const element = node instanceof Element ? node : node.parentElement;
    const line = element?.closest<HTMLElement>('[data-file-line]');
    return line ? Number(line.dataset.fileLine) : undefined;
  };
  const anchor = lineAt(selection.anchorNode);
  const end = lineAt(selection.focusNode);
  if (anchor && end) return { anchor, end };
}

export function CodeView({ code, lang, line, className, onReference }: {
  code: string; lang: string; line?: number; className?: string;
  onReference?: (lines: string[], first: number, last: number) => void;
}) {
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
  const [gutterRange, setGutterRange] = useState<LineRange>();
  const [nativeRange, setNativeRange] = useState<LineRange>();
  const canReference = !!onReference;
  useEffect(() => {
    if (!canReference) return;
    const container = scroller.current;
    if (!container) return;
    const update = () => setNativeRange(selectionLines(container, document.getSelection()));
    document.addEventListener('selectionchange', update);
    return () => document.removeEventListener('selectionchange', update);
  }, [canReference]);
  const range = nativeRange ?? gutterRange;
  const first = range ? Math.min(range.anchor, range.end) : undefined;
  const last = range ? Math.max(range.anchor, range.end) : undefined;
  const virtual = useVirtualizer({ count: lines.length, getScrollElement: () => scroller.current, estimateSize: () => ROW, overscan: 30 });
  const target = line && line >= 1 && line <= lines.length ? line : undefined;
  useEffect(() => {
    if (target) virtual.scrollToIndex(target - 1, { align: 'center' });
  }, [target, virtual]);

  const gutter = `${String(lines.length).length + 1}ch`;
  const longest = useMemo(() => lines.reduce((m, l) => Math.max(m, l.length), 0), [lines]);

  return (
    <div className="relative h-full min-h-0">
      <div ref={scroller} className={cn('cc-file-code h-full min-h-0 overflow-auto bg-background font-mono text-[12px] leading-5 text-foreground', className)}>
        <div className="relative py-2" style={{ height: virtual.getTotalSize() + 16, minWidth: `calc(${gutter} + ${longest}ch + 3rem)` }}>
          {virtual.getVirtualItems().map(row => {
            const n = row.index + 1;
            const toks = highlighted?.[row.index];
            const selected = first !== undefined && last !== undefined && n >= first && n <= last;
            const gutterClass = cn('sticky left-0 shrink-0 bg-background pr-4 pl-3 text-right text-foreground-subtlest select-none', n === target && 'bg-transparent text-foreground');
            const gutterStyle = { width: `calc(${gutter} + 1.75rem)` };
            return (
              <div
                key={row.key}
                className={cn('absolute inset-x-0 flex whitespace-pre', n === target && 'bg-warning-surface', selected && 'bg-surface-hover')}
                style={{ top: row.start + 8, height: ROW }}
              >
                {canReference ? (
                  <button type="button" onClick={event => {
                    if (nativeRange) document.getSelection()?.removeAllRanges();
                    setNativeRange(undefined);
                    setGutterRange(previous => ({ anchor: event.shiftKey ? (previous?.anchor ?? n) : n, end: n }));
                  }} aria-pressed={selected} aria-label={t(`Select line ${n}`, `选择第 ${n} 行`)}
                    className={cn(gutterClass, 'hover:text-foreground focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-ring', selected && 'bg-surface-hover text-foreground')}
                    style={gutterStyle}>{n}</button>
                ) : <span className={gutterClass} style={gutterStyle} aria-hidden>{n}</span>}
                <span data-file-line={n} className="pr-6">
                  {toks
                    ? toks.map((tok, i) => <span key={i} data-tok="" style={tok.htmlStyle as CSSProperties}>{tok.content}</span>)
                    : lines[row.index] || ' '}
                </span>
              </div>
            );
          })}
        </div>
      </div>
      {onReference && first !== undefined && last !== undefined && (
        <Button type="button" size="sm" variant="secondary" className="absolute right-3 bottom-3 z-10 shadow-sm"
          onPointerDown={event => { if (nativeRange) event.preventDefault(); }}
          onClick={() => {
            if (nativeRange) document.getSelection()?.removeAllRanges();
            setNativeRange(undefined);
            setGutterRange(undefined);
            onReference(lines, first, last);
          }}>
          <MessageSquarePlus className="size-3.5" aria-hidden />
          {first === last ? t(`Reference line ${first}`, `引用第 ${first} 行`) : t(`Reference lines ${first}–${last}`, `引用第 ${first}–${last} 行`)}
        </Button>
      )}
    </div>
  );
}
