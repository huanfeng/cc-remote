// Messages are written inline as pairs, t('English', '中文'), like the host's
// i18n package. The language is fixed for the page's lifetime: the host's
// (served in <html lang>) unless this browser chose one; changing it reloads.
// Outside a browser (node tests) there is no document and the language is English.
export type Lang = 'en' | 'zh';

const KEY = 'cc-lang';
const browser = typeof document !== 'undefined';

function parse(value: string | null | undefined): Lang | '' {
  const v = (value || '').toLowerCase();
  return v.startsWith('zh') ? 'zh' : v.startsWith('en') ? 'en' : '';
}

// The standalone page has no host to follow before one is added: it follows the browser.
const standalone = import.meta.env?.MODE === 'standalone';

/** The host's language, as served in index.html; on the standalone page, the browser's. */
export const hostLang: Lang = (browser && (standalone ? parse(navigator.languages?.find(x => parse(x)) || navigator.language) : parse(document.documentElement.lang))) || 'en';

/** This browser's choice, or '' to follow the host. */
export function langOverride(): Lang | '' {
  try { return parse(localStorage.getItem(KEY)); } catch { return ''; }
}

export const lang: Lang = (browser && langOverride()) || hostLang;
if (browser) document.documentElement.lang = lang === 'zh' ? 'zh-CN' : 'en';

/** Locale for number and date formatting in the page's language. */
export const locale = lang === 'zh' ? 'zh-CN' : 'en';

/** Picks the text of a message in the page's language. */
export function t(en: string, zh: string): string { return lang === 'zh' ? zh : en; }

/** Saves this browser's language ('' follows the host) and reloads the page. */
export function setLang(value: Lang | '') {
  try { if (value) localStorage.setItem(KEY, value); else localStorage.removeItem(KEY); } catch {}
  location.reload();
}
