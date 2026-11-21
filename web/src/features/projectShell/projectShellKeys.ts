import type { ProjectShellShortcut } from '../../domain/preferences.ts';

export interface ProjectShellBinding {
  key: string;
  ctrl?: boolean;
  alt?: boolean;
  shift?: boolean;
  meta?: boolean;
}

export type ProjectShellAction = 'newTab' | 'splitColumns' | 'splitRows';
export type ProjectShellBindings = Record<ProjectShellAction, ProjectShellBinding | null>;

export const defaultProjectShellBindings: ProjectShellBindings = {
  newTab: { key: 'Enter', ctrl: true, shift: true },
  splitColumns: { key: 'ArrowRight', ctrl: true, shift: true },
  splitRows: { key: 'ArrowDown', ctrl: true, shift: true },
};

export function projectShellActionForKey(event: KeyboardEvent, bindings: ProjectShellBindings): ProjectShellAction | undefined {
  if (event.defaultPrevented || event.isComposing || event.getModifierState('AltGraph')) return;
  return (Object.keys(bindings) as ProjectShellAction[]).find(action => {
    const binding = bindings[action];
    return binding && event.key.toLowerCase() === binding.key.toLowerCase() &&
      event.ctrlKey === Boolean(binding.ctrl) && event.altKey === Boolean(binding.alt) &&
      event.shiftKey === Boolean(binding.shift) && event.metaKey === Boolean(binding.meta);
  });
}

export function projectShellBindingLabel(binding: ProjectShellBinding | null, aria = false): string | undefined {
  if (!binding) return;
  const arrows: Record<string, string> = { ArrowRight: '→', ArrowLeft: '←', ArrowDown: '↓', ArrowUp: '↑' };
  return [binding.ctrl && (aria ? 'Control' : 'Ctrl'), binding.alt && 'Alt', binding.shift && 'Shift', binding.meta && 'Meta',
    aria ? binding.key : arrows[binding.key] ?? binding.key].filter(Boolean).join('+');
}

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
