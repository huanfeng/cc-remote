import { lang, t } from './i18n';
import { useEffect, useRef, useState } from 'react';
import './features.css';
import { CloseIcon, PlusIcon } from './icons';
import { apiUrl } from './hosts';

export type Attachment = { id: string; kind?: string; name: string; mime: string; size: number; url: string };

export const MAX_ATTACHMENTS = 8;
const MAX_FILE_BYTES = 20 * 1024 * 1024;
const IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/gif'];
// Older image uploads carry no kind; their MIME identifies them.
const isImage = (item: Attachment) => item.kind ? item.kind === 'image' : IMAGE_TYPES.includes(item.mime);
export const fileSize = (n: number) => n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MiB` : n >= 1024 ? `${(n / 1024).toFixed(1)} KiB` : `${n} B`;

export async function uploadFiles(token: string, sessionId: string, files: File[], existingCount = 0): Promise<Attachment[]> {
  if (files.length + existingCount > MAX_ATTACHMENTS) throw new Error(t(`At most ${MAX_ATTACHMENTS} attachments per message (images and files combined)`, `每条消息最多 ${MAX_ATTACHMENTS} 个附件（图片和文件合计）`));
  for (const file of files) if (file.size > MAX_FILE_BYTES) throw new Error(t(`"${file.name}" is larger than 20 MiB`, `「${file.name}」超过 20 MiB`));
  const result: Attachment[] = [];
  for (const file of files) {
    const body = new FormData(); body.append('file', file);
    const response = await fetch(apiUrl(`/api/sessions/${encodeURIComponent(sessionId)}/uploads`), { method: 'POST', headers: { Authorization: `Bearer ${token}`, 'X-CC-Lang': lang }, body });
    if (!response.ok) { const error = await response.json().catch(() => ({})); throw new Error(error.error || t(`Failed to upload "${file.name}"`, `「${file.name}」上传失败`)); }
    result.push(await response.json());
  }
  return result;
}

// URLs come only from the authenticated host upload API.
function fetchUpload(item: Attachment, token: string, signal?: AbortSignal) {
  if (!item.url.startsWith('/api/sessions/')) return Promise.reject(new Error(t('Invalid attachment URL', '附件地址无效')));
  return fetch(apiUrl(item.url), { headers: { Authorization: `Bearer ${token}` }, signal }).then(response => { if (!response.ok) throw new Error(t('Attachment unavailable', '附件不可用')); return response.blob(); });
}

function ImageAttachment({ item, token, remove }: { item: Attachment; token: string; remove?: () => void }) {
  const [url, setUrl] = useState('');
  const [failed, setFailed] = useState(false);
  const [expanded, setExpanded] = useState(false);
  useEffect(() => {
    const controller = new AbortController(); let objectURL = '';
    setFailed(false);
    fetchUpload(item, token, controller.signal).then(blob => { if (controller.signal.aborted) return; objectURL = URL.createObjectURL(blob); setUrl(objectURL); }).catch(err => { if (err.name !== 'AbortError') setFailed(true); });
    return () => { controller.abort(); if (objectURL) URL.revokeObjectURL(objectURL); };
  }, [item.url, token]);
  return <div className="image-attachment"><button type="button" className="image-preview" onClick={() => setExpanded(true)} disabled={!url} aria-label={t(`View image ${item.name}`, `查看图片 ${item.name}`)}>{url ? <img src={url} alt={item.name} /> : <span>{failed ? t('Image unavailable', '图片不可用') : t('Loading image…', '加载图片…')}</span>}<span>{item.name}</span></button>{remove && <button type="button" className="remove-image" aria-label={t(`Remove ${item.name}`, `移除 ${item.name}`)} onClick={remove}><CloseIcon size={12} /></button>}{expanded && <div className="image-lightbox" role="dialog" aria-label={t('Image preview', '截图预览')} onClick={() => setExpanded(false)}><button type="button" className="icon-button" aria-label={t('Close image preview', '关闭截图预览')}><CloseIcon size={26} /></button><img src={url} alt={item.name} /></div>}</div>;
}

function FileAttachment({ item, token, remove }: { item: Attachment; token: string; remove?: () => void }) {
  const [state, setState] = useState<'idle' | 'loading' | 'failed'>('idle');
  async function download() {
    setState('loading');
    try {
      const url = URL.createObjectURL(await fetchUpload(item, token));
      const link = document.createElement('a'); link.href = url; link.download = item.name; document.body.append(link); link.click(); link.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 10_000); setState('idle');
    } catch { setState('failed'); }
  }
  const ext = item.name.includes('.') ? item.name.split('.').pop()!.slice(0, 4).toUpperCase() : 'FILE';
  return <div className={`file-attachment${state === 'failed' ? ' failed' : ''}`}>
    <button type="button" className="file-chip" title={remove ? item.name : t(`Download ${item.name}`, `下载 ${item.name}`)} aria-label={t(`Download ${item.name}`, `下载 ${item.name}`)} disabled={state === 'loading'} onClick={() => void download()}>
      <span className="file-icon" aria-hidden="true">{ext}</span>
      <span className="file-text"><strong>{item.name}</strong><small>{state === 'loading' ? t('Downloading…', '下载中…') : state === 'failed' ? t('Download failed, click to retry', '下载失败，点击重试') : `${fileSize(item.size)}${remove ? '' : t(' · Download', ' · 下载')}`}</small></span>
    </button>
    {remove && <button type="button" className="remove-file" aria-label={t(`Remove ${item.name}`, `移除 ${item.name}`)} onClick={remove}><CloseIcon size={12} /></button>}
  </div>;
}

export function AttachmentList({ items, token, remove }: { items: Attachment[]; token: string; remove?: (id: string) => void }) {
  return <div className="attachments">{items.map(item => {
    const Component = isImage(item) ? ImageAttachment : FileAttachment;
    return <Component key={item.id} item={item} token={token} remove={remove ? () => remove(item.id) : undefined} />;
  })}</div>;
}

export function AttachButton({ disabled, upload }: { disabled: boolean; upload: (files: File[]) => void }) {
  const input = useRef<HTMLInputElement>(null);
  return <><input ref={input} type="file" multiple hidden onChange={event => { upload(Array.from(event.target.files || [])); event.target.value = ''; }} /><button type="button" className="attach-button" data-tip={t('Add images or files (≤ 20 MiB each, up to 8 per message); you can also paste or drop them into the message box', '添加图片或文件（单个 ≤ 20 MiB，每条最多 8 个），也可以粘贴或拖放到输入框')} aria-label={t('Add attachments', '添加附件')} disabled={disabled} onClick={() => input.current?.click()}><PlusIcon size={17} /></button></>;
}
