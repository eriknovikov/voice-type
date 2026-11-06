// @vitest-environment jsdom
import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  act,
  render,
  screen,
  waitFor,
  fireEvent,
  within,
} from "@testing-library/react";
import { App } from "./App";
import { api, type Settings, type DesktopInfo } from "./api";
const eventHandlers = vi.hoisted(
  () => new Map<string, (event: { data: unknown }) => void>(),
);
vi.mock("@wailsio/runtime", () => ({
  Call: { ByName: vi.fn() },
  Events: {
    On: (name: string, callback: (event: { data: unknown }) => void) => {
      eventHandlers.set(name, callback);
      return () => eventHandlers.delete(name);
    },
  },
}));
vi.mock("./api", () => ({
  api: {
    settings: vi.fn(),
    info: vi.fn(),
    status: vi.fn(),
    save: vi.fn(),
    logs: vi.fn(),
    microphones: vi.fn(),
    stopTest: vi.fn(),
    startTest: vi.fn(),
    workspace: vi.fn(),
    beginShortcutCapture: vi.fn(),
    endShortcutCapture: vi.fn(),
    preferences: vi.fn(),
    upsertProfile: vi.fn(),
    deleteProfile: vi.fn(),
    selectProfile: vi.fn(),
    revealProfileKey: vi.fn(),
    copyProfileKey: vi.fn(),
    frontendReady: vi.fn(),
    requestHide: vi.fn(),
    completeWindowAction: vi.fn(),
    exportLogs: vi.fn(),
    clearLogs: vi.fn(),
  },
}));
const base: Settings = {
  revision: "r1",
  profiles: [
    {
      id: "personal",
      name: "Personal",
      provider: "openai",
      masked_key: "sk-p****1234",
    },
    {
      id: "work",
      name: "Work",
      provider: "openai",
      masked_key: "sk-w****5678",
    },
  ],
  active_profile: "personal",
  microphone: "",
  vocabulary: [{ name: "general", terms: ["Wails"] }],
  workspace: "",
};
let saved: Settings;
let info: DesktopInfo;
beforeEach(() => {
  vi.clearAllMocks();
  eventHandlers.clear();
  saved = structuredClone(base);
  info = {
    preferences: {
      shortcut: "Ctrl+Space",
      autostart: false,
      setup_complete: true,
    },
    tray_available: true,
    shortcut_error: "",
    version: "test",
  };
  vi.mocked(api.settings).mockImplementation(async () =>
    structuredClone(saved),
  );
  vi.mocked(api.save).mockImplementation(async (update) => {
    saved = { ...saved, ...update, revision: `${saved.revision}+` };
    return structuredClone(saved);
  });
  vi.mocked(api.info).mockImplementation(async () => structuredClone(info));
  vi.mocked(api.preferences).mockImplementation(async (preferences) => {
    info.preferences = preferences;
  });
  vi.mocked(api.status).mockResolvedValue({
    state: "idle",
    error: "",
    level: 0,
  });
  vi.mocked(api.logs).mockResolvedValue([]);
  vi.mocked(api.microphones).mockResolvedValue([]);
  for (const method of [
    api.stopTest,
    api.startTest,
    api.workspace,
    api.beginShortcutCapture,
    api.endShortcutCapture,
    api.frontendReady,
    api.requestHide,
    api.completeWindowAction,
    api.copyProfileKey,
    api.exportLogs,
  ])
    vi.mocked(method).mockResolvedValue();
});
afterEach(cleanup);
const ready = async () => {
  render(<App />);
  await screen.findByRole("button", { name: "Edit Personal" });
};
describe("profiles", () => {
  it("shows masked summaries and reveals or copies only on explicit actions", async () => {
    vi.mocked(api.revealProfileKey).mockResolvedValue("sk-private-secret-1234");
    await ready();
    expect(screen.getByText("sk-p****1234")).toBeTruthy();
    expect(api.revealProfileKey).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Edit Personal" }));
    const key = screen.getByLabelText("API key") as HTMLInputElement;
    expect(key.value).toBe("");
    fireEvent.click(screen.getByRole("button", { name: "Copy saved API key" }));
    await waitFor(() =>
      expect(api.copyProfileKey).toHaveBeenCalledWith("personal"),
    );
    expect(api.revealProfileKey).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Reveal API key" }));
    await waitFor(() => expect(key.value).toBe("sk-private-secret-1234"));
    fireEvent.click(screen.getByRole("button", { name: "Hide API key" }));
    expect(key.value).toBe("");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    fireEvent.click(screen.getByRole("button", { name: "Edit Personal" }));
    expect((screen.getByLabelText("API key") as HTMLInputElement).value).toBe(
      "",
    );
  });
  it("creates a provider profile with a sensible name and explicit dialog save", async () => {
    vi.mocked(api.upsertProfile).mockImplementation(async (update) => ({
      ...saved,
      revision: "r2",
      profiles: [
        ...saved.profiles,
        {
          id: "new",
          name: update.name,
          provider: update.provider,
          masked_key: "sk-n****9999",
        },
      ],
    }));
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Add profile" }));
    expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe(
      "OpenAI 1",
    );
    fireEvent.change(screen.getByLabelText("API key"), {
      target: { value: "sk-new-9999" },
    });
    expect(api.upsertProfile).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Save profile" }));
    await waitFor(() =>
      expect(api.upsertProfile).toHaveBeenCalledWith({
        revision: "r1",
        name: "OpenAI 1",
        provider: "openai",
        api_key: "sk-new-9999",
      }),
    );
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });
  it("confirms deleting an active profile and leaves no implicit selection", async () => {
    vi.mocked(api.deleteProfile).mockResolvedValue({
      ...saved,
      profiles: saved.profiles.slice(1),
      active_profile: "",
      revision: "r2",
    });
    await ready();
    expect(
      screen.queryByRole("button", { name: "Delete Personal" }),
    ).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Edit Personal" }));
    fireEvent.click(screen.getByRole("button", { name: "Delete profile" }));
    expect(api.deleteProfile).not.toHaveBeenCalled();
    fireEvent.click(
      within(
        screen.getByRole("dialog", { name: "Delete Personal?" }),
      ).getByRole("button", { name: "Delete profile" }),
    );
    await screen.findByText("Select a profile to start dictating.");
    expect(api.deleteProfile).toHaveBeenCalledWith("r1", "personal");
  });
});
describe("autosave", () => {
  it("retains failed vocabulary edits for explicit retry without changing the selected microphone", async () => {
    vi.mocked(api.save).mockRejectedValueOnce(
      new Error("settings changed elsewhere; reload before saving"),
    );
    saved.microphone = "saved-device";
    await ready();
    expect(screen.queryByText("Advanced")).toBeNull();
    expect(screen.queryByLabelText("Local control port")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Vocabulary" }));
    const words = await screen.findByLabelText("Words and phrases");
    fireEvent.change(words, { target: { value: "Alpha\nBeta" } });
    fireEvent.blur(words);
    await screen.findByRole("button", { name: "Retry save" });
    expect((words as HTMLTextAreaElement).value).toBe("Alpha\nBeta");
    saved.revision = "external";
    fireEvent.click(screen.getByRole("button", { name: "Retry save" }));
    await waitFor(() =>
      expect(api.save).toHaveBeenLastCalledWith({
        revision: "external",
        vocabulary: [{ name: "general", terms: ["Alpha", "Beta"] }],
      }),
    );
    expect(saved.microphone).toBe("saved-device");
  });
  it("flushes vocabulary before navigation and native hide", async () => {
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Vocabulary" }));
    const textarea = await screen.findByLabelText("Words and phrases");
    fireEvent.change(textarea, { target: { value: "Alpha\nBeta" } });
    expect(api.save).not.toHaveBeenCalled();
    act(() =>
      eventHandlers.get("flush-settings")?.({ data: [{ action: "hide" }] }),
    );
    await waitFor(() =>
      expect(api.completeWindowAction).toHaveBeenCalledWith("hide"),
    );
    expect(api.save).toHaveBeenCalledWith({
      revision: "r1",
      vocabulary: [{ name: "general", terms: ["Alpha", "Beta"] }],
    });
  });
  it("does not hide when persistence fails", async () => {
    vi.mocked(api.save).mockRejectedValue(new Error("Disk is read-only."));
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Vocabulary" }));
    fireEvent.change(await screen.findByLabelText("Words and phrases"), {
      target: { value: "Unsaved term" },
    });
    act(() =>
      eventHandlers.get("flush-settings")?.({ data: { action: "quit" } }),
    );
    await screen.findByRole("button", { name: "Retry save" });
    expect(api.completeWindowAction).not.toHaveBeenCalled();
    expect(
      (screen.getByLabelText("Words and phrases") as HTMLTextAreaElement).value,
    ).toBe("Unsaved term");
  });
});
it("loads logs only on opening, retains expansion and exports the outcome without search", async () => {
  vi.mocked(api.logs).mockResolvedValue([
    {
      event: "config_recovery",
      outcome: "ok",
      error: "Configuration restored.",
      timestamp: "2026-11-06T10:00:00Z",
      provider: "",
      configured_model: "",
      total_ms: 0,
      audio_ms: 0,
      attempt_count: 0,
      attempts: [],
    },
  ]);
  await ready();
  expect(api.logs).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Logs" }));
  const summary = await screen.findByText("Settings recovered");
  expect(summary.closest("details")).toBeTruthy();
  expect(screen.queryByLabelText("Search logs")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Export" }));
  await waitFor(() => expect(api.exportLogs).toHaveBeenCalledWith("all"));
});
it("keeps inline keyboard capture and immediate login preference saving", async () => {
  await ready();
  fireEvent.click(screen.getByRole("switch", { name: "Start at login" }));
  await waitFor(() =>
    expect(api.preferences).toHaveBeenCalledWith(
      expect.objectContaining({ autostart: true }),
    ),
  );
  fireEvent.click(screen.getByRole("button", { name: "Keyboard Shortcuts" }));
  const button = await screen.findByRole("button", {
    name: "Change start or stop recording shortcut",
  });
  fireEvent.click(button);
  await screen.findByText("Press keys…");
  fireEvent.keyDown(button, { key: " ", ctrlKey: true, shiftKey: true });
  fireEvent.blur(button);
  await waitFor(() =>
    expect(api.preferences).toHaveBeenCalledWith(
      expect.objectContaining({ shortcut: "Ctrl+Shift+Space" }),
    ),
  );
  expect(screen.queryByRole("dialog")).toBeNull();
});

it("waits for shortcut release and its preference write before native Quit", async () => {
  let release!: () => void;
  vi.mocked(api.endShortcutCapture).mockImplementationOnce(
    () =>
      new Promise<void>((resolve) => {
        release = resolve;
      }),
  );
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "Keyboard Shortcuts" }));
  const button = await screen.findByRole("button", {
    name: "Change start or stop recording shortcut",
  });
  button.focus();
  fireEvent.click(button);
  await screen.findByText("Press keys…");
  fireEvent.keyDown(button, { key: "F9" });
  act(() =>
    eventHandlers.get("flush-settings")?.({ data: { action: "quit" } }),
  );
  await waitFor(() => expect(api.endShortcutCapture).toHaveBeenCalled());
  expect(api.completeWindowAction).not.toHaveBeenCalled();
  await act(async () => release());
  await waitFor(() =>
    expect(api.completeWindowAction).toHaveBeenCalledWith("quit"),
  );
  expect(api.preferences).toHaveBeenCalledWith(
    expect.objectContaining({ shortcut: "F9" }),
  );
});

it("shows a new recovery notice after an earlier identical notice was dismissed", async () => {
  const message = "Settings were restored from the last valid backup.";
  const status = {
    state: "idle",
    error: "",
    level: 0,
    notice: message,
    notice_version: 1,
  };
  vi.mocked(api.status).mockResolvedValue(status);
  await ready();
  await screen.findByText(message);
  fireEvent.click(screen.getByRole("button", { name: "Dismiss notice" }));
  expect(screen.queryByText(message)).toBeNull();
  vi.mocked(api.status).mockResolvedValue({ ...status, notice_version: 2 });
  await screen.findByText(message);
});

it("selects profiles through a labelled radio group", async () => {
  vi.mocked(api.selectProfile).mockImplementation(async (_revision, id) => ({
    ...saved,
    revision: "r2",
    active_profile: id,
  }));
  await ready();
  const group = screen.getByRole("radiogroup", { name: "Profiles" });
  expect(
    (
      within(group).getByRole("radio", {
        name: "Use Personal",
      }) as HTMLInputElement
    ).checked,
  ).toBe(true);
  fireEvent.click(within(group).getByRole("radio", { name: "Use Work" }));
  await waitFor(() =>
    expect(api.selectProfile).toHaveBeenCalledWith("r1", "work"),
  );
  await waitFor(() =>
    expect(
      (screen.getByRole("radio", { name: "Use Work" }) as HTMLInputElement)
        .checked,
    ).toBe(true),
  );
});

it("focuses invalid profile fields without submitting", async () => {
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "Add profile" }));
  fireEvent.change(screen.getByLabelText("Name"), {
    target: { value: "Personal" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save profile" }));
  expect(document.activeElement).toBe(screen.getByLabelText("Name"));
  expect(screen.getByLabelText("Name").getAttribute("aria-invalid")).toBe(
    "true",
  );
  expect(screen.getByText("Choose a unique profile name.")).toBeTruthy();
  fireEvent.change(screen.getByLabelText("Name"), {
    target: { value: "New account" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save profile" }));
  expect(document.activeElement).toBe(screen.getByLabelText("API key"));
  expect(screen.getByLabelText("API key").getAttribute("aria-invalid")).toBe(
    "true",
  );
  expect(api.upsertProfile).not.toHaveBeenCalled();
});

it("offers providers directly and preserves a custom profile name", async () => {
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "Add profile" }));
  fireEvent.click(screen.getByRole("radio", { name: "OpenRouter" }));
  expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe(
    "OpenRouter 1",
  );
  fireEvent.change(screen.getByLabelText("Name"), {
    target: { value: "Personal router" },
  });
  fireEvent.click(screen.getByRole("radio", { name: "OpenAI" }));
  expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe(
    "Personal router",
  );
});

it("guards dirty profile drafts on native hide and Quit until explicitly discarded", async () => {
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "Edit Personal" }));
  fireEvent.change(screen.getByLabelText("Name"), {
    target: { value: "Unsaved personal" },
  });
  act(() =>
    eventHandlers.get("flush-settings")?.({ data: { action: "hide" } }),
  );
  await screen.findByRole("dialog", { name: "Discard profile changes?" });
  expect(api.completeWindowAction).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
  await waitFor(() =>
    expect(
      screen.queryByRole("dialog", { name: "Discard profile changes?" }),
    ).toBeNull(),
  );
  expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe(
    "Unsaved personal",
  );
  act(() =>
    eventHandlers.get("flush-settings")?.({ data: { action: "quit" } }),
  );
  await screen.findByRole("button", { name: "Discard changes" });
  fireEvent.click(screen.getByRole("button", { name: "Discard changes" }));
  await waitFor(() =>
    expect(api.completeWindowAction).toHaveBeenCalledWith("quit"),
  );
  expect(api.upsertProfile).not.toHaveBeenCalled();
  expect(screen.queryByRole("dialog")).toBeNull();
});

it("guards Escape but closes an unchanged profile immediately", async () => {
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "Edit Personal" }));
  fireEvent.change(screen.getByLabelText("Name"), {
    target: { value: "Changed" },
  });
  fireEvent.keyDown(screen.getByLabelText("Name"), { key: "Escape" });
  await screen.findByRole("dialog", { name: "Discard profile changes?" });
  fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
  fireEvent.change(screen.getByLabelText("Name"), {
    target: { value: "Personal" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
});

it("keeps a failed deletion open and allows an explicit retry", async () => {
  vi.mocked(api.deleteProfile)
    .mockRejectedValueOnce(new Error("Disk is read-only."))
    .mockResolvedValueOnce({
      ...saved,
      profiles: saved.profiles.slice(1),
      active_profile: "",
      revision: "r2",
    });
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "Edit Personal" }));
  fireEvent.click(screen.getByRole("button", { name: "Delete profile" }));
  const confirm = screen.getByRole("dialog", { name: "Delete Personal?" });
  fireEvent.click(
    within(confirm).getByRole("button", { name: "Delete profile" }),
  );
  await within(confirm).findByText("Disk is read-only.");
  expect(confirm.isConnected).toBe(true);
  fireEvent.click(
    within(confirm).getByRole("button", { name: "Delete profile" }),
  );
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(api.deleteProfile).toHaveBeenCalledTimes(2);
});

it("keeps expanded logs open when polling prepends another event", async () => {
  const old = {
    event: "dictation.completed",
    outcome: "ok",
    timestamp: "2026-11-06T10:00:00Z",
    provider: "openai",
    configured_model: "whisper-1",
    total_ms: 1400,
    audio_ms: 1000,
    attempt_count: 1,
    attempts: [{ outcome: "ok" }],
  };
  vi.mocked(api.logs).mockResolvedValue([old]);
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "Logs" }));
  const title = await screen.findByText("Dictation completed");
  const details = title.closest("details")!;
  fireEvent.click(title.closest("summary")!);
  expect(details.open).toBe(true);
  expect(
    within(details).getByText("Technical details").closest("details")!.open,
  ).toBe(false);
  vi.mocked(api.logs).mockResolvedValue([
    {
      ...old,
      event: "startup.failed",
      outcome: "error",
      timestamp: "2026-11-06T11:00:00Z",
    },
    old,
  ]);
  await screen.findByText("Startup failed", {}, { timeout: 3000 });
  expect(screen.getByText("Dictation completed").closest("details")).toBe(
    details,
  );
  expect(details.open).toBe(true);
  fireEvent.click(screen.getByRole("radio", { name: "Errors" }));
  expect(screen.queryByText("Dictation completed")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Export" }));
  await waitFor(() => expect(api.exportLogs).toHaveBeenCalledWith("errors"));
});

it("keeps vocabulary newline editing and makes renaming explicit", async () => {
  saved.vocabulary.push({ name: "Medical", terms: ["Alpha", "Beta"] });
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "Vocabulary" }));
  fireEvent.click(
    within(
      await screen.findByRole("navigation", { name: "Vocabularies" }),
    ).getByRole("button", { name: /Medical/ }),
  );
  await screen.findByRole("heading", { name: "Medical" });
  expect(
    (screen.getByLabelText("Words and phrases") as HTMLTextAreaElement).value,
  ).toBe("Alpha\nBeta");
  expect(screen.queryByRole("textbox", { name: "Vocabulary name" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Rename vocabulary" }));
  const name = screen.getByRole("textbox", { name: "Vocabulary name" });
  expect(document.activeElement).toBe(name);
  fireEvent.change(name, { target: { value: "Discarded" } });
  fireEvent.keyDown(name, { key: "Escape" });
  await screen.findByRole("heading", { name: "Medical" });
  await waitFor(() =>
    expect(document.activeElement).toBe(
      screen.getByRole("button", { name: "Rename vocabulary" }),
    ),
  );
  fireEvent.click(screen.getByRole("button", { name: "Rename vocabulary" }));
  const renamed = screen.getByRole("textbox", { name: "Vocabulary name" });
  fireEvent.change(renamed, { target: { value: "Clinical" } });
  fireEvent.keyDown(renamed, { key: "Enter" });
  await screen.findByRole("heading", { name: "Clinical" });
  await waitFor(() =>
    expect(document.activeElement).toBe(
      screen.getByRole("button", { name: "Rename vocabulary" }),
    ),
  );
  await waitFor(() =>
    expect(api.save).toHaveBeenCalledWith(
      expect.objectContaining({
        vocabulary: expect.arrayContaining([
          expect.objectContaining({
            name: "Clinical",
            terms: ["Alpha", "Beta"],
          }),
        ]),
      }),
    ),
  );
});

it("renders keycaps and restores the default shortcut inline", async () => {
  info.preferences.shortcut = "F9";
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "Keyboard Shortcuts" }));
  fireEvent.click(
    await screen.findByRole("button", { name: "Reset to Ctrl+Space" }),
  );
  await waitFor(() =>
    expect(api.preferences).toHaveBeenCalledWith(
      expect.objectContaining({ shortcut: "Ctrl+Space" }),
    ),
  );
  await waitFor(() =>
    expect(
      screen.queryByRole("button", { name: "Reset to Ctrl+Space" }),
    ).toBeNull(),
  );
  expect(screen.getByText("Ctrl").tagName).toBe("KBD");
  expect(screen.getByText("Space").tagName).toBe("KBD");
  expect(screen.queryByRole("dialog")).toBeNull();
});

it("keeps a pending native action superseded by the user's latest request", async () => {
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "Edit Personal" }));
  fireEvent.change(screen.getByLabelText("Name"), {
    target: { value: "Changed" },
  });
  act(() =>
    eventHandlers.get("flush-settings")?.({ data: { action: "hide" } }),
  );
  await screen.findByRole("button", { name: "Discard changes" });
  act(() =>
    eventHandlers.get("flush-settings")?.({ data: { action: "quit" } }),
  );
  fireEvent.click(screen.getByRole("button", { name: "Discard changes" }));
  await waitFor(() =>
    expect(api.completeWindowAction).toHaveBeenCalledWith("quit"),
  );
  expect(api.completeWindowAction).toHaveBeenCalledTimes(1);
});

it("shows microphone timing only during a test and explains a blocked test", async () => {
  vi.mocked(api.status).mockResolvedValue({
    state: "recording",
    error: "",
    level: 0,
  });
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "Audio" }));
  const test = await screen.findByRole("button", { name: "Test microphone" });
  expect((test as HTMLButtonElement).disabled).toBe(true);
  expect(
    screen.getByText("Stop recording to test the microphone."),
  ).toBeTruthy();
  expect(screen.queryByText(/Stops after 60 seconds/)).toBeNull();
  vi.mocked(api.status).mockResolvedValue({
    state: "testing",
    error: "",
    level: 0,
  });
  await screen.findByText("Listening… Stops after 60 seconds.");
  expect(screen.getByRole("button", { name: "Stop test" })).toBeTruthy();
});
