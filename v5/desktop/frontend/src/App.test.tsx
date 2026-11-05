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
import { api, type Settings } from "./api";
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
    connect: vi.fn(),
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
  },
}));
const settings: Settings = {
  revision: "r1",
  provider: "openai",
  port: 3232,
  microphone: "",
  vocabulary: [{ name: "general", terms: ["Wails"] }],
  workspace: "",
  credentials: {
    openai: { saved: true, environment: false },
    openrouter: { saved: false, environment: false },
  },
};
beforeEach(() => {
  vi.clearAllMocks();
  eventHandlers.clear();
  vi.mocked(api.beginShortcutCapture).mockResolvedValue();
  vi.mocked(api.endShortcutCapture).mockResolvedValue();
  vi.mocked(api.settings).mockResolvedValue(structuredClone(settings));
  vi.mocked(api.connect).mockResolvedValue();
  vi.mocked(api.status).mockResolvedValue({
    state: "idle",
    error: "",
    port: 3232,
    level: 0,
  });
  vi.mocked(api.info).mockResolvedValue({
    preferences: {
      shortcut: "Ctrl+Space",
      daemon_shortcut: "Ctrl+Alt+Space",
      autostart: false,
      setup_complete: true,
    },
    tray_available: true,
    shortcut_error: "",
    version: "test",
  });
  vi.mocked(api.logs).mockResolvedValue([]);
  vi.mocked(api.microphones).mockResolvedValue([]);
  vi.mocked(api.stopTest).mockResolvedValue();
});
afterEach(cleanup);
describe("desktop workflows", () => {
  it("loads diagnostics only when the logs section is opened", async () => {
    render(<App />);
    await screen.findByText("Dictation");
    expect(api.logs).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Logs" }));
    await screen.findByText("No logs yet");
    expect(api.logs).toHaveBeenCalledTimes(1);
  });
  it("sends only editable settings and new credentials, without read-only fields", async () => {
    vi.mocked(api.save).mockResolvedValue({ ...settings, revision: "r2" });
    render(<App />);
    await screen.findByText("Dictation");
    fireEvent.click(screen.getByRole("button", { name: "General" }));
    fireEvent.change(screen.getByLabelText("API key"), {
      target: { value: "replacement-key" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() =>
      expect(api.save).toHaveBeenCalledWith({
        revision: "r1",
        provider: "openai",
        port: 3232,
        microphone: "",
        vocabulary: settings.vocabulary,
        api_keys: { openai: "replacement-key" },
      }),
    );
    await screen.findByText("Saved. Changes apply to the next dictation.");
    expect((screen.getByLabelText("API key") as HTMLInputElement).value).toBe(
      "",
    );
  });
  it("preserves unsaved edits when a conflicting save is rejected", async () => {
    vi.mocked(api.save).mockRejectedValue(
      new Error("settings changed elsewhere; reload before saving"),
    );
    render(<App />);
    await screen.findByText("Dictation");
    fireEvent.click(screen.getByRole("button", { name: "General" }));
    fireEvent.change(screen.getByLabelText("Local control port"), {
      target: { value: "4242" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await screen.findByRole("alert");
    expect(
      (screen.getByLabelText("Local control port") as HTMLInputElement).value,
    ).toBe("4242");
    expect(screen.getByText("Unsaved changes")).toBeTruthy();
  });
  it("stops the microphone test when navigating away from audio", async () => {
    render(<App />);
    await screen.findByText("Dictation");
    fireEvent.click(screen.getByRole("button", { name: "Audio" }));
    await screen.findByText("Test microphone");
    fireEvent.click(screen.getByRole("button", { name: "Overview" }));
    await waitFor(() => expect(api.stopTest).toHaveBeenCalled());
  });
});

it("tray events navigate to settings and update workspace without discarding edits", async () => {
  render(<App />);
  await screen.findByText("Dictation");
  act(() => eventHandlers.get("navigate")?.({ data: ["settings"] }));
  const port = screen.getByLabelText("Local control port") as HTMLInputElement;
  fireEvent.change(port, { target: { value: "4242" } });
  act(() => eventHandlers.get("workspace-changed")?.({ data: ["project"] }));
  expect(port.value).toBe("4242");
  fireEvent.click(screen.getByRole("button", { name: "Overview" }));
  expect(screen.getByRole("heading", { name: "project" })).toBeTruthy();
  expect(screen.getByText("Unsaved changes")).toBeTruthy();
});

it("captures a shortcut interactively without activating dictation", async () => {
  render(<App />);
  await screen.findByText("Dictation");
  fireEvent.click(screen.getByRole("button", { name: "Keyboard Shortcuts" }));
  fireEvent.click(
    screen.getByRole("button", {
      name: "Change transcription toggle shortcut",
    }),
  );
  await screen.findByText("Press the keys you want to use.");
  expect(document.activeElement).toBe(
    screen.getByRole("group", { name: "Shortcut keys" }),
  );
  fireEvent.keyDown(screen.getByRole("group", { name: "Shortcut keys" }), {
    key: " ",
    ctrlKey: true,
    shiftKey: true,
  });
  fireEvent.click(screen.getByRole("button", { name: "Set shortcut" }));
  await waitFor(() => expect(api.endShortcutCapture).toHaveBeenCalled());
  expect(
    screen.getByRole("button", { name: "Change transcription toggle shortcut" })
      .textContent,
  ).toBe("Ctrl + Shift + Space");
});
