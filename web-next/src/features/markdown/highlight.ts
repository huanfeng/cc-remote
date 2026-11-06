// Syntax highlighting with shiki, loaded on first use and shared by every code
// block. We highlight code ourselves instead of using streamdown's code plugin:
// ZCode traced React #185 (update loop) to it for restored messages.
// Output carries both themes as CSS variables (--shiki-light / --shiki-dark);
// markdown.css picks one by the page theme.
import { useEffect, useState } from 'react';
import type { HighlighterGeneric } from 'shiki';

type Highlighter = HighlighterGeneric<string, string>;

let highlighter: Promise<Highlighter> | undefined;
const loaded = new Set<string>();
const cache = new Map<string, string>();
const CACHE_MAX = 200;
/** Larger code is shown plain: highlighting it would hold up the page. */
export const HIGHLIGHT_MAX = 100_000;

const ALIASES: Record<string, string> = { sh: 'bash', shell: 'bash', zsh: 'bash', console: 'bash', ps1: 'powershell', yml: 'yaml', golang: 'go', 'c++': 'cpp', py: 'python', ts: 'typescript', js: 'javascript', md: 'markdown', rs: 'rust' };

function getHighlighter(): Promise<Highlighter> {
  highlighter ??= import('shiki').then(m => m.createHighlighter({ themes: ['github-light', 'github-dark'], langs: [] }) as Promise<Highlighter>);
  return highlighter;
}

/** Highlights code to HTML, or resolves undefined for unknown languages. */
export async function highlight(code: string, language: string): Promise<string | undefined> {
  const lang = ALIASES[language.toLowerCase()] || language.toLowerCase();
  if (!lang || lang === 'text' || lang === 'plaintext' || code.length > HIGHLIGHT_MAX) return undefined;
  const key = `${lang}\0${code}`;
  const hit = cache.get(key);
  if (hit !== undefined) return hit;
  const shiki = await import('shiki');
  if (!(lang in shiki.bundledLanguages)) return undefined;
  const h = await getHighlighter();
  if (!loaded.has(lang)) { await h.loadLanguage(lang as never); loaded.add(lang); }
  const html = h.codeToHtml(code, { lang, themes: { light: 'github-light', dark: 'github-dark' }, defaultColor: false });
  if (cache.size >= CACHE_MAX) cache.delete(cache.keys().next().value!);
  cache.set(key, html);
  return html;
}

/** The highlighted HTML of code once ready; undefined meanwhile (render plain text). */
export function useHighlight(code: string, language: string): string | undefined {
  const [state, setState] = useState<{ key: string; html?: string }>({ key: '' });
  const key = `${language}\0${code}`;
  useEffect(() => {
    let live = true;
    highlight(code, language).then(html => { if (live) setState({ key, html }); }, () => { if (live) setState({ key }); });
    return () => { live = false; };
  }, [code, language, key]);
  return state.key === key ? state.html : undefined;
}
