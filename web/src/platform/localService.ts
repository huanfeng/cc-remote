// The cc-remote service on this computer, for the desktop app: its state, start
// / stop / restart, and adding it as a host with the token read from its data
// directory (desktop/src-tauri/src/service.rs runs the installed CLI).
import { invoke } from '@tauri-apps/api/core';
import { lang } from '@/lib/i18n';

export type LocalService =
  | { available: false }
  | {
    available: true;
    installed: boolean;
    enabled: boolean;
    running: boolean;
    /** The address the running host answers on, e.g. http://127.0.0.1:8787. */
    url?: string;
    exe: string;
    manager?: string;
    config?: { version?: string; dataDir?: string; listen?: string[] } | null;
    status?: { version?: string } | null;
    statusError?: string;
  };

export type ServiceAction = 'start' | 'stop' | 'restart';

export const localServiceStatus = () => invoke<LocalService>('local_service_status', { lang });
export const localServiceAction = (action: ServiceAction) => invoke<string>('local_service_action', { action, lang });
export const localServiceToken = () => invoke<string>('local_service_token', { lang });

/** The command that installs the service, for a computer without it. */
export const INSTALL_COMMAND = navigator.userAgent.includes('Windows')
  ? 'irm https://github.com/huanfeng/cc-remote/releases/latest/download/install.ps1 | iex'
  : 'curl -fsSL https://github.com/huanfeng/cc-remote/releases/latest/download/install.sh | sh';
