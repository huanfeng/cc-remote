import { t } from './i18n.ts'; // with extension: node tests import this file directly

// The legacy path supports remote HTTP origins without the Clipboard API.
// Some browsers block both paths; the caller must offer manual selection.
export async function copyText(text: string): Promise<void> {
  if (window.isSecureContext && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch { /* Permission denied: try selection-based copying. */ }
  }

  const focused = document.activeElement;
  const selection = window.getSelection();
  const ranges = selection ? Array.from({ length: selection.rangeCount }, (_, i) => selection.getRangeAt(i).cloneRange()) : [];
  const input = document.createElement('textarea');
  input.value = text;
  input.readOnly = true;
  input.style.cssText = 'position:fixed;top:0;left:0;opacity:0;font-size:16px;pointer-events:none';
  document.body.appendChild(input);
  try {
    input.focus({ preventScroll: true });
    input.select();
    input.setSelectionRange(0, text.length);
    if (!document.execCommand('copy')) throw new Error(t('Automatic copy is unavailable', '自动复制不可用'));
  } finally {
    input.remove();
    if (focused instanceof HTMLElement && focused.isConnected) focused.focus({ preventScroll: true });
    if (selection) {
      selection.removeAllRanges();
      for (const range of ranges) selection.addRange(range);
    }
  }
}
