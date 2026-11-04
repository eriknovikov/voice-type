// @vitest-environment jsdom
import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  render,
  screen,
  waitFor,
  fireEvent,
} from "@testing-library/react";
import { App } from "./App";
import { api, type Settings } from "./api";
vi.mock("@wailsio/runtime", () => ({
  Call: { ByName: vi.fn() },
  Events: { On: () => () => {} },
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
      shortcut: "Ctrl+Alt+Space",
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
    await screen.findByText("Speak naturally.");
    expect(api.logs).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Logs" }));
    await screen.findByText("Nothing to troubleshoot");
    expect(api.logs).toHaveBeenCalledTimes(1);
  });
  it("sends only editable settings and new credentials, without read-only fields", async () => {
    vi.mocked(api.save).mockResolvedValue({ ...settings, revision: "r2" });
    render(<App />);
    await screen.findByText("Speak naturally.");
    fireEvent.click(screen.getByRole("button", { name: "Settings" }));
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
    await screen.findByText(
      "Settings saved. Changes apply to your next dictation.",
    );
    expect((screen.getByLabelText("API key") as HTMLInputElement).value).toBe(
      "",
    );
  });
  it("preserves unsaved edits when a conflicting save is rejected", async () => {
    vi.mocked(api.save).mockRejectedValue(
      new Error("settings changed elsewhere; reload before saving"),
    );
    render(<App />);
    await screen.findByText("Speak naturally.");
    fireEvent.click(screen.getByRole("button", { name: "Settings" }));
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
    await screen.findByText("Speak naturally.");
    fireEvent.click(screen.getByRole("button", { name: "Audio & shortcuts" }));
    await screen.findByText("Test microphone");
    fireEvent.click(screen.getByRole("button", { name: "Overview" }));
    await waitFor(() => expect(api.stopTest).toHaveBeenCalled());
  });
});
