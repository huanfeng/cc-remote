import { memo, useContext, useMemo, useRef, useState, type ReactNode } from 'react';
import ReactMarkdown, { defaultUrlTransform, type Components } from 'react-markdown';
import remarkBreaks from 'remark-breaks';
import remarkGfm from 'remark-gfm';
import { remarkCjkAutolinks } from './autolinks';
import { copyText } from './clipboard';
import { BasesContext, FileImage, FileLink, PathText, useLinkedPaths } from './fileContext';
import { absoluteDirs, FILE_SCHEME, headingSlug, parseFileHref, relativeTarget, remarkFilePaths } from './filePaths';
import { useHighlight } from './highlight';
import { t } from './i18n';
import { INSIGHT_LANG, normalizeClaudeMarkdown } from './markdownPreprocess';
import './markdown.css';

type Hast = { type: string; value?: string; tagName?: string; properties?: { className?: unknown }; children?: Hast[] };

const plugins = [remarkGfm, remarkCjkAutolinks, remarkBreaks, remarkFilePaths];
const urlTransform = (url: string) => url.startsWith(FILE_SCHEME) ? url : defaultUrlTransform(url);

function textOf(node: Hast | undefined): string {
  if (!node) return '';
  if (node.type === 'text') return node.value || '';
  return (node.children || []).map(textOf).join('');
}

function language(code: Hast | undefined): string {
  const classes = code?.properties?.className;
  const name = Array.isArray(classes) ? classes.find(item => typeof item === 'string' && item.startsWith('language-')) : undefined;
  return typeof name === 'string' ? name.slice('language-'.length) : '';
}

// File paths in code open the viewer too: in the highlighted markup, or in the plain text before it.
function CodeBlock({ code, lang }: { code: string; lang: string }) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle');
  const html = useHighlight(code.replace(/\n$/, ''), lang.toLowerCase());
  const highlighted = useRef<HTMLElement>(null);
  useLinkedPaths(highlighted, html);
  const copy = async () => {
    try { await copyText(code.replace(/\n$/, '')); setState('copied'); }
    catch { setState('failed'); }
    window.setTimeout(() => setState('idle'), 1600);
  };
  return <div className="md-code">
    <div className="md-code-bar"><span>{lang}</span><button type="button" onClick={() => void copy()}>{state === 'copied' ? t('Copied', '已复制') : state === 'failed' ? t('Copy failed', '复制失败') : t('Copy', '复制')}</button></div>
    <pre>{html !== undefined ? <code ref={highlighted} className="hljs" dangerouslySetInnerHTML={{ __html: html }} /> : <code><PathText text={code} /></code>}</pre>
  </div>;
}

function scrollToHeading(from: Element, anchor: string) {
  let slug = anchor;
  try { slug = decodeURIComponent(anchor); } catch {}
  const scope = from.closest('.md') || document;
  const heading = [...scope.querySelectorAll('h1, h2, h3, h4, h5, h6')].find(item => headingSlug(item.textContent || '') === slug.toLowerCase());
  heading?.scrollIntoView({ behavior: 'smooth', block: 'start' });
}

// Relative links and images name files next to the document (or the message's directories).
function Link({ href, children, ...props }: { href?: string; children?: ReactNode }) {
  const file = parseFileHref(href) || relativeTarget(href);
  if (file) return <FileLink file={file}>{children}</FileLink>;
  // The page's own hash is its route: "#install" scrolls to the heading instead.
  if (href?.startsWith('#')) return <a {...props} href={href} onClick={event => { event.preventDefault(); scrollToHeading(event.currentTarget, href.slice(1)); }}>{children}</a>;
  const external = !!href;
  return <a {...props} href={href} {...(external ? { target: '_blank', rel: 'noopener noreferrer' } : {})}>{children}</a>;
}

const components: Components = {
  pre({ node }) {
    const code = (node as Hast | undefined)?.children?.find(child => child.tagName === 'code');
    const lang = language(code);
    if (lang === INSIGHT_LANG) {
      return <aside className="md-insight"><div className="md-insight-title">★ Insight</div><div className="md-insight-body"><Body source={textOf(code)} /></div></aside>;
    }
    return <CodeBlock code={textOf(code)} lang={lang} />;
  },
  table({ node, children, ...props }) {
    return <div className="md-table"><table {...props}>{children}</table></div>;
  },
  a({ node, ...props }) {
    return <Link {...props} />;
  },
  img({ node, src, alt, ...props }) {
    const file = typeof src === 'string' ? relativeTarget(src) : undefined;
    if (file) return <FileImage file={file} alt={alt} />;
    return <img {...props} src={src} alt={alt} loading="lazy" />;
  },
};

function Body({ source }: { source: string }) {
  return <ReactMarkdown remarkPlugins={plugins} components={components} urlTransform={urlTransform}>{source}</ReactMarkdown>;
}

// Renders Claude Code output (written for a terminal) as chat-friendly markdown.
// Relative paths in it resolve against dir (a document's directory) and the
// absolute directories the text names, then the session's working directory.
function Markdown({ text, dir }: { text: string; dir?: string }) {
  const source = useMemo(() => normalizeClaudeMarkdown(text), [text]);
  const outer = useContext(BasesContext);
  const bases = useMemo(() => [...new Set([...(dir ? [dir] : []), ...absoluteDirs(text), ...outer])], [text, dir, outer]);
  return <BasesContext.Provider value={bases}><div className="md"><Body source={source} /></div></BasesContext.Provider>;
}

export default memo(Markdown);
