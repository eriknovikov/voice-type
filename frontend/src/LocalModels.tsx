import {
  Check,
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
      <p className="quiet local-models-intro">
        Models are stored privately on this device. Audio selected for a local
        profile stays on-device; a local failure never switches to a cloud
        provider. Downloaded models are checksum-verified before use.
      </p>
      {models.map((model) => (
        <article className="settings-group local-model-card" key={model.id}>
          <div className="local-model-heading">
            <div>
              <h2>{model.name}</h2>
              <p className="quiet">
                {model.languages} · {formatSize(model.bytes)} download
              </p>
            </div>
            <span className={`badge model-state ${model.state}`} role="status">
              {model.state === "ready" && <Check size={14} aria-hidden="true" />}
              {stateLabel[model.state]}
            </span>
          </div>
          <div className="local-model-capabilities">
            <span>
              {model.supports_streaming
                ? "Streaming: process while recording, live insertion, or no streaming"
                : "Batch only: transcribe after recording stops"}
            </span>
            <span>Saved vocabulary supported</span>
            <span>
              License: {" "}
              <a href={model.license_url} target="_blank" rel="noreferrer">
                {model.license}
              </a>
            </span>
            <span>
              Model card: {" "}
              <a href={model.model_card_url} target="_blank" rel="noreferrer">
                pinned source
              </a>
            </span>
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
              <span>
                {model.state === "verifying"
                  ? "Checking size and SHA-256…"
                  : `${formatSize(model.downloaded_bytes)} of ${formatSize(model.bytes)}`}
              </span>
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
                    <X size={15} aria-hidden="true" /> Cancel download
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
      {!models.length && <p className="quiet">Loading local model catalog…</p>}
      <p className="quiet local-models-footnote">
        Downloads resume only when the server confirms the same pinned artifact.
        Model weights are not included in the application installer.
      </p>
    </section>
  );
}
