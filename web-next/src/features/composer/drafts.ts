// Per-session composer drafts: text and uploaded attachments survive switching
// sessions and reloading the page, for 7 days. Storage may throw (private
// mode, quota); persist swallows it and drafts stay in memory.
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import type { Attachment } from '@/backend/types';
import { pruneDrafts } from './composerLogic';

export interface Draft {
  text: string;
  attachments: Attachment[];
  at: number;
}

interface DraftStore {
  drafts: Record<string, Draft>;
  setText(id: string, text: string): void;
  setAttachments(id: string, update: (a: Attachment[]) => Attachment[]): void;
  clear(id: string, sent?: { text: string; attachmentIds: string[] }): void;
}

const EMPTY: Draft = { text: '', attachments: [], at: 0 };

export const useDrafts = create<DraftStore>()(
  persist(
    set => ({
      drafts: {},
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
        if (!sent) { delete drafts[id]; return { drafts }; }
        const text = cur.text.trim() === sent.text.trim() ? '' : cur.text;
        const attachments = cur.attachments.filter(a => !sent.attachmentIds.includes(a.id));
        if (!text && !attachments.length) delete drafts[id];
        else drafts[id] = { text, attachments, at: Date.now() };
        return { drafts };
      }),
    }),
    {
      name: 'ccn-drafts',
      version: 1,
      partialize: s => ({ drafts: pruneDrafts(s.drafts, Date.now()) }),
      merge: (persisted, current) => ({
        ...current,
        drafts: pruneDrafts((persisted as { drafts?: Record<string, Draft> } | undefined)?.drafts ?? {}, Date.now()),
      }),
    },
  ),
);

export const useDraft = (id: string): Draft => useDrafts(s => s.drafts[id]) ?? EMPTY;
