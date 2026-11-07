// A host's icon as an image: its glyph on its colour, for the tab icon. The
// glyph is a preset of public/host-icons.json (the logo, "code", by default) or
// a few characters or an emoji drawn as text. internal/server/identity.go draws
// the same SVG for the host's own page before it loads, from the same file.
import presetsJson from '../../public/host-icons.json?raw';

const GLYPHS: Record<string, string> = JSON.parse(presetsJson);
const GLYPH_COLOR = '#d6ebbc';

export const isPresetIcon = (icon: string) => Object.hasOwn(GLYPHS, icon);

/** Font size of a text icon: the more characters, the smaller. Variation selectors, skin
 * tones and what a joiner adds to an emoji take no room of their own. */
export function iconFontSize(text: string): number {
  let n = 0, joined = false;
  for (const ch of text) {
    const c = ch.codePointAt(0)!;
    if (c === 0x200d) joined = true;
    else if (!joined && c !== 0xfe0f && c !== 0xfe0e && (c < 0x1f3fb || c > 0x1f3ff)) n++;
    else joined = false;
  }
  return n <= 1 ? 112 : n === 2 ? 84 : n === 3 ? 64 : 50;
}

// As Go's html.EscapeString, so both draw the same bytes.
const ESCAPES: Record<string, string> = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&#34;', "'": '&#39;' };
const escape = (text: string) => text.replace(/[&<>"']/g, c => ESCAPES[c]);

export function iconSvg(color: string, icon = ''): string {
  const fill = /^#[0-9a-f]{6}$/i.test(color) ? color : '#293c2e';
  const glyph = !icon || isPresetIcon(icon)
    ? `<g fill="none" stroke="${GLYPH_COLOR}" stroke-width="12" stroke-linecap="round" stroke-linejoin="round">${GLYPHS[icon || 'code']}</g>`
    : `<text x="96" y="100" text-anchor="middle" dominant-baseline="central" font-family="system-ui,-apple-system,'Segoe UI',sans-serif" font-weight="700" font-size="${iconFontSize(icon)}" fill="${GLYPH_COLOR}">${escape(icon)}</text>`;
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 192 192"><rect width="192" height="192" rx="44" fill="${fill}"/>${glyph}</svg>`;
}

/** The icon as a data: address: the pages' CSP allows data: images and no remote ones. */
export const iconURL = (color: string, icon = '') => `data:image/svg+xml,${encodeURIComponent(iconSvg(color, icon))}`;
