// Markdown for assistant messages, previews and the file viewer, on streamdown
// (as in ZCode).
//
// Streaming vs static: only text that is actually streaming (the tail
// assistant message of a running turn) uses mode="streaming" with
// parseIncompleteMarkdown. Finished text always renders static: remend's
// "complete unclosed markdown" otherwise misreads finished text such as
// `./src/**/*`, and ZCode traced a React #185 update loop to restored
// messages rendered in streaming mode (message.tsx).
//
// Extensions over plain GFM:
// - file paths in text and inline code become links that open the file viewer
//   (domain/filePaths; only inside a <MarkdownFiles> provider, else plain text);
// - "★ Insight" boxes (normalizeClaudeMarkdown → fenced INSIGHT_LANG) render as a card;
// - code blocks get a language label and copy button, highlighted by our own
//   shiki pass (highlight.ts), not streamdown's code plugin;
// - tables scroll horizontally; single newlines are line breaks (Claude writes for a terminal);
// - CJK text ends bare URLs (domain/autolinks).
import { createContext, memo, useContext, useMemo, useState, type ReactNode } from 'react';
import { defaultRehypePlugins, defaultRemarkPlugins, defaultUrlTransform, Streamdown, type Components } from 'streamdown';
import { cjk } from '@streamdown/cjk';
import { CheckIcon, CopyIcon, SparklesIcon } from 'lucide-react';
import { cn } from '@/components/lib/utils';
import { normalizeClaudeMarkdown, INSIGHT_LANG } from '@/domain/markdownPreprocess';
import { absoluteDirs, FILE_SCHEME, headingSlug, parseFileHref, relativeTarget, remarkFilePaths, type FileRef } from '@/domain/filePaths';
import { remarkCjkAutolinks } from '@/domain/autolinks';
import { copyText } from '@/domain/clipboard';
import { t } from '@/lib/i18n';
import { useHighlight } from './highlight';
import { HostImage } from './images';
import './markdown.css';

// ---------- File links ----------

/** Opens a host file named in Markdown; absent where there is no session (plain text then). */
export type OpenFile = (ref: FileRef, bases: string[]) => void;
export const MarkdownFiles = createContext<OpenFile | null>(null);
/** Directories relative paths in the Markdown being rendered may be relative to. */
const Bases = createContext<string[]>([]);

/** An image named by a host path; other sources (http(s), data:) as they are. */
function MarkdownImage({ src, alt }: { src?: unknown; alt?: string }) {
  const open = useContext(MarkdownFiles);
  const bases = useContext(Bases);
  const file = typeof src === 'string' ? relativeTarget(src) : undefined;
  if (file) return <HostImage file={file} alt={alt} bases={bases} onOpen={open ? () => open(file, bases) : undefined} />;
  return typeof src === 'string' ? <img src={src} alt={alt || ''} loading="lazy" className="max-w-full rounded-lg" /> : null;
}

export function FileLink({ file, children, className }: { file: FileRef; children: ReactNode; className?: string }) {
  const open = useContext(MarkdownFiles);
  const bases = useContext(Bases);
  if (!open) return <>{children}</>;
  const label = file.line ? `${file.path}:${file.line}` : file.path;
  return (
    <a
      href="#"
      className={cn('cursor-pointer text-icon-blue underline decoration-icon-blue/30 underline-offset-2 hover:decoration-icon-blue', className)}
      title={t(`Open ${label}`, `打开 ${label}`)}
      onClick={e => { e.preventDefault(); open(file, bases); }}
    >
      {children}
    </a>
  );
}

// ---------- Plugins ----------

type MdNode = { type: string; value?: string; children?: MdNode[] };

/** remark-breaks: a single newline inside a paragraph is a line break. */
function splitBreaks(node: MdNode): void {
  if (!node.children) return;
  const out: MdNode[] = [];
  for (const child of node.children) {
    if (child.type === 'text' && child.value?.includes('\n')) {
      child.value.split('\n').forEach((part, i) => { if (i) out.push({ type: 'break' }); if (part) out.push({ type: 'text', value: part }); });
    } else { splitBreaks(child); out.push(child); }
  }
  node.children = out;
}
const remarkBreaks = () => splitBreaks;

const remarkPlugins = [...Object.values(defaultRemarkPlugins), remarkCjkAutolinks, remarkBreaks, remarkFilePaths];
const plugins = { cjk };

// streamdown's sanitize schema allows only web protocols in href; file links use their own scheme.
const [sanitize, schema] = defaultRehypePlugins.sanitize as unknown as [unknown, { protocols?: Record<string, string[]> }];
const fileSchema = { ...schema, protocols: { ...schema.protocols, href: [...(schema.protocols?.href ?? []), FILE_SCHEME.slice(0, -1)] } };
const rehypePlugins = Object.entries(defaultRehypePlugins).map(([name, plugin]) => name === 'sanitize' ? [sanitize, fileSchema] : plugin) as never;
const urlTransform = (url: string, key: string, node: never) => url.startsWith(FILE_SCHEME) ? url : defaultUrlTransform(url, key, node);

// ---------- Code ----------

function CopyButton({ text, className }: { text: string; className?: string }) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle');
  const copy = async () => {
    try { await copyText(text); setState('copied'); } catch { setState('failed'); }
    window.setTimeout(() => setState('idle'), 1600);
  };
  const label = state === 'copied' ? t('Copied', '已复制') : state === 'failed' ? t('Copy failed', '复制失败') : t('Copy', '复制');
  return (
    <button
      type="button"
      onClick={() => void copy()}
      className={cn('inline-flex h-6 items-center gap-1 rounded-md px-1.5 text-ui-sm text-foreground-subtlest transition-colors hover:bg-surface-hover hover:text-foreground', className)}
      aria-label={label}
    >
      {state === 'copied' ? <CheckIcon className="size-3.5" /> : <CopyIcon className="size-3.5" />}
      <span>{label}</span>
    </button>
  );
}

export function CodeBlock({ code, language }: { code: string; language: string }) {
  const text = code.replace(/\n$/, '');
  const html = useHighlight(text, language);
  return (
    <div className="cc-code group/code my-3 overflow-hidden rounded-lg border border-border/60 bg-surface" data-language={language || undefined}>
      <div className="flex h-8 items-center justify-between border-b border-border/60 pr-1 pl-3">
        <span className="font-mono text-ui-sm text-foreground-subtlest">{language || 'text'}</span>
        <CopyButton text={text} />
      </div>
      {html !== undefined
        ? <div className="overflow-x-auto px-3 py-2.5 font-mono text-[12.5px] leading-[1.6]" dangerouslySetInnerHTML={{ __html: html }} />
        : <pre className="overflow-x-auto px-3 py-2.5 font-mono text-[12.5px] leading-[1.6] text-foreground"><code>{text}</code></pre>}
    </div>
  );
}

function Insight({ body }: { body: string }) {
  return (
    <aside className="my-3 rounded-xl border border-border/60 bg-surface/60 px-3.5 py-3">
      <div className="mb-1.5 flex items-center gap-1.5 text-ui-sm font-medium tracking-wide text-icon-blue">
        <SparklesIcon className="size-3.5" />
        Insight
      </div>
      <MarkdownBody text={body} streaming={false} className="text-ui-base text-foreground-subtle" />
    </aside>
  );
}

const languageOf = (className: unknown) => {
  const m = typeof className === 'string' ? /language-([^\s]+)/.exec(className) : null;
  return m ? m[1] : '';
};
const textOf = (children: ReactNode): string => typeof children === 'string' ? children : Array.isArray(children) ? children.map(textOf).join('') : '';

// ---------- Elements ----------

function scrollToHeading(from: Element, anchor: string) {
  let slug = anchor;
  try { slug = decodeURIComponent(anchor); } catch { /* keep raw */ }
  const scope = from.closest('.cc-markdown') || document;
  const heading = [...scope.querySelectorAll('h1, h2, h3, h4, h5, h6')].find(item => headingSlug(item.textContent || '') === slug.toLowerCase());
  heading?.scrollIntoView({ behavior: 'smooth', block: 'start' });
}

const heading = (Tag: 'h1' | 'h2' | 'h3' | 'h4', cls: string) =>
  ({ children, node: _n, className: _c, ...props }: { children?: ReactNode; node?: unknown; className?: string }) =>
    <Tag {...props} id={headingSlug(textOf(children)) || undefined} className={cn('scroll-mt-4 font-semibold text-foreground', cls)}>{children}</Tag>;

const components: Components = {
  h1: heading('h1', 'mt-5 mb-2 text-ui-xl'),
  h2: heading('h2', 'mt-5 mb-2 text-ui-lg'),
  h3: heading('h3', 'mt-4 mb-1.5 text-ui-base'),
  h4: heading('h4', 'mt-3 mb-1 text-ui-base text-foreground-subtle'),
  h5: heading('h4', 'mt-3 mb-1 text-ui-sm text-foreground-subtle'),
  h6: heading('h4', 'mt-3 mb-1 text-ui-sm text-foreground-subtlest'),
  ul: ({ children, node: _n, className: _c, ...props }) => <ul {...props} className="my-2 list-disc space-y-1 pl-5 marker:text-foreground-subtlest">{children}</ul>,
  ol: ({ children, node: _n, className: _c, ...props }) => <ol {...props} className="my-2 list-decimal space-y-1 pl-5 marker:text-foreground-subtlest">{children}</ol>,
  li: ({ children, node: _n, className, ...props }) => <li {...props} className={cn('pl-0.5 [&>p]:my-0', className)}>{children}</li>,
  hr: () => <hr className="my-4 border-border/60" />,
  blockquote: ({ children, node: _n, className: _c, ...props }) => <blockquote {...props} className="my-3 border-l-2 border-border pl-3 text-foreground-subtle">{children}</blockquote>,
  strong: ({ children }) => <strong className="font-semibold text-foreground">{children}</strong>,
  table: ({ children, node: _n, className: _c, ...props }) => (
    <div className="my-3 overflow-x-auto rounded-lg border border-border/60">
      <table {...props} className="w-full border-collapse text-ui-sm">{children}</table>
    </div>
  ),
  thead: ({ children }) => <thead className="bg-surface">{children}</thead>,
  tr: ({ children }) => <tr className="border-b border-border/60 last:border-b-0">{children}</tr>,
  th: ({ children, node: _n, className: _c, ...props }) => <th {...props} className="px-3 py-1.5 text-left font-medium whitespace-nowrap text-foreground-subtle">{children}</th>,
  td: ({ children, node: _n, className: _c, ...props }) => <td {...props} className="px-3 py-1.5 align-top">{children}</td>,
  a: ({ href, children, node: _n, className: _c, ...props }) => {
    const file = parseFileHref(href) || relativeTarget(href);
    if (file) return <FileLink file={file}>{children}</FileLink>;
    if (href?.startsWith('#')) {
      return <a {...props} href={href} className="text-icon-blue hover:underline" onClick={e => { e.preventDefault(); scrollToHeading(e.currentTarget, href.slice(1)); }}>{children}</a>;
    }
    return <a {...props} href={href} target="_blank" rel="noopener noreferrer" className="text-icon-blue underline-offset-2 hover:underline">{children}</a>;
  },
  img: ({ src, alt }) => <MarkdownImage src={src} alt={alt} />,
  code: ({ children, className, node: _n, ...props }) => {
    if ('data-block' in props) {
      const language = languageOf(className);
      const code = textOf(children);
      return language === INSIGHT_LANG ? <Insight body={code} /> : <CodeBlock code={code} language={language} />;
    }
    return <code className="rounded-[5px] bg-markdown-inline-code px-1 py-px font-mono text-[0.88em] break-words">{children}</code>;
  },
};

// ---------- Entry ----------

function MarkdownBody({ text, streaming, className, dir }: { text: string; streaming: boolean; className?: string; dir?: string }) {
  const outer = useContext(Bases);
  // Relative paths resolve against the document's directory, the absolute directories the text names, then outer ones.
  const bases = useMemo(() => [...new Set([...(dir ? [dir] : []), ...absoluteDirs(text), ...outer])], [text, dir, outer]);
  const source = useMemo(() => normalizeClaudeMarkdown(text), [text]);
  return (
    <Bases.Provider value={bases}>
      <Streamdown
        mode={streaming ? 'streaming' : 'static'}
        parseIncompleteMarkdown={streaming}
        isAnimating={streaming}
        plugins={plugins}
        remarkPlugins={remarkPlugins}
        rehypePlugins={rehypePlugins}
        urlTransform={urlTransform as never}
        components={components}
        controls={false}
        className={cn('cc-markdown space-y-3 text-ui-base leading-relaxed break-words text-foreground', className)}
      >
        {source}
      </Streamdown>
    </Bases.Provider>
  );
}

export const Markdown = memo(function Markdown({ text, streaming = false, className, dir }: {
  text: string; streaming?: boolean; className?: string;
  /** A document's directory (the file viewer): its relative links and images resolve there first. */
  dir?: string;
}) {
  return <MarkdownBody text={text} streaming={streaming} className={className} dir={dir} />;
});
