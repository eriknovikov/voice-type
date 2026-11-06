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
  port: 3232,
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
    port: 3232,
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
    fireEvent.click(screen.getByRole("button", { name: "Delete Personal" }));
    expect(api.deleteProfile).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    await screen.findByText("Select a profile to start dictating.");
    expect(api.deleteProfile).toHaveBeenCalledWith("r1", "personal");
  });
});
describe("autosave", () => {
  it("saves port on blur and retains failed edits for explicit retry", async () => {
    vi.mocked(api.save).mockRejectedValueOnce(
      new Error("settings changed elsewhere; reload before saving"),
    );
    await ready();
    fireEvent.click(screen.getByText("Advanced"));
    const port = screen.getByLabelText(
      "Local control port",
    ) as HTMLInputElement;
    fireEvent.change(port, { target: { value: "4242" } });
    expect(api.save).not.toHaveBeenCalled();
    fireEvent.blur(port);
    await screen.findByRole("button", { name: "Retry save" });
    expect(port.value).toBe("4242");
    saved.revision = "external";
    fireEvent.click(screen.getByRole("button", { name: "Retry save" }));
    await waitFor(() =>
      expect(api.save).toHaveBeenLastCalledWith({
        revision: "external",
        port: 4242,
      }),
    );
    expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
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
  it("preserves invalid port draft across navigation without saving it", async () => {
    await ready();
    fireEvent.click(screen.getByText("Advanced"));
    fireEvent.change(screen.getByLabelText("Local control port"), {
      target: { value: "50" },
    });
    fireEvent.blur(screen.getByLabelText("Local control port"));
    expect(screen.getByText("Use a port from 1024 to 65535.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Audio" }));
    await screen.findByText("Test microphone");
    fireEvent.click(screen.getByRole("button", { name: "General" }));
    await screen.findByRole("button", { name: "Edit Personal" });
    expect(
      (screen.getByLabelText("Local control port") as HTMLInputElement).value,
    ).toBe("50");
    expect(api.save).not.toHaveBeenCalled();
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
  const summary = await screen.findByText("config recovery");
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
    name: "Change transcription toggle shortcut",
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
    name: "Change transcription toggle shortcut",
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
