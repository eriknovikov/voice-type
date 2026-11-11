import {
  Download,
  Pause,
  Play,
  RotateCcw,
  Trash2,
  X,
} from "lucide-react";
import type { LocalModel } from "./api";

const stateLabel: Record<LocalModel["state"], string> = {
  not_installed: "Not installed",
  downloading: "Downloading",
  pausing: "Pausing…",
  paused: "Paused",
  verifying: "Verifying",
  ready: "Ready",
  failed: "Failed",
  cancelling: "Cancelling…",
};

const formatSize = (bytes: number) => `${(bytes / 1_000_000).toFixed(0)} MB`;
const formatRAM = (bytes: number) =>
  `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GiB`;

export function LocalModels({
  models,
  busy,
  onInstall,
  onResume,
  onRetry,
  onPause,
  onCancel,
  onRemove,
}: {
  models: LocalModel[];
  busy: boolean;
  onInstall: (id: string) => void;
  onResume: (id: string) => void;
  onRetry: (id: string) => void;
  onPause: (id: string) => void;
  onCancel: (id: string) => void;
  onRemove: (id: string) => void;
}) {
  return (
    <section className="settings-section local-models">
      {models.map((model) => (
        <article className="settings-group local-model-card" key={model.id}>
          <div className="local-model-heading">
            <div>
              <h2>{model.name}</h2>
              <p className="quiet">
                {model.languages} · {formatSize(model.bytes)}
              </p>
              <p
                className="quiet"
                title="Approximate resident memory while the model is loaded"
              >
                ~{formatRAM(model.ram_bytes)} RAM when loaded
              </p>
            </div>
            {model.state !== "ready" && model.state !== "not_installed" && (
              <span className={`badge model-state ${model.state}`} role="status">
                {stateLabel[model.state]}
              </span>
            )}
          </div>
          {(model.state === "downloading" ||
            model.state === "pausing" ||
            model.state === "verifying") && (
            <div className="local-model-progress">
              <progress
                aria-label={`${model.name} download progress`}
                max={100}
                value={model.state === "verifying" ? 100 : model.progress}
              />
              {model.state !== "verifying" && (
                <span>
                  {formatSize(model.downloaded_bytes)} of{" "}
                  {formatSize(model.bytes)}
                </span>
              )}
            </div>
          )}
          {model.error && (
            <p className="field-error model-error" role="alert">
              {model.error}
            </p>
          )}
          <div className="actions local-model-actions">
            {model.state === "not_installed" && (
              <button disabled={busy} onClick={() => onInstall(model.id)}>
                <Download size={15} aria-hidden="true" /> Download
              </button>
            )}
            {model.state === "downloading" && (
              <>
                <button disabled={busy} onClick={() => onPause(model.id)}>
                  <Pause size={15} aria-hidden="true" /> Pause
                </button>
                <button disabled={busy} onClick={() => onCancel(model.id)}>
                  <X size={15} aria-hidden="true" /> Cancel
                </button>
              </>
            )}
            {model.state === "paused" && (
              <>
                <button disabled={busy} onClick={() => onResume(model.id)}>
                  <Play size={15} aria-hidden="true" /> Resume
                </button>
                <button disabled={busy} onClick={() => onCancel(model.id)}>
                  <X size={15} aria-hidden="true" /> Cancel
                </button>
              </>
            )}
            {model.state === "failed" && (
              <>
                <button disabled={busy} onClick={() => onRetry(model.id)}>
                  <RotateCcw size={15} aria-hidden="true" /> Retry
                </button>
                {model.downloaded_bytes > 0 && (
                  <button disabled={busy} onClick={() => onCancel(model.id)}>
                    <X size={15} aria-hidden="true" /> Cancel
                  </button>
                )}
              </>
            )}
            {model.state === "ready" && (
              <button
                className="danger-text"
                disabled={busy}
                onClick={() => onRemove(model.id)}
              >
                <Trash2 size={15} aria-hidden="true" /> Remove
              </button>
            )}
          </div>
        </article>
      ))}
      {!models.length && <p className="quiet">Loading…</p>}
    </section>
  );
}
