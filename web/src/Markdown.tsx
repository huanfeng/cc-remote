import { memo, useMemo, useState, type ReactNode } from 'react';
import ReactMarkdown, { type Components } from 'react-markdown';
import remarkBreaks from 'remark-breaks';
import remarkGfm from 'remark-gfm';
import { copyText } from './clipboard';
import { INSIGHT_LANG, normalizeClaudeMarkdown } from './markdownPreprocess';
import './markdown.css';

type Hast = { type: string; value?: string; tagName?: string; properties?: { className?: unknown }; children?: Hast[] };

const plugins = [remarkGfm, remarkBreaks];

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

function CodeBlock({ code, lang, children }: { code: string; lang: string; children: ReactNode }) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle');
  const copy = async () => {
    try { await copyText(code.replace(/\n$/, '')); setState('copied'); }
    catch { setState('failed'); }
    window.setTimeout(() => setState('idle'), 1600);
  };
  return <div className="md-code">
    <div className="md-code-bar"><span>{lang}</span><button type="button" onClick={() => void copy()}>{state === 'copied' ? '已复制' : state === 'failed' ? '复制失败' : '复制'}</button></div>
    <pre>{children}</pre>
  </div>;
}

const components: Components = {
  pre({ node, children }) {
    const code = (node as Hast | undefined)?.children?.find(child => child.tagName === 'code');
    const lang = language(code);
    if (lang === INSIGHT_LANG) {
      return <aside className="md-insight"><div className="md-insight-title">★ Insight</div><div className="md-insight-body"><Body source={textOf(code)} /></div></aside>;
    }
    return <CodeBlock code={textOf(code)} lang={lang}>{children}</CodeBlock>;
  },
  table({ node, children, ...props }) {
    return <div className="md-table"><table {...props}>{children}</table></div>;
  },
  a({ node, href, children, ...props }) {
    const external = !!href && !href.startsWith('#');
    return <a {...props} href={href} {...(external ? { target: '_blank', rel: 'noopener noreferrer' } : {})}>{children}</a>;
  },
  img({ node, ...props }) {
    return <img {...props} loading="lazy" />;
  },
};

function Body({ source }: { source: string }) {
  return <ReactMarkdown remarkPlugins={plugins} components={components}>{source}</ReactMarkdown>;
}

// Renders Claude Code output (written for a terminal) as chat-friendly markdown.
function Markdown({ text }: { text: string }) {
  const source = useMemo(() => normalizeClaudeMarkdown(text), [text]);
  return <div className="md"><Body source={source} /></div>;
}

export default memo(Markdown);
