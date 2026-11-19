import type { ProjectShellShortcut } from '@/lib/prefs';

export function projectShellShortcut(value: ProjectShellShortcut, desktop: boolean) {
  return value === 'default' ? (desktop ? 'alt-f' : 'ctrl-backquote') : value;
}

export function projectShellShortcutLabel(value: ProjectShellShortcut, desktop: boolean): string {
  switch (projectShellShortcut(value, desktop)) {
    case 'alt-f': return 'Alt+F';
    case 'ctrl-backquote': return 'Ctrl+`';
    case 'ctrl-shift-f12': return 'Ctrl+Shift+F12';
    default: return '';
  }
}

export function matchesProjectShellShortcut(event: KeyboardEvent, value: ProjectShellShortcut, desktop: boolean): boolean {
  if (event.defaultPrevented || event.isComposing || event.metaKey) return false;
  switch (projectShellShortcut(value, desktop)) {
    case 'alt-f': return event.altKey && !event.ctrlKey && !event.shiftKey && event.key.toLowerCase() === 'f';
    case 'ctrl-backquote': return event.ctrlKey && !event.altKey && !event.shiftKey && event.code === 'Backquote';
    case 'ctrl-shift-f12': return event.ctrlKey && !event.altKey && event.shiftKey && event.key === 'F12';
    default: return false;
  }
}
