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
import {
  api,
  type Settings,
  type DesktopInfo,
  type LocalModel,
  type LogPage,
  type LogRecord,
} from "./api";
const eventHandlers = vi.hoisted(
  () => new Map<string, (event: { data: unknown }) => void>(),
);
const localModel = (overrides: Partial<LocalModel> = {}): LocalModel => ({
  id: "parakeet-unified-en",
  name: "Parakeet Unified English",
  bytes: 731357568,
  ram_bytes: 1181116006,
  downloaded_bytes: 731357568,
  progress: 100,
  languages: "English",
  supports_streaming: true,
  license:
    "NVIDIA Open Model License (upstream; GGUF card also claims CC BY 4.0)",
  license_url: "https://example.test/license",
  model_card_url: "https://example.test/model",
  state: "ready",
  error: "",
  ...overrides,
});
const tdtModel = (overrides: Partial<LocalModel> = {}): LocalModel =>
  localModel({
    id: "parakeet-tdt-v3",
    name: "Parakeet TDT v3",
    bytes: 739508576,
    ram_bytes: 1288490189,
    downloaded_bytes: 739508576,
    languages: "25 European languages",
    supports_streaming: false,
    license: "CC BY 4.0",
    ...overrides,
  });

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
    localModels: vi.fn(),
    installLocalModel: vi.fn(),
    resumeLocalModel: vi.fn(),
    retryLocalModel: vi.fn(),
    pauseLocalModel: vi.fn(),
    cancelLocalModel: vi.fn(),
    removeLocalModel: vi.fn(),
    stopTest: vi.fn(),
    startTest: vi.fn(),
    workspace: vi.fn(),
    beginShortcutCapture: vi.fn(),
    endShortcutCapture: vi.fn(),
    preferences: vi.fn(),
    upsertProfile: vi.fn(),
    deleteProfile: vi.fn(),
    selectProfile: vi.fn(),
    saveAPIKey: vi.fn(),
    deleteAPIKey: vi.fn(),
    copyAPIKey: vi.fn(),
    frontendReady: vi.fn(),
    requestHide: vi.fn(),
    quit: vi.fn(),
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
    },
    {
      id: "work",
      name: "Work",
      provider: "openai",
    },
  ],
  active_profile: "personal",
  credential_storage: "keyring",
  api_keys: {
    openai: {
      masked_key: "sk-p••••1234",
      saved: true,
    },
    openrouter: {
      saved: false,
    },
  },
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
  vi.mocked(api.logs).mockResolvedValue(logPage());
  vi.mocked(api.microphones).mockResolvedValue([]);
  vi.mocked(api.localModels).mockResolvedValue([localModel(), tdtModel()]);
  for (const method of [
    api.stopTest,
    api.startTest,
    api.workspace,
    api.beginShortcutCapture,
    api.endShortcutCapture,
    api.requestHide,
    api.quit,
    api.completeWindowAction,
    api.copyAPIKey,
    api.exportLogs,
    api.installLocalModel,
    api.resumeLocalModel,
    api.retryLocalModel,
    api.pauseLocalModel,
    api.cancelLocalModel,
    api.removeLocalModel,
  ])
    vi.mocked(method).mockResolvedValue();
  vi.mocked(api.frontendReady).mockResolvedValue(true);
});
const logPage = (
  records: LogRecord[] = [],
  overrides: Partial<LogPage> = {},
): LogPage => ({
  records,
  revision: "revision",
  outcome: "all",
  offset: 0,
  limit: 200,
  total: records.length,
  retained: records.length,
  ...overrides,
});
afterEach(cleanup);
const ready = async () => {
  render(<App />);
  await screen.findByRole("button", { name: "Edit Personal" });
};

describe("window controls", () => {
  it("distinguishes minimizing to the background from quitting", async () => {
    await ready();
    const controls = screen.getByRole("group", { name: "Window controls" });
    const minimize = within(controls).getByRole("button", {
      name: "Minimize window",
    });
    const quit = within(controls).getByRole("button", {
      name: "Quit Voice Type",
    });
    expect(minimize.title).toBe("Keep Voice Type running in the background");
    expect(quit.title).toBe("Quit Voice Type");
    expect(within(controls).getAllByRole("button")).toEqual([minimize, quit]);
    fireEvent.click(minimize);
    await waitFor(() => expect(api.requestHide).toHaveBeenCalledTimes(1));
    expect(api.quit).not.toHaveBeenCalled();
    fireEvent.click(quit);
    await waitFor(() => expect(api.quit).toHaveBeenCalledTimes(1));
    expect(api.completeWindowAction).not.toHaveBeenCalled();
  });

  it.each([
    ["Minimize window", "hide", "requestHide"],
    ["Quit Voice Type", "quit", "quit"],
  ] as const)(
    "flushes pending vocabulary edits before %s completes",
    async (label, action, method) => {
      let finishSave!: () => void;
      vi.mocked(api.save).mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finishSave = () => resolve({ ...saved, revision: "r2" });
          }),
      );
      vi.mocked(api[method]).mockImplementation(async () => {
        eventHandlers.get("flush-settings")?.({ data: { action } });
      });
      await ready();
      fireEvent.click(screen.getByRole("button", { name: "Vocabulary" }));
      fireEvent.change(await screen.findByLabelText("Terms"), {
        target: { value: "Pending vocabulary" },
      });
      fireEvent.click(screen.getByRole("button", { name: label }));
      await waitFor(() => expect(api.save).toHaveBeenCalled());
      expect(api.completeWindowAction).not.toHaveBeenCalled();
      await act(async () => finishSave());
      await waitFor(() =>
        expect(api.completeWindowAction).toHaveBeenCalledWith(action),
      );
    },
  );

  it.each([
    ["Minimize window", "requestHide"],
    ["Quit Voice Type", "quit"],
  ] as const)(
    "reports a failed %s request without completing it",
    async (label, method) => {
      vi.mocked(api[method]).mockRejectedValueOnce(
        new Error("Window action unavailable"),
      );
      await ready();
      fireEvent.click(screen.getByRole("button", { name: label }));
      expect((await screen.findByRole("alert")).textContent).toContain(
        "Window action unavailable",
      );
      expect(api.completeWindowAction).not.toHaveBeenCalled();
    },
  );
});

const chooseProfileOption = (label: string, id: string) => {
  const select = screen
    .getByRole("combobox", { name: label })
    .parentElement?.querySelector("select");
  if (!select) throw new Error(`${label} picker is missing`);
  fireEvent.change(select, { target: { value: JSON.stringify(id) } });
};
const chooseLocalModel = (id: string) => chooseProfileOption("Local model", id);
describe("dictation warnings", () => {
  const message =
    "No speech detected. Try speaking louder or check your microphone.";

  it("keeps one status area mounted and empty after dismissing errors and warnings", async () => {
    await ready();
    const area = document.querySelector(".status-area")!;
    expect(area.previousElementSibling).toBe(
      document.querySelector(".page-content"),
    );
    expect(document.querySelector("main")!.lastElementChild).toBe(area);
    expect(area.childElementCount).toBe(0);
    act(() =>
      eventHandlers.get("operation-error")!({ data: "Microphone unavailable" }),
    );
    expect(area.querySelector('[role="alert"]')).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Dismiss error" }));
    expect(document.querySelector(".status-area")).toBe(area);
    expect(area.childElementCount).toBe(0);
    vi.mocked(api.status).mockResolvedValue({
      state: "idle",
      error: "",
      level: 0,
      warning: message,
      warning_version: 1,
    });
    await screen.findByRole("status", { name: "Dictation warning" });
    expect(document.querySelector(".status-area")).toBe(area);
    expect(area.childElementCount).toBe(1);
    fireEvent.click(screen.getByRole("button", { name: "Dismiss warning" }));
    expect(document.querySelector(".status-area")).toBe(area);
    expect(area.childElementCount).toBe(0);
  });

  it("shows a dismissible warning without Diagnostics, repeats for a new attempt, and clears on expiry", async () => {
    let status = {
      state: "idle",
      error: "",
      level: 0,
      warning: message,
      warning_version: 1,
    };
    vi.mocked(api.status).mockImplementation(async () => status);
    await ready();
    const warning = await screen.findByRole("status", {
      name: "Dictation warning",
    });
    expect(warning.textContent).toContain(message);
    expect(warning.classList.contains("warning")).toBe(true);
    expect(
      within(warning).queryByRole("button", { name: "Diagnostics" }),
    ).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Dismiss warning" }));
    expect(
      screen.queryByRole("status", { name: "Dictation warning" }),
    ).toBeNull();
    status = { ...status, warning_version: 2 };
    await screen.findByRole("status", { name: "Dictation warning" });
    status = { ...status, warning: "" };
    await waitFor(() =>
      expect(
        screen.queryByRole("status", { name: "Dictation warning" }),
      ).toBeNull(),
    );
  });

  it("stops displaying the warning as soon as the next recording is observed", async () => {
    let status = {
      state: "idle",
      error: "",
      level: 0,
      warning: message,
      warning_version: 1,
    };
    vi.mocked(api.status).mockImplementation(async () => status);
    await ready();
    await screen.findByRole("status", { name: "Dictation warning" });
    status = { ...status, state: "recording" };
    await waitFor(() =>
      expect(
        screen.queryByRole("status", { name: "Dictation warning" }),
      ).toBeNull(),
    );
  });

  it("keeps real operation failures as errors with Diagnostics", async () => {
    await ready();
    act(() =>
      eventHandlers.get("operation-error")!({
        data: "Microphone access denied",
      }),
    );
    const error = screen.getByRole("alert");
    expect(error.classList.contains("error")).toBe(true);
    expect(error.textContent).toContain("Microphone access denied");
    expect(
      within(error).getByRole("button", { name: "Diagnostics" }),
    ).toBeTruthy();
    expect(
      screen.queryByRole("status", { name: "Dictation warning" }),
    ).toBeNull();
  });

  it("replaces an older dictation error when a new attempt detects no speech", async () => {
    await ready();
    act(() =>
      eventHandlers.get("operation-error")!({ data: "Provider unavailable" }),
    );
    expect(screen.getByRole("alert").textContent).toContain(
      "Provider unavailable",
    );
    vi.mocked(api.status).mockResolvedValue({
      state: "idle",
      error: "",
      level: 0,
      warning: message,
      warning_version: 1,
    });
    await screen.findByRole("status", { name: "Dictation warning" });
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("labels no-speech logs as warnings and offers a separate warning filter", async () => {
    const record: LogRecord = {
      timestamp: "2026-11-13T15:00:00Z",
      event: "dictation.completed",
      provider: "openai",
      configured_model: "gpt-transcribe",
      outcome: "no_speech",
      level: "warn",
      message,
      total_ms: 100,
      audio_ms: 400,
      attempt_count: 1,
      attempts: [],
    };
    vi.mocked(api.logs).mockResolvedValue(logPage([record]));
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Logs" }));
    const title = await screen.findByText("No speech detected");
    expect(
      title.closest(".log-entry")!.querySelector(".log-icon.warning"),
    ).toBeTruthy();
    expect(screen.queryByText("Dictation failed")).toBeNull();
    fireEvent.click(screen.getByLabelText("Warnings"));
    await waitFor(() => expect(api.logs).toHaveBeenCalledWith("warnings", 0));
  });
});
describe("profiles", () => {
  it("names Local profiles after the selected model until the user enters a custom name", async () => {
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Add profile" }));
    const name = screen.getByLabelText("Name") as HTMLInputElement;
    expect(name.value).toBe("Parakeet Unified English");
    chooseLocalModel("parakeet-tdt-v3");
    expect(name.value).toBe("Parakeet TDT v3");
    fireEvent.click(screen.getByLabelText("OpenRouter"));
    expect(name.value).toBe("OpenRouter");
    fireEvent.click(screen.getByLabelText("Local"));
    expect(name.value).toBe("Parakeet TDT v3");
    fireEvent.change(name, { target: { value: "My dictation" } });
    chooseLocalModel("parakeet-unified-en");
    expect(name.value).toBe("My dictation");
  });
  it("adds a suffix only when a generated provider or model name already exists", async () => {
    saved.profiles.push(
      {
        id: "local-one",
        name: "parakeet unified english",
        provider: "local",
        model_id: "parakeet-unified-en",
      },
      {
        id: "local-two",
        name: "Parakeet Unified English 2",
        provider: "local",
        model_id: "parakeet-unified-en",
      },
      { id: "cloud-one", name: "OpenAI", provider: "openai" },
    );
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Add profile" }));
    const name = screen.getByLabelText("Name") as HTMLInputElement;
    expect(name.value).toBe("Parakeet Unified English 3");
    fireEvent.click(screen.getByLabelText("OpenAI"));
    expect(name.value).toBe("OpenAI 2");
    fireEvent.click(screen.getByLabelText("Local"));
    expect(name.value).toBe("Parakeet Unified English 3");
  });
  it("guards the close icon when the profile has unsaved edits", async () => {
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Add profile" }));
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "My profile" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Close profile editor" }),
    );
    await screen.findByRole("dialog", { name: "Discard profile changes?" });
    expect(api.upsertProfile).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    await waitFor(() =>
      expect(
        screen.queryByRole("dialog", { name: "Discard profile changes?" }),
      ).toBeNull(),
    );
    expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe(
      "My profile",
    );
  });
  it("uses the shared saved key without exposing it in profile dialogs", async () => {
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Edit Personal" }));
    expect(
      screen.getByText("sk-p••••1234"),
    ).toBeTruthy();
    expect(screen.queryByRole("textbox", { name: "API key" })).toBeNull();
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
        },
      ],
    }));
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Add profile" }));
    fireEvent.click(screen.getByLabelText("OpenAI"));
    expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe(
      "OpenAI",
    );
    expect(screen.getByText("sk-p••••1234")).toBeTruthy();
    expect(api.upsertProfile).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Save profile" }));
    await waitFor(() =>
      expect(api.upsertProfile).toHaveBeenCalledWith({
        revision: "r1",
        name: "OpenAI",
        provider: "openai",
        streaming_mode: "process",
        languages: [],
      }),
    );
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });
  it("preserves existing OpenAI batch mode and saves optional language hints", async () => {
    vi.mocked(api.upsertProfile).mockResolvedValue(saved);
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Edit Personal" }));
    expect((screen.getByLabelText("Record then upload") as HTMLInputElement).checked).toBe(true);
    fireEvent.click(screen.getByLabelText("Upload while recording"));
    fireEvent.change(screen.getByLabelText("Languages (optional)"), {target: {value: "EN, es, de"}});
    fireEvent.click(screen.getByRole("button", { name: "Save profile" }));
    await waitFor(() => expect(api.upsertProfile).toHaveBeenCalledWith({revision: "r1", id: "personal", name: "Personal", provider: "openai", streaming_mode: "process", languages: ["en", "es", "de"]}));
  });
  it("shows a missing shared key without offering a profile-level credential", async () => {
    saved.api_keys.openai = { saved: false };
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Edit Personal" }));
    expect(screen.getByText("No OpenAI key")).toBeTruthy();
    expect(screen.queryByRole("textbox", { name: "API key" })).toBeNull();
  });
  it("closes without a discard prompt after switching back to the original new-profile defaults", async () => {
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Add profile" }));
    fireEvent.click(screen.getByLabelText("OpenAI"));
    fireEvent.click(screen.getByLabelText("Local"));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });
  it("closes untouched Local defaults without prompting when only the batch model is installed", async () => {
    vi.mocked(api.localModels).mockResolvedValue([tdtModel()]);
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Add profile" }));
    expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe(
      "Parakeet TDT v3",
    );
    expect(
      screen.getByRole("combobox", { name: "Local model" }).textContent,
    ).toBe("Parakeet TDT v3");
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });
  it("defaults to Local first with streaming and no cloud key", async () => {
    vi.mocked(api.upsertProfile).mockResolvedValue(saved);
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Add profile" }));
    const providers = within(screen.getByRole("group", { name: "Provider" }));
    expect(
      providers
        .getAllByRole("radio")
        .map((radio) => (radio as HTMLInputElement).value),
    ).toEqual(["local", "openai", "openrouter"]);
    expect(
      (providers.getByLabelText("Local") as HTMLInputElement).checked,
    ).toBe(true);
    expect(screen.queryByText("sk-p••••1234")).toBeNull();
    expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe(
      "Parakeet Unified English",
    );
    expect(screen.getByLabelText("Local model")).toBeTruthy();
    const mode = screen.getByRole("combobox", { name: "Dictation mode" });
    const modeSelect = mode.parentElement!.querySelector("select")!;
    expect(
      Array.from(modeSelect.options).map((option) => JSON.parse(option.value)),
    ).toEqual(["process", "live", "none"]);
    expect(mode.textContent).toBe("Process while recording");
    expect(screen.getByText(/Recommended. Text is inserted/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Save profile" }));
    await waitFor(() =>
      expect(api.upsertProfile).toHaveBeenCalledWith({
        revision: "r1",
        name: "Parakeet Unified English",
        provider: "local",
        unload_after_dictation: false,
        model_id: "parakeet-unified-en",
        streaming_mode: "process",
      }),
    );
  });
  it("can unload the local model after each dictation", async () => {
    vi.mocked(api.upsertProfile).mockResolvedValue(saved);
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Add profile" }));
    fireEvent.click(screen.getByLabelText("Local"));
    expect(
      screen.getByRole("combobox", { name: "Model memory" }).textContent,
    ).toBe("Keep loaded");
    expect(
      screen.getByText("Uses ~1.1 GiB while idle. Instant reuse."),
    ).toBeTruthy();
    chooseProfileOption("Model memory", "unload");
    expect(screen.getByText(/Frees ~1.1 GiB while idle/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Save profile" }));
    await waitFor(() =>
      expect(api.upsertProfile).toHaveBeenCalledWith({
        revision: "r1",
        name: "Parakeet Unified English",
        provider: "local",
        model_id: "parakeet-unified-en",
        streaming_mode: "process",
        unload_after_dictation: true,
      }),
    );
  });
  it("lists installed and uninstalled models together with status and size", async () => {
    vi.mocked(api.localModels).mockResolvedValue([
      tdtModel(),
      localModel({ state: "not_installed", downloaded_bytes: 0, progress: 0 }),
    ]);
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Add profile" }));
    fireEvent.click(screen.getByLabelText("Local"));
    const dialog = within(screen.getByRole("dialog"));
    expect(
      dialog.getByRole("combobox", { name: "Local model" }).textContent,
    ).toBe("Parakeet TDT v3");
    const options = dialog
      .getByRole("combobox", { name: "Local model" })
      .parentElement!.querySelector("select")!;
    expect(options.textContent).toMatch(
      /Parakeet Unified English.*731 MB.*Not installed/,
    );
    expect(options.textContent).toMatch(/Parakeet TDT v3.*740 MB.*Installed/);
    expect(api.installLocalModel).not.toHaveBeenCalled();
  });
  it("does not download on opening Local, then downloads an explicitly selected model and waits for readiness", async () => {
    let model = localModel({
      state: "not_installed",
      downloaded_bytes: 0,
      progress: 0,
    });
    vi.mocked(api.localModels).mockImplementation(async () => [model]);
    vi.mocked(api.installLocalModel).mockImplementation(async () => {
      model = {
        ...model,
        state: "downloading",
        downloaded_bytes: 100_000_000,
        progress: 14,
      };
    });
    vi.mocked(api.upsertProfile).mockResolvedValue(saved);
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Add profile" }));
    expect(
      screen.getByRole("combobox", { name: "Local model" }).textContent,
    ).toContain("Choose a model…");
    expect(api.installLocalModel).not.toHaveBeenCalled();
    const save = screen.getByRole("button", {
      name: "Save profile",
    }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    chooseLocalModel(model.id);
    await screen.findByRole("progressbar", {
      name: "Parakeet Unified English download progress",
    });
    expect(api.installLocalModel).toHaveBeenCalledExactlyOnceWith(model.id);
    expect(save.disabled).toBe(true);
    fireEvent.submit(save.closest("form")!);
    expect(api.upsertProfile).not.toHaveBeenCalled();
    model = { ...model, state: "verifying", progress: 100 };
    await screen.findByText("Verifying");
    expect(save.disabled).toBe(true);
    model = { ...model, state: "ready", downloaded_bytes: model.bytes };
    await waitFor(() => expect(save.disabled).toBe(false));
    fireEvent.click(save);
    await waitFor(() =>
      expect(api.upsertProfile).toHaveBeenCalledWith({
        revision: "r1",
        name: "Parakeet Unified English",
        provider: "local",
        model_id: model.id,
        streaming_mode: "process",
        unload_after_dictation: false,
      }),
    );
  });
  it("keeps an uninstalled selection instead of falling back to an installed model and exposes full download controls", async () => {
    let model = localModel({
      state: "not_installed",
      downloaded_bytes: 0,
      progress: 0,
    });
    vi.mocked(api.localModels).mockImplementation(async () => [
      model,
      tdtModel(),
    ]);
    vi.mocked(api.installLocalModel).mockImplementation(async () => {
      model = {
        ...model,
        state: "downloading",
        progress: 14,
        downloaded_bytes: 100_000_000,
      };
    });
    vi.mocked(api.pauseLocalModel).mockImplementation(async () => {
      model = { ...model, state: "paused" };
    });
    vi.mocked(api.resumeLocalModel).mockImplementation(async () => {
      model = { ...model, state: "downloading" };
    });
    vi.mocked(api.retryLocalModel).mockImplementation(async () => {
      model = { ...model, state: "downloading", error: "" };
    });
    vi.mocked(api.cancelLocalModel).mockImplementation(async () => {
      model = {
        ...model,
        state: "not_installed",
        downloaded_bytes: 0,
        progress: 0,
      };
    });
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Add profile" }));
    chooseLocalModel(model.id);
    await screen.findByRole("button", { name: "Pause" });
    expect(
      screen.getByRole("combobox", { name: "Local model" }).textContent,
    ).toContain("Parakeet Unified English");
    fireEvent.click(screen.getByRole("button", { name: "Pause" }));
    await screen.findByRole("button", { name: "Resume" });
    expect(api.pauseLocalModel).toHaveBeenCalledWith(model.id);
    fireEvent.click(screen.getByRole("button", { name: "Resume" }));
    await screen.findByRole("button", { name: "Pause" });
    expect(api.resumeLocalModel).toHaveBeenCalledWith(model.id);
    model = { ...model, state: "failed", error: "Connection interrupted" };
    await screen.findByRole("button", { name: "Retry" });
    expect(screen.getByRole("alert").textContent).toBe(
      "Connection interrupted",
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await screen.findByRole("button", { name: "Pause" });
    expect(api.retryLocalModel).toHaveBeenCalledWith(model.id);
    fireEvent.click(screen.getByRole("button", { name: "Cancel download" }));
    await screen.findByRole("button", { name: "Download" });
    expect(api.cancelLocalModel).toHaveBeenCalledExactlyOnceWith(model.id);
    expect(api.installLocalModel).toHaveBeenCalledTimes(1);
    expect(api.upsertProfile).not.toHaveBeenCalled();
  });
  it("continues downloading after switching providers and discarding the profile", async () => {
    let model = localModel({
      state: "not_installed",
      downloaded_bytes: 0,
      progress: 0,
    });
    vi.mocked(api.localModels).mockImplementation(async () => [model]);
    vi.mocked(api.installLocalModel).mockImplementation(async () => {
      model = { ...model, state: "downloading" };
    });
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Add profile" }));
    chooseLocalModel(model.id);
    await screen.findByRole("button", { name: "Pause" });
    fireEvent.click(screen.getByLabelText("OpenAI"));
    expect(api.pauseLocalModel).not.toHaveBeenCalled();
    expect(api.cancelLocalModel).not.toHaveBeenCalled();
    fireEvent.click(screen.getByLabelText("Local"));
    expect(screen.getByRole("button", { name: "Pause" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await screen.findByRole("dialog", { name: "Discard profile changes?" });
    expect(screen.getByText(/Model downloads will continue/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Discard changes" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    fireEvent.click(screen.getByRole("button", { name: "Local Models" }));
    await screen.findByRole("progressbar", {
      name: "Parakeet Unified English download progress",
    });
    expect(api.installLocalModel).toHaveBeenCalledTimes(1);
    expect(api.pauseLocalModel).not.toHaveBeenCalled();
    expect(api.cancelLocalModel).not.toHaveBeenCalled();
  });
  it("shows a download-start failure inline and allows retrying without saving the profile", async () => {
    vi.mocked(api.localModels).mockResolvedValue([
      localModel({ state: "not_installed", downloaded_bytes: 0, progress: 0 }),
    ]);
    vi.mocked(api.installLocalModel).mockRejectedValueOnce(
      new Error("Not enough disk space"),
    );
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Add profile" }));
    chooseLocalModel("parakeet-unified-en");
    expect((await screen.findByRole("alert")).textContent).toBe(
      "Not enough disk space",
    );
    expect(
      (
        screen.getByRole("button", {
          name: "Save profile",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Download" }));
    await waitFor(() => expect(api.installLocalModel).toHaveBeenCalledTimes(2));
    expect(api.upsertProfile).not.toHaveBeenCalled();
  });
  it("limits TDT v3 to after-recording mode", async () => {
    vi.mocked(api.upsertProfile).mockResolvedValue(saved);
    saved.profiles.push({
      id: "tdt",
      name: "TDT",
      provider: "local",
      unload_after_dictation: false,
      model_id: "parakeet-tdt-v3",
      streaming_mode: "none",
    });
    await ready();
    fireEvent.click(screen.getByRole("button", { name: "Edit TDT" }));
    expect(
      (
        screen.getByRole("combobox", {
          name: "Dictation mode",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    expect(
      screen.getByText("This model transcribes after recording."),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Save profile" }));
    await waitFor(() =>
      expect(api.upsertProfile).toHaveBeenCalledWith({
        revision: "r1",
        id: "tdt",
        name: "TDT",
        provider: "local",
        unload_after_dictation: false,
        model_id: "parakeet-tdt-v3",
        streaming_mode: "none",
      }),
    );
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
    await screen.findByRole("button", { name: "Add profile" });
    expect(api.deleteProfile).toHaveBeenCalledWith("r1", "personal");
  });
});

it("shows managed local models and starts a download", async () => {
  vi.mocked(api.localModels).mockResolvedValue([
    localModel({ state: "not_installed", downloaded_bytes: 0, progress: 0 }),
  ]);
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "Local Models" }));
  await screen.findByRole("heading", { name: "Parakeet Unified English" });
  fireEvent.click(screen.getByRole("button", { name: "Download" }));
  await waitFor(() =>
    expect(api.installLocalModel).toHaveBeenCalledWith("parakeet-unified-en"),
  );
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
    const words = await screen.findByLabelText("Terms");
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
    const textarea = await screen.findByLabelText("Terms");
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
    fireEvent.change(await screen.findByLabelText("Terms"), {
      target: { value: "Unsaved term" },
    });
    act(() =>
      eventHandlers.get("flush-settings")?.({ data: { action: "quit" } }),
    );
    await screen.findByRole("button", { name: "Retry save" });
    expect(api.completeWindowAction).not.toHaveBeenCalled();
    expect(
      (screen.getByLabelText("Terms") as HTMLTextAreaElement).value,
    ).toBe("Unsaved term");
  });
});
it("loads logs only on opening, retains expansion and exports the outcome without search", async () => {
  vi.mocked(api.logs).mockResolvedValue(
    logPage([
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
    ]),
  );
  await ready();
  expect(api.logs).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Logs" }));
  const summary = await screen.findByText("Settings recovered");
  expect(summary.closest("details")).toBeTruthy();
  expect(api.logs).toHaveBeenCalledWith("all", 0);
  expect(screen.queryByLabelText("Search logs")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Export" }));
  await waitFor(() => expect(api.exportLogs).toHaveBeenCalledWith("all"));
});
it("opens the FAQ page from the sidebar", async () => {
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "FAQ" }));
  await screen.findByRole("heading", {
    level: 1,
    name: "FAQ",
  });
  expect(
    screen.getByRole("region", { name: "Common questions" }),
  ).toBeTruthy();
});
it("pauses status polling while the native window is hidden and refreshes when shown", async () => {
  await ready();
  await waitFor(() => expect(api.status).toHaveBeenCalled());
  act(() =>
    eventHandlers.get("window-visibility")?.({ data: false }),
  );
  await new Promise((resolve) => setTimeout(resolve, 100));
  const paused = vi.mocked(api.status).mock.calls.length;
  await new Promise((resolve) => setTimeout(resolve, 700));
  expect(api.status).toHaveBeenCalledTimes(paused);
  act(() => eventHandlers.get("window-visibility")?.({ data: true }));
  await waitFor(() =>
    expect(vi.mocked(api.status).mock.calls.length).toBeGreaterThan(paused),
  );
});

it("keeps inline keyboard capture and immediate login preference saving", async () => {
  await ready();
  fireEvent.click(screen.getByRole("switch", { name: "Start at login" }));
  await waitFor(() =>
    expect(api.preferences).toHaveBeenCalledWith(
      expect.objectContaining({ autostart: true }),
    ),
  );
  fireEvent.click(screen.getByRole("button", { name: "Shortcuts" }));
  const button = await screen.findByRole("button", {
    name: "Change dictation shortcut",
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
  fireEvent.click(screen.getByRole("button", { name: "Shortcuts" }));
  const button = await screen.findByRole("button", {
    name: "Change dictation shortcut",
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
  await waitFor(() =>
    expect(api.upsertProfile).toHaveBeenCalledWith({
      revision: "r1",
      name: "New account",
      provider: "local",
      model_id: "parakeet-unified-en",
      streaming_mode: "process",
      unload_after_dictation: false,
    }),
  );
});

it("offers providers directly and preserves a custom profile name", async () => {
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "Add profile" }));
  fireEvent.click(screen.getByRole("radio", { name: "OpenRouter" }));
  expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe(
    "OpenRouter",
  );
  fireEvent.change(screen.getByLabelText("Name"), {
    target: { value: "Personal router" },
  });
  fireEvent.click(screen.getByRole("radio", { name: "OpenAI" }));
  expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe(
    "Personal router",
  );
});

it("changes an existing profile provider without changing its identity or name", async () => {
  vi.mocked(api.upsertProfile).mockImplementation(async (update) => ({
    ...saved,
    revision: `${saved.revision}+`,
    profiles: saved.profiles.map((profile) =>
      profile.id === update.id
        ? {
            ...profile,
            provider: update.provider,
            model_id: update.model_id,
            streaming_mode: update.streaming_mode,
            languages: update.languages,
          }
        : profile,
    ),
  }));
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "Edit Personal" }));
  fireEvent.click(screen.getByRole("radio", { name: "Local" }));
  expect((screen.getByLabelText("Name") as HTMLInputElement).value).toBe(
    "Personal",
  );
  expect(screen.getByLabelText("Local model")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Save profile" }));
  await waitFor(() =>
    expect(api.upsertProfile).toHaveBeenCalledWith({
      revision: "r1",
      id: "personal",
      name: "Personal",
      provider: "local",
      unload_after_dictation: false,
      model_id: "parakeet-unified-en",
      streaming_mode: "process",
    }),
  );
  await screen.findByText("Local");
  fireEvent.click(screen.getByRole("button", { name: "Edit Personal" }));
  fireEvent.click(screen.getByRole("radio", { name: "OpenAI" }));
  fireEvent.click(screen.getByRole("button", { name: "Save profile" }));
  await waitFor(() =>
    expect(api.upsertProfile).toHaveBeenLastCalledWith({
      revision: "r1+",
      id: "personal",
      name: "Personal",
      provider: "openai",
      streaming_mode: "process",
      languages: [],
    }),
  );
});

it("groups API key actions with each provider and labels missing keys", async () => {
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "API Keys" }));
  const openai = (await screen.findByText("OpenAI")).closest<HTMLDivElement>(
    ".api-key-row",
  )!;
  const openrouter = screen
    .getByText("OpenRouter")
    .closest<HTMLDivElement>(".api-key-row")!;
  for (const row of [openai, openrouter]) {
    expect(row.querySelector(".api-key-heading .api-key-actions")).toBeTruthy();
  }
  expect(within(openai).getByText("sk-p••••1234")).toBeTruthy();
  expect(within(openrouter).getByText("Not added")).toBeTruthy();
  expect(within(openai).getAllByRole("button")).toHaveLength(3);
  expect(within(openrouter).getAllByRole("button")).toHaveLength(1);
  fireEvent.click(within(openai).getByRole("button", { name: "Replace" }));
  expect(screen.getByLabelText("OpenAI API key")).toBeTruthy();
  const actions = within(openai).getByRole("button", {
    name: "Save",
  }).parentElement!;
  expect(within(actions).getByRole("button", { name: "Cancel" })).toBeTruthy();
  fireEvent.click(within(actions).getByRole("button", { name: "Cancel" }));
  expect(within(openai).getByText("sk-p••••1234")).toBeTruthy();
});

it("adds, copies and deletes provider API keys in the sidebar page", async () => {
  vi.mocked(api.saveAPIKey).mockResolvedValue({
    ...saved,
    api_keys: {
      ...saved.api_keys,
      openrouter: {
        saved: true,
        masked_key: "sk-o••••9876",
      },
    },
  });
  vi.mocked(api.deleteAPIKey).mockResolvedValue({
    ...saved,
    api_keys: {
      ...saved.api_keys,
      openai: { saved: false },
    },
  });
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "API Keys" }));
  await screen.findByText("sk-p••••1234");
  fireEvent.click(
    within(screen.getByText("OpenRouter").closest(".api-key-row")!)
      .getByRole("button", { name: "Add" }),
  );
  fireEvent.change(screen.getByLabelText("OpenRouter API key"), {
    target: { value: "sk-or-new-9876" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() =>
    expect(api.saveAPIKey).toHaveBeenCalledWith(
      "openrouter",
      "sk-or-new-9876",
    ),
  );
  await screen.findByText("sk-o••••9876");
  fireEvent.click(screen.getByRole("button", { name: "Copy OpenAI API key" }));
  await waitFor(() => expect(api.copyAPIKey).toHaveBeenCalledWith("openai"));
  fireEvent.click(
    screen.getByRole("button", { name: "Delete OpenAI API key" }),
  );
  const confirm = screen.getByRole("dialog", {
    name: "Delete OpenAI API key?",
  });
  fireEvent.click(within(confirm).getByRole("button", { name: "Delete key" }));
  await waitFor(() =>
    expect(api.deleteAPIKey).toHaveBeenCalledWith("openai"),
  );
  expect(screen.getAllByRole("button", { name: "Add" })).toHaveLength(2);
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

it("loads older logs below existing entries and keeps their expansion", async () => {
  const ok: LogRecord = {
    event: "dictation.completed",
    outcome: "ok",
    timestamp: "2026-11-06T10:00:00Z",
    provider: "openai",
    configured_model: "whisper-1",
    total_ms: 1000,
    audio_ms: 900,
    attempt_count: 1,
    attempts: [],
  };
  const failed: LogRecord = {
    ...ok,
    event: "startup.failed",
    outcome: "error",
    timestamp: "2026-11-06T09:00:00Z",
  };
  vi.mocked(api.logs).mockImplementation(async (_outcome, offset) =>
    offset === 0
      ? logPage([ok], { revision: "first", limit: 1, total: 2, retained: 2 })
      : logPage([failed], {
          revision: "first",
          offset,
          limit: 1,
          total: 2,
          retained: 2,
        }),
  );
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "Logs" }));
  const title = await screen.findByText("Dictation completed");
  const details = title.closest("details")!;
  fireEvent.click(title.closest("summary")!);
  const more = screen.getByRole("button", { name: "Load more" });
  expect(
    document.querySelector(".log-list")!.nextElementSibling!.contains(more),
  ).toBe(true);
  expect(screen.queryByRole("button", { name: "Older" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Newer" })).toBeNull();
  fireEvent.click(more);
  await screen.findByText("Startup failed");
  expect(api.logs).toHaveBeenCalledWith("all", 1);
  expect(screen.getByText("Dictation completed").closest("details")).toBe(
    details,
  );
  expect(details.open).toBe(true);
  expect(screen.getByText("Showing 2 of 2")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
});

it("keeps expanded logs open when polling prepends another event", async () => {
  const old: LogRecord = {
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
  vi.mocked(api.logs).mockResolvedValue(logPage([old], { revision: "a" }));
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "Logs" }));
  const title = await screen.findByText("Dictation completed");
  const details = title.closest("details")!;
  fireEvent.click(title.closest("summary")!);
  expect(details.open).toBe(true);
  expect(
    within(details).getByText("Technical details").closest("details")!.open,
  ).toBe(false);
  const failure: LogRecord = {
    ...old,
    event: "startup.failed",
    outcome: "error",
    timestamp: "2026-11-06T11:00:00Z",
  };
  vi.mocked(api.logs).mockResolvedValue(
    logPage([failure, old], { revision: "b", total: 2 }),
  );
  await screen.findByText("Startup failed", {}, { timeout: 3000 });
  expect(screen.getByText("Dictation completed").closest("details")).toBe(
    details,
  );
  expect(details.open).toBe(true);
  vi.mocked(api.logs).mockResolvedValue(
    logPage([failure], { revision: "c", outcome: "errors" }),
  );
  fireEvent.click(screen.getByRole("radio", { name: "Errors" }));
  await waitFor(() => expect(api.logs).toHaveBeenCalledWith("errors", 0));
  await waitFor(() =>
    expect(screen.queryByText("Dictation completed")).toBeNull(),
  );
  fireEvent.click(screen.getByRole("button", { name: "Export" }));
  await waitFor(() => expect(api.exportLogs).toHaveBeenCalledWith("errors"));
});

it("keeps vocabulary newline editing and makes renaming explicit", async () => {
  Element.prototype.scrollIntoView = vi.fn();
  saved.vocabulary.push({ name: "Medical", terms: ["Alpha", "Beta"] });
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "Vocabulary" }));
  const picker = await screen.findByRole("combobox", {
    name: "Vocabulary to edit",
  });
  fireEvent.keyDown(picker, { key: "ArrowDown" });
  fireEvent.click(await screen.findByRole("option", { name: /Medical/ }));
  await screen.findByRole("heading", { name: "Medical" });
  expect((screen.getByLabelText("Terms") as HTMLTextAreaElement).value).toBe(
    "Alpha\nBeta",
  );
  expect(screen.queryByRole("textbox", { name: "Vocabulary name" })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Rename vocabulary" }));
  const name = await screen.findByRole("textbox", { name: "Vocabulary name" });
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
  const renamed = await screen.findByRole("textbox", {
    name: "Vocabulary name",
  });
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
  fireEvent.click(screen.getByRole("button", { name: "Shortcuts" }));
  fireEvent.click(
    await screen.findByRole("button", { name: "Reset" }),
  );
  await waitFor(() =>
    expect(api.preferences).toHaveBeenCalledWith(
      expect.objectContaining({ shortcut: "Ctrl+Space" }),
    ),
  );
  await waitFor(() =>
    expect(
      screen.queryByRole("button", { name: "Reset" }),
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

it("shows microphone state and blocks a test while recording", async () => {
  vi.mocked(api.status).mockResolvedValue({
    state: "recording",
    error: "",
    level: 0,
  });
  await ready();
  fireEvent.click(screen.getByRole("button", { name: "Audio" }));
  const test = await screen.findByRole("button", { name: "Test" });
  expect((test as HTMLButtonElement).disabled).toBe(true);
  expect(screen.getByText("Stop recording first.")).toBeTruthy();
  vi.mocked(api.status).mockResolvedValue({
    state: "testing",
    error: "",
    level: 0,
  });
  await screen.findByText("Listening…");
  expect(screen.getByRole("button", { name: "Stop" })).toBeTruthy();
});


it("shows preparation distinctly from idle and recording", async () => {
  vi.mocked(api.status).mockResolvedValue({state: "checking", error: "", level: 0});
  await ready();
  expect(await screen.findByText("Preparing")).toBeTruthy();
});
