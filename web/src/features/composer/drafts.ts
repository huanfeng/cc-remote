// Per-session composer drafts: text and uploaded attachments survive switching
// sessions and reloading the page, for 7 days. Storage may throw (private
// mode, quota); persist swallows it and drafts stay in memory.
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { Attachment } from '@/backend/types';
import { pruneDrafts } from './composerLogic';
import { pruneTombstones, type Tombstones } from '@/lib/draftSync';

export interface Draft {
  text: string;
  attachments: Attachment[];
  at: number;
}

interface DraftStore {
  drafts: Record<string, Draft>;
  /** When a draft was removed (sent or released), so other tabs drop it too. */
  removed: Tombstones;
  setText(id: string, text: string): void;
  setAttachments(id: string, update: (a: Attachment[]) => Attachment[]): void;
  clear(id: string, sent?: { text: string; attachmentIds: string[] }): void;
}

const EMPTY: Draft = { text: '', attachments: [], at: 0 };

export const useDrafts = create<DraftStore>()(
  persist(
    set => ({
      drafts: {},
      removed: {},
      setText: (id, text) => set(s => {
        const cur = s.drafts[id] ?? EMPTY;
        return { drafts: { ...s.drafts, [id]: { ...cur, text, at: Date.now() } } };
      }),
      setAttachments: (id, update) => set(s => {
        const cur = s.drafts[id] ?? EMPTY;
        return { drafts: { ...s.drafts, [id]: { ...cur, attachments: update(cur.attachments), at: Date.now() } } };
      }),
      // After a send, only what was sent is removed: text typed meanwhile stays.
      clear: (id, sent) => set(s => {
        const cur = s.drafts[id];
        if (!cur) return s;
        const drafts = { ...s.drafts };
        const gone = () => { delete drafts[id]; return { drafts, removed: { ...s.removed, [id]: Date.now() } }; };
        if (!sent) return gone();
        const text = cur.text.trim() === sent.text.trim() ? '' : cur.text;
        const attachments = cur.attachments.filter(a => !sent.attachmentIds.includes(a.id));
        if (!text && !attachments.length) return gone();
        drafts[id] = { text, attachments, at: Date.now() };
        return { drafts };
      }),
    }),
    {
      name: 'ccn-drafts',
      version: 1,
      partialize: s => {
        const now = Date.now();
        const removed = pruneTombstones(s.removed, now);
        // A draft emptied by hand is a removal too, for the other tabs.
        for (const [id, d] of Object.entries(s.drafts)) if (!d.text && !d.attachments.length && d.at > (removed[id] ?? -1)) removed[id] = d.at;
        return { drafts: pruneDrafts(s.drafts, now), removed };
      },
      merge: (persisted, current) => {
        const p = persisted as { drafts?: Record<string, Draft>; removed?: Tombstones } | undefined;
        return { ...current, drafts: pruneDrafts(p?.drafts ?? {}, Date.now()), removed: pruneTombstones(p?.removed, Date.now()) };
      },
    },
  ),
);

export const useDraft = (id: string): Draft => useDrafts(s => s.drafts[id]) ?? EMPTY;
