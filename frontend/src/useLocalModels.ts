import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, type LocalModel } from "./api";
import type { LocalModelActions } from "./LocalModelDownload";

const errorText = (error: unknown) =>
  error instanceof Error ? error.message : String(error);

// The app owns this lifecycle; closing an editor never cancels a native job.
export function useLocalModels(active: boolean) {
  const [models, setModels] = useState<LocalModel[]>([]);
  const [pending, setPending] = useState<{
    id: string;
    action: keyof LocalModelActions;
  } | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [loadError, setLoadError] = useState("");
  const mounted = useRef(true);
  const inFlight = useRef(false);
  const requestVersion = useRef(0);
  const refresh = useCallback(async () => {
    const version = ++requestVersion.current;
    const next = await api.localModels();
    if (!mounted.current || version !== requestVersion.current) return;
    setModels((previous) =>
      JSON.stringify(previous) === JSON.stringify(next) ? previous : next,
    );
    setLoadError("");
  }, []);
  useEffect(() => {
    mounted.current = true;
    void refresh().catch((error) => {
      if (mounted.current) setLoadError(errorText(error));
    });
    return () => {
      mounted.current = false;
    };
  }, [refresh]);
  useEffect(() => {
    if (!active) return;
    let alive = true;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        await refresh();
      } catch (error) {
        if (alive) setLoadError(errorText(error));
      }
      if (alive) timer = setTimeout(poll, 700);
    };
    void poll();
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [active, refresh]);
  const run = useCallback(
    async (
      operation: (id: string) => Promise<void>,
      id: string,
      action: keyof LocalModelActions,
    ) => {
      if (inFlight.current) return;
      inFlight.current = true;
      setPending({ id, action });
      setErrors((previous) => ({ ...previous, [id]: "" }));
      try {
        await operation(id);
        await refresh();
      } catch (error) {
        if (mounted.current)
          setErrors((previous) => ({ ...previous, [id]: errorText(error) }));
      } finally {
        inFlight.current = false;
        if (mounted.current) setPending(null);
      }
    },
    [refresh],
  );
  const actions = useMemo<LocalModelActions>(
    () => ({
      onInstall: (id) => void run(api.installLocalModel, id, "onInstall"),
      onResume: (id) => void run(api.resumeLocalModel, id, "onResume"),
      onRetry: (id) => void run(api.retryLocalModel, id, "onRetry"),
      onPause: (id) => void run(api.pauseLocalModel, id, "onPause"),
      onCancel: (id) => void run(api.cancelLocalModel, id, "onCancel"),
      onRemove: (id) => void run(api.removeLocalModel, id, "onRemove"),
    }),
    [run],
  );
  return {
    models,
    busy: pending !== null,
    pendingID: pending?.id ?? null,
    pendingAction: pending?.action ?? null,
    errors,
    loadError,
    actions,
  };
}

export type LocalModelDownloads = ReturnType<typeof useLocalModels>;
