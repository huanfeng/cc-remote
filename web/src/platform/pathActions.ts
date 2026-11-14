import { invoke, isTauri } from '@tauri-apps/api/core';
import { isAbsoluteLocalPath, isLoopbackHost } from '../domain/localPaths.ts';

export { isAbsoluteLocalPath } from '../domain/localPaths.ts';
export type PathAction = 'open' | 'reveal' | 'terminal';

export function canUseLocalPaths(hostUrl: string): boolean {
  return typeof window !== 'undefined' && isTauri() && isLoopbackHost(hostUrl);
}

export async function openLocalPath(hostUrl: string, path: string, action: PathAction): Promise<void> {
  if (!canUseLocalPaths(hostUrl)) throw new Error('仅桌面客户端连接本机主机时支持系统路径操作');
  if (!isAbsoluteLocalPath(path)) throw new Error('需要本机绝对路径');
  await invoke('open_local_path', { hostUrl, path, action });
}
