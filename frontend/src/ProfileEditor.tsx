import { useEffect, useRef, useState, type RefObject } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { Trash2, X } from "lucide-react";
import type { APIKeySummary, ProfileSummary, ProfileUpdate } from "./api";
import { Dropdown } from "./Dropdown";
import {
  LocalModelDownload,
  formatModelSize,
  modelStateLabel,
} from "./LocalModelDownload";
import type { LocalModelDownloads } from "./useLocalModels";
import { ConfirmDialog, SegmentedControl } from "./ui";

export const providerName = (provider: string) =>
  provider === "openai"
    ? "OpenAI"
    : provider === "openrouter"
      ? "OpenRouter"
      : provider === "local"
        ? "Local"
        : provider;
const unifiedModel = "parakeet-unified-en";
const nextName = (baseName: string, profiles: ProfileSummary[]) => {
  let name = baseName;
  let suffix = 2;
  while (
    profiles.some(
      (profile) => profile.name.toLowerCase() === name.toLowerCase(),
    )
  )
    name = `${baseName} ${suffix++}`;
  return name;
};
// Providers own their fields; credentials are not an invariant of a profile.
const providerForms = {
  local: { label: "Local", requiresKey: false },
  openai: { label: "OpenAI", requiresKey: true },
  openrouter: { label: "OpenRouter", requiresKey: true },
};
type ProviderDraft = {
  modelID: string;
  streamingMode: string;
  languages: string;
  unloadAfter: boolean;
};
export function ProfileEditor({
  profile,
  profiles,
  apiKeys,
  modelDownloads,
  onSave,
  onDelete,
  onClose,
  closeGuard,
}: {
  profile: ProfileSummary | null;
  profiles: ProfileSummary[];
  apiKeys: Record<string, APIKeySummary>;
  modelDownloads: LocalModelDownloads;
  onSave: (update: Omit<ProfileUpdate, "revision">) => Promise<void>;
  onDelete: () => Promise<void>;
  onClose: () => void;
  closeGuard: RefObject<(() => Promise<boolean>) | null>;
}) {
  const {
    models: localModels,
    busy: modelBusy,
    actions: modelActions,
  } = modelDownloads;
  const modelSupportsStreaming = (modelID: string) =>
    localModels.find((model) => model.id === modelID)?.supports_streaming ??
    modelID === unifiedModel;
  const defaultMode = (modelID: string) =>
    modelSupportsStreaming(modelID) ? "process" : "none";
  const suggestName = (provider: string, modelID: string) =>
    nextName(
      provider === "local"
        ? (localModels.find((model) => model.id === modelID)?.name ?? "Local")
        : providerName(provider),
      profiles,
    );
  const initialProvider = profile?.provider ?? "local";
  const [provider, setProvider] = useState(initialProvider);
  const initialLanguages = (profile?.languages ?? []).join(", ");
  const localModelID =
    profile?.provider === "local"
      ? (profile.model_id ?? unifiedModel)
      : (localModels.find((model) => model.state === "ready")?.id ?? "");
  const initialName = useRef(
    profile?.name ?? suggestName(initialProvider, localModelID),
  );
  const [name, setName] = useState(initialName.current);
  const autoName = useRef(profile ? null : initialName.current);
  const localMode =
    profile?.provider === "local"
      ? (profile.streaming_mode ?? defaultMode(localModelID))
      : defaultMode(localModelID);
  const initialUnload =
    profile?.provider === "local"
      ? (profile.unload_after_dictation ?? false)
      : false;
  const initialDrafts = useRef<Record<string, ProviderDraft>>({
    openai: {
      modelID: unifiedModel,
      streamingMode:
        profile?.provider === "openai"
          ? (profile.streaming_mode ?? "none")
          : "process",
      languages: initialLanguages,
      unloadAfter: false,
    },
    openrouter: {
      modelID: unifiedModel,
      streamingMode: "none",
      languages: "",
      unloadAfter: false,
    },
    local: {
      modelID: localModelID,
      streamingMode: localMode,
      languages: "",
      unloadAfter: initialUnload,
    },
  });
  const providerDrafts = useRef(structuredClone(initialDrafts.current));
  const startingDraft = initialDrafts.current[initialProvider];
  const [modelID, setModelID] = useState(startingDraft.modelID);
  const [streamingMode, setStreamingMode] = useState(
    startingDraft.streamingMode,
  );
  const [languages, setLanguages] = useState(startingDraft.languages);
  const [unloadAfter, setUnloadAfter] = useState(startingDraft.unloadAfter);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [nameError, setNameError] = useState("");
  const [deleting, setDeleting] = useState(false);
  const [discarding, setDiscarding] = useState(false);
  const updateAutoName = (provider: string, modelID: string) => {
    if (autoName.current === null) return;
    autoName.current = suggestName(provider, modelID);
    setName(autoName.current);
    setNameError("");
  };
  const selectedModel = localModels.find((model) => model.id === modelID);
  const localModelReady =
    provider !== "local" || selectedModel?.state === "ready";
  const selectedRAM = selectedModel?.ram_bytes ?? 0;
  const ramGiB = selectedRAM
    ? `~${(selectedRAM / (1024 * 1024 * 1024)).toFixed(1)} GiB`
    : "memory";
  const nameInput = useRef<HTMLInputElement>(null);
  const alive = useRef(true);
  const dismiss = useRef<{
    promise: Promise<boolean>;
    resolve: (value: boolean) => void;
  } | null>(null);
  const previousFocus = useRef(document.activeElement);
  const dirty =
    (profile ? name !== initialName.current : name !== autoName.current) ||
    provider !== initialProvider ||
    (provider === "local" &&
      (modelID !== initialDrafts.current.local.modelID ||
        streamingMode !== initialDrafts.current.local.streamingMode ||
        unloadAfter !== initialDrafts.current.local.unloadAfter)) ||
    (provider === "openai" &&
      (streamingMode !== initialDrafts.current.openai.streamingMode ||
        languages !== initialDrafts.current.openai.languages));
  const requestClose = (): Promise<boolean> => {
    if (busy || deleting) return Promise.resolve(false);
    if (!dirty) {
      onClose();
      return Promise.resolve(true);
    }
    if (dismiss.current) return dismiss.current.promise;
    let resolve!: (value: boolean) => void;
    const promise = new Promise<boolean>((done) => {
      resolve = done;
    });
    dismiss.current = { promise, resolve };
    setDiscarding(true);
    return promise;
  };
  useEffect(() => {
    closeGuard.current = requestClose;
    return () => {
      closeGuard.current = null;
    };
  });
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      dismiss.current?.resolve(false);
    };
  }, []);
  const definition = providerForms[provider as keyof typeof providerForms];
  const keySummary = apiKeys[provider];
  const validateName = () => {
    const message = !name.trim()
      ? "Enter a profile name."
      : profiles.some(
            (item) =>
              item.id !== profile?.id &&
              item.name.toLowerCase() === name.trim().toLowerCase(),
          )
        ? "Choose a unique profile name."
        : "";
    setNameError(message);
    return !message;
  };
  const settleDismiss = (discard: boolean) => {
    const pending = dismiss.current;
    dismiss.current = null;
    setDiscarding(false);
    if (discard) onClose();
    pending?.resolve(discard);
  };
  const saveHint =
    provider !== "local"
      ? "Changes apply after saving."
      : !selectedModel
        ? "Choose a model to continue."
        : localModelReady
          ? "Ready for offline transcription."
          : selectedModel.state === "paused"
            ? "Resume the download to finish setup."
            : selectedModel.state === "failed"
              ? "Retry the download to finish setup."
              : selectedModel.state === "verifying"
                ? "Verifying your model…"
                : "You can save when the model is ready.";
  return (
    <>
      <Dialog.Root
        open
        onOpenChange={(open) => {
          if (!open) void requestClose();
        }}
      >
        <Dialog.Portal>
          <Dialog.Overlay className="dialog-overlay" />
          <Dialog.Content
            className="dialog profile-dialog"
            aria-describedby="profile-description"
            onOpenAutoFocus={(event) => {
              event.preventDefault();
              nameInput.current?.focus();
              if (!profile) nameInput.current?.select();
            }}
            onCloseAutoFocus={(event) => {
              event.preventDefault();
              if (previousFocus.current instanceof HTMLElement)
                previousFocus.current.focus();
            }}
            onEscapeKeyDown={(event) => {
              event.preventDefault();
              if (!discarding && !deleting) void requestClose();
            }}
            onInteractOutside={(event) => {
              event.preventDefault();
              if (!discarding && !deleting) void requestClose();
            }}
          >
            <div className="profile-dialog-header">
              <div className="profile-dialog-titlebar">
                <Dialog.Title>
                  {profile ? "Edit profile" : "New profile"}
                </Dialog.Title>
                <div className="profile-header-actions">
                  {profile && (
                    <button
                      type="button"
                      className="icon-button danger-text"
                      aria-label="Delete profile"
                      title="Delete profile"
                      disabled={busy}
                      onClick={() => setDeleting(true)}
                    >
                      <Trash2 size={17} aria-hidden="true" />
                    </button>
                  )}
                  <button
                    type="button"
                    className="icon-button"
                    aria-label="Close profile editor"
                    title="Close profile editor"
                    disabled={busy}
                    onClick={() => void requestClose()}
                  >
                    <X size={18} aria-hidden="true" />
                  </button>
                </div>
              </div>
              <Dialog.Description id="profile-description" className="sr-only">
                Choose a provider and configure how this profile transcribes
                your speech.
              </Dialog.Description>
              <div className="profile-provider">
                <SegmentedControl
                  label="Provider"
                  value={provider}
                  disabled={busy}
                  options={Object.entries(providerForms).map(
                    ([value, form]) => ({ value, label: form.label }),
                  )}
                  onChange={(value) => {
                    providerDrafts.current[provider] = {
                      modelID,
                      streamingMode,
                      languages,
                      unloadAfter,
                    };
                    setProvider(value);
                    const next = providerDrafts.current[value];
                    setModelID(next.modelID);
                    setStreamingMode(next.streamingMode);
                    setLanguages(next.languages);
                    setUnloadAfter(next.unloadAfter);
                    updateAutoName(value, next.modelID);
                  }}
                />
              </div>
            </div>
            <form
              className="profile-dialog-form"
              noValidate
              onSubmit={async (event) => {
                event.preventDefault();
                if (busy || !localModelReady) return;
                const validName = validateName();
                if (!validName) {
                  nameInput.current?.focus();
                  return;
                }
                setBusy(true);
                setError("");
                try {
                  await onSave({
                    id: profile?.id,
                    name: name.trim(),
                    provider,
                    ...(provider === "local"
                      ? {
                          model_id: modelID,
                          streaming_mode: streamingMode,
                          unload_after_dictation: unloadAfter,
                        }
                      : {}),
                    ...(provider === "openai"
                      ? {
                          streaming_mode: streamingMode,
                          languages: languages
                            .split(",")
                            .map((code) => code.trim().toLowerCase())
                            .filter(Boolean),
                        }
                      : {}),
                  });
                  if (alive.current) onClose();
                } catch (e) {
                  if (alive.current)
                    setError(e instanceof Error ? e.message : String(e));
                } finally {
                  if (alive.current) setBusy(false);
                }
              }}
            >
              <div className="profile-dialog-body">
                <div className="profile-form-field">
                  <div className="profile-name-label">
                    <label htmlFor="profile-name">Name</label>
                    {nameError && (
                      <p
                        id="profile-name-error"
                        className="field-error"
                        role="alert"
                      >
                        {nameError}
                      </p>
                    )}
                  </div>
                  <input
                    ref={nameInput}
                    id="profile-name"
                    name="profile-name"
                    value={name}
                    autoComplete="off"
                    disabled={busy}
                    aria-invalid={!!nameError}
                    aria-describedby={
                      nameError ? "profile-name-error" : undefined
                    }
                    onChange={(event) => {
                      setName(event.target.value);
                      autoName.current = null;
                      setNameError("");
                    }}
                    onBlur={validateName}
                  />
                </div>
                {definition.requiresKey && (
                  <div className="profile-form-field">
                    <span className="field-label">API key</span>
                    <p className="quiet key-summary" role="status">
                      {keySummary?.saved
                        ? keySummary.masked_key
                        : keySummary?.error || `No ${definition.label} key`}
                    </p>
                  </div>
                )}
                {provider === "openai" && (
                  <>
                    <div className="profile-form-field">
                      <SegmentedControl
                        label="Transcription mode"
                        value={streamingMode}
                        disabled={busy}
                        options={[
                          { value: "process", label: "Upload while recording" },
                          { value: "none", label: "Record then upload" },
                        ]}
                        onChange={setStreamingMode}
                      />
                    </div>
                    <div className="profile-form-field">
                      <label htmlFor="profile-languages">
                        Languages (optional)
                      </label>
                      <input
                        id="profile-languages"
                        value={languages}
                        disabled={busy}
                        autoComplete="off"
                        spellCheck={false}
                        placeholder="Auto"
                        onChange={(event) => setLanguages(event.target.value)}
                      />
                    </div>
                  </>
                )}
                {provider === "local" && (
                  <>
                    <div className="profile-form-field profile-model-card">
                      <label htmlFor="local-model">Local model</label>
                      <Dropdown
                        id="local-model"
                        label="Local model"
                        value={modelID}
                        placeholder="Choose a model…"
                        disabled={busy || modelBusy || !localModels.length}
                        options={localModels.map((model) => ({
                          value: model.id,
                          label: model.name,
                          description: `${model.languages} · ${formatModelSize(model.bytes)} · ${modelStateLabel[model.state]}`,
                        }))}
                        onChange={(value) => {
                          setModelID(value);
                          setStreamingMode(defaultMode(value));
                          updateAutoName("local", value);
                          if (
                            localModels.find((model) => model.id === value)
                              ?.state === "not_installed"
                          )
                            modelActions.onInstall(value);
                        }}
                      />
                      <p className="profile-model-description quiet">
                        {selectedModel
                          ? `${selectedModel.languages} · ${formatModelSize(selectedModel.bytes)} download`
                          : "Selecting an uninstalled model starts its download."}
                      </p>
                      <LocalModelDownload
                        model={selectedModel}
                        busy={busy || modelBusy}
                        pendingAction={
                          selectedModel &&
                          modelDownloads.pendingID === selectedModel.id
                            ? modelDownloads.pendingAction
                            : null
                        }
                        error={
                          modelDownloads.loadError ||
                          (selectedModel &&
                            modelDownloads.errors[selectedModel.id])
                        }
                        {...modelActions}
                        onRemove={undefined}
                      />
                    </div>
                    <div className="profile-local-options">
                      <div className="profile-form-field">
                        <label htmlFor="local-mode">Dictation mode</label>
                        <Dropdown
                          id="local-mode"
                          label="Dictation mode"
                          value={selectedModel ? streamingMode : ""}
                          placeholder="Choose a model first"
                          disabled={
                            busy ||
                            !selectedModel ||
                            !modelSupportsStreaming(modelID)
                          }
                          options={
                            modelSupportsStreaming(modelID)
                              ? [
                                  {
                                    value: "process",
                                    label: "Process while recording",
                                    description:
                                      "Recommended · Insert text when you stop.",
                                  },
                                  {
                                    value: "live",
                                    label: "Live insertion",
                                    description:
                                      "Insert words while you speak.",
                                  },
                                  {
                                    value: "none",
                                    label: "After recording",
                                    description:
                                      "Start transcription when you stop.",
                                  },
                                ]
                              : [{ value: "none", label: "After recording" }]
                          }
                          onChange={setStreamingMode}
                        />
                        <p className="profile-option-hint quiet">
                          {!selectedModel
                            ? ""
                            : !modelSupportsStreaming(modelID)
                              ? "This model transcribes after recording."
                              : streamingMode === "live"
                                ? "Words appear as you speak."
                                : streamingMode === "none"
                                  ? "Transcription starts when you stop."
                                  : "Recommended. Text is inserted when you stop."}
                        </p>
                      </div>
                      <div className="profile-form-field">
                        <label htmlFor="local-memory">Model memory</label>
                        <Dropdown
                          id="local-memory"
                          label="Model memory"
                          value={
                            selectedModel
                              ? unloadAfter
                                ? "unload"
                                : "keep"
                              : ""
                          }
                          placeholder="Choose a model first"
                          disabled={busy || !selectedModel}
                          options={[
                            {
                              value: "keep",
                              label: "Keep loaded",
                              description:
                                "Recommended · Ready for instant reuse.",
                            },
                            {
                              value: "unload",
                              label: "Unload after dictation",
                              description: "Free memory between recordings.",
                            },
                          ]}
                          onChange={(value) =>
                            setUnloadAfter(value === "unload")
                          }
                        />
                        <p className="profile-option-hint quiet">
                          {!selectedModel
                            ? ""
                            : unloadAfter
                              ? `Frees ${ramGiB} while idle; next start takes ~0.1–0.5 s longer.`
                              : `Uses ${ramGiB} while idle. Instant reuse.`}
                        </p>
                      </div>
                    </div>
                  </>
                )}
              </div>
              <div className="profile-dialog-footer">
                <div
                  className={`profile-footer-note${error ? " field-error" : " quiet"}`}
                  role={error ? "alert" : "status"}
                >
                  {error || saveHint}
                </div>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => void requestClose()}
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  className="primary profile-save"
                  disabled={busy || !localModelReady}
                >
                  {busy ? "Saving…" : "Save profile"}
                </button>
              </div>
            </form>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
      {discarding && (
        <ConfirmDialog
          title="Discard profile changes?"
          description={
            localModels.some((model) =>
              ["downloading", "pausing", "verifying"].includes(model.state),
            )
              ? "Unsaved changes will be lost. Model downloads will continue."
              : "Unsaved changes will be lost."
          }
          confirmLabel="Discard changes"
          cancelLabel="Keep editing"
          onConfirm={() => settleDismiss(true)}
          onCancel={() => settleDismiss(false)}
        />
      )}
      {deleting && (
        <ConfirmDialog
          title={`Delete ${profile?.name}?`}
          description="The shared API key is kept."
          confirmLabel="Delete profile"
          onConfirm={async () => {
            await onDelete();
            onClose();
          }}
          onCancel={() => setDeleting(false)}
        />
      )}
    </>
  );
}
