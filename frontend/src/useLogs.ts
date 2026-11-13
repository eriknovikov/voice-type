import { useEffect, useRef, useState } from "react";
import { api, type LogPage } from "./api";

const historyRevision = (page: LogPage) => page.revision.replace(/\|\d+$/, "");

export function useLogs(
  enabled: boolean,
  outcome: string,
  onError: (message: string) => void,
) {
  const [page, setPage] = useState<LogPage | null>(null);
  const [loadingMore, setLoadingMore] = useState(false);
  const [request, setRequest] = useState({ outcome, pages: 1, version: 0 });
  const heads = useRef(new Map<string, LogPage>());
  const loaded = useRef<{
    outcome: string;
    pages: number;
    head: LogPage;
  } | null>(null);

  useEffect(() => {
    if (!enabled) return;
    if (request.outcome !== outcome) {
      setRequest((current) => ({
        outcome,
        pages: 1,
        version: current.version + 1,
      }));
      loaded.current = null;
      setPage(null);
      setLoadingMore(false);
      return;
    }
    if (loaded.current?.outcome !== outcome) {
      loaded.current = null;
      setPage(null);
    }
    const requestedPages = request.pages;
    let alive = true;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const first = await api.logs(outcome, 0);
        // Keep the head even if navigation cancelled this render's request.
        // The backend omits records on subsequent unchanged polls.
        if (!first.unchanged) heads.current.set(outcome, first);
        const head = heads.current.get(outcome);
        if (!alive || !head) return;
        if (
          loaded.current?.head.revision === head.revision &&
          loaded.current.pages >= requestedPages
        )
          return;

        const records = [...head.records];
        for (
          let offset = head.limit;
          offset < head.total && offset < requestedPages * head.limit;
          offset += head.limit
        ) {
          const older = await api.logs(outcome, offset);
          if (!alive || historyRevision(older) !== historyRevision(head))
            return;
          records.push(...older.records);
        }
        if (!alive) return;
        loaded.current = { outcome, pages: requestedPages, head };
        setPage({ ...head, records });
      } catch (error) {
        if (alive)
          onError(error instanceof Error ? error.message : String(error));
      } finally {
        if (alive) {
          setLoadingMore(false);
          timer = setTimeout(poll, 2000);
        }
      }
    };
    void poll();
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [enabled, outcome, request, onError]);

  return {
    page,
    loadingMore,
    loadMore: () => {
      setLoadingMore(true);
      setRequest((current) => ({
        outcome,
        pages: (loaded.current?.pages ?? 1) + 1,
        version: current.version + 1,
      }));
    },
    reset: () => {
      loaded.current = null;
      setPage(null);
      setLoadingMore(false);
      setRequest((current) => ({
        outcome,
        pages: 1,
        version: current.version + 1,
      }));
    },
  };
}
