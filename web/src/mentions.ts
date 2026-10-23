export type Token = { start: number; end: number; query: string };

// The @word the caret is in: an @ at the start or after a space, up to the caret.
export function mentionAt(text: string, caret: number): Token | undefined {
  let start = caret;
  while (start > 0 && !/\s/.test(text[start - 1])) start--;
  if (text[start] !== '@') return;
  const query = text.slice(start + 1, caret);
  if (/["'`]/.test(query)) return;
  return { start, end: caret, query };
}
