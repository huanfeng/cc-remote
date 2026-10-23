import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { t } from './i18n';
import { mentionAt } from './mentions';

// Typing @ in the message box offers the session's files and directories
// (GET /api/sessions/{id}/mentions, internal/server/mentions.go). ↑ ↓ choose,
// Enter or Tab inserts, Esc closes; a directory inserts its path and keeps
// the menu open for what is in it.

export function useMentions({ sessionId, draft, setDraft, api }: { sessionId: string; draft: string; setDraft: (text: string) => void; api: <T>(path: string) => Promise<T> }) {
  const [caret, setCaret] = useState(0);
  const [paths, setPaths] = useState<string[]>([]);
  const [active, setActive] = useState(0);
  const [closedAt, setClosedAt] = useState(-1); // Esc closes the menu for this @
  const asked = useRef(0);
  const token = sessionId ? mentionAt(draft, Math.min(caret, draft.length)) : undefined;
  const query = token?.query;
  const open = !!token && token.start !== closedAt && paths.length > 0;
  useEffect(() => {
    if (query === undefined) { setPaths([]); return; }
    const ask = ++asked.current;
    const timer = setTimeout(() => {
      api<{ paths: string[] }>(`/api/sessions/${encodeURIComponent(sessionId)}/mentions?q=${encodeURIComponent(query)}`)
        .then(result => { if (ask === asked.current) { setPaths(result.paths || []); setActive(0); } })
        .catch(() => { if (ask === asked.current) setPaths([]); });
    }, 120);
    return () => clearTimeout(timer);
  }, [query, sessionId, api]);
  useEffect(() => { if (!token) setClosedAt(-1); }, [token?.start]);
  useEffect(() => { document.querySelector('.mention-menu .active')?.scrollIntoView({ block: 'nearest' }); }, [active, open]);

  function pick(path: string) {
    if (!token) return;
    const dir = path.endsWith('/');
    const insert = `@${path}${dir ? '' : ' '}`;
    const after = draft.slice(token.end);
    const next = draft.slice(0, token.start) + insert + (!dir && after.startsWith(' ') ? after.slice(1) : after);
    const at = token.start + insert.length;
    setDraft(next);
    setCaret(at);
    requestAnimationFrame(() => {
      const box = document.querySelector<HTMLTextAreaElement>('.composer textarea');
      if (box) { box.focus(); box.setSelectionRange(at, at); }
    });
  }

  // Handles the menu's keys; true when the key was the menu's.
  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (!open || event.nativeEvent.isComposing) return false;
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      const step = event.key === 'ArrowDown' ? 1 : -1;
      setActive(old => (old + step + paths.length) % paths.length);
      return true;
    }
    if (event.key === 'Enter' || event.key === 'Tab') {
      event.preventDefault();
      pick(paths[Math.min(active, paths.length - 1)]);
      return true;
    }
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      setClosedAt(token!.start);
      return true;
    }
    return false;
  }

  const onCaret = (event: { currentTarget: HTMLTextAreaElement }) => setCaret(event.currentTarget.selectionStart);
  const menu = open ? <div className="command-menu mention-menu" role="listbox" aria-label={t('Files in the session', '会话中的文件')}>
    {paths.map((path, index) => {
      const dir = path.endsWith('/');
      const trimmed = dir ? path.slice(0, -1) : path;
      const cut = trimmed.lastIndexOf('/');
      return <button type="button" role="option" aria-selected={index === active} key={path} className={index === active ? 'active' : ''}
        onMouseDown={event => event.preventDefault()} onMouseEnter={() => setActive(index)} onClick={() => pick(path)}>
        <strong>{trimmed.slice(cut + 1)}{dir ? '/' : ''}</strong>{cut > 0 && <small>{trimmed.slice(0, cut + 1)}</small>}
      </button>;
    })}
  </div> : null;
  return { menu, onKeyDown, onCaret };
}
