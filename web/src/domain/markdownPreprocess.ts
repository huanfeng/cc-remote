// Claude Code writes for a terminal: "★ Insight ───" boxes are delimited by
// bare box-drawing rules that CommonMark would glue into the surrounding
// paragraph. This pass rewrites them into constructs the web renderer
// understands, leaving fenced code untouched.

export const INSIGHT_LANG = 'cc-insight';

const FENCE = /^\s*(`{3,}|~{3,})(.*)$/;
const INSIGHT_OPEN = /^(.*?)`?★\s*Insight\s*─{3,}`?\s*(.*)$/;
const RULE = /^(.*?)\s*`?─{10,}`?\s*(.*)$/;
const BARE_RULE = /^\s*`?─{10,}`?\s*$/;
const GLUED_RULE = /^(.*\S)\s+`?─{10,}`?\s*$/;

type Fence = { char: string; size: number };

function fenceOpen(line: string): Fence | undefined {
  const match = FENCE.exec(line);
  if (!match || (match[1][0] === '`' && match[2].includes('`'))) return undefined;
  return { char: match[1][0], size: match[1].length };
}

function fenceCloses(line: string, fence: Fence): boolean {
  const match = /^\s*(`{3,}|~{3,})\s*$/.exec(line);
  return !!match && match[1][0] === fence.char && match[1].length >= fence.size;
}

function insightBlock(body: string[]): string[] {
  while (body.length && !body[0].trim()) body.shift();
  while (body.length && !body[body.length - 1].trim()) body.pop();
  const longest = Math.max(2, ...body.map(line => /^\s*(`*)/.exec(line)![1].length));
  const fence = '`'.repeat(longest + 1);
  return ['', fence + INSIGHT_LANG, ...body, fence, ''];
}

export function normalizeClaudeMarkdown(text: string): string {
  const lines = text.replace(/\r\n?/g, '\n').split('\n');
  const out: string[] = [];
  let fence: Fence | undefined;
  let insight: string[] | undefined;
  let insightFence: Fence | undefined;

  for (let index = 0; index < lines.length; index++) {
    const line = lines[index];
    if (insight) {
      if (insightFence) {
        if (fenceCloses(line, insightFence)) insightFence = undefined;
        insight.push(line);
        continue;
      }
      const close = RULE.exec(line);
      if (close) {
        if (close[1].trim()) insight.push(close[1]);
        out.push(...insightBlock(insight));
        insight = undefined;
        // Anything after the closing rule is ordinary text again.
        if (close[2].trim()) lines.splice(index + 1, 0, close[2]);
        continue;
      }
      insightFence = fenceOpen(line);
      insight.push(line);
      continue;
    }
    if (fence) {
      if (fenceCloses(line, fence)) fence = undefined;
      out.push(line);
      continue;
    }
    const opened = fenceOpen(line);
    if (opened) {
      fence = opened;
      out.push(line);
      continue;
    }
    const open = INSIGHT_OPEN.exec(line);
    if (open) {
      if (open[1].trim() && !/^\s*[-*+>]\s*$/.test(open[1])) out.push(open[1].trimEnd());
      insight = [];
      if (open[2].trim()) lines.splice(index + 1, 0, open[2]);
      continue;
    }
    if (BARE_RULE.test(line)) {
      out.push('', '***', '');
      continue;
    }
    const glued = GLUED_RULE.exec(line);
    if (glued && !line.trimStart().startsWith('|')) {
      out.push(glued[1], '', '***', '');
      continue;
    }
    out.push(line);
  }
  // A still-streaming insight has no closing rule yet; show it as a card anyway.
  if (insight) out.push(...insightBlock(insight));
  return out.join('\n');
}
