// What every transcript item needs from the conversation around it, passed by
// context so memoized items do not re-render when unrelated props change.
import { createContext, useContext } from 'react';
import type { PendingRequest, Provider, Session, TaskInfo } from '@/backend/types';
import type { Interaction } from '@/features/requests/QuestionCard';

export interface ConversationContextValue {
  tkey: string;
  session: Session;
  native?: Provider;
  mobile: boolean;
  tasks: TaskInfo[];
  /** Pending requests rendered with the call of this seq. */
  requestsByCall: Map<number, PendingRequest>;
  interactions: Map<number, Interaction>;
  streamingSeq?: number;
  /** Assistant messages that open a turn (they carry the provider label). */
  openers: Set<number>;
  /** Opens a host file in the viewer; undefined for read-only history. */
  openFile?: (path: string, base?: string[], line?: number) => void;
}

export const ConversationContext = createContext<ConversationContextValue | null>(null);

export function useConversation(): ConversationContextValue {
  const value = useContext(ConversationContext);
  if (!value) throw new Error('useConversation outside ConversationView');
  return value;
}
