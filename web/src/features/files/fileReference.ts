import { useLayoutEffect, type RefObject } from 'react';

export interface FileReference {
  path: string;
  startLine?: number;
  endLine?: number;
  text?: string;
  context?: string;
}

export type FileReferenceTarget = RefObject<((reference: FileReference) => void) | null>;

export function useFileReferenceTarget(target: FileReferenceTarget | undefined, receive: (reference: FileReference) => void) {
  useLayoutEffect(() => {
    if (!target) return;
    target.current = receive;
    return () => { if (target.current === receive) target.current = null; };
  }, [target, receive]);
}

export function appendFileReference(draft: string, { path, startLine, endLine, text, context }: FileReference): string {
  const location = path.replace(/\r/g, '\\r').replace(/\n/g, '\\n') + (startLine ? `:${startLine}${endLine && endLine !== startLine ? `-${endLine}` : ''}` : '');
  const delimiter = '`'.repeat(longestRun(location, /`+/g) + 1);
  const padded = /^[ `]|[ `]$/.test(location) ? ` ${location} ` : location;
  const pointer = `${delimiter}${padded}${delimiter}${context ? ` (${context})` : ''}`;
  const separator = !draft ? '' : draft.endsWith('\n\n') ? '' : draft.endsWith('\n') ? '\n' : '\n\n';
  if (text === undefined) return `${draft}${separator}${pointer}\n`;
  const maxChars = 8000;
  const excerpt = text.length > maxChars ? `${text.slice(0, maxChars)}\n…` : text;
  const backticks = Math.max(3, longestRun(excerpt, /`+/g) + 1);
  const tildes = Math.max(3, longestRun(excerpt, /~+/g) + 1);
  const fence = (backticks === 3 || tildes >= backticks ? '`' : '~').repeat(Math.min(backticks, tildes));
  return `${draft}${separator}${pointer}\n${fence}\n${excerpt}${excerpt.endsWith('\n') ? '' : '\n'}${fence}\n`;
}

function longestRun(text: string, pattern: RegExp): number {
  let longest = 0;
  for (const run of text.matchAll(pattern)) longest = Math.max(longest, run[0].length);
  return longest;
}

export function fileLineReference(path: string, lines: string[], first: number, last: number): FileReference {
  const startLine = Math.min(first, last);
  const endLine = Math.max(first, last);
  const maxLines = 200;
  const snippet = lines.slice(startLine - 1, Math.min(endLine, startLine + maxLines - 1)).join('\n');
  return { path, startLine, endLine, text: endLine - startLine >= maxLines ? `${snippet}\n…` : snippet };
}
