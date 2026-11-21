import { useRef, type KeyboardEvent } from 'react';
import { isSendKey } from '@/features/composer/composerLogic';
import { usePrefs } from '@/lib/prefs';

/** Attach to a reply input or a group containing the choice buttons. */
export function useRequestSubmit(submit?: () => void) {
  const sendKey = usePrefs(s => s.sendKey);
  const composing = useRef(false);

  function onKeyDown(event: KeyboardEvent<HTMLElement>) {
    if (!submit || event.defaultPrevented || !isSendKey(event.nativeEvent, sendKey, composing.current)) return;
    // An unselected button still needs Enter for selection. Space toggles a choice.
    if (event.target instanceof HTMLButtonElement && event.target.getAttribute('aria-checked') !== 'true') return;
    event.preventDefault();
    if (!event.repeat) submit();
  }

  return {
    keyShortcut: sendKey === 'enter' ? 'Enter' : 'Control+Enter Meta+Enter',
    handlers: {
      onKeyDown,
      onCompositionStart: () => { composing.current = true; },
      onCompositionEnd: () => { composing.current = false; },
      onBlur: () => { composing.current = false; },
    },
  };
}
