// GFM turns a bare URL into a link up to the next space, but Chinese text has
// no spaces: "（http://a.com/?k=1），用同一份…" would link the closing bracket
// and the rest of the sentence. CJK characters and full-width punctuation end
// a bare URL here; a URL that really holds them can be written as <…> or [..](…).

const CJK = /[⺀-⿿　-〿぀-鿿가-힯豈-﫿︰-﹏＀-￯]/;
// GFM drops these from the end of a bare URL; cutting can leave them there again.
const TRAILING = /[?!.,:*_~'"]+$/;

type MdNode = { type: string; value?: string; url?: string; title?: null; children?: MdNode[] };

// The scheme GFM adds to a bare "www." or e-mail link.
function added(node: MdNode, text: string): string | undefined {
  for (const prefix of ['', 'http://', 'mailto:']) if (node.url === prefix + text) return prefix;
  return undefined;
}

function cutAutolinks(node: MdNode): void {
  if (!node.children) return;
  const children: MdNode[] = [];
  for (const child of node.children) {
    const text = child.type === 'link' && child.children?.length === 1 && child.children[0].type === 'text' ? child.children[0].value || '' : '';
    const prefix = text ? added(child, text) : undefined;
    const at = prefix === undefined ? -1 : text.search(CJK);
    if (at > 0) {
      const kept = text.slice(0, at).replace(TRAILING, '');
      const rest = text.slice(kept.length);
      if (kept) children.push({ ...child, url: prefix + kept, children: [{ type: 'text', value: kept }] });
      children.push({ type: 'text', value: kept ? rest : text });
      continue;
    }
    cutAutolinks(child);
    children.push(child);
  }
  // Text the cut left next to other text joins it, so later passes (file paths) see it whole.
  node.children = children.reduce<MdNode[]>((out, child) => {
    const last = out[out.length - 1];
    if (child.type === 'text' && last?.type === 'text') last.value = (last.value || '') + (child.value || '');
    else out.push(child);
    return out;
  }, []);
}

/** remark plugin: bare URLs end where CJK text begins. Runs after remark-gfm. */
export const remarkCjkAutolinks = () => cutAutolinks;
