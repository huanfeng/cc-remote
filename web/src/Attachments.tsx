import { useEffect, useRef, useState } from 'react';
import './features.css';

export type Attachment = { id: string; name: string; mime: string; size: number; url: string };

export async function uploadImages(token: string, sessionId: string, files: File[], existingCount = 0): Promise<Attachment[]> {
  if (files.length + existingCount > 8) throw new Error('每条消息最多 8 张截图');
  for (const file of files) {
    if (!['image/png', 'image/jpeg', 'image/gif'].includes(file.type)) throw new Error('请选择 PNG、JPEG 或 GIF 图片');
    if (file.size > 10 * 1024 * 1024) throw new Error('单张图片不能超过 10 MiB');
  }
  const result: Attachment[] = [];
  for (const file of files) {
    const body = new FormData(); body.append('file', file);
    const response = await fetch(`/api/sessions/${encodeURIComponent(sessionId)}/uploads`, { method: 'POST', headers: { Authorization: `Bearer ${token}` }, body });
    if (!response.ok) { const error = await response.json().catch(() => ({})); throw new Error(error.error || '截图上传失败'); }
    result.push(await response.json());
  }
  return result;
}

function ImageAttachment({ item, token, remove }: { item: Attachment; token: string; remove?: () => void }) {
  const [url, setUrl] = useState('');
  const [failed, setFailed] = useState(false);
  const [expanded, setExpanded] = useState(false);
  useEffect(() => {
    const controller = new AbortController(); let objectURL = '';
    // URLs come only from the authenticated host upload API.
    if (!item.url.startsWith('/api/sessions/')) { setFailed(true); return; }
    setFailed(false);
    fetch(item.url, { headers: { Authorization: `Bearer ${token}` }, signal: controller.signal }).then(response => {
      if (!response.ok) throw new Error('图片不可用'); return response.blob();
    }).then(blob => { if (controller.signal.aborted) return; objectURL = URL.createObjectURL(blob); setUrl(objectURL); }).catch(err => { if (err.name !== 'AbortError') setFailed(true); });
    return () => { controller.abort(); if (objectURL) URL.revokeObjectURL(objectURL); };
  }, [item.url, token]);
  return <div className="image-attachment"><button type="button" className="image-preview" onClick={() => setExpanded(true)} disabled={!url} aria-label={`查看图片 ${item.name}`}>{url ? <img src={url} alt={item.name} /> : <span>{failed ? '图片不可用' : '加载图片…'}</span>}<span>{item.name}</span></button>{remove && <button type="button" className="remove-image" aria-label={`移除 ${item.name}`} onClick={remove}>×</button>}{expanded && <div className="image-lightbox" role="dialog" aria-label="截图预览" onClick={() => setExpanded(false)}><button type="button" aria-label="关闭截图预览">×</button><img src={url} alt={item.name} /></div>}</div>;
}

export function AttachmentList({ items, token, remove }: { items: Attachment[]; token: string; remove?: (id: string) => void }) {
  return <div className="attachments">{items.map(item => <ImageAttachment key={item.id} item={item} token={token} remove={remove ? () => remove(item.id) : undefined} />)}</div>;
}

export function AttachButton({ disabled, upload }: { disabled: boolean; upload: (files: File[]) => void }) {
  const input = useRef<HTMLInputElement>(null);
  return <><input ref={input} type="file" accept="image/png,image/jpeg,image/gif" multiple hidden onChange={event => { upload(Array.from(event.target.files || [])); event.target.value = ''; }} /><button type="button" className="attach-button" title="上传截图，也可以粘贴图片" aria-label="添加截图" disabled={disabled} onClick={() => input.current?.click()}>＋ 图片</button></>;
}
