import { useEffect, useState } from 'react';
import type { HLJSApi } from 'highlight.js';

// highlight.js loads on first use, in its own chunk: pages without code never fetch it.
let loading: Promise<HLJSApi> | undefined;
const load = () => loading ??= import('highlight.js/lib/common').then(module => module.default);

// Larger texts stay plain: highlighting them would hold up the page.
const MAX_CHARS = 400_000;

/** Highlighted HTML for code in language lang (escaped by highlight.js), or undefined while loading or for plain text. */
export function useHighlight(code: string, lang: string): string | undefined {
  const [html, setHtml] = useState<{ code: string; lang: string; value: string }>();
  useEffect(() => {
    if (!lang || code.length > MAX_CHARS) return;
    let live = true;
    load().then(hljs => {
      if (!live || !hljs.getLanguage(lang)) return;
      setHtml({ code, lang, value: hljs.highlight(code, { language: lang, ignoreIllegals: true }).value });
    }).catch(() => {});
    return () => { live = false; };
  }, [code, lang]);
  return html && html.code === code && html.lang === lang ? html.value : undefined;
}
