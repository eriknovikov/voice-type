// @vitest-environment jsdom
import React from "react";
import {
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LocalModels } from "./LocalModels";
import type { LocalModel } from "./api";

afterEach(cleanup);

const model = (
  state: LocalModel["state"],
  downloadedBytes = 0,
): LocalModel => ({
  id: "parakeet-unified-en",
  name: "Parakeet Unified English",
  bytes: 731_357_568,
  downloaded_bytes: downloadedBytes,
  progress: (downloadedBytes / 731_357_568) * 100,
  languages: "English",
  supports_streaming: true,
  license: "NVIDIA Open Model License",
  license_url: "https://example.test/license",
  model_card_url: "https://example.test/model",
  state,
  error: "",
});

const renderModel = (
  value: LocalModel,
  callbacks: Partial<{
    onInstall: (id: string) => void;
    onResume: (id: string) => void;
    onRetry: (id: string) => void;
    onPause: (id: string) => void;
    onCancel: (id: string) => void;
    onRemove: (id: string) => void;
  }> = {},
) =>
  render(
    <LocalModels
      models={[value]}
      busy={false}
      onInstall={callbacks.onInstall ?? vi.fn()}
      onResume={callbacks.onResume ?? vi.fn()}
      onRetry={callbacks.onRetry ?? vi.fn()}
      onPause={callbacks.onPause ?? vi.fn()}
      onCancel={callbacks.onCancel ?? vi.fn()}
      onRemove={callbacks.onRemove ?? vi.fn()}
    />,
  );

describe("local model download controls", () => {
  it("starts an uninstalled model download", () => {
    const onInstall = vi.fn();
    renderModel(model("not_installed"), { onInstall });
    fireEvent.click(screen.getByRole("button", { name: "Download" }));
    expect(onInstall).toHaveBeenCalledWith("parakeet-unified-en");
  });

  it("shows progress and exposes pause and cancel while downloading", () => {
    const onPause = vi.fn();
    const onCancel = vi.fn();
    renderModel(model("downloading", 100_000_000), { onPause, onCancel });
    expect(
      (screen.getByRole("progressbar", {
        name: "Parakeet Unified English download progress",
      }) as HTMLProgressElement).value,
    ).toBeCloseTo((100_000_000 / 731_357_568) * 100);
    fireEvent.click(screen.getByRole("button", { name: "Pause" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onPause).toHaveBeenCalledWith("parakeet-unified-en");
    expect(onCancel).toHaveBeenCalledWith("parakeet-unified-en");
  });

  it("exposes resume and cancel while paused", () => {
    const onResume = vi.fn();
    const onCancel = vi.fn();
    renderModel(model("paused", 100_000_000), { onResume, onCancel });
    fireEvent.click(screen.getByRole("button", { name: "Resume" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onResume).toHaveBeenCalledWith("parakeet-unified-en");
    expect(onCancel).toHaveBeenCalledWith("parakeet-unified-en");
  });

  it("exposes retry and partial-download cancellation after failure", () => {
    const onRetry = vi.fn();
    const onCancel = vi.fn();
    renderModel(model("failed", 100_000_000), { onRetry, onCancel });
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel download" }));
    expect(onRetry).toHaveBeenCalledWith("parakeet-unified-en");
    expect(onCancel).toHaveBeenCalledWith("parakeet-unified-en");
  });

  it("removes an installed model", () => {
    const onRemove = vi.fn();
    renderModel(model("ready", 731_357_568), { onRemove });
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    expect(onRemove).toHaveBeenCalledWith("parakeet-unified-en");
  });
});
