import {
  CheckCircle2,
  Download,
  LoaderCircle,
  Pause,
  Play,
  RotateCcw,
  Trash2,
  X,
} from "lucide-react";
import type { LocalModel } from "./api";

export const modelStateLabel: Record<LocalModel["state"], string> = {
  not_installed: "Not installed",
  downloading: "Downloading",
  pausing: "Pausing…",
  paused: "Paused",
  verifying: "Verifying",
  ready: "Installed",
  failed: "Failed",
  cancelling: "Cancelling…",
};

export const formatModelSize = (bytes: number) =>
  `${(bytes / 1_000_000).toFixed(0)} MB`;

export type LocalModelActions = {
  onInstall: (id: string) => void;
  onResume: (id: string) => void;
  onRetry: (id: string) => void;
  onPause: (id: string) => void;
  onCancel: (id: string) => void;
  onRemove?: (id: string) => void;
};

export function LocalModelDownload({
  model,
  busy,
  pendingAction,
  error,
  onInstall,
  onResume,
  onRetry,
  onPause,
  onCancel,
  onRemove,
}: {
  model?: LocalModel;
  busy: boolean;
  pendingAction?: keyof LocalModelActions | null;
  error?: string;
} & LocalModelActions) {
  const state = model?.state ?? "not_installed";
  const ready = state === "ready";
  const progress =
    ready || state === "verifying" ? 100 : (model?.progress ?? 0);
  const failure = error || model?.error;
  const actions = {
    not_installed: { label: "Download", icon: Download, run: onInstall },
    downloading: { label: "Pause", icon: Pause, run: onPause },
    paused: { label: "Resume", icon: Play, run: onResume },
    failed: { label: "Retry", icon: RotateCcw, run: onRetry },
    ready: { label: "Remove", icon: Trash2, run: onRemove },
    pausing: { label: "Pausing…", icon: LoaderCircle, run: undefined },
    verifying: { label: "Verifying…", icon: LoaderCircle, run: undefined },
    cancelling: { label: "Cancelling…", icon: LoaderCircle, run: undefined },
  };
  const action = actions[state];
  const pendingLabels = {
    onInstall: "Starting download…",
    onResume: "Resuming…",
    onRetry: "Retrying…",
    onPause: "Pausing…",
    onCancel: "Cancelling…",
    onRemove: "Removing…",
  };
  const pendingLabel = pendingAction ? pendingLabels[pendingAction] : "";
  const Icon = pendingAction ? LoaderCircle : action.icon;
  const canCancel =
    !!model &&
    (state === "downloading" ||
      state === "paused" ||
      (state === "failed" && model.downloaded_bytes > 0));
  const messages = {
    not_installed: "Download required before use.",
    downloading: "Continues in the background.",
    paused: "Download progress is saved.",
    failed: "The download could not finish.",
    ready: "Verified and available offline.",
    pausing: "Saving your download progress…",
    verifying: "Checking the downloaded file…",
    cancelling: "Removing the partial download…",
  };
  return (
    <div className="local-model-download" data-state={model ? state : "empty"}>
      <div className="model-download-summary">
        <span className="model-download-status" role="status">
          {!model
            ? "No model selected"
            : pendingLabel || modelStateLabel[state]}
        </span>
        <span className="model-download-size" aria-hidden="true">
          {!model
            ? "—"
            : ready
              ? formatModelSize(model.bytes)
              : `${formatModelSize(model.downloaded_bytes)} / ${formatModelSize(model.bytes)} · ${Math.round(progress)}%`}
        </span>
      </div>
      <progress
        aria-label={`${model?.name ?? "Model"} download progress`}
        max={100}
        value={progress}
        aria-hidden={!model}
      />
      <div className="model-download-bottom">
        <div
          className={`model-download-message${failure ? " field-error" : ""}`}
          role={failure ? "alert" : undefined}
          tabIndex={failure ? 0 : undefined}
        >
          {failure ||
            (!model
              ? "Download sizes are shown in the list."
              : messages[state])}
        </div>
        <div className="model-download-actions">
          {ready && !onRemove ? (
            <span className="model-download-ready">
              <CheckCircle2 size={16} aria-hidden="true" /> Ready
            </span>
          ) : (
            <button
              type="button"
              className={`model-download-primary${ready ? " danger-text" : ""}`}
              disabled={busy || !model || !action.run}
              onClick={() => {
                if (model) action.run?.(model.id);
              }}
            >
              <Icon size={15} aria-hidden="true" />{" "}
              {pendingAction === "onInstall"
                ? "Starting…"
                : pendingLabel || action.label}
            </button>
          )}
          <button
            type="button"
            className="icon-button model-download-cancel"
            aria-label="Cancel download"
            title="Cancel download"
            disabled={busy || !canCancel}
            hidden={!canCancel}
            onClick={() => {
              if (model) onCancel(model.id);
            }}
          >
            <X size={16} aria-hidden="true" />
          </button>
        </div>
      </div>
    </div>
  );
}
