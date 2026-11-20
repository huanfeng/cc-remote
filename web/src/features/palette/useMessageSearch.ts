import { useEffect, useRef, useState } from 'react';
import { useBackend } from '@/backend/backend';
import type { MessageSearchPage, MessageSearchQuery } from '@/backend/types';

interface SearchState extends MessageSearchPage {
  query: MessageSearchQuery;
  loading: boolean;
  error?: string;
}

export function useMessageSearch(query: MessageSearchQuery | undefined) {
  const backend = useBackend();
  const [state, setState] = useState<SearchState>();
  const [attempt, setAttempt] = useState(0);
  const request = useRef<AbortController>(null);
  const current = state?.query === query ? state : undefined;

  useEffect(() => {
    if (!query) return;
    const abort = new AbortController();
    request.current = abort;
    setState({ query, hits: [], loading: true });
    const timer = window.setTimeout(() => {
      void backend.searchMessages(query, abort.signal).then(page => {
        if (!abort.signal.aborted) setState({ ...page, query, loading: false });
      }).catch((error: unknown) => {
        if (!abort.signal.aborted) setState({ query, hits: [], loading: false, error: String(error instanceof Error ? error.message : error) });
      });
    }, 250);
    return () => {
      window.clearTimeout(timer);
      abort.abort();
    };
  }, [backend, query, attempt]);

  async function loadMore() {
    const abort = request.current;
    if (!current?.next || current.loading || !abort || abort.signal.aborted) return;
    setState({ ...current, error: undefined, loading: true });
    try {
      const page = await backend.searchMessages({ ...current.query, before: current.next }, abort.signal);
      if (!abort.signal.aborted) setState({
        ...page,
        query: current.query,
        hits: [...current.hits, ...page.hits],
        partial: current.partial || page.partial,
        loading: false,
      });
    } catch (error) {
      if (!abort.signal.aborted) setState({ ...current, loading: false, error: String(error instanceof Error ? error.message : error) });
    }
  }

  return {
    hits: current?.hits ?? [],
    next: current?.next,
    partial: current?.partial,
    loading: !!query && (current?.loading ?? true),
    error: current?.error,
    retry: () => setAttempt(value => value + 1),
    loadMore,
  };
}
