// @vitest-environment jsdom
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api, type LogRecord, type LogPage } from "./api";
import { useLogs } from "./useLogs";

vi.mock("./api", () => ({ api: { logs: vi.fn() } }));

const record = (message: string): LogRecord => ({
  message,
  timestamp: "2026-11-13T10:00:00Z",
  event: "dictation.completed",
  outcome: "ok",
  provider: "local",
  configured_model: "parakeet-unified-en",
  total_ms: 1000,
  audio_ms: 900,
  attempt_count: 1,
  attempts: [],
});

let records: LogRecord[];
let revision: string;
let lastToken: string;
const onError = vi.fn();
const readPage = async (outcome: string, offset: number): Promise<LogPage> => {
  const token = `${revision}|${outcome}|${offset}`;
  const unchanged = token === lastToken;
  lastToken = token;
  return {
    records: unchanged ? [] : records.slice(offset, offset + 2),
    revision: token,
    outcome,
    offset,
    limit: 2,
    total: unchanged ? 0 : records.length,
    retained: unchanged ? 0 : records.length,
    unchanged,
  };
};

beforeEach(() => {
  vi.useFakeTimers();
  vi.clearAllMocks();
  records = ["newest", "recent", "older", "oldest", "last"].map(record);
  revision = "a";
  lastToken = "";
  vi.mocked(api.logs).mockImplementation(readPage);
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("useLogs", () => {
  it("loads bounded pages on demand, including an unchanged head and the final partial page", async () => {
    const { result } = renderHook(() => useLogs(true, "all", onError));
    await act(async () => {});
    expect(result.current.page?.records).toEqual(records.slice(0, 2));
    await act(async () => result.current.loadMore());
    expect(result.current.page?.records).toEqual(records.slice(0, 4));
    expect(api.logs).toHaveBeenCalledWith("all", 2);
    await act(async () => result.current.loadMore());
    expect(result.current.page?.records).toEqual(records);
    expect(api.logs).toHaveBeenCalledWith("all", 4);
    expect(result.current.loadingMore).toBe(false);
    expect(onError).not.toHaveBeenCalled();
  });

  it("keeps loaded history on unchanged polls without fetching older pages again", async () => {
    const { result } = renderHook(() => useLogs(true, "all", onError));
    await act(async () => {});
    await act(async () => result.current.loadMore());
    const loaded = result.current.page;
    vi.mocked(api.logs).mockClear();
    await act(async () => vi.advanceTimersByTimeAsync(4000));
    expect(result.current.page).toBe(loaded);
    expect(api.logs).toHaveBeenCalledTimes(2);
    expect(api.logs).toHaveBeenNthCalledWith(1, "all", 0);
    expect(api.logs).toHaveBeenNthCalledWith(2, "all", 0);
  });

  it("refreshes the complete loaded prefix when new entries shift page boundaries", async () => {
    const { result } = renderHook(() => useLogs(true, "all", onError));
    await act(async () => {});
    await act(async () => result.current.loadMore());
    records = [record("just arrived"), ...records];
    revision = "b";
    await act(async () => vi.advanceTimersByTimeAsync(2000));
    expect(result.current.page?.records).toEqual(records.slice(0, 4));
    expect(result.current.page?.total).toBe(6);
    await act(async () => result.current.loadMore());
    expect(result.current.page?.records).toEqual(records);
  });

  it("ignores a pending older page after changing filters and starts at the first page", async () => {
    const { result, rerender } = renderHook(
      ({ outcome }) => useLogs(true, outcome, onError),
      { initialProps: { outcome: "all" } },
    );
    await act(async () => {});
    let finish!: (page: LogPage) => void;
    vi.mocked(api.logs).mockImplementation((outcome, offset) =>
      offset
        ? new Promise((resolve) => {
            finish = resolve;
          })
        : readPage(outcome, offset),
    );
    await act(async () => result.current.loadMore());
    expect(result.current.loadingMore).toBe(true);
    await act(async () => rerender({ outcome: "errors" }));
    const filtered = result.current.page;
    expect(filtered?.outcome).toBe("errors");
    expect(filtered?.records).toHaveLength(2);
    await act(async () => finish(await readPage("all", 2)));
    expect(result.current.page).toBe(filtered);
    expect(result.current.loadingMore).toBe(false);
    expect(api.logs).not.toHaveBeenCalledWith("errors", 2);
  });

  it("keeps existing entries after a load failure and retries the same page", async () => {
    const { result } = renderHook(() => useLogs(true, "all", onError));
    await act(async () => {});
    const original = result.current.page;
    vi.mocked(api.logs).mockImplementation((outcome, offset) => {
      if (offset) return Promise.reject(new Error("History unavailable"));
      return readPage(outcome, offset);
    });
    await act(async () => result.current.loadMore());
    expect(result.current.page).toBe(original);
    expect(result.current.loadingMore).toBe(false);
    expect(onError).toHaveBeenCalledWith("History unavailable");
    vi.mocked(api.logs).mockImplementation(readPage);
    await act(async () => result.current.loadMore());
    expect(result.current.page?.records).toEqual(records.slice(0, 4));
    expect(api.logs).not.toHaveBeenCalledWith("all", 4);
  });

  it("resets loaded depth whenever filters change, including returning to All", async () => {
    const { result, rerender } = renderHook(
      ({ outcome }) => useLogs(true, outcome, onError),
      { initialProps: { outcome: "all" } },
    );
    await act(async () => {});
    await act(async () => result.current.loadMore());
    expect(result.current.page?.records).toHaveLength(4);
    await act(async () => rerender({ outcome: "errors" }));
    expect(result.current.page?.records).toHaveLength(2);
    vi.mocked(api.logs).mockClear();
    await act(async () => rerender({ outcome: "all" }));
    expect(result.current.page?.records).toHaveLength(2);
    expect(api.logs).not.toHaveBeenCalledWith("all", 2);
  });

  it("does not combine pages from different history revisions", async () => {
    const { result } = renderHook(() => useLogs(true, "all", onError));
    await act(async () => {});
    const original = result.current.page;
    vi.mocked(api.logs).mockImplementation((outcome, offset) => {
      if (offset && revision === "a") {
        records.unshift(record("arrived during request"));
        revision = "b";
      }
      return readPage(outcome, offset);
    });
    await act(async () => result.current.loadMore());
    expect(result.current.page).toBe(original);
    await act(async () => vi.advanceTimersByTimeAsync(2000));
    expect(result.current.page?.records).toEqual(records.slice(0, 4));
    expect(onError).not.toHaveBeenCalled();
  });

  it("resets loaded depth after clearing and preserves distinct identical records", async () => {
    records = Array.from({ length: 5 }, () => record("identical"));
    const { result } = renderHook(() => useLogs(true, "all", onError));
    await act(async () => {});
    await act(async () => result.current.loadMore());
    expect(result.current.page?.records).toHaveLength(4);
    records = [];
    revision = "cleared";
    await act(async () => result.current.reset());
    expect(result.current.page?.records).toEqual([]);
    records = ["one", "two", "three"].map(record);
    revision = "new";
    await act(async () => vi.advanceTimersByTimeAsync(2000));
    expect(result.current.page?.records).toEqual(records.slice(0, 2));
  });

  it("does not poll while hidden and retains the loaded history when reopened", async () => {
    const { result, rerender } = renderHook(
      ({ enabled }) => useLogs(enabled, "all", onError),
      { initialProps: { enabled: false } },
    );
    expect(api.logs).not.toHaveBeenCalled();
    await act(async () => rerender({ enabled: true }));
    await act(async () => result.current.loadMore());
    const loaded = result.current.page;
    await act(async () => rerender({ enabled: false }));
    vi.mocked(api.logs).mockClear();
    await act(async () => vi.advanceTimersByTimeAsync(4000));
    expect(api.logs).not.toHaveBeenCalled();
    await act(async () => rerender({ enabled: true }));
    expect(result.current.page).toBe(loaded);
  });
});
