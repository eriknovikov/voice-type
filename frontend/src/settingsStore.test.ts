import { afterEach, describe, expect, it, vi } from "vitest";
import { SettingsStore } from "./settingsStore";
import type { Settings } from "./api";
const initial: Settings = {
  revision: "1",
  profiles: [],
  active_profile: "a",
  microphone: "",
  vocabulary: [{ name: "general", terms: [] }],
  workspace: "",
  api_keys: {},
  credential_storage: "keyring",
};
const deferred = <T>() => {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
};
afterEach(() => vi.useRealTimers());
describe("serialized settings", () => {
  it("keeps key updates when an older settings save completes", async () => {
    const pending = deferred<Settings>();
    const store = new SettingsStore(async () => initial, () => pending.promise);
    await store.load();
    store.patch({ microphone: "new-device" });
    await Promise.resolve();
    const update = store.mutateAPIKeys(async () => ({
      ...initial,
      api_keys: { openai: { saved: true } },
    }));
    pending.resolve({ ...initial, revision: "2", microphone: "new-device" });
    await update;
    expect(store.getSnapshot().settings?.api_keys.openai.saved).toBe(true);
    expect(store.getSnapshot().settings?.microphone).toBe("new-device");
    store.dispose();
  });
  it("serializes key changes so each response sees the previous change", async () => {
    const pending = deferred<Settings>();
    const store = new SettingsStore(async () => initial, vi.fn());
    await store.load();
    const first = store.mutateAPIKeys(() => pending.promise);
    const secondOperation = vi.fn(async () => ({
      ...initial,
      api_keys: { openai: { saved: true }, openrouter: { saved: true } },
    }));
    const second = store.mutateAPIKeys(secondOperation);
    await Promise.resolve();
    expect(secondOperation).not.toHaveBeenCalled();
    pending.resolve({ ...initial, api_keys: { openai: { saved: true } } });
    await Promise.all([first, second]);
    expect(store.getSnapshot().settings?.api_keys).toEqual({
      openai: { saved: true },
      openrouter: { saved: true },
    });
    store.dispose();
  });
  it("refreshes a stale profile revision without losing drafts or repeating the action", async () => {
    let latest = initial;
    const store = new SettingsStore(async () => latest, vi.fn());
    await store.load();
    store.patch({ vocabulary: [{ name: "general", terms: ["Draft"] }] }, true);
    latest = { ...initial, revision: "external", microphone: "saved-device" };
    const operation = vi
      .fn()
      .mockRejectedValueOnce(
        new Error("settings changed elsewhere; reload before saving"),
      )
      .mockImplementationOnce(async (revision) => ({
        ...latest,
        revision: revision + "-selected",
        active_profile: "b",
      }));
    await expect(store.mutate(operation)).rejects.toThrow("Try again");
    expect(operation).toHaveBeenCalledTimes(1);
    expect(store.getSnapshot().settings?.vocabulary[0].terms).toEqual([
      "Draft",
    ]);
    await store.mutate(operation);
    expect(operation.mock.calls[1][0]).toBe("external");
    expect(store.getSnapshot().settings?.active_profile).toBe("b");
    store.dispose();
  });
  it("keeps newer typing when an older response completes and uses the new revision", async () => {
    const first = deferred<Settings>();
    const save = vi
      .fn()
      .mockReturnValueOnce(first.promise)
      .mockImplementationOnce(async (update) => ({
        ...initial,
        ...update,
        revision: "3",
      }));
    const store = new SettingsStore(async () => initial, save);
    await store.load();
    store.patch({ vocabulary: [{ name: "general", terms: ["Old"] }] });
    await Promise.resolve();
    store.patch(
      { vocabulary: [{ name: "general", terms: ["New", "typing"] }] },
      true,
    );
    first.resolve({
      ...initial,
      revision: "2",
      vocabulary: [{ name: "general", terms: ["Old"] }],
    });
    await store.flush();
    expect(save.mock.calls[1][0]).toEqual({
      revision: "2",
      vocabulary: [
        { name: "general", original_name: "general", terms: ["New", "typing"] },
      ],
    });
    expect(store.getSnapshot().settings?.vocabulary[0].terms).toEqual([
      "New",
      "typing",
    ]);
    store.dispose();
  });
  it("profile selection preserves debounced vocabulary and saves using its revision", async () => {
    const save = vi.fn(async (update) => ({
      ...initial,
      ...update,
      revision: "3",
      active_profile: "b",
    }));
    const store = new SettingsStore(async () => initial, save);
    await store.load();
    store.patch({ vocabulary: [{ name: "general", terms: ["Draft"] }] }, true);
    await store.mutate(async (revision) => {
      expect(revision).toBe("1");
      return { ...initial, revision: "2", active_profile: "b" };
    });
    expect(store.getSnapshot().settings?.vocabulary[0].terms).toEqual([
      "Draft",
    ]);
    await store.flush();
    expect(save.mock.calls[0][0].revision).toBe("2");
    expect(store.getSnapshot().settings?.active_profile).toBe("b");
    store.dispose();
  });
  it("debounces rapid input and keeps failed edits when retrying", async () => {
    vi.useFakeTimers();
    let saved = initial;
    const save = vi
      .fn()
      .mockRejectedValueOnce(new Error("read only"))
      .mockImplementationOnce(async (update) => ({
        ...saved,
        ...update,
        revision: "3",
      }));
    const store = new SettingsStore(async () => saved, save);
    await store.load();
    store.patch({ microphone: "a" }, true);
    store.patch({ microphone: "b" }, true);
    await vi.advanceTimersByTimeAsync(499);
    expect(save).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(save).toHaveBeenCalledTimes(1);
    expect(store.getSnapshot().settings?.microphone).toBe("b");
    expect(store.getSnapshot().status).toBe("error");
    saved = { ...initial, revision: "2", active_profile: "other-profile" };
    await store.retry();
    expect(save.mock.calls[1][0]).toEqual({ revision: "2", microphone: "b" });
    expect(store.getSnapshot().settings?.active_profile).toBe("other-profile");
    store.dispose();
  });
});
