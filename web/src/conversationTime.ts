export type ConversationClock = { id: string; lastConversationAt?: string; conversationTimeEstimated?: boolean; createdAt?: string; updatedAt?: string; status?: string };

const validTime = (iso?: string) => iso && Number.isFinite(Date.parse(iso)) ? Date.parse(iso) : undefined;

export function conversationTime(item: ConversationClock, now = Date.now(), language = 'en') {
  const at = validTime(item.lastConversationAt);
  const zh = language.startsWith('zh');
  if (at === undefined) return { label: validTime(item.createdAt) !== undefined ? (zh ? '尚无对话' : 'No conversation') : (zh ? '时间未知' : 'Time unknown'), title: zh ? '尚无可靠的对话时间记录' : 'No reliable conversation time available', iso: undefined };
  const date = new Date(at), today = new Date(now), yesterday = new Date(now);
  yesterday.setDate(yesterday.getDate() - 1);
  const sameDay = (a: Date, b: Date) => a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
  const clock = date.toLocaleTimeString(zh ? 'zh-CN' : 'en-GB', { hour: '2-digit', minute: '2-digit', hour12: false });
  const elapsed = now - at;
  let label: string;
  if (elapsed >= 0 && elapsed < 60_000) label = zh ? '刚刚' : 'Just now';
  else if (elapsed >= 0 && elapsed < 3600_000) label = zh ? `${Math.floor(elapsed / 60_000)} 分钟前` : `${Math.floor(elapsed / 60_000)} min ago`;
  else if (sameDay(date, today)) label = clock;
  else if (sameDay(date, yesterday)) label = `${zh ? '昨天' : 'Yesterday'} ${clock}`;
  else if (date.getFullYear() === today.getFullYear()) label = `${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')} ${clock}`;
  else label = `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
  const full = date.toLocaleString(zh ? 'zh-CN' : 'en-GB', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', timeZoneName: 'short', hour12: false });
  return { label: `${item.conversationTimeEstimated ? '≈ ' : ''}${label}`, title: `${zh ? '最后对话' : 'Last conversation'}: ${full}${item.conversationTimeEstimated ? (zh ? '（估算）' : ' (estimated)') : ''}`, iso: date.toISOString() };
}

/** Keep active sessions at their turn-start position instead of reordering on every streamed fragment. */
export function conversationOrder<T extends ConversationClock>(items: T[], activeClocks = new Map<string, number>()): T[] {
  const present = new Set(items.map(item => item.id));
  for (const id of activeClocks.keys()) if (!present.has(id)) activeClocks.delete(id);
  const clocks = new Map(items.map(item => {
    let at = validTime(item.lastConversationAt) ?? validTime(item.createdAt) ?? 0;
    if (item.status === 'running' || item.status === 'waiting') {
      if (!activeClocks.has(item.id)) activeClocks.set(item.id, at);
      at = activeClocks.get(item.id)!;
    } else activeClocks.delete(item.id);
    return [item.id, at];
  }));
  return [...items].sort((a, b) => clocks.get(b.id)! - clocks.get(a.id)! || a.id.localeCompare(b.id));
}
